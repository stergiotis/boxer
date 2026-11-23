package opengine

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/semistructured/cbor/buscodec"
)

// work is a test app with one Async command: it runs until done or failed
// is set, as a query running off the render goroutine would.
type work struct {
	runs   int
	done   bool
	failed error
	// settleRuns makes the settlement handler move runs, as a handler that
	// starts a run for its call does.
	settleRuns bool
}

type workResult struct{ Runs int }

func workSet() *appops.Set[*work, struct{}] {
	s := appops.NewSet(func(*work) struct{} { return struct{}{} })
	s.Resource("runs", "how many ran", func(w *work) any { return w.runs })
	appops.Command(s, app.OperationSpec{Name: "start", Version: 1, Summary: "start the work",
		Effect: app.OperationEffectRun, Writes: []string{"runs"}, Agents: true, Async: true, AnyRevision: true},
		func(w *work, _ app.OperationCall, _ appops.None) (workResult, error) {
			w.runs++
			return workResult{}, nil
		})
	appops.Settles(s, "start", func(w *work, _ app.OperationCall) (out workResult, settled bool, err error) {
		if w.settleRuns {
			w.settleRuns = false
			w.runs++
		}
		return workResult{Runs: w.runs}, w.done || w.failed != nil, w.failed
	})
	return s
}

func workEngine(t *testing.T) (w *work, eng *Engine, frame func()) {
	t.Helper()
	set := workSet()
	w = &work{}
	eng = New(context.Background(), set.Catalog(), set.Bind(w))
	eng.SetAttached(true)
	frame = func() {
		eng.BeginFrame()
		eng.ApplyQueued()
		eng.EndFrame()
		eng.TakeSnapshot()
	}
	frame()
	return
}

// An Async command stays running after it applies, however many frames its
// work takes, then completes with the settlement's result (ADR-0358 §SD1).
func TestAnAsyncCommandRunsUntilItSettles(t *testing.T) {
	w, eng, frame := workEngine(t)
	out := eng.Submit(t.Context(), "start", opwire.CallRequest{CallId: "c1"})
	require.Equal(t, opwire.PhaseAccepted, out.Phase)
	for range 3 {
		frame()
		st, ok := eng.Status("c1")
		require.True(t, ok)
		assert.Equal(t, opwire.PhaseRunning, st.Phase, "not final until the handler says so")
		assert.False(t, st.Phase.Final())
		assert.Empty(t, st.Result, "the result is the settlement's")
	}
	assert.Equal(t, 1, w.runs, "the command applied once")
	w.done = true
	frame()
	st, _ := eng.Status("c1")
	assert.Equal(t, opwire.PhaseCompleted, st.Phase)
	res, err := buscodec.Decode[workResult](st.Result)
	require.NoError(t, err)
	assert.Equal(t, 1, res.Runs)
	frame()
	again, _ := eng.Status("c1")
	assert.Equal(t, opwire.PhaseCompleted, again.Phase, "a settled call is asked no more")
}

func TestAnAsyncCommandCanFailOrBeRefusedAtTheEnd(t *testing.T) {
	w, eng, frame := workEngine(t)
	eng.Submit(t.Context(), "start", opwire.CallRequest{CallId: "c1"})
	frame()
	w.failed = errors.New("the server went away")
	frame()
	st, _ := eng.Status("c1")
	assert.Equal(t, opwire.PhaseFailed, st.Phase)
	assert.Contains(t, st.Reason, "went away")

	w, eng, frame = workEngine(t)
	eng.Submit(t.Context(), "start", opwire.CallRequest{CallId: "c2"})
	frame()
	w.failed = app.RefuseOperation("no longer allowed")
	frame()
	st, _ = eng.Status("c2")
	assert.Equal(t, opwire.PhaseRefused, st.Phase)
}

// A call running is not one the engine forgets while it holds on to finished
// ones, and a Cancel reports where it stands without pretending to stop it:
// the work has its own cancel operation.
func TestARunningCallIsKeptAndKeepsItsPhase(t *testing.T) {
	_, eng, frame := workEngine(t)
	eng.Submit(t.Context(), "start", opwire.CallRequest{CallId: "c1"})
	frame()
	out, ok := eng.Cancel("c1")
	require.True(t, ok)
	assert.Equal(t, opwire.PhaseRunning, out.Phase)
}

// A catalog declaring an Async command with nothing to settle it would run
// forever; the catalog does not bind.
func TestAnAsyncCommandWithoutSettlesDoesNotBind(t *testing.T) {
	s := appops.NewSet(func(*work) struct{} { return struct{}{} })
	s.Resource("runs", "how many ran", func(w *work) any { return w.runs })
	appops.Command(s, app.OperationSpec{Name: "start", Version: 1, Summary: "start",
		Effect: app.OperationEffectRun, Writes: []string{"runs"}, Async: true},
		func(*work, app.OperationCall, appops.None) (appops.None, error) { return appops.None{}, nil })
	assert.Panics(t, func() { s.Bind(&work{}) })
}

// Work that lands in the frame its command settles moves what the command
// writes; the completed outcome carries that revision, so the caller's next
// write expects the state the work left rather than a conflict with it.
func TestASettledCommandCarriesTheRevisionItsWorkLeft(t *testing.T) {
	w, eng, frame := workEngine(t)
	eng.Submit(t.Context(), "start", opwire.CallRequest{CallId: "c1"})
	frame()
	// The work lands during the app's frame, between the command stage and
	// the frame's end.
	eng.BeginFrame()
	eng.ApplyQueued()
	w.runs++
	w.done = true
	eng.EndFrame()
	eng.TakeSnapshot()
	st, _ := eng.Status("c1")
	require.Equal(t, opwire.PhaseCompleted, st.Phase)
	assert.Equal(t, eng.Revisions()["runs"], st.Revisions["runs"])
}

// A settlement handler that changes a resource carries its call on, as a
// run it starts for the call does: the change is the call's, with its
// writer and call id, and the completed outcome carries the revision it
// left. The next frame's start takes nothing of it for the person's edit,
// and a change that lands between frames still is the person's.
func TestASettlementsChangeIsTheCallsAndNotThePersons(t *testing.T) {
	w, eng, frame := workEngine(t)
	out := eng.Submit(t.Context(), "start", opwire.CallRequest{CallId: "c1", Writer: "task:t"})
	require.Equal(t, opwire.PhaseAccepted, out.Phase)
	frame()
	since := eng.LogSeq()
	// The handler is asked at the frame's end; settling, it moves runs.
	eng.BeginFrame()
	eng.ApplyQueued()
	w.done = true
	w.settleRuns = true
	eng.EndFrame()
	eng.TakeSnapshot()
	st, _ := eng.Status("c1")
	require.Equal(t, opwire.PhaseCompleted, st.Phase)
	assert.Equal(t, eng.Revisions()["runs"], st.Revisions["runs"], "the completed outcome carries the revision the handler left")
	frame()
	entries := eng.LogSince(since)
	require.Len(t, entries, 2)
	assert.Equal(t, LogEntry{Seq: entries[0].Seq, Frame: entries[0].Frame, Writer: "task:t", Op: "start", CallId: "c1",
		Phase: opwire.PhaseApplied, Resources: []string{"runs"}, Revisions: map[string]uint64{"runs": eng.Revisions()["runs"]}}, entries[0])
	assert.Equal(t, opwire.PhaseCompleted, entries[1].Phase)
	assert.Equal(t, "task:t", eng.Writer("runs"))

	since = eng.LogSeq()
	w.runs++
	frame()
	entries = eng.LogSince(since)
	require.Len(t, entries, 1)
	assert.Equal(t, opwire.WriterPerson, entries[0].Writer, "a change that landed between frames is the person's")
}

// A call without a writer settles with the app as the writer of what its
// handler changed.
func TestASettlementWithoutAWriterIsTheApps(t *testing.T) {
	w, eng, frame := workEngine(t)
	eng.Submit(t.Context(), "start", opwire.CallRequest{CallId: "c1"})
	frame()
	since := eng.LogSeq()
	w.done, w.settleRuns = true, true
	frame()
	entries := eng.LogSince(since)
	require.NotEmpty(t, entries)
	assert.Equal(t, opwire.WriterApp, entries[0].Writer)
	assert.Equal(t, "c1", entries[0].CallId)
}
