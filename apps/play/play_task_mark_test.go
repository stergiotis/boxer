package play

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/fairlead"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// taskRecorder is a fairlead pass that keeps the task each statement
// carrying marker left with.
type taskRecorder struct {
	mu     sync.Mutex
	marker string
	tasks  []string
}

func (inst *taskRecorder) Lead(_ context.Context, st fairlead.Statement) (string, error) {
	if st.Site == "chserver.deliver" && strings.Contains(st.SQL, inst.marker) {
		inst.mu.Lock()
		inst.tasks = append(inst.tasks, st.Task)
		inst.mu.Unlock()
	}
	return st.SQL, nil
}

func (inst *taskRecorder) take() (tasks []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	tasks, inst.tasks = inst.tasks, nil
	return
}

// ADR-0403: the run path names the task whose text a run ships, so the
// host's pass sees it at the engine's exit; play's own statement under the
// same mark, and the person's run, name none.
func TestARunCarriesTheTaskThatWroteIt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	runs := &taskRecorder{marker: "fairlead-task-probe"}
	removeRuns := fairlead.Install(runs)
	defer removeRuns()

	client := NewClient(ClientConfig{URL: srv.URL, AllowWrites: true}, srv.Client())
	lane := clientExecutor{client: client, opts: newExecOptions("main")}
	run := func() {
		_, _, _, _ = lane.execute(context.Background(), compiledNode{SQL: "SELECT 'fairlead-task-probe'"}, memory.NewGoAllocator())
	}
	grant := &app.OnBehalfOf{Task: "task-1", Destinations: []string{DestinationClickHouse(endpointHost(srv.URL))}}

	client.SetAgentMark(grant)
	run()
	assert.Equal(t, []string{"task-1"}, runs.take(), "a run of the window's input under a task's mark")

	explicitOpts := newExecOptions("explain-op")
	explicitOpts.Agent = &app.OnBehalfOf{Task: "task-2", Destinations: grant.Destinations}
	explicit := clientExecutor{client: client, opts: explicitOpts}
	_, _, _, _ = explicit.execute(context.Background(), compiledNode{SQL: "SELECT 'fairlead-task-probe'"}, memory.NewGoAllocator())
	assert.Equal(t, []string{"task-2"}, runs.take(), "a lane's explicit agent")

	client.SetAgentMark(nil)
	run()
	assert.Equal(t, []string{""}, runs.take(), "the person's run")

	docsRuns := &taskRecorder{marker: "system.documentation"}
	removeDocs := fairlead.Install(docsRuns)
	defer removeDocs()
	client.SetAgentMark(grant)
	docs := NewClickHouseDocsSource(client)
	defer docs.Close()
	_, _, _, _ = docs.lane.exec.execute(context.Background(), compiledNode{SQL: docs.Query, Params: map[string]string{"n": "count"}}, memory.NewGoAllocator())
	got := docsRuns.take()
	require.NotEmpty(t, got, "the docs lookup was sent")
	for _, task := range got {
		assert.Empty(t, task, "play's own statement names no task under the window's mark")
	}
}
