package play

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
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

func TestSplitGraphSetClassificationIsGrammarBased(t *testing.T) {
	// The former textual HasPrefix("SET ") check misread these prelude
	// spellings as query statements and rejected the buffer as multi-statement
	// (review finding); classification now matches ExtractParams' grammar-level
	// view, so the splitter and the client agree on what is a prelude.
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"block-comment prefix", "/* c */ SET param_a = 1; SELECT {a:UInt64}"},
		{"line-comment prefix", "-- lead\nSET param_a = 1;\nSELECT {a:UInt64}"},
		{"newline after SET", "SET\nparam_a = 1;\nSELECT {a:UInt64}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := splitGraph(tc.sql)
			require.NoError(t, err)
			require.Len(t, res.Prelude, 1, "the SET statement is a prelude, not a statement node")
			require.Equal(t, mainNodeID, res.Sink)
			sink, ok := nodeByID(res, mainNodeID)
			require.True(t, ok)
			require.Equal(t, []SignalID{"a"}, sink.Reads)
		})
	}
}

func TestFuseNodeMergesBodyOwnWithClause(t *testing.T) {
	// A CTE body opening its own WITH clause, plus a sibling dep: fusing must
	// CONTINUE that WITH list with the dep definitions — prepending a second
	// `WITH` produced `WITH a AS (…) WITH x AS (…) SELECT …`, invalid SQL
	// (review finding).
	_, res, err := fuseToSink("WITH a AS (SELECT 1 AS v), b AS (WITH x AS (SELECT 2 AS w) SELECT v, w FROM a, x) SELECT * FROM b")
	require.NoError(t, err)
	b, ok := nodeByID(res, "b")
	require.True(t, ok)
	require.True(t, b.OwnWith)

	exec := fuseNode(res, "b")
	require.Contains(t, exec, "a AS (")
	require.Contains(t, exec, "x AS (SELECT 2 AS w)")
	require.Equal(t, 1, strings.Count(exec, "WITH"), "one WITH clause, continued with a comma")
	_, pErr := nanopass.Parse(exec)
	require.NoError(t, pErr, "the fused SQL must parse: %s", exec)
}

func TestFuseNodeMergesScalarAliasWith(t *testing.T) {
	// The body's own WITH list may hold scalar aliases, not just CTEs; the
	// merged list mixes both item kinds, which ClickHouse accepts.
	_, res, err := fuseToSink("WITH a AS (SELECT 1 AS v), b AS (WITH 42 AS answer SELECT answer, v FROM a) SELECT * FROM b")
	require.NoError(t, err)
	exec := fuseNode(res, "b")
	require.Contains(t, exec, "42 AS answer")
	_, pErr := nanopass.Parse(exec)
	require.NoError(t, pErr, "mixed CTE + scalar-alias WITH items fuse parseably: %s", exec)
}

func TestSplitGraphSinkDepsIncludeSubqueryRefs(t *testing.T) {
	// A CTE referenced only inside a derived table drew no sink edge (review
	// finding): dep derivation now descends FROM/expression subqueries.
	res, err := splitGraph("WITH a AS (SELECT 1 AS v) SELECT * FROM (SELECT * FROM a)")
	require.NoError(t, err)
	sink, ok := nodeByID(res, res.Sink)
	require.True(t, ok)
	require.Equal(t, []NodeID{"a"}, sink.DependsOn)
}

func TestSplitGraphSinkDepsIncludeUnionMemberRefs(t *testing.T) {
	res, err := splitGraph("WITH a AS (SELECT 1 AS v) SELECT 2 AS n UNION ALL SELECT v FROM a")
	require.NoError(t, err)
	sink, ok := nodeByID(res, res.Sink)
	require.True(t, ok)
	require.Equal(t, []NodeID{"a"}, sink.DependsOn, "refs in later UNION members are sink edges too")
}

func TestSplitGraphNestedCTERefIsNotAPhantomEdge(t *testing.T) {
	// b's body references its own nested CTE x; x is not a graph node, so it
	// must not surface as an edge (the Graph view previously showed
	// "reads nodes: a, x" — review finding). References inside x's body still
	// count as b's edges: x's body is part of b's fused unit.
	res, err := splitGraph("WITH a AS (SELECT 1 AS v), b AS (WITH x AS (SELECT v FROM a) SELECT w FROM x) SELECT * FROM b")
	require.NoError(t, err)
	b, ok := nodeByID(res, "b")
	require.True(t, ok)
	require.Equal(t, []NodeID{"a"}, b.DependsOn, "the ref inside nested x's body is b's edge; x itself is not")
}

func TestSplitGraphShadowingNestedCTEIsNotAnEdge(t *testing.T) {
	// b's nested CTE shadows the top-level a: references in b's body resolve to
	// the nested definition, so there is no b→a edge — and fusing b must NOT
	// inline the top-level a (a duplicate definition with different content).
	res, err := splitGraph("WITH a AS (SELECT 1 AS v), b AS (WITH a AS (SELECT 2 AS w) SELECT * FROM a) SELECT * FROM b")
	require.NoError(t, err)
	b, ok := nodeByID(res, "b")
	require.True(t, ok)
	require.Empty(t, b.DependsOn, "the shadowed reference resolves to the nested definition")
	exec := fuseNode(res, "b")
	require.NotContains(t, exec, "SELECT 1 AS v", "top-level a is not inlined")
	_, pErr := nanopass.Parse(exec)
	require.NoError(t, pErr)
}

// --- WITH RECURSIVE (ADR-0097 SD9, realized once grammar1 learned it) ---

const recursiveBody = "SELECT 1 AS x UNION ALL SELECT x + 1 FROM t WHERE x < 10"

func TestSplitGraphRecursiveCTEIsOneNodeNoCycle(t *testing.T) {
	// SD9: the self-reference stays INSIDE the node — it is not a graph edge,
	// so the split succeeds (previously the buffer failed the grammar and took
	// the raw fallback; a naive self-edge would read as a bogus cycle).
	res, err := splitGraph("WITH RECURSIVE t AS (" + recursiveBody + ") SELECT * FROM t")
	require.NoError(t, err)
	require.Len(t, res.Nodes, 2)

	n, ok := nodeByID(res, "t")
	require.True(t, ok)
	require.True(t, n.Recursive)
	require.Empty(t, n.DependsOn, "the self-reference is node-internal, not an edge")

	sink, ok := nodeByID(res, res.Sink)
	require.True(t, ok)
	require.Equal(t, []NodeID{"t"}, sink.DependsOn)
}

func TestFuseNodeRecursiveCTEWrapsAsWithItem(t *testing.T) {
	// Observing a recursive CTE cannot execute the bare body (it references
	// its own name); it materialises as a self-contained WITH RECURSIVE item
	// read by a SELECT.
	_, res, err := fuseToSink("WITH RECURSIVE t AS (" + recursiveBody + ") SELECT * FROM t")
	require.NoError(t, err)
	exec := fuseNode(res, "t")
	require.Contains(t, exec, "WITH RECURSIVE t AS (")
	require.Contains(t, exec, "SELECT * FROM t")
	_, pErr := nanopass.Parse(exec)
	require.NoError(t, pErr, "the fused SQL must parse: %s", exec)
}

func TestFuseNodeDepOnRecursiveEmitsRecursiveClause(t *testing.T) {
	// A node inlining a recursive dep needs the clause-wide RECURSIVE keyword;
	// both nodes come from the same recursive clause here, so the dependent
	// also wraps (its own name may be referenced under RECURSIVE semantics).
	_, res, err := fuseToSink("WITH RECURSIVE t AS (" + recursiveBody + "), agg AS (SELECT max(x) AS m FROM t) SELECT * FROM agg")
	require.NoError(t, err)
	agg, ok := nodeByID(res, "agg")
	require.True(t, ok)
	require.True(t, agg.Recursive, "RECURSIVE is clause-wide")
	require.Equal(t, []NodeID{"t"}, agg.DependsOn)

	exec := fuseNode(res, "agg")
	require.Contains(t, exec, "WITH RECURSIVE t AS (")
	require.Contains(t, exec, "agg AS (")
	require.Contains(t, exec, "SELECT * FROM agg")
	_, pErr := nanopass.Parse(exec)
	require.NoError(t, pErr, "the fused SQL must parse: %s", exec)
}

func TestFuseNodeOwnWithRecursiveBodyWrapsWithDeps(t *testing.T) {
	// A NON-recursive clause whose CTE body opens its own WITH RECURSIVE:
	// the comma-merge cannot continue that list (the keyword would land
	// mid-list), so attaching the dep uses the wrap form with the body's own
	// clause intact inside the parens.
	sql := "WITH a AS (SELECT 1 AS v), b AS (WITH RECURSIVE i AS (SELECT 1 AS x UNION ALL SELECT x + 1 FROM i WHERE x < 3) SELECT max(x) AS m, v FROM i, a) SELECT * FROM b"
	_, res, err := fuseToSink(sql)
	require.NoError(t, err)
	b, ok := nodeByID(res, "b")
	require.True(t, ok)
	require.False(t, b.Recursive, "the outer clause is not recursive")
	require.True(t, b.OwnWith)
	require.True(t, b.OwnWithRecursive)
	require.Equal(t, []NodeID{"a"}, b.DependsOn)

	exec := fuseNode(res, "b")
	require.Contains(t, exec, "b AS (")
	require.Contains(t, exec, "WITH RECURSIVE i AS (")
	require.Contains(t, exec, "SELECT * FROM b")
	_, pErr := nanopass.Parse(exec)
	require.NoError(t, pErr, "the fused SQL must parse: %s", exec)
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
