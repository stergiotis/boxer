package play

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// stringStreamBytes is an Arrow IPC stream of string columns, the shape an
// EXPLAIN or the documentation lookup answers in.
func stringStreamBytes(t *testing.T, names []string, rows [][]string) []byte {
	t.Helper()
	mem := memory.NewGoAllocator()
	fields := make([]arrow.Field, len(names))
	cols := make([]arrow.Array, len(names))
	for i, n := range names {
		fields[i] = arrow.Field{Name: n, Type: arrow.BinaryTypes.String}
		b := array.NewStringBuilder(mem)
		for _, r := range rows {
			b.Append(r[i])
		}
		cols[i] = b.NewArray()
		b.Release()
	}
	schema := arrow.NewSchema(fields, nil)
	rec := array.NewRecordBatch(schema, cols, int64(len(rows)))
	defer rec.Release()
	for _, c := range cols {
		c.Release()
	}
	var buf bytes.Buffer
	w := ipc.NewWriter(&buf, ipc.WithSchema(schema), ipc.WithAllocator(mem))
	require.NoError(t, w.Write(rec))
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// stubEndpoint answers every request with body and records what was sent.
type stubEndpoint struct {
	srv    *httptest.Server
	mu     sync.Mutex
	bodies []string
	params []string
}

func newStubEndpoint(t *testing.T, body []byte) (inst *stubEndpoint) {
	t.Helper()
	inst = &stubEndpoint{}
	inst.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		inst.mu.Lock()
		inst.bodies = append(inst.bodies, string(b))
		inst.params = append(inst.params, r.URL.RawQuery)
		inst.mu.Unlock()
		_, _ = w.Write(body)
	}))
	t.Cleanup(inst.srv.Close)
	return
}

func (inst *stubEndpoint) sent() (bodies []string, params []string) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	return append([]string(nil), inst.bodies...), append([]string(nil), inst.params...)
}

func (inst *stubEndpoint) destination() string {
	return DestinationClickHouse(endpointHost(inst.srv.URL))
}

func TestFlowAndExplainCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opSqlFlow)
	require.True(t, ok)
	assert.Equal(t, app.OperationClassQuery, spec.Class)
	assert.Equal(t, app.OperationEffectNone, spec.Effect)
	assert.True(t, spec.Untrusted, "the graph quotes the statement")
	assert.True(t, spec.Agents)
	assert.Equal(t, []string{opsResSql}, spec.Reads)
	assert.Empty(t, spec.Writes)

	spec, ok = m.Operations.Lookup(opExplainSql)
	require.True(t, ok)
	assert.Equal(t, app.OperationClassExternalRead, spec.Class, "it reaches the endpoint")
	assert.Equal(t, app.OperationEffectNone, spec.Effect)
	assert.True(t, spec.Untrusted)
	assert.True(t, spec.Agents)
	assert.Equal(t, []string{opsResSql}, spec.Reads)
	assert.Empty(t, spec.Writes)
}

const flowBuffer = "WITH recent AS (SELECT id, ts FROM events WHERE ts > now() - 60)\nSELECT r.id AS id, count() AS n FROM recent AS r GROUP BY r.id ORDER BY n DESC LIMIT 10"

// sql_flow derives the pane's local lenses from what run would ship, or
// from a given statement, and picks a node; nothing is run.
func TestSqlFlowDerivesTheClauseGraphAndTheLineage(t *testing.T) {
	l, h := opsLauncher(t)
	l.inner.sql = flowBuffer

	out := queryOp[FlowReading](t, h, opSqlFlow, FlowArgs{})
	assert.Equal(t, "statement", out.Lens)
	assert.Equal(t, "buffer", out.Source)
	assert.Contains(t, out.Nodes, "recent")
	assert.Contains(t, out.Nodes, out.Node, "the sink when no node is named")
	kinds := map[string]bool{}
	for _, n := range out.Graph {
		kinds[n.Kind] = true
	}
	for _, k := range []string{"cte", "aggregate", "project", "sort", "limit", "result"} {
		assert.True(t, kinds[k], "the clause graph has a %s node", k)
	}
	require.NotEmpty(t, out.Edges)
	ids := map[string]bool{}
	for _, n := range out.Graph {
		ids[n.Id] = true
	}
	for _, e := range out.Edges {
		assert.True(t, ids[e.From] && ids[e.To], "edges join listed nodes")
	}

	cte := queryOp[FlowReading](t, h, opSqlFlow, FlowArgs{Node: "recent"})
	assert.Equal(t, "recent", cte.Node)
	hasFilter := false
	for _, n := range cte.Graph {
		hasFilter = hasFilter || n.Kind == "filter"
		if n.Kind == "filter" {
			require.Less(t, n.Start, n.End, "the clause is anchored in the node's SQL")
			assert.Contains(t, cte.Sql[n.Start:n.End], "ts >")
		}
	}
	assert.True(t, hasFilter, "the CTE's WHERE")

	lin := queryOp[FlowReading](t, h, opSqlFlow, FlowArgs{Lens: "lineage"})
	outs := map[string]bool{}
	for _, n := range lin.Graph {
		if n.Kind == "column output" {
			outs[n.Label] = true
		}
	}
	assert.True(t, outs["id"] && outs["n"], "every output column: %v", outs)

	given := queryOp[FlowReading](t, h, opSqlFlow, FlowArgs{Sql: "SELECT a FROM t WHERE a > 1"})
	assert.Equal(t, "sql", given.Source)
	assert.Equal(t, flowBuffer, l.inner.sql, "the buffer is unchanged")

	var refusal *app.OperationRefusal
	_, err := h.Snapshot().Query(opSqlFlow, mustEncode(t, FlowArgs{Node: "nope"}))
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Reason, "recent", "the refusal names the nodes there are")
	_, err = h.Snapshot().Query(opSqlFlow, mustEncode(t, FlowArgs{Lens: "plan"}))
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Reason, "explain_sql")

	_, err = h.Snapshot().Query(opSqlFlow, mustEncode(t, FlowArgs{Sql: "INSERT INTO t VALUES (1)"}))
	require.ErrorAs(t, err, &refusal, "a statement outside the query graph's grammar is refused")
}

// explain_sql asks the endpoint for the EXPLAIN of the node, wrapped on the
// wire only, and returns its lines and graph; an agent's call needs the
// endpoint in the grant before anything is sent.
func TestExplainSqlAsksTheEndpointUnderTheGrant(t *testing.T) {
	ep := newStubEndpoint(t, stringStreamBytes(t, []string{"explain"}, [][]string{
		{"Expression ((Project names + Projection))"},
		{"  Limit (preliminary LIMIT)"},
		{"    ReadFromStorage (SystemNumbers)"},
	}))
	l, h := opsLauncher(t)
	l.inner.sql = "SELECT number FROM numbers(10) LIMIT 3"
	l.inner.client = NewClient(ClientConfig{URL: ep.srv.URL}, ep.srv.Client())

	var refusal *app.OperationRefusal
	agent := app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}
	_, _, err := h.Snapshot().ExternalRead(agent, opExplainSql, mustEncode(t, ExplainArgs{Kind: "pipeline"}))
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{ep.destination()}, refusal.Destinations)
	bodies, _ := ep.sent()
	assert.Empty(t, bodies, "nothing is sent for a call the grant does not cover")

	_, _, err = h.Snapshot().ExternalRead(agent, opExplainSql, mustEncode(t, ExplainArgs{Kind: "statement"}))
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Reason, "sql_flow")

	agent.OnBehalfOf.Destinations = []string{ep.destination()}
	out := externalReadOp[ExplainResult](t, h, agent, opExplainSql, ExplainArgs{Kind: "pipeline", Graph: true})
	assert.Equal(t, "pipeline", out.Kind)
	assert.Equal(t, "buffer", out.Source)
	require.Len(t, out.Lines, 3)
	assert.Equal(t, "  Limit (preliminary LIMIT)", out.Lines[1], "indentation is kept")
	assert.NotEmpty(t, out.Graph, "the pipeline parses into the pane's graph")
	assert.False(t, out.Confined)
	bodies, _ = ep.sent()
	require.Len(t, bodies, 1)
	assert.Contains(t, bodies[0], "SELECT * FROM (EXPLAIN PIPELINE ")
	assert.Contains(t, bodies[0], "numbers(10)")
	assert.Equal(t, "SELECT number FROM numbers(10) LIMIT 3", l.inner.sql, "the buffer is unchanged")
}

// A node's signal reads ride the URL as a run's do.
func TestExplainSqlSendsTheNodesSignals(t *testing.T) {
	ep := newStubEndpoint(t, stringStreamBytes(t, []string{"explain"}, [][]string{{"SelectWithUnionQuery"}}))
	l, h := opsLauncher(t)
	l.inner.client = NewClient(ClientConfig{URL: ep.srv.URL}, ep.srv.Client())
	l.inner.graph.setSignalRawFrom("limit_n", "7", signalWriterApp)
	out := externalReadOp[ExplainResult](t, h, app.OperationCall{Writer: "person"}, opExplainSql,
		ExplainArgs{Kind: "ast", Sql: "SELECT number FROM numbers(100) LIMIT {limit_n:UInt64}"})
	assert.Equal(t, "sql", out.Source)
	_, params := ep.sent()
	require.Len(t, params, 1)
	assert.Contains(t, params[0], "param_limit_n=7")
}
