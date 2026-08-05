package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the lineage lens (play_flow_lineage.go). Pure fixtures. The
// assertions pin the id scheme (out:<i>, src:<key>.<col>) and the resolution
// precedence: select-list alias first (ClickHouse alias shadowing), then the
// FROM sources, with ambiguity flagged rather than guessed.

func lineage(t *testing.T, sql string, sibs ...string) (flowGraph, string) {
	t.Helper()
	set := make(map[string]struct{}, len(sibs))
	for _, s := range sibs {
		set[s] = struct{}{}
	}
	g, note, err := buildLineageGraph(sql, set)
	require.NoError(t, err)
	return g, note
}

func TestLineagePlainColumns(t *testing.T) {
	g, note := lineage(t, "SELECT a, b FROM t")
	require.Empty(t, note)
	byID := flowByID(t, g)
	require.Equal(t, "a", byID["out:0"].Label)
	require.Equal(t, flowColumnOut, byID["out:0"].Kind)
	require.Equal(t, flowColumnSrc, byID["src:t.a"].Kind)
	require.Equal(t, "table t", byID["src:t.a"].Detail)
	require.Contains(t, g.Edges, flowEdge{From: "src:t.a", To: "out:0"})
	require.Contains(t, g.Edges, flowEdge{From: "src:t.b", To: "out:1"})
}

func TestLineageAliasExpr(t *testing.T) {
	g, _ := lineage(t, "SELECT x + 1 AS y FROM t")
	byID := flowByID(t, g)
	require.Equal(t, "y", byID["out:0"].Label)
	require.Contains(t, byID["out:0"].Detail, "x + 1 AS y")
	require.Contains(t, g.Edges, flowEdge{From: "src:t.x", To: "out:0"})
}

// A later expression consuming an earlier alias draws an output→output edge —
// alias resolution wins over table resolution (the CH shadowing rule).
func TestLineageAliasChain(t *testing.T) {
	g, _ := lineage(t, "SELECT a AS b, b + 1 AS c FROM t")
	require.Contains(t, g.Edges, flowEdge{From: "src:t.a", To: "out:0"})
	require.Contains(t, g.Edges, flowEdge{From: "out:0", To: "out:1"})
	for _, e := range g.Edges {
		require.NotEqual(t, "src:t.b", e.From, "b must resolve to the alias, not a table column")
	}
}

func TestLineageQualifiedJoin(t *testing.T) {
	g, _ := lineage(t, "SELECT d.id, u.name FROM d INNER JOIN e AS u ON d.id = u.id")
	byID := flowByID(t, g)
	require.Equal(t, flowColumnSrc, byID["src:d.id"].Kind)
	require.Equal(t, "table e", byID["src:u.name"].Detail, "the alias keys the node, the detail names the table")
	require.Contains(t, g.Edges, flowEdge{From: "src:d.id", To: "out:0"})
	require.Contains(t, g.Edges, flowEdge{From: "src:u.name", To: "out:1"})
}

func TestLineageBareAmbiguous(t *testing.T) {
	g, _ := lineage(t, "SELECT id FROM a INNER JOIN b ON a.x = b.x")
	byID := flowByID(t, g)
	amb := byID["src:?.id"]
	require.Equal(t, flowColumnSrc, amb.Kind)
	require.Contains(t, amb.Detail, "ambiguous — 2 candidate sources")
	require.Contains(t, g.Edges, flowEdge{From: "src:?.id", To: "out:0"})
}

func TestLineageAggregateNoSource(t *testing.T) {
	g, _ := lineage(t, "SELECT count() AS c FROM t")
	byID := flowByID(t, g)
	require.Equal(t, "c", byID["out:0"].Label)
	for _, e := range g.Edges {
		require.NotEqual(t, "out:0", e.To, "count() reads no columns")
	}
}

func TestLineageStar(t *testing.T) {
	g, _ := lineage(t, "SELECT * FROM a INNER JOIN b ON a.x = b.x")
	require.Contains(t, g.Edges, flowEdge{From: "src:a.*", To: "out:0"})
	require.Contains(t, g.Edges, flowEdge{From: "src:b.*", To: "out:0"})
}

func TestLineageQualifiedStar(t *testing.T) {
	g, _ := lineage(t, "SELECT a.* FROM a INNER JOIN b ON a.x = b.x")
	byID := flowByID(t, g)
	require.Equal(t, "a.*", byID["out:0"].Label)
	require.Contains(t, g.Edges, flowEdge{From: "src:a.*", To: "out:0"})
	_, hasB := byID["src:b.*"]
	require.False(t, hasB, "a qualified star reads one source")
}

// A sibling CTE reference reads as a CTE source — the body carries no WITH,
// so only the split-level dependency set can say so (the statement-lens rule).
func TestLineageSiblingCTE(t *testing.T) {
	g, _ := lineage(t, "SELECT id FROM recent", "recent")
	require.Equal(t, "CTE recent", flowByID(t, g)["src:recent.id"].Detail)
}

func TestLineageScalarSubqueryNotTraced(t *testing.T) {
	g, _ := lineage(t, "SELECT (SELECT max(x) FROM t2) AS m, a FROM t")
	byID := flowByID(t, g)
	require.Contains(t, byID["out:0"].Detail, "subquery inside — not traced")
	for id := range byID {
		require.NotContains(t, id, "t2", "inner-scope identifiers must not leak")
		require.NotContains(t, id, ".x", "inner-scope identifiers must not leak")
	}
	require.Contains(t, g.Edges, flowEdge{From: "src:t.a", To: "out:1"})
}

func TestLineageUnionFirstMemberNote(t *testing.T) {
	g, note := lineage(t, "SELECT a FROM t UNION ALL SELECT b FROM u")
	require.Contains(t, note, "first member")
	byID := flowByID(t, g)
	_, hasU := byID["src:u.b"]
	require.False(t, hasU, "only the first member is traced")
	require.Contains(t, g.Edges, flowEdge{From: "src:t.a", To: "out:0"})
}

func TestLineageDedupEdges(t *testing.T) {
	g, _ := lineage(t, "SELECT a + a AS s FROM t")
	n := 0
	for _, e := range g.Edges {
		if e.From == "src:t.a" && e.To == "out:0" {
			n++
		}
	}
	require.Equal(t, 1, n, "a column referenced twice draws one edge")
}

// The ranges anchor the editor highlight: an output node covers its item, a
// source node its first identifier occurrence — node-SQL-relative, like the
// statement lens.
func TestLineageRanges(t *testing.T) {
	const sql = "SELECT x + 1 AS y, b FROM t"
	g, _ := lineage(t, sql)
	byID := flowByID(t, g)
	out := byID["out:0"]
	require.Equal(t, "x + 1 AS y", sql[out.Start:out.End])
	src := byID["src:t.x"]
	require.Equal(t, "x", sql[src.Start:src.End])
}

func TestLineageDeterminism(t *testing.T) {
	const sql = "SELECT d.id, count() AS c, id + c AS z FROM d INNER JOIN e AS u ON d.id = u.id GROUP BY d.id"
	g1, n1 := lineage(t, sql)
	g2, n2 := lineage(t, sql)
	require.Equal(t, g1, g2)
	require.Equal(t, n1, n2)
}

func TestLineageNonSelect(t *testing.T) {
	_, _, err := buildLineageGraph("INSERT INTO t VALUES (1)", nil)
	require.Error(t, err)
}
