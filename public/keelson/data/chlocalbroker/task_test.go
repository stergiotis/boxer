package chlocalbroker

import (
	"context"
	"io"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/fairlead"
	"github.com/stergiotis/boxer/public/db/fairlead/taskread"
)

func TestWireRequestTaskRoundTrip(t *testing.T) {
	b, err := encodeRequest(wireRequest{SQL: "SELECT 1", Task: "task-1"})
	require.NoError(t, err)
	req, err := decodeRequest(b)
	require.NoError(t, err)
	assert.Equal(t, "task-1", req.Task)

	b, err = encodeRequest(wireRequest{SQL: "SELECT 1"})
	require.NoError(t, err)
	req, err = decodeRequest(b)
	require.NoError(t, err)
	assert.Empty(t, req.Task)
}

// The task crosses the bus and the worker leads the request's statement
// under it and the broker's prelude without it (ADR-0403): a task's read
// with bound parameters and an input table runs, and a task's write is
// refused at the worker.
func TestExecOnPool_TaskReachesTheWorkersExit(t *testing.T) {
	_, caller := newTestBroker(t)
	var mu sync.Mutex
	seen := map[string]string{}
	removeRecord := fairlead.Install(fairlead.PassFunc(func(_ context.Context, st fairlead.Statement) (string, error) {
		mu.Lock()
		seen[st.Site] = st.Task
		mu.Unlock()
		return st.SQL, nil
	}))
	defer removeRecord()
	removeGate := taskread.Install()
	defer removeGate()

	rep, err := ExecOnPool(context.Background(), caller, "introspect", ExecRequest{
		SQL:         "SELECT sum(n) + {k:UInt64} FROM mytab",
		Format:      "TabSeparated",
		Params:      map[string]string{"k": "1"},
		InputTables: map[string][]byte{"mytab": arrowFileI32(t, "n", []int32{1, 2})},
		Task:        "task-1",
	})
	require.NoError(t, err)
	require.NoError(t, rep.Err())
	body, err := io.ReadAll(rep)
	require.NoError(t, err)
	assert.Equal(t, "4\n", string(body))
	mu.Lock()
	assert.Equal(t, map[string]string{"chlocalpool.prelude": "", "chlocalpool.worker": "task-1"}, seen)
	mu.Unlock()

	rep, err = ExecOnPool(fairlead.WithTask(context.Background(), "task-1"), caller, "introspect", ExecRequest{
		SQL:    "CREATE TABLE t (a UInt8) ENGINE = Memory",
		Format: "TabSeparated",
	})
	require.NoError(t, err)
	require.ErrorContains(t, rep.Err(), "statement refused at the fairlead", "the context's mark crosses the bus too")

	rep, err = ExecOnPool(context.Background(), caller, "introspect", ExecRequest{
		SQL:    "CREATE TEMPORARY TABLE t (a UInt8) ENGINE = Memory",
		Format: "TabSeparated",
	})
	require.NoError(t, err)
	require.NoError(t, rep.Err(), "a statement no task supplied is not judged")
}
