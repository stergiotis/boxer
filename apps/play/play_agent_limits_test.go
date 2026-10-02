package play

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func TestAgentLimits(t *testing.T) {
	remote := dispatchDecision{targetURL: "http://db.example:8123/", class: dispatchClassManual}
	local := dispatchDecision{class: dispatchClassIntrospection}
	grant := &app.OnBehalfOf{Task: "t", Destinations: []string{DestinationKeelson("apps"), DestinationClickHouse("db.example:8123")}}
	narrow := &app.OnBehalfOf{Task: "t", Destinations: []string{DestinationKeelson("apps")}}
	var limit *AgentLimitError
	for name, tc := range map[string]struct {
		sql  string
		dec  dispatchDecision
		obo  *app.OnBehalfOf
		pass bool
	}{
		"a read the grant covers":            {"SELECT 1", remote, grant, true},
		"an introspection read":              {"SELECT * FROM keelson('apps')", local, narrow, true},
		"a keelson table outside the grant":  {"SELECT * FROM keelson('windows')", local, grant, false},
		"an endpoint outside the grant":      {"SELECT 1", remote, narrow, false},
		"an egress read":                     {"SELECT * FROM url('http://x/', 'LineAsString')", remote, grant, false},
		"a write":                            {"INSERT INTO t SELECT 1", remote, grant, false},
		"a settings change":                  {"SET max_threads = 1; SELECT 1", remote, grant, false},
		"a statement that does not classify": {"DROP TABLE t", remote, grant, false},
		// The residual is canonical: row literals in a table function's
		// arguments are tuple() calls by then, and still a plain read.
		"inline rows, canonical":   {`SELECT * FROM "values"('a String, b String', "tuple"('x', 'y'))`, remote, grant, true},
		"a call to an AI provider": {"SELECT aiGenerate('x')", remote, grant, false},
		"a state-changing call":    {"SELECT generateSerialID('s')", remote, grant, false},
	} {
		err := checkAgentLimits(tc.sql, tc.dec, tc.obo)
		if tc.pass {
			assert.NoError(t, err, name)
			continue
		}
		require.Error(t, err, name)
		assert.ErrorAs(t, err, &limit, name)
	}
}

// ADR-0270 §SD2: a pane's lane runs SQL derived from the window's input, so
// while a task's mark is on the window its runs are checked like the task's
// own — refused before anything is sent — and once the mark is gone they
// are the person's again.
func TestAPaneLaneRunsUnderTheWindowsMark(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	lane := clientExecutor{client: client, opts: newExecOptions("map")}
	run := func() error {
		_, _, _, err := lane.execute(context.Background(), compiledNode{SQL: "SELECT 1"}, memory.NewGoAllocator())
		return err
	}

	client.SetAgentMark(&app.OnBehalfOf{Task: "t", Destinations: []string{DestinationKeelson("apps")}})
	var limit *AgentLimitError
	require.ErrorAs(t, run(), &limit, "the grant does not list the endpoint")
	assert.Zero(t, hits.Load(), "refused before anything is sent")

	client.SetAgentMark(nil)
	require.Error(t, run(), "the stub answers 500")
	assert.Equal(t, int32(1), hits.Load(), "the person's run is sent")
}
