package imzrt

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/bgjob"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/inprocbus"
)

// opsFixture gives the catalog a sampler of its own, never started, so a
// test ticks it by hand and nothing runs between the assertions.
func opsFixture(t *testing.T) (s *Sampler) {
	t.Helper()
	s, err := NewSampler(SamplerOptions{})
	require.NoError(t, err)
	prev := opsSampler
	opsSampler = func() (*Sampler, error) { return s, nil }
	t.Cleanup(func() { opsSampler = prev })
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

func command[In any](t *testing.T, inst *App, name string, in In) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	_, err = inst.Operations().ApplyCommand(app.OperationCall{}, name, args)
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
	for _, name := range []string{opGetView, opGetSample, opListProfiles, opSetInterval, opSetSmoothing, opSetFrozen, opShowTab, opCaptureProfile, opOpenInPlay} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
	for _, name := range []string{opCaptureProfile, opOpenInPlay} {
		spec, _ := m.Operations.Lookup(name)
		assert.Equal(t, app.OperationEffectConsequential, spec.Effect, name)
		assert.True(t, spec.Async, name)
	}
}

func TestSetIntervalSetsAndNeverTogglesAndNamesTheClamp(t *testing.T) {
	s := opsFixture(t)
	inst := newApp()
	for range 2 {
		require.NoError(t, command(t, inst, opSetInterval, SetIntervalArgs{IntervalMs: 2500}))
		assert.Equal(t, 2500*time.Millisecond, s.Interval())
	}
	assert.Equal(t, int64(2500), inst.Operations().ResourceValue(resView).(viewValue).IntervalMs)
	for _, ms := range []int64{99, 60_001, -1} {
		r := refusal(command(t, inst, opSetInterval, SetIntervalArgs{IntervalMs: ms}))
		require.NotNil(t, r, ms)
		assert.Contains(t, r.Reason, "between 100 and 60000")
	}
	assert.Equal(t, 2500*time.Millisecond, s.Interval(), "a refused command changes nothing")
}

func TestSetFrozenAndSmoothingSetAndNeverToggle(t *testing.T) {
	s := opsFixture(t)
	inst := newApp()
	for range 2 {
		require.NoError(t, command(t, inst, opSetFrozen, SetFrozenArgs{Frozen: true}))
		assert.True(t, s.IsPaused())
	}
	require.NoError(t, command(t, inst, opSetFrozen, SetFrozenArgs{Frozen: false}))
	assert.False(t, s.IsPaused())

	for range 2 {
		require.NoError(t, command(t, inst, opSetSmoothing, SetSmoothingArgs{On: true, HalfWidth: 20}))
		assert.True(t, inst.smooth.On)
		assert.Equal(t, int32(20), inst.smooth.HalfWidth())
	}
	require.NoError(t, command(t, inst, opSetSmoothing, SetSmoothingArgs{On: false}))
	assert.False(t, inst.smooth.On)
	assert.Equal(t, int32(20), inst.smooth.HalfWidth(), "a zero half-width keeps the current one")
	r := refusal(command(t, inst, opSetSmoothing, SetSmoothingArgs{On: true, HalfWidth: 1}))
	require.NotNil(t, r)
	assert.Contains(t, r.Reason, "half_width is between")
}

func TestGetViewAndSampleReadTheSampler(t *testing.T) {
	s := opsFixture(t)
	inst := newApp()
	inst.latchView()
	_, err := query[struct{}, Sample](t, inst, opGetSample, struct{}{})
	assert.Equal(t, noSampleReason, refusal(err).Reason)

	s.tick()
	sample, err := query[struct{}, Sample](t, inst, opGetSample, struct{}{})
	require.NoError(t, err)
	assert.Positive(t, sample.Goroutines)
	assert.Positive(t, sample.SampledAtUnixMs)

	require.NoError(t, command(t, inst, opShowTab, ShowTabArgs{Tab: "gc"}))
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, View{IntervalMs: s.Interval().Milliseconds(), Smoothing: false, SmoothHalfWidth: inst.smooth.HalfWidth(),
		Tab: "gc", Sampling: true, SampledAtUnixMs: sample.SampledAtUnixMs}, v)
	assert.Equal(t, dockTabGC, inst.pendingTab)

	r := refusal(command(t, inst, opShowTab, ShowTabArgs{Tab: "stacks"}))
	require.NotNil(t, r)
	assert.Contains(t, r.Reason, "heap, gc, scheduler, profiles")
}

func TestListProfilesIsBounded(t *testing.T) {
	opsFixture(t)
	inst := newApp()
	out, err := query[ListProfilesArgs, ListProfilesResult](t, inst, opListProfiles, ListProfilesArgs{})
	require.NoError(t, err)
	assert.Equal(t, len(profileKinds), out.Matched)
	assert.Len(t, out.Profiles, len(profileKinds))
	assert.False(t, out.Truncated)
	assert.Equal(t, "pprof_cpu", out.Profiles[0].Alias)

	out, err = query[ListProfilesArgs, ListProfilesResult](t, inst, opListProfiles, ListProfilesArgs{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, out.Profiles, 2)
	assert.True(t, out.Truncated)
	_, err = query[ListProfilesArgs, ListProfilesResult](t, inst, opListProfiles, ListProfilesArgs{Limit: -1})
	assert.NotNil(t, refusal(err))
}

// The profile commands need the app runtime, refuse a kind they do not
// know or one already in flight, and settle from the kind's runner.
func TestProfileCommandsRefuseAndSettle(t *testing.T) {
	opsFixture(t)
	inst := newApp()
	ok, reason := inst.Operations().Snapshot().Available(opCaptureProfile)
	assert.False(t, ok)
	assert.Equal(t, noBusReason, reason)
	assert.Equal(t, noBusReason, refusal(command(t, inst, opCaptureProfile, ProfileKindArgs{Kind: "heap"})).Reason)

	bus := inprocbus.NewInst(zerolog.Nop())
	inst.bus = bus.NewClient(manifest.Id, manifest.Caps)
	ok, _ = inst.Operations().Snapshot().Available(opCaptureProfile)
	assert.True(t, ok)
	r := refusal(command(t, inst, opCaptureProfile, ProfileKindArgs{Kind: "block"}))
	require.NotNil(t, r)
	assert.Contains(t, r.Reason, "kind is one of")
	assert.Contains(t, refusal(command(t, inst, opOpenInPlay, ProfileKindArgs{Kind: "heap"})).Reason, "capture_profile publishes one")

	// A run in flight: the command refuses, the call does not settle.
	e := profiles.entry("heap")
	release := make(chan struct{})
	require.True(t, e.job.Start(nil, bgjob.Spec{Kind: "test"}, func(ctx context.Context) (*profileCapture, error) {
		<-release
		return &profileCapture{revision: 7, rows: 3}, nil
	}))
	t.Cleanup(func() { e.job.Cancel() })
	r = refusal(command(t, inst, opCaptureProfile, ProfileKindArgs{Kind: "heap"}))
	require.NotNil(t, r)
	assert.Contains(t, r.Reason, "capture is running")
	h, isAsync := inst.Operations().(app.OperationsAsyncI)
	require.True(t, isAsync)
	args, err := buscodec.Encode(ProfileKindArgs{Kind: "heap"})
	require.NoError(t, err)
	settled, _, err := h.SettleAsync(app.OperationCall{}, opCaptureProfile, args)
	require.NoError(t, err)
	assert.False(t, settled)

	close(release)
	require.Eventually(t, func() bool { return !e.job.Running() }, 5*time.Second, 5*time.Millisecond)
	settled, raw, err := h.SettleAsync(app.OperationCall{}, opCaptureProfile, args)
	require.NoError(t, err)
	require.True(t, settled)
	row, err := buscodec.Decode[ProfileRow](raw)
	require.NoError(t, err)
	assert.Equal(t, ProfileRow{Kind: "heap", Label: "Heap", Alias: "pprof_heap", Revision: 7, Rows: 3}, row)

	// A failed run settles with its error.
	require.True(t, e.job.Start(nil, bgjob.Spec{Kind: "test"}, func(ctx context.Context) (*profileCapture, error) {
		return nil, errors.New("boom")
	}))
	require.Eventually(t, func() bool { return !e.job.Running() }, 5*time.Second, 5*time.Millisecond)
	settled, _, err = h.SettleAsync(app.OperationCall{}, opCaptureProfile, args)
	assert.True(t, settled)
	assert.ErrorContains(t, err, "boom")
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
	inst.inst.latchView()
	if body != nil {
		body()
	}
	inst.eng.EndFrame()
}

func TestTheEngineAttributesEachWriter(t *testing.T) {
	s := opsFixture(t)
	w := newEngineWindow()

	args, err := buscodec.Encode(SetFrozenArgs{Frozen: true})
	require.NoError(t, err)
	w.eng.Submit(opSetFrozen, opwire.CallRequest{CallId: "c1", Args: args, Writer: "task:t",
		Expects: map[string]uint64{resView: w.eng.Revisions()[resView]}})
	w.frame(nil)
	out, ok := w.eng.Status("c1")
	require.True(t, ok)
	assert.Equal(t, opwire.PhaseApplied, out.Phase, out.Reason)
	assert.Equal(t, "task:t", w.eng.Writer(resView))
	assert.True(t, s.IsPaused())

	// The Freeze button and the interval buttons are the person's, through
	// the same commands; the edge of the clamp is reached, never refused.
	w.frame(func() { w.inst.freezeGesture(s, false) })
	assert.Equal(t, opwire.WriterPerson, w.eng.Writer(resView))
	assert.False(t, s.IsPaused())
	s.SetInterval(200 * time.Millisecond)
	w.frame(func() { w.inst.intervalGesture(s, -intervalStep) })
	assert.Equal(t, 100*time.Millisecond, s.Interval())
	log := w.eng.Log()
	assert.Equal(t, opSetInterval, log[len(log)-1].Op)
	assert.Equal(t, opwire.PhaseApplied, log[len(log)-1].Phase)
	assert.Empty(t, w.eng.UndeclaredWrites(0))
}
