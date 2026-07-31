package play

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// Tests for the flow-selection → editor coordinate chain (ADR-0153 editor
// highlight): splitNode.SrcOff anchoring, flowNode range capture, and the
// buffer-locate gate. The full path is exercised by slicing with the same
// arithmetic flowSelectionSection uses — the slice-equality guard IS the
// contract.

func TestSplitNodeSrcOffAnchorsBodies(t *testing.T) {
	const sql = "WITH d AS (SELECT number AS id FROM numbers(10)), e AS (SELECT id FROM d) SELECT * FROM e"
	res, err := splitGraph(sql)
	require.NoError(t, err)

	sink, ok := findSplitNode(res, res.Sink)
	require.True(t, ok)
	require.Equal(t, 0, sink.SrcOff, "the sink is the statement verbatim")
	require.Equal(t, sql, sink.SQL)

	for _, id := range []NodeID{"d", "e"} {
		node, ok := findSplitNode(res, id)
		require.True(t, ok)
		require.GreaterOrEqual(t, node.SrcOff, 0, "CTE %s must anchor", id)
		require.Equal(t, node.SQL, sql[node.SrcOff:node.SrcOff+len(node.SQL)],
			"CTE %s: SrcOff must slice the statement back to the body", id)
	}
}

// The flow graph's clause ranges slice the derived SQL back to the clause —
// the node-SQL-relative half of the chain.
func TestFlowNodeRangesSliceTheirClauses(t *testing.T) {
	const sql = "SELECT a, count() AS c FROM t WHERE a > 0 GROUP BY a HAVING c > 1 ORDER BY c LIMIT 5"
	g, err := buildFlowGraph(sql, nil, "main")
	require.NoError(t, err)
	byID := flowByID(t, g)

	for id, want := range map[string]string{
		"q:where":  "WHERE a > 0",
		"q:group":  "GROUP BY a",
		"q:having": "HAVING c > 1",
		"q:order":  "ORDER BY c",
		"q:limit":  "LIMIT 5",
		"q:select": "SELECT a, count() AS c",
		"q:src0":   "t",
	} {
		n := byID[id]
		require.Positive(t, n.End, "%s carries a range", id)
		require.Equal(t, want, sql[n.Start:n.End], "%s range slices its clause", id)
	}
}

func TestFlowJoinRangeIsTheConstraint(t *testing.T) {
	const sql = "SELECT * FROM a INNER JOIN b ON a.id = b.id"
	g, err := buildFlowGraph(sql, nil, "main")
	require.NoError(t, err)
	n := flowByID(t, g)["q:join0"]
	require.Equal(t, "ON a.id = b.id", sql[n.Start:n.End])
}

func TestLocateStatementStart(t *testing.T) {
	buffer := "SET a = 1;\n\nSELECT x FROM t;\nSELECT y FROM u"
	mk := func(s, e int) statementRange {
		return statementRange{Src: nanopass.SourceRange{Start: s, End: e}}
	}
	ranges := []statementRange{
		mk(12, 27), // "SELECT x FROM t"
		mk(29, 44), // "SELECT y FROM u"
	}
	start, ok := locateStatementStart(buffer, ranges, "SELECT y FROM u")
	require.True(t, ok)
	require.Equal(t, 29, start)
	require.Equal(t, "SELECT y FROM u", buffer[start:start+len("SELECT y FROM u")])

	_, ok = locateStatementStart(buffer, ranges, "SELECT z FROM v")
	require.False(t, ok, "an edited statement stops matching — the staleness gate")

	// A slice that trims to something else (here: it kept the delimiter)
	// declines — equality, not containment.
	_, ok = locateStatementStart(buffer, []statementRange{mk(10, 28)}, "SELECT x FROM t")
	require.False(t, ok, "a slice that trims to something else declines")
	// Leading whitespace inside the range still anchors on the trimmed text.
	start, ok = locateStatementStart(buffer, []statementRange{mk(10, 27)}, "SELECT x FROM t")
	require.True(t, ok)
	require.Equal(t, 12, start)
}

// End-to-end arithmetic over a real split: statement located mid-buffer, CTE
// body offset applied, clause range applied — the final slice must be the
// clause text.
func TestFlowHighlightChainArithmetic(t *testing.T) {
	const stmt = "WITH d AS (SELECT number AS id FROM numbers(10) WHERE number > 2) SELECT * FROM d"
	buffer := "SET max_threads = 1;\n" + stmt
	res, err := splitGraph(stmt)
	require.NoError(t, err)
	node, ok := findSplitNode(res, NodeID("d"))
	require.True(t, ok)
	require.GreaterOrEqual(t, node.SrcOff, 0)

	g, err := buildFlowGraph(node.SQL, nil, string(node.ID))
	require.NoError(t, err)
	where := flowByID(t, g)["q:where"]
	require.Positive(t, where.End)

	stmtStart := strings.Index(buffer, stmt)
	require.Positive(t, stmtStart)
	start := stmtStart + node.SrcOff + where.Start
	end := stmtStart + node.SrcOff + where.End
	require.Equal(t, "WHERE number > 2", buffer[start:end])
	require.Equal(t, node.SQL[where.Start:where.End], buffer[start:end],
		"the slice-equality guard holds on an unedited buffer")
}
