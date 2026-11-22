package chserver

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/fairlead"
	"github.com/stergiotis/boxer/public/db/fairlead/taskread"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

// The task a request names reaches the fairlead at the engine's exit point,
// where the host's pass refuses a task's write before anything is sent
// (ADR-0403). The parallel tests of this package send no marked statement,
// so the installed pass leaves them alone.
func TestDeliverRefusesATasksWriteBeforeSending(t *testing.T) {
	remove := taskread.Install()
	defer remove()
	var requests atomic.Int32
	endpoint, cap := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
	})
	eng := newEngine(t, endpoint)
	ctx := context.Background()

	_, _, err := eng.Deliver(ctx, queryengine.Request{SQL: "INSERT INTO t VALUES (1)", Task: "task-1"})
	require.ErrorContains(t, err, "statement refused at the fairlead")
	assert.Zero(t, requests.Load(), "a refused statement reaches no server")

	_, _, err = eng.Deliver(fairlead.WithTask(ctx, "task-1"), queryengine.Request{SQL: "DROP TABLE t"})
	require.Error(t, err, "a mark on the caller's context counts as the request's own")
	assert.Zero(t, requests.Load())

	st, _, err := eng.Deliver(ctx, queryengine.Request{SQL: "SELECT 1", Task: "task-1"})
	require.NoError(t, err)
	_, term, err := queryengine.Collect(st)
	require.NoError(t, err)
	assert.Equal(t, runstream.TerminalComplete, term.State)
	assert.EqualValues(t, 1, requests.Load(), "a task's read is sent")

	st, _, err = eng.Deliver(ctx, queryengine.Request{SQL: "INSERT INTO t VALUES (1)"})
	require.NoError(t, err)
	_ = st.Close()
	assert.EqualValues(t, 2, requests.Load(), "a write no task supplied is sent")
	_, body, _, _ := cap.get()
	assert.Equal(t, "INSERT INTO t VALUES (1)", body)
}

func TestKillIsLedWithoutTheCallersMark(t *testing.T) {
	var seen atomic.Pointer[fairlead.Statement]
	remove := fairlead.Install(fairlead.PassFunc(func(_ context.Context, st fairlead.Statement) (string, error) {
		if st.Site == "chserver.kill" {
			seen.Store(&st)
		}
		return st.SQL, nil
	}))
	defer remove()
	endpoint, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {})
	eng := newEngine(t, endpoint)

	require.NoError(t, eng.Kill(fairlead.WithTask(context.Background(), "task-1"), "play-main-box-7-1"))
	got := seen.Load()
	require.NotNil(t, got)
	assert.Empty(t, got.Task, "the kill statement is the adapter's own text")
}
