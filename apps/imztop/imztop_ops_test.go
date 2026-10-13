package imztop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// fakeSampler serves one fixed snapshot.
type fakeSampler struct {
	snap   *PublishedSnapshot
	paused bool
}

func (inst *fakeSampler) Start(context.Context)             {}
func (inst *fakeSampler) Latest() (snap *PublishedSnapshot) { return inst.snap }
func (inst *fakeSampler) Pause(p bool)                      { inst.paused = p }
func (inst *fakeSampler) IsPaused() (p bool)                { return inst.paused }
func (inst *fakeSampler) Interval() (d time.Duration)       { return time.Second }
func (inst *fakeSampler) Close() (err error)                { return }

func opsFixture(t *testing.T) (fake *fakeSampler) {
	t.Helper()
	fake = &fakeSampler{snap: &PublishedSnapshot{
		SampledAtUnixMs:    3000,
		HistoryTimeUnixSec: []float64{1, 2, 3, 4},
		HistoryCPUTotal:    []float64{10, 20, 30, 40},
		HistoryNetRxByIface: []NamedSeries{
			{Name: "eth0", Y: []float64{5, 7}},
		},
		LatestCPU: &sysmsnap.CPUSnapshot{TotalPercent: 40, PerCorePercent: []uint8{30, 50}},
		LatestNet: &sysmsnap.NetSnapshot{Interfaces: []sysmsnap.NetInterface{{Name: "lo"}, {Name: "eth0"}}},
		Procs: []sysmsnap.ProcInfo{
			{PID: 1, Name: "init", RSSBytes: 100, CPUPercent: 1},
			{PID: 20, PPID: 1, Name: "busy", RSSBytes: 50, CPUPercent: 90},
			{PID: 21, PPID: 1, Name: "fat", RSSBytes: 900, CPUPercent: 5},
		},
		ProcCPUSmoothed: []float32{1, 80, 4},
	}}
	prevSampler := opsLiveSampler
	opsLiveSampler = func() (SamplerI, error) { return fake, nil }
	prevController := controller.Load()
	controller.Store(nil)
	t.Cleanup(func() {
		opsLiveSampler = prevSampler
		controller.Store(prevController)
	})
	return
}

func query[In any, Out any](t *testing.T, inst *App, name string, in In) (out Out, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	raw, err := inst.Operations().Snapshot().Query(name, args)
	if err != nil {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

func command[In any](t *testing.T, inst *App, call app.OperationCall, name string, in In) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	_, err = inst.Operations().ApplyCommand(call, name, args)
	return
}

func refusal(err error) (r *app.OperationRefusal) {
	errors.As(err, &r)
	return
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opGetOverview, opListProcesses, opGetProcess, opGetHistory, opGetHardware,
		opGetView, opGetCoverage, opSetProcView, opSetProcMapMetric, opSetTopologyDim, opSelectInterface,
		opShowPanel, opSetFrozen, opEnterReplay, opLeaveReplay, opSetReplayPaused, opSetReplaySpeed, opSetReplayWindow} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
}

func TestListProcessesReadsItsOwnArgumentsNotTheView(t *testing.T) {
	opsFixture(t)
	inst := newApp()
	inst.procView = procViewState{SortBy: ProcSortByName, Filter: "init"}

	out, err := query[ListProcessesArgs, ListProcessesResult](t, inst, opListProcesses, ListProcessesArgs{SortBy: "mem", Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, 3, out.Matched)
	require.Len(t, out.Rows, 2)
	assert.Equal(t, []uint32{21, 1}, []uint32{out.Rows[0].PID, out.Rows[1].PID})

	out, err = query[ListProcessesArgs, ListProcessesResult](t, inst, opListProcesses, ListProcessesArgs{})
	require.NoError(t, err)
	assert.Equal(t, uint32(20), out.Rows[0].PID, "default is smoothed CPU, descending")
	assert.Equal(t, float32(80), out.Rows[0].CPUSmoothed)

	_, err = query[ListProcessesArgs, ListProcessesResult](t, inst, opListProcesses, ListProcessesArgs{SortBy: "vsz"})
	assert.NotNil(t, refusal(err))
}

func TestGetProcessAndHistory(t *testing.T) {
	opsFixture(t)
	inst := newApp()

	p, err := query[GetProcessArgs, GetProcessResult](t, inst, opGetProcess, GetProcessArgs{PID: 1})
	require.NoError(t, err)
	assert.Equal(t, 2, p.ChildCount)
	assert.Equal(t, uint32(20), p.Children[0].PID, "children by smoothed CPU")
	_, err = query[GetProcessArgs, GetProcessResult](t, inst, opGetProcess, GetProcessArgs{PID: 99})
	assert.NotNil(t, refusal(err))

	h, err := query[GetHistoryArgs, GetHistoryResult](t, inst, opGetHistory, GetHistoryArgs{Series: "cpu_total", Points: 2})
	require.NoError(t, err)
	assert.Equal(t, 4, h.Samples)
	assert.Equal(t, []float64{10, 40, 25, 40, 40}, []float64{h.Min, h.Max, h.Mean, h.P95, h.Last})
	assert.Equal(t, []float64{15, 35}, h.Points)
	assert.Equal(t, int64(1000), h.FromUnixMs)

	h, err = query[GetHistoryArgs, GetHistoryResult](t, inst, opGetHistory, GetHistoryArgs{Series: "net_rx:eth0"})
	require.NoError(t, err)
	assert.Equal(t, int64(3000), h.FromUnixMs, "a short series aligns to the fold's end")
	_, err = query[GetHistoryArgs, GetHistoryResult](t, inst, opGetHistory, GetHistoryArgs{Series: "nope"})
	assert.Contains(t, refusal(err).Reason, "net_rx:<key>")
}

func TestViewCommandsSetAndNeverToggle(t *testing.T) {
	opsFixture(t)
	inst := newApp()
	in := SetProcViewArgs{SortBy: "pid", Order: "desc", Filter: "x", Tree: true}
	for range 2 {
		require.NoError(t, command(t, inst, app.OperationCall{}, opSetProcView, in))
		assert.Equal(t, procViewState{SortBy: ProcSortByPID, Desc: true, Filter: "x", Tree: true}, inst.procView)
	}
	assert.Equal(t, "x", inst.procFilterDraft)

	// The header click toggles by computing the next view; without a host
	// it applies directly.
	inst.setProcSort(ProcSortByPID)
	assert.False(t, inst.procView.Desc)

	require.NoError(t, command(t, inst, app.OperationCall{}, opSelectInterface, SelectInterfaceArgs{Name: "eth0"}))
	assert.Equal(t, 1, inst.netSelectedIfaceIdx)
	require.NoError(t, command(t, inst, app.OperationCall{}, opShowPanel, ShowPanelArgs{Panel: "proc_map"}))
	assert.Equal(t, dockTabProcMap, inst.pendingTab)

	v, err := query[struct{}, ViewResult](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, "eth0", v.Interface)
	assert.Equal(t, "pid", v.ProcSortBy)
}

func TestProcessWideCommandsGoThroughTheController(t *testing.T) {
	fake := opsFixture(t)
	first, second := newApp(), newApp()
	first.claimController()
	second.claimController()

	ok, reason := second.Operations().Snapshot().Available(opSetFrozen)
	assert.False(t, ok)
	assert.Equal(t, notControllerReason, reason)
	assert.NotNil(t, refusal(command(t, second, app.OperationCall{}, opSetFrozen, SetFrozenArgs{Frozen: true})))
	assert.False(t, fake.paused)

	ok, _ = first.Operations().Snapshot().Available(opSetFrozen)
	assert.True(t, ok)
	require.NoError(t, command(t, first, app.OperationCall{}, opSetFrozen, SetFrozenArgs{Frozen: true}))
	assert.True(t, fake.paused)
	assert.Equal(t, true, first.Operations().ResourceValue(resLive))
	assert.Equal(t, false, second.Operations().ResourceValue(resLive), "only the controller's value moves")

	first.releaseController()
	second.claimController()
	assert.True(t, second.isController())
}

func TestAnAgentEntersReplayOnlyWithTheEndpointGranted(t *testing.T) {
	opsFixture(t)
	inst := newApp()
	inst.claimController()
	call := app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t"}}
	r := refusal(command(t, inst, call, opEnterReplay, ReplayWindowArgs{}))
	require.NotNil(t, r)
	assert.Equal(t, []string{clickHouseDestination()}, r.Destinations)
	assert.Equal(t, ReplayOff, CurrentReplayStatus().State)

	r = refusal(command(t, inst, app.OperationCall{}, opEnterReplay, ReplayWindowArgs{FromUnixMs: 5, ToUnixMs: 1}))
	require.NotNil(t, r)
}

func TestSeriesListingIsBounded(t *testing.T) {
	fake := opsFixture(t)
	for range 40 {
		fake.snap.HistoryCPUPerCore = append(fake.snap.HistoryCPUPerCore, []float64{1})
	}
	out, err := query[struct{}, Overview](t, newApp(), opGetOverview, struct{}{})
	require.NoError(t, err)
	assert.NotContains(t, out.HistorySeries, "cpu_core:0")
	require.Len(t, out.HistoryFamilies, 2)
	assert.Equal(t, "cpu_core:", out.HistoryFamilies[0].Prefix)
	assert.Equal(t, 40, out.HistoryFamilies[0].Count)
	assert.Len(t, out.HistoryFamilies[0].Keys, opsMaxFamilyKeys)
	assert.Equal(t, []string{"eth0"}, out.HistoryFamilies[1].Keys)
}

func TestControllerRefusalSaysWhetherAnyWindowHoldsTheRole(t *testing.T) {
	opsFixture(t)
	inst := newApp()
	ok, reason := inst.Operations().Snapshot().Available(opSetFrozen)
	assert.False(t, ok)
	assert.Equal(t, noControllerReason, reason)
	assert.Equal(t, noControllerReason, refusal(command(t, inst, app.OperationCall{}, opLeaveReplay, struct{}{})).Reason)
}

func TestLeaveReplayIsIdempotent(t *testing.T) {
	opsFixture(t)
	inst := newApp()
	inst.claimController()
	for range 2 {
		require.NoError(t, command(t, inst, app.OperationCall{}, opLeaveReplay, struct{}{}))
	}
	assert.Equal(t, ReplayOff, CurrentReplayStatus().State)
}

// engineWindow is a window served by a host engine, with the gesture path
// installed; frame runs one host frame around body (windowhost's order:
// begin, command stage, snapshot, the app's frame, end).
type engineWindow struct {
	inst *App
	eng  *opengine.Engine
}

func newEngineWindow() (w *engineWindow) {
	inst := newApp()
	eng := opengine.New(ops.Catalog(), inst.Operations())
	ctx := app.NewStaticFrameContext(nil, nil)
	ctx.SetOperationsGesture(eng.Gesture)
	inst.frameCtx = ctx
	w = &engineWindow{inst: inst, eng: eng}
	w.frame(nil)
	return
}

func (inst *engineWindow) frame(body func()) {
	inst.eng.BeginFrame()
	inst.eng.ApplyQueued()
	inst.eng.TakeSnapshot()
	inst.inst.claimController()
	inst.inst.latchProcessWide()
	if body != nil {
		body()
	}
	inst.eng.EndFrame()
}

func (inst *engineWindow) submit(t *testing.T, id string, op string, in any) (out opwire.Outcome) {
	t.Helper()
	args, err := buscodec.Encode(in)
	require.NoError(t, err)
	// The task read every resource: a revision never bumped is zero.
	revs := inst.eng.Revisions()
	expects := map[string]uint64{}
	for _, r := range []string{resProcView, resProcMapMetric, resTopologyDim, resInterface, resPanel, resLive, resReplay} {
		expects[r] = revs[r]
	}
	inst.eng.Submit(op, opwire.CallRequest{CallId: id, Args: args, Writer: "task:t", Expects: expects})
	inst.frame(nil)
	out, ok := inst.eng.Status(id)
	require.True(t, ok)
	return
}

func TestTheEngineAttributesEachWriter(t *testing.T) {
	opsFixture(t)
	w := newEngineWindow()

	// An agent's command is the task's, and writes only what it declares.
	out := w.submit(t, "c1", opSetProcView, SetProcViewArgs{SortBy: "mem"})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Equal(t, "task:t", w.eng.Writer(resProcView))
	// Setting the value already set applies again (ADR-0269 §SD1).
	out = w.submit(t, "c2", opSetProcView, SetProcViewArgs{SortBy: "mem"})
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Empty(t, w.eng.UndeclaredWrites(0))

	// A header click is the person's, through the same command.
	w.frame(func() { w.inst.setProcSort(ProcSortByPID) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resProcView))
	log := w.eng.Log()
	assert.Equal(t, opSetProcView, log[len(log)-1].Op)

	// The filter box commits inside the frame without a command, so its
	// writes are the app's (ADR-0329 (proposed) §SD5).
	w.frame(func() { w.inst.setProcFilter("x") })
	assert.Equal(t, opwire.WriterApp, w.eng.Writer(resProcView))

	// Freeze by the agent, then again: idempotent.
	for _, id := range []string{"f1", "f2"} {
		out = w.submit(t, id, opSetFrozen, SetFrozenArgs{Frozen: true})
		assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	}
	assert.Equal(t, "task:t", w.eng.Writer(resLive))
	assert.Empty(t, w.eng.UndeclaredWrites(0))
}

func TestAClickInAnotherWindowActsWithoutARefusalAndTheControllerLatchesIt(t *testing.T) {
	fake := opsFixture(t)
	a := newEngineWindow()
	b := newEngineWindow()
	require.True(t, a.inst.isController())

	seq := b.eng.LogSeq()
	b.frame(func() { gestureOrShared(b.inst, opSetFrozen, SetFrozenArgs{Frozen: true}, func() { fake.Pause(true) }) })
	assert.True(t, fake.paused)
	for _, e := range b.eng.LogSince(seq) {
		assert.NotEqual(t, opwire.PhaseRefused, e.Phase, "no refusal is logged for a click that acts")
	}

	a.frame(nil)
	assert.Equal(t, opwire.WriterApp, a.eng.Writer(resLive), "the controller's latch lands inside its frame")
	assert.Equal(t, true, a.inst.Operations().ResourceValue(resLive))
}
