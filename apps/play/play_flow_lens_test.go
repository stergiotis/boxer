package play

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the ADR-0153 EXPLAIN lens layer. The fixtures are verbatim
// captures from a live ClickHouse 26.7 answering the subquery wire form
// `SELECT * FROM (EXPLAIN … <stmt>)` — if a server version changes the output
// dialect, these pin what the parsers were written against.

func TestWrapExplainKinds(t *testing.T) {
	sql := "SELECT 1"
	require.Equal(t, "SELECT * FROM (EXPLAIN AST SELECT 1)", wrapExplain(lensAST, sql))
	require.Equal(t, "SELECT * FROM (EXPLAIN PLAN json = 1 SELECT 1)", wrapExplain(lensPlan, sql))
	require.Equal(t, "SELECT * FROM (EXPLAIN PIPELINE SELECT 1)", wrapExplain(lensPipeline, sql))
	require.Equal(t, sql, wrapExplain(lensStatement, sql), "the static lens passes through")
}

// A SET prelude cannot ride inside the parens — the wrapper re-lifts it in
// front, where the client harvests it onto the URL as usual.
func TestWrapExplainReliftsPrelude(t *testing.T) {
	fused := "SET max_threads = 1;\nSELECT number FROM numbers(10)"
	got := wrapExplain(lensPlan, fused)
	require.Equal(t,
		"SET max_threads = 1;\nSELECT * FROM (EXPLAIN PLAN json = 1 SELECT number FROM numbers(10))",
		got)
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
