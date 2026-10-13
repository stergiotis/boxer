package imztop

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
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
	assert.Contains(t, refusal(err).Reason, "net_rx:eth0")
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
