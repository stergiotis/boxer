package play

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the ADR-0153 flow-graph derivation (play_flow_model.go). Pure
// fixtures — no UI, no server. Node ids are positional (see flowBuilder), so
// the assertions pin the id scheme on purpose: layout caching and selection
// survival depend on its stability.

func flowByID(t *testing.T, g flowGraph) map[string]flowNode {
	t.Helper()
	m := make(map[string]flowNode, len(g.Nodes))
	for _, n := range g.Nodes {
		_, dup := m[n.ID]
		require.False(t, dup, "duplicate node id %q", n.ID)
		m[n.ID] = n
	}
	return m
}

func TestFlowPlainSelect(t *testing.T) {
	g, err := buildFlowGraph("SELECT a FROM t", nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)

	src := byID["q:src0"]
	require.Equal(t, flowSourceTable, src.Kind)
	require.Equal(t, "t", src.Label)
	require.Equal(t, flowProject, byID["q:select"].Kind)
	require.Equal(t, flowResult, byID[flowResultNodeID].Kind)
	require.Equal(t, "main", byID[flowResultNodeID].Label)

	require.Contains(t, g.Edges, flowEdge{From: "q:src0", To: "q:select"})
	require.Contains(t, g.Edges, flowEdge{From: "q:select", To: flowResultNodeID})
	require.Len(t, g.Nodes, 3)
	require.False(t, g.Capped)
}

func TestFlowSelectNoFrom(t *testing.T) {
	g, err := buildFlowGraph("SELECT 1", nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)
	require.Len(t, g.Nodes, 2)
	require.Equal(t, flowProject, byID["q:select"].Kind)
	require.Contains(t, g.Edges, flowEdge{From: "q:select", To: flowResultNodeID})
}

func TestFlowJoinChainOn(t *testing.T) {
	g, err := buildFlowGraph(
		"SELECT * FROM a INNER JOIN b ON a.id=b.id LEFT JOIN c USING (id)", nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)

	require.Equal(t, "INNER JOIN", byID["q:join0"].Label)
	require.Equal(t, "ON a.id=b.id", byID["q:join0"].Detail)
	require.Equal(t, "LEFT JOIN", byID["q:join1"].Label)
	require.Equal(t, "USING (id)", byID["q:join1"].Detail)

	require.Contains(t, g.Edges, flowEdge{From: "q:src0", To: "q:join0", Label: "l"})
	require.Contains(t, g.Edges, flowEdge{From: "q:src1", To: "q:join0", Label: "r"})
	require.Contains(t, g.Edges, flowEdge{From: "q:join0", To: "q:join1", Label: "l"})
	require.Contains(t, g.Edges, flowEdge{From: "q:src2", To: "q:join1", Label: "r"})
	require.Contains(t, g.Edges, flowEdge{From: "q:join1", To: "q:select"})
}

func TestFlowBareAndCommaJoin(t *testing.T) {
	g, err := buildFlowGraph("SELECT * FROM a JOIN b ON a.x=b.x", nil, "main")
	require.NoError(t, err)
	require.Equal(t, "JOIN", flowByID(t, g)["q:join0"].Label)

	g, err = buildFlowGraph("SELECT * FROM a, b", nil, "main")
	require.NoError(t, err)
	require.Equal(t, "CROSS JOIN", flowByID(t, g)["q:join0"].Label)
}

func TestFlowFullClausePipeline(t *testing.T) {
	g, err := buildFlowGraph(
		"SELECT x, count() AS c FROM t WHERE x > 0 GROUP BY x HAVING c > 1 ORDER BY c DESC LIMIT 10",
		nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)

	require.Equal(t, flowFilter, byID["q:where"].Kind)
	require.Equal(t, "WHERE", byID["q:where"].Label)
	require.Equal(t, "WHERE x > 0", byID["q:where"].Detail)
	require.Equal(t, flowAggregate, byID["q:group"].Kind)
	require.Equal(t, flowFilter, byID["q:having"].Kind)
	require.Equal(t, flowSort, byID["q:order"].Kind)
	require.Equal(t, flowLimit, byID["q:limit"].Kind)

	// The chain order is the ClickHouse logical order.
	for _, e := range [][2]string{
		{"q:src0", "q:where"}, {"q:where", "q:group"}, {"q:group", "q:having"},
		{"q:having", "q:select"}, {"q:select", "q:order"}, {"q:order", "q:limit"},
		{"q:limit", flowResultNodeID},
	} {
		require.Contains(t, g.Edges, flowEdge{From: e[0], To: e[1]}, "edge %v", e)
	}
}

func TestFlowPrewhere(t *testing.T) {
	g, err := buildFlowGraph("SELECT * FROM t PREWHERE a = 1 WHERE b = 2", nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)
	require.Equal(t, "PREWHERE", byID["q:prewhere"].Label)
	require.Contains(t, g.Edges, flowEdge{From: "q:src0", To: "q:prewhere"})
	require.Contains(t, g.Edges, flowEdge{From: "q:prewhere", To: "q:where"})
}

func TestFlowArrayJoin(t *testing.T) {
	g, err := buildFlowGraph("SELECT x FROM t LEFT ARRAY JOIN arr AS x", nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)
	require.Equal(t, flowJoin, byID["q:arrayjoin"].Kind)
	require.Equal(t, "LEFT ARRAY JOIN", byID["q:arrayjoin"].Label)
	require.Contains(t, g.Edges, flowEdge{From: "q:src0", To: "q:arrayjoin"})
	require.Contains(t, g.Edges, flowEdge{From: "q:arrayjoin", To: "q:select"})
}

func TestFlowGroupByModifiers(t *testing.T) {
	g, err := buildFlowGraph("SELECT x, count() FROM t GROUP BY x WITH CUBE", nil, "main")
	require.NoError(t, err)
	require.Equal(t, "GROUP BY CUBE", flowByID(t, g)["q:group"].Label)

	g, err = buildFlowGraph("SELECT x, count() FROM t GROUP BY x WITH TOTALS", nil, "main")
	require.NoError(t, err)
	require.Equal(t, "GROUP BY +TOTALS", flowByID(t, g)["q:group"].Label)
}

func TestFlowQualifyWindow(t *testing.T) {
	g, err := buildFlowGraph(
		"SELECT x, row_number() OVER w AS rn FROM t WINDOW w AS (ORDER BY x) QUALIFY rn <= 3",
		nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)
	require.Equal(t, "QUALIFY", byID["q:qualify"].Label)
	require.Contains(t, byID["q:select"].Detail, "WINDOW w AS (ORDER BY x)")
	require.Contains(t, g.Edges, flowEdge{From: "q:select", To: "q:qualify"})
}

func TestFlowDistinct(t *testing.T) {
	g, err := buildFlowGraph("SELECT DISTINCT x FROM t ORDER BY x", nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)
	require.Equal(t, flowDistinct, byID["q:distinct"].Kind)
	require.Contains(t, g.Edges, flowEdge{From: "q:select", To: "q:distinct"})
	require.Contains(t, g.Edges, flowEdge{From: "q:distinct", To: "q:order"})
}

func TestFlowLimitByAndOffset(t *testing.T) {
	g, err := buildFlowGraph(
		"SELECT * FROM t ORDER BY x LIMIT 2 BY g LIMIT 10 OFFSET 5", nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)
	require.Equal(t, "LIMIT BY", byID["q:limitby"].Label)
	require.Equal(t, "LIMIT", byID["q:limit"].Label)
	require.Contains(t, byID["q:limit"].Detail, "OFFSET 5")
	require.Contains(t, g.Edges, flowEdge{From: "q:order", To: "q:limitby"})
	require.Contains(t, g.Edges, flowEdge{From: "q:limitby", To: "q:limit"})
}

// TestFlowCTERefOpaque pins the sibling-CTE contract: a lifted CTE referenced
// by a node's body parses as a plain table (the body has no WITH); the sibling
// set is what marks it as a CTE reference, and it stays opaque.
func TestFlowCTERefOpaque(t *testing.T) {
	g, err := buildFlowGraph("SELECT * FROM recent WHERE x > 0",
		map[string]struct{}{"recent": {}}, "by_kind")
	require.NoError(t, err)
	byID := flowByID(t, g)
	src := byID["q:src0"]
	require.Equal(t, flowSourceCTE, src.Kind)
	require.Equal(t, "recent", src.Label)
	require.Len(t, g.Nodes, 4) // src, where, select, result — no expansion
}

// TestFlowOwnWithLocalCTE: a statement carrying its own WITH clause (the sink
// node case, and recursive CTE nodes) classifies references via BuildScopes.
// The CTE body is not expanded — WITH-clause bodies are other split nodes or
// deliberately opaque (ADR-0153 §SD2).
func TestFlowOwnWithLocalCTE(t *testing.T) {
	g, err := buildFlowGraph("WITH local AS (SELECT 1 AS x) SELECT * FROM local", nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)
	src := byID["q:src0"]
	require.Equal(t, flowSourceCTE, src.Kind)
	require.Equal(t, "local", src.Label)
	require.Len(t, g.Nodes, 3) // src, select, result
}

func TestFlowRecursiveWithBody(t *testing.T) {
	g, err := buildFlowGraph(
		"WITH RECURSIVE r AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM r WHERE n < 10) SELECT * FROM r",
		nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)
	require.Equal(t, flowSourceCTE, byID["q:src0"].Kind)
	require.Len(t, g.Nodes, 3) // the recursive body stays inside the WITH clause
}

func TestFlowFromSubqueryNesting(t *testing.T) {
	g, err := buildFlowGraph("SELECT * FROM (SELECT a FROM t WHERE a > 0) s", nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)

	outer := byID["q:src0"]
	require.Equal(t, flowSourceSubquery, outer.Kind)
	require.Equal(t, "(subquery) s", outer.Label)
	require.Equal(t, flowSourceTable, byID["qf0:src0"].Kind)
	require.Contains(t, g.Edges, flowEdge{From: "qf0:src0", To: "qf0:where"})
	require.Contains(t, g.Edges, flowEdge{From: "qf0:where", To: "qf0:select"})
	// The inner chain drains into the subquery collector node.
	require.Contains(t, g.Edges, flowEdge{From: "qf0:select", To: "q:src0"})
	require.False(t, g.Capped)
}

func TestFlowSubqueryDepthCap(t *testing.T) {
	// Nest one level past the cap; the innermost subquery must stay opaque.
	sql := "SELECT 1"
	for range flowMaxDepth + 1 {
		sql = "SELECT * FROM (" + sql + ") s"
	}
	g, err := buildFlowGraph(sql, nil, "main")
	require.NoError(t, err)
	require.True(t, g.Capped)
	found := false
	for _, n := range g.Nodes {
		if n.Kind == flowSourceSubquery && strings.Contains(n.Detail, "not expanded") {
			found = true
		}
	}
	require.True(t, found, "expected an unexpanded subquery marker")
}

func TestFlowUnionMembers(t *testing.T) {
	g, err := buildFlowGraph("SELECT a FROM t UNION ALL SELECT b FROM u", nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)

	un := byID["q:union"]
	require.Equal(t, flowUnion, un.Kind)
	require.Equal(t, "UNION ALL", un.Label)
	require.Contains(t, g.Edges, flowEdge{From: "qu0:select", To: "q:union"})
	require.Contains(t, g.Edges, flowEdge{From: "qu1:select", To: "q:union"})
	require.Contains(t, g.Edges, flowEdge{From: "q:union", To: flowResultNodeID})
}

func TestFlowExceptLabel(t *testing.T) {
	g, err := buildFlowGraph("SELECT 1 EXCEPT SELECT 2", nil, "main")
	require.NoError(t, err)
	require.Equal(t, "EXCEPT", flowByID(t, g)["q:union"].Label)
}

// TestFlowScopeTableOrderInvariant pins the order invariant the extractor
// leans on: BuildScopes collects scope.Tables in the same DFS left-to-right
// order the join-tree walk visits leaves — across parens, a subquery and
// qualified names (ADR-0153 Consequences).
func TestFlowScopeTableOrderInvariant(t *testing.T) {
	g, err := buildFlowGraph(
		"SELECT * FROM ((SELECT 1 AS x) AS s INNER JOIN db.t2 AS b ON s.x = b.x) INNER JOIN t3 ON t3.y = b.x",
		nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)

	require.Equal(t, flowSourceSubquery, byID["q:src0"].Kind)
	require.Equal(t, "(subquery) s", byID["q:src0"].Label)
	require.Equal(t, flowSourceTable, byID["q:src1"].Kind)
	require.Equal(t, "db.t2", byID["q:src1"].Label)
	require.Equal(t, "AS b", byID["q:src1"].Detail)
	require.Equal(t, flowSourceTable, byID["q:src2"].Kind)
	require.Equal(t, "t3", byID["q:src2"].Label)
}

func TestFlowTableFunctionSource(t *testing.T) {
	g, err := buildFlowGraph("SELECT * FROM numbers(10)", nil, "main")
	require.NoError(t, err)
	src := flowByID(t, g)["q:src0"]
	require.Equal(t, flowSourceFunction, src.Kind)
	require.Equal(t, "numbers(…)", src.Label)
}

func TestFlowFinalMarker(t *testing.T) {
	g, err := buildFlowGraph("SELECT * FROM t FINAL", nil, "main")
	require.NoError(t, err)
	require.Contains(t, flowByID(t, g)["q:src0"].Detail, "FINAL")
}

func TestFlowNonSelectUnsupported(t *testing.T) {
	_, err := buildFlowGraph("INSERT INTO t VALUES (1)", nil, "main")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not a SELECT-shaped")
}

func TestFlowParseErrorPath(t *testing.T) {
	_, err := buildFlowGraph("SELECT * FROM", nil, "main")
	require.Error(t, err)
}

func TestFlowDeterministicIDs(t *testing.T) {
	const sql = "SELECT x FROM (SELECT a AS x FROM t) s INNER JOIN u ON s.x = u.x WHERE x > 0 " +
		"GROUP BY x UNION ALL SELECT y FROM v"
	g1, err := buildFlowGraph(sql, nil, "main")
	require.NoError(t, err)
	g2, err := buildFlowGraph(sql, nil, "main")
	require.NoError(t, err)
	require.Equal(t, g1, g2)
}

func TestFlowNodeCap(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("SELECT 0")
	for range flowMaxNodes {
		sb.WriteString(" UNION ALL SELECT 1")
	}
	g, err := buildFlowGraph(sb.String(), nil, "main")
	require.NoError(t, err)
	require.True(t, g.Capped)
	require.LessOrEqual(t, len(g.Nodes), flowMaxNodes)
	// Every edge endpoint must exist — cap casualties drop their edges.
	byID := flowByID(t, g)
	for _, e := range g.Edges {
		require.Contains(t, byID, e.From)
		require.Contains(t, byID, e.To)
	}
}
