package play

import (
	"context"
	"errors"
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
		// ClickHouse nests block comments and the grammar does not, so the
		// server would evaluate the quoted alias as code.
		"a nested block comment":      {"SELECT 1 /* /* */ AS \"x */ , throwIf(1) AS z --\"\nFROM numbers(1)", remote, grant, false},
		"comments read alike by both": {"SELECT '/*' AS s /* a *//* b */", remote, grant, true},
		// The aggregate filter's condition is read like any expression.
		"an aggregate filter":        {"SELECT quantile(0.05)(L) FILTER (WHERE L BETWEEN 177 AND 1502) AS p05 FROM t", remote, grant, true},
		"an egress read in a filter": {"SELECT count() FILTER (WHERE L IN (SELECT L FROM url('http://x/', 'CSV'))) FROM t", remote, grant, false},
		// The query of an EXISTS is read like any subquery, in the spelling
		// whose words also read as an expression.
		"an exists":                    {"SELECT a FROM t WHERE exists(SELECT 1 FROM u WHERE u.k = t.k)", remote, grant, true},
		"an egress read in an exists":  {"SELECT exists(SELECT * FROM url('http://x/', 'CSV'))", remote, grant, false},
		"a keelson table in an exists": {"SELECT * FROM keelson('apps') WHERE EXISTS (SELECT * FROM keelson('windows'))", local, narrow, false},
		"an exists the grant covers":   {"SELECT * FROM keelson('apps') AS a WHERE NOT EXISTS (SELECT 1 FROM keelson('apps') AS b WHERE b.id = a.id)", local, narrow, true},
	} {
		err := checkAgentLimits(tc.sql, tc.dec, tc.obo, nil)
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

// The Docs pane's lookup is play's own statement with the name bound as a
// parameter: under a task's mark it is sent, read-only, while a pane lane
// deriving from the window's input is still refused.
func TestTheDocsLookupIsNotBoundByTheMark(t *testing.T) {
	var hits atomic.Int32
	var readonly atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		readonly.Store(r.URL.Query().Get("readonly"))
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client := NewClient(ClientConfig{URL: srv.URL, AllowWrites: true}, srv.Client())
	client.SetAgentMark(&app.OnBehalfOf{Task: "t", Destinations: []string{DestinationKeelson("apps")}})

	docs := NewClickHouseDocsSource(client)
	defer docs.Close()
	_, _, _, err := docs.lane.exec.execute(context.Background(), compiledNode{SQL: docs.Query, Params: map[string]string{"n": "count"}}, memory.NewGoAllocator())
	var limit *AgentLimitError
	require.False(t, errors.As(err, &limit), "the lookup is not refused: %v", err)
	assert.Equal(t, int32(1), hits.Load(), "the lookup is sent")
	assert.Equal(t, "2", readonly.Load(), "under a mark it is read-only even where writes are allowed")

	pane := clientExecutor{client: client, opts: newExecOptions("map")}
	_, _, _, err = pane.execute(context.Background(), compiledNode{SQL: "SELECT 1"}, memory.NewGoAllocator())
	require.ErrorAs(t, err, &limit, "a pane lane is still bound by the mark")
}

// A refusal for the grammar carries the parser's hint: a scalar written the
// way a subquery is named is told how a scalar is written.
func TestAGrammarRefusalSaysHowToWriteAScalarWithItem(t *testing.T) {
	remote := dispatchDecision{targetURL: "http://db.example:8123/", class: dispatchClassManual}
	grant := &app.OnBehalfOf{Task: "t", Destinations: []string{DestinationClickHouse("db.example:8123")}}
	err := checkAgentLimits("WITH bounds AS ([177, 1502]) SELECT count() FROM t WHERE L BETWEEN bounds[1] AND bounds[2]", remote, grant, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boxer's grammar does not parse the statement")
	assert.Contains(t, err.Error(), "hint: WITH name AS (…) names a subquery; a scalar or an array is written WITH <expression> AS name")
}

// DESCRIBE is not a statement the grammar reads, so an agent's is refused;
// the refusal names the catalog operation that answers it, with the table as
// that operation takes it when the statement names one.
func TestADescribeRefusalNamesDescribeTable(t *testing.T) {
	remote := dispatchDecision{targetURL: "http://db.example:8123/", class: dispatchClassManual}
	grant := &app.OnBehalfOf{Task: "t", Destinations: []string{DestinationClickHouse("db.example:8123")}}
	for sql, want := range map[string]string{
		"DESCRIBE sales.orders":                  `the describe_table operation reads a table's columns: call it with table "sales.orders"; list_tables lists the endpoint's tables`,
		"DESC system.tables":                     `call it with table "system.tables"`,
		"describe table orders;":                 `call it with table "orders"`,
		"DESC `sales`.\"order lines\"":           `call it with table "sales.order lines"`,
		"DESCRIBE TABLE sales.orders FORMAT TSV": "reads a table's columns when called with a table's name, and get_schema the columns of a result",
		"DESCRIBE (SELECT 1 AS a)":               "reads a table's columns when called with a table's name, and get_schema the columns of a result",
		"DESCRIBE TABLE url('http://x/')":        "reads a table's columns when called with a table's name, and get_schema the columns of a result",
		"DESCRIBE `a.b`":                         "reads a table's columns when called with a table's name",
	} {
		err := checkAgentLimits(sql, remote, grant, nil)
		var limit *AgentLimitError
		require.ErrorAs(t, err, &limit, sql)
		assert.Contains(t, err.Error(), "boxer's grammar does not parse the statement, so it cannot be classified and an agent cannot run it: DESCRIBE is not a statement an agent runs", sql)
		assert.Contains(t, err.Error(), want, sql)
		assert.Empty(t, limit.Destination, sql)
	}
	// Any other statement the grammar does not read keeps the parser's
	// diagnostics, and a statement that only mentions the word is not hinted.
	for _, sql := range []string{"SHOW TABLES", "EXISTS TABLE t", "SELEC describe", "SELECT 1 FROM describe WHERE"} {
		err := checkAgentLimits(sql, remote, grant, nil)
		require.Error(t, err, sql)
		assert.NotContains(t, err.Error(), "describe_table", sql)
	}
}
