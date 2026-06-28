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
