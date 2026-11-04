package opengine

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// work is a test app with one Async command: it runs until done or failed
// is set, as a query running off the render goroutine would.
type work struct {
	runs   int
	done   bool
	failed error
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
		return workResult{Runs: w.runs}, w.done || w.failed != nil, w.failed
	})
	return s
}

func workEngine(t *testing.T) (w *work, eng *Engine, frame func()) {
	t.Helper()
	set := workSet()
	w = &work{}
	eng = New(set.Catalog(), set.Bind(w))
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
	out := eng.Submit("start", opwire.CallRequest{CallId: "c1"})
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
	eng.Submit("start", opwire.CallRequest{CallId: "c1"})
	frame()
	w.failed = errors.New("the server went away")
	frame()
	st, _ := eng.Status("c1")
	assert.Equal(t, opwire.PhaseFailed, st.Phase)
	assert.Contains(t, st.Reason, "went away")

	w, eng, frame = workEngine(t)
	eng.Submit("start", opwire.CallRequest{CallId: "c2"})
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
	eng.Submit("start", opwire.CallRequest{CallId: "c1"})
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
