package play

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

// Tests for the ADR-0153 EXPLAIN lens layer. The fixtures are verbatim
// captures from a live ClickHouse 26.7 answering the subquery wire form
// `SELECT * FROM (EXPLAIN … <stmt>)` — if a server version changes the output
// dialect, these pin what the parsers were written against.

func TestExplainWrapKinds(t *testing.T) {
	require.Equal(t, "SELECT * FROM (EXPLAIN AST SELECT 1)", explainWrap(lensAST)("SELECT 1"))
	require.Equal(t, "SELECT * FROM (EXPLAIN PLAN json = 1 SELECT 1)", explainWrap(lensPlan)("SELECT 1"))
	require.Equal(t, "SELECT * FROM (EXPLAIN PIPELINE SELECT 1)", explainWrap(lensPipeline)("SELECT 1"))
	require.Equal(t, "SELECT * FROM (EXPLAIN ESTIMATE SELECT 1)", explainWrap(lensEstimate)("SELECT 1"))
	require.Equal(t, "SELECT * FROM (EXPLAIN PLAN indexes = 1, json = 1 SELECT 1)", explainWrap(lensIndexes)("SELECT 1"))
	require.Nil(t, explainWrap(lensStatement), "the static lens has no wire wrap")
	require.Equal(t, "SELECT * FROM (EXPLAIN AST SELECT 1)", explainWrap(lensAST)("SELECT 1;\n"),
		"a trailing delimiter must not end up inside the parens")
}

// ESTIMATE rows arrive tab-joined (database, table, parts, rows, marks):
// each table becomes a source node carrying its estimate, draining into one
// terminal; a duplicate table folds; a short row is skipped.
func TestParseExplainEstimate(t *testing.T) {
	g := parseExplainEstimate([]string{
		"boxer\tfacts\t2\t5887\t2",
		"db2\tevents\t10\t123456\t40",
		"boxer\tfacts\t2\t5887\t2", // duplicate folds
		"malformed line",           // skipped
	})
	byID := flowByID(t, g)
	require.Len(t, g.Nodes, 3) // two tables + the terminal
	require.Equal(t, flowSourceTable, byID["boxer.facts"].Kind)
	require.Equal(t, "parts 2 · rows 5887 · marks 2", byID["boxer.facts"].Detail)
	require.Equal(t, flowResult, byID["estimate"].Kind)
	require.Contains(t, g.Edges, flowEdge{From: "boxer.facts", To: "estimate"})
	require.Contains(t, g.Edges, flowEdge{From: "db2.events", To: "estimate"})

	empty := parseExplainEstimate(nil)
	require.Empty(t, empty.Nodes, "no MergeTree reads ⇒ empty graph, the panel reports it")
}

// Captured shape of EXPLAIN PLAN indexes = 1, json = 1 over a MergeTree read:
// the Indexes entries fold into the ReadFrom node's detail.
const explainPlanIndexesFixture = `[
  {
    "Plan": {
      "Node Type": "Filter",
      "Node Id": "Filter_7",
      "Plans": [
        {
          "Node Type": "ReadFromMergeTree",
          "Node Id": "ReadFromMergeTree_0",
          "Description": "boxer.facts",
          "Indexes": [
            {
              "Type": "PrimaryKey",
              "Condition": "true",
              "Initial Parts": 2,
              "Selected Parts": 2,
              "Initial Granules": 2,
              "Selected Granules": 2
            },
            {
              "Type": "Skip",
              "Name": "idx_ts",
              "Keys": ["ts"],
              "Condition": "(ts > 100)",
              "Initial Parts": 2,
              "Selected Parts": 1,
              "Initial Granules": 2,
              "Selected Granules": 1
            }
          ]
        }
      ]
    }
  }
]`

func TestParseExplainPlanIndexesDetail(t *testing.T) {
	g, err := parseExplainPlanJSON(explainPlanIndexesFixture)
	require.NoError(t, err)
	read := flowByID(t, g)["ReadFromMergeTree_0"]
	require.Equal(t, flowSourceTable, read.Kind)
	require.Contains(t, read.Detail, "boxer.facts")
	require.Contains(t, read.Detail, "PrimaryKey parts 2/2 granules 2/2")
	require.Contains(t, read.Detail, "Skip idx_ts keys(ts) cond (ts > 100) parts 1/2 granules 1/2")
}

// The wrap is wire-body-only (ExecOptions.WrapStatement): the SET prelude is
// harvested onto the URL — never inside the parens — the rewrites see the
// plain statement, and the outer FORMAT lands after the wrapper. This is the
// contract that lets a lens statement ROUTE as itself: the dispatch decision
// is made from the plain SQL before the transport ever wraps (index structure
// and schema are endpoint-local, so the EXPLAIN must reach the endpoint the
// query would actually run on).
func TestExecuteArrowStreamWrapStatement(t *testing.T) {
	stream := arrowStreamBytes(t, []int64{1})
	var mu sync.Mutex
	var bodies []string
	var urls []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		urls = append(urls, r.URL.Query())
		mu.Unlock()
		w.Header().Set("X-ClickHouse-Summary", `{"read_rows":"1","read_bytes":"8"}`)
		_, _ = w.Write(stream)
	}))
	defer srv.Close()
	c := NewClient(ClientConfig{URL: srv.URL}, srv.Client())

	opts := newExecOptions("flow-plan-test")
	opts.WrapStatement = explainWrap(lensPlan)
	sql := "SET param_a = '7';\nSELECT {a:UInt64} FROM numbers(10) LIMIT 3"
	rdr, closer, _, err := c.ExecuteArrowStream(context.Background(), sql, memory.NewGoAllocator(), opts,
		nil, c.Dispatch(sql, ""))
	require.NoError(t, err)
	rdr.Release()
	_ = closer.Close()

	require.Len(t, bodies, 1)
	body := bodies[0]
	require.Contains(t, body, "SELECT * FROM (EXPLAIN PLAN json = 1 ")
	require.True(t, strings.HasSuffix(body, ") FORMAT ArrowStream"),
		"the outer FORMAT must land after the wrapper, got: %s", body)
	require.NotContains(t, body, "SET param_a", "the prelude rides the URL, never the parens")
	require.Equal(t, "7", urls[0].Get("param_a"))
}

// Captured: SELECT * FROM (EXPLAIN AST SELECT 1 UNION ALL SELECT 2).
const explainASTFixture = `SelectWithUnionQuery (children 1)
 ExpressionList (children 2)
  SelectWithUnionQuery (children 1)
   ExpressionList (children 1)
    SelectQuery (children 2)
     ExpressionList (children 1)
      Literal UInt64_1
     TablesInSelectQuery (children 1)
      TablesInSelectQueryElement (children 1)
       TableExpression (children 1)
        TableIdentifier system.one
  SelectWithUnionQuery (children 1)`

func TestParseExplainAST(t *testing.T) {
	g := parseExplainAST(strings.Split(explainASTFixture, "\n"))
	byID := flowByID(t, g)

	root := byID["e0"]
	require.Equal(t, "SelectWithUnionQuery (childr…", root.Label) // truncated at flowLabelRunes
	require.Equal(t, flowOp, root.Kind)
	// Depth-1 child hangs off the root; edges point child→parent.
	require.Contains(t, g.Edges, flowEdge{From: "e0.0", To: "e0"})
	// The two depth-2 union members are siblings under the ExpressionList.
	require.Contains(t, g.Edges, flowEdge{From: "e0.0.0", To: "e0.0"})
	require.Contains(t, g.Edges, flowEdge{From: "e0.0.1", To: "e0.0"})
	require.Len(t, g.Nodes, 12)
	require.False(t, g.Capped)
}

// Captured: SELECT * FROM (EXPLAIN PIPELINE SELECT number, count() FROM
// numbers(10) GROUP BY number).
const explainPipelineFixture = `(Expression)
ExpressionTransform × 32
  (Aggregating)
  Resize 1 → 32
    AggregatingTransform
      (Expression)
      ExpressionTransform
        (ReadFromSystemNumbers)
        NumbersRange 0 → 1`

func TestParseExplainPipeline(t *testing.T) {
	g := parseExplainPipeline(strings.Split(explainPipelineFixture, "\n"))
	byID := flowByID(t, g)

	require.Len(t, g.Nodes, 5, "group-marker lines are not processors")
	require.Equal(t, "ExpressionTransform × 32", byID["e0"].Label)
	require.Equal(t, "Expression", byID["e0"].Detail, "the (X) marker folds into the processor's detail")
	require.Equal(t, "NumbersRange 0 → 1", byID["e0.0.0.0.0"].Label)
	require.Equal(t, "ReadFromSystemNumbers", byID["e0.0.0.0.0"].Detail)
	// The deepest processor is the source; edges point child→parent.
	require.Contains(t, g.Edges, flowEdge{From: "e0.0.0.0.0", To: "e0.0.0.0"})
	require.Contains(t, g.Edges, flowEdge{From: "e0.0", To: "e0"})
}

// Captured shape of SELECT * FROM (EXPLAIN PLAN json = 1 SELECT 1), plus a
// second sibling to exercise fan-in.
const explainPlanJSONFixture = `[
  {
    "Plan": {
      "Node Type": "Expression",
      "Node Id": "Expression_5",
      "Description": "(Project names + Projection)",
      "Plans": [
        {
          "Node Type": "ReadFromSystemOne",
          "Node Id": "ReadFromSystemOne_0"
        },
        {
          "Node Type": "ReadFromMergeTree",
          "Node Id": "ReadFromMergeTree_1",
          "Description": "db.t"
        }
      ]
    }
  }
]`

func TestParseExplainPlanJSON(t *testing.T) {
	g, err := parseExplainPlanJSON(explainPlanJSONFixture)
	require.NoError(t, err)
	byID := flowByID(t, g)

	root := byID["Expression_5"]
	require.Equal(t, flowOp, root.Kind)
	require.Equal(t, "Expression", root.Label)
	require.Equal(t, "(Project names + Projection)", root.Detail)
	require.Equal(t, flowSourceTable, byID["ReadFromSystemOne_0"].Kind, "ReadFrom* leaves read as sources")
	require.Equal(t, "db.t", byID["ReadFromMergeTree_1"].Detail)
	require.Contains(t, g.Edges, flowEdge{From: "ReadFromSystemOne_0", To: "Expression_5"})
	require.Contains(t, g.Edges, flowEdge{From: "ReadFromMergeTree_1", To: "Expression_5"})
}

func TestParseExplainPlanJSONBadInput(t *testing.T) {
	_, err := parseExplainPlanJSON("not json")
	require.Error(t, err)
}

func TestParseLensRecordDispatch(t *testing.T) {
	g, err := parseLensRecord(lensAST, []string{"Root (children 1)", " Leaf"})
	require.NoError(t, err)
	require.Len(t, g.Nodes, 2)

	// PLAN output may arrive as one row or split lines; joining is total.
	g, err = parseLensRecord(lensPlan, strings.Split(explainPlanJSONFixture, "\n"))
	require.NoError(t, err)
	require.Len(t, g.Nodes, 3)

	g, err = parseLensRecord(lensIndexes, strings.Split(explainPlanIndexesFixture, "\n"))
	require.NoError(t, err)
	require.Len(t, g.Nodes, 2, "indexes rides the PLAN json parser")

	g, err = parseLensRecord(lensEstimate, []string{"a\tb\t1\t2\t3"})
	require.NoError(t, err)
	require.Len(t, g.Nodes, 2)

	g, err = parseLensRecord(lensStatement, []string{"x"})
	require.NoError(t, err)
	require.Empty(t, g.Nodes, "the static lens never parses lines")
}

func TestLensGraphNodeCap(t *testing.T) {
	lines := make([]string, 0, flowMaxNodes+10)
	for range flowMaxNodes + 10 {
		lines = append(lines, "Node")
	}
	g := parseExplainAST(lines)
	require.True(t, g.Capped)
	require.LessOrEqual(t, len(g.Nodes), flowMaxNodes)
}

func TestEnsureLensGraphMemo(t *testing.T) {
	d := newFlowDriver(nil, nil)
	d.lens = lensAST
	d.ensureLensGraph("k1", []string{"Root (children 1)", " Leaf"})
	require.Len(t, d.lensGraph.Nodes, 2)
	first := &d.lensGraph.Nodes[0]
	d.ensureLensGraph("k1", []string{"IGNORED — same key"})
	require.Same(t, first, &d.lensGraph.Nodes[0], "same served key ⇒ no reparse")

	d.selectedID = "e0.0"
	d.ensureLensGraph("k2", []string{"OnlyRoot"})
	require.Len(t, d.lensGraph.Nodes, 1)
	require.Empty(t, d.selectedID, "selection pruned when its node vanishes")
}

// The "endpoint cannot EXPLAIN" class is recognised by the introspection
// plane's error namespace; anything else stays a plain error line.
func TestExplainUnsupportedByEndpoint(t *testing.T) {
	require.False(t, explainUnsupportedByEndpoint(nil))
	require.False(t, explainUnsupportedByEndpoint(
		errors.New("clickhouse http 400: Syntax error: failed at position 15")))
	require.True(t, explainUnsupportedByEndpoint(
		errors.New("clientExecutor.execute: clickhouse http 400: apply failed: keelsonsql: parse: syntax error: 1:33")))
}

// Switching lenses must clear the shown graph — a parsed graph from another
// lens's lane must not keep rendering while the new lens loads.
func TestLensSwitchClearsShownGraph(t *testing.T) {
	d := newFlowDriver(nil, nil)
	d.lens = lensAST
	d.syncLens()
	d.ensureLensGraph("k1", []string{"Root (children 1)", " Leaf"})
	require.Len(t, d.lensGraph.Nodes, 2)

	d.selectedID = "e0"
	d.lens = lensPlan
	d.syncLens()
	require.Empty(t, d.lensGraph.Nodes)
	require.Empty(t, d.lensKey)
	require.Empty(t, d.selectedID, "ids do not carry across lenses")

	d.lens = lensPlan // unchanged lens is a no-op
	d.ensureLensGraph("k2", []string{explainPlanJSONFixture})
	before := len(d.lensGraph.Nodes)
	d.syncLens()
	require.Equal(t, before, len(d.lensGraph.Nodes))
}
