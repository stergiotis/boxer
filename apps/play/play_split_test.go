package play

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func nodeByID(res splitResult, id NodeID) (splitNode, bool) {
	for _, n := range res.Nodes {
		if n.ID == id {
			return n, true
		}
	}
	return splitNode{}, false
}

func TestStatementSplitQuoteAndCommentAware(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
		want []string
	}{
		{"single", "SELECT 1", []string{"SELECT 1"}},
		{"two", "SELECT 1; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
		{"trailing semicolon", "SELECT 1;", []string{"SELECT 1"}},
		{"semicolon in string", "SELECT ';' AS x", []string{"SELECT ';' AS x"}},
		{"semicolon in backtick ident", "SELECT `a;b` FROM t", []string{"SELECT `a;b` FROM t"}},
		{"semicolon in block comment", "SELECT 1 /* ; */ ; SELECT 2", []string{"SELECT 1 /* ; */", "SELECT 2"}},
		{"blank fragments dropped", ";; SELECT 1 ;;", []string{"SELECT 1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, statementSplit(tc.sql))
		})
	}
}

func TestSplitGraphPlainStatementIsOneNode(t *testing.T) {
	res, err := splitGraph("SELECT * FROM t")
	require.NoError(t, err)
	require.Equal(t, mainNodeID, res.Sink)
	require.Len(t, res.Nodes, 1)
	sink, ok := nodeByID(res, mainNodeID)
	require.True(t, ok)
	require.Equal(t, splitNodeStatement, sink.Kind)
	require.Empty(t, sink.DependsOn)
}

func TestSplitGraphLiftsCTEsAsNodesWithDataEdges(t *testing.T) {
	res, err := splitGraph("WITH a AS (SELECT 1 AS x), b AS (SELECT x FROM a) SELECT count() FROM b")
	require.NoError(t, err)
	require.Equal(t, mainNodeID, res.Sink)

	a, ok := nodeByID(res, "a")
	require.True(t, ok)
	require.Equal(t, splitNodeCTE, a.Kind)
	require.Empty(t, a.DependsOn)
	require.Contains(t, a.SQL, "SELECT 1")

	b, ok := nodeByID(res, "b")
	require.True(t, ok)
	require.Equal(t, []NodeID{"a"}, b.DependsOn, "b reads a → data edge")

	sink, ok := nodeByID(res, mainNodeID)
	require.True(t, ok)
	require.Equal(t, []NodeID{"b"}, sink.DependsOn, "the main SELECT reads b")
}

func TestSplitGraphSignalEdgesFromParamSlots(t *testing.T) {
	res, err := splitGraph("SELECT * FROM t WHERE x = {p:String} AND y = {q:UInt8}")
	require.NoError(t, err)
	sink, ok := nodeByID(res, mainNodeID)
	require.True(t, ok)
	require.Equal(t, []SignalID{"p", "q"}, sink.Reads)
}

func TestSplitGraphSinkIsLastNonSetStatement(t *testing.T) {
	// A SET prelude contributes no node; the SELECT is the sink.
	res, err := splitGraph("SET param_event = 'DDOS'; SELECT * FROM t WHERE has(c, {event:String})")
	require.NoError(t, err)
	require.Equal(t, mainNodeID, res.Sink)
	sink, ok := nodeByID(res, mainNodeID)
	require.True(t, ok)
	require.Contains(t, sink.Reads, SignalID("event"))
}

func TestSplitGraphRejectsSetOnlyBuffer(t *testing.T) {
	_, err := splitGraph("SET param_x = 1")
	require.Error(t, err)
}

func TestSplitGraphRejectsMultipleStatements(t *testing.T) {
	// Executing only the last statement would silently drop the others
	// (review finding); the split rejects, Run falls back to the raw buffer,
	// and the server reports its native multi-statement error.
	_, err := splitGraph("CREATE TABLE t (x Int64) ENGINE = Memory; SELECT 1 AS a")
	require.ErrorContains(t, err, "multi-statement")
	_, err = splitGraph("SELECT 1; SELECT 2")
	require.ErrorContains(t, err, "multi-statement")
}

func TestSplitGraphSinkIDStepsAsideForReferencedMainCTE(t *testing.T) {
	// A user CTE named "main" that the sink reads: previously the shared id
	// made checkAcyclic see a self-edge and reject a legal query as a
	// "dependency cycle" (review finding).
	exec, res, err := fuseToSink("WITH main AS (SELECT 1 AS n) SELECT n + 1 AS m FROM main")
	require.NoError(t, err)
	require.Len(t, res.Nodes, 2)
	require.NotEqual(t, mainNodeID, res.Sink, "the synthetic sink id must step aside")
	sink, ok := nodeByID(res, res.Sink)
	require.True(t, ok)
	require.Equal(t, splitNodeStatement, sink.Kind)
	require.Equal(t, []NodeID{"main"}, sink.DependsOn)
	require.Contains(t, exec, "n + 1", "the sink statement executes, not the CTE body")

	cte, ok := nodeByID(res, "main")
	require.True(t, ok)
	require.Equal(t, splitNodeCTE, cte.Kind, "the user CTE keeps its SQL-meaningful id")
}

func TestSplitGraphSinkIDStepsAsideForUnreferencedMainCTE(t *testing.T) {
	// Unreferenced variant: previously the duplicate id made fuseToSink fuse
	// the CTE body instead of the statement — wrong results (review finding).
	exec, res, err := fuseToSink("WITH main AS (SELECT 1 AS n) SELECT 2 AS m")
	require.NoError(t, err)
	require.NotEqual(t, mainNodeID, res.Sink)
	require.Equal(t, "WITH main AS (SELECT 1 AS n) SELECT 2 AS m", exec,
		"the sink statement round-trips verbatim (previously the CTE body executed)")
}

func TestCheckUniqueIDsRejectsDuplicates(t *testing.T) {
	require.NoError(t, checkUniqueIDs([]splitNode{{ID: "a"}, {ID: "b"}}))
	require.ErrorContains(t,
		checkUniqueIDs([]splitNode{{ID: "a"}, {ID: "a"}}),
		"duplicate node id")
}

func TestUniqueSinkIDDisambiguates(t *testing.T) {
	require.Equal(t, mainNodeID, uniqueSinkID(nil))
	require.Equal(t, NodeID("main (sink)"), uniqueSinkID([]splitNode{{ID: "main"}}))
	require.Equal(t, NodeID("main (sink2)"), uniqueSinkID([]splitNode{{ID: "main"}, {ID: "main (sink)"}}))
}

func TestFuseToSinkPlainQueryIsTheOriginal(t *testing.T) {
	exec, res, err := fuseToSink("SELECT * FROM t")
	require.NoError(t, err)
	require.Equal(t, "SELECT * FROM t", exec)
	require.Equal(t, mainNodeID, res.Sink)
}

func TestFuseToSinkPreservesSetPrelude(t *testing.T) {
	exec, _, err := fuseToSink("SET param_x = 1; SELECT {x:UInt8} AS v")
	require.NoError(t, err)
	require.Contains(t, exec, "SET param_x = 1")
	require.Contains(t, exec, "{x:UInt8}")
}

func TestFuseToSinkCTEQueryInlinesCTEs(t *testing.T) {
	exec, res, err := fuseToSink("WITH a AS (SELECT 1 AS x) SELECT x FROM a")
	require.NoError(t, err)
	require.Len(t, res.Nodes, 2) // a + main
	require.Contains(t, exec, "WITH a")
	require.Contains(t, exec, "FROM a")
}

func TestFuseToSinkErrorsOnUnparseable(t *testing.T) {
	_, _, err := fuseToSink("this is not sql ;;;")
	require.Error(t, err)
}

func TestFuseNodeLeafCTEIsItsBody(t *testing.T) {
	_, res, err := fuseToSink("WITH recent AS (SELECT 1 AS x), bk AS (SELECT x FROM recent) SELECT * FROM bk")
	require.NoError(t, err)
	exec := fuseNode(res, "recent")
	require.Contains(t, exec, "SELECT 1")
	require.NotContains(t, exec, "WITH", "a leaf CTE has no deps, so no WITH clause")
}

func TestFuseNodeDependentCTEAssemblesWith(t *testing.T) {
	_, res, err := fuseToSink("WITH recent AS (SELECT 1 AS x), bk AS (SELECT x, count() AS n FROM recent GROUP BY x) SELECT * FROM bk")
	require.NoError(t, err)
	exec := fuseNode(res, "bk")
	require.Contains(t, exec, "WITH recent AS")
	require.Contains(t, exec, "FROM recent")
}

func TestFuseNodePrependsPrelude(t *testing.T) {
	_, res, err := fuseToSink("SET param_x = 1; WITH a AS (SELECT {x:UInt8} AS v) SELECT v FROM a")
	require.NoError(t, err)
	exec := fuseNode(res, "a")
	require.Contains(t, exec, "SET param_x = 1")
	require.Contains(t, exec, "{x:UInt8}")
}

func TestTransitiveDepsTopoOrder(t *testing.T) {
	res := splitResult{Nodes: []splitNode{
		{ID: "a"},
		{ID: "b", DependsOn: []NodeID{"a"}},
		{ID: "c", DependsOn: []NodeID{"b"}},
	}}
	require.Equal(t, []NodeID{"a", "b"}, transitiveDeps(res, "c"))
}

func TestCheckAcyclic(t *testing.T) {
	require.NoError(t, checkAcyclic([]splitNode{
		{ID: "a"},
		{ID: "b", DependsOn: []NodeID{"a"}},
		{ID: "main", DependsOn: []NodeID{"a", "b"}},
	}))
	require.Error(t, checkAcyclic([]splitNode{
		{ID: "a", DependsOn: []NodeID{"b"}},
		{ID: "b", DependsOn: []NodeID{"a"}},
	}))
}
