package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

const stateTestSql = "SET param_level = 2;\n-- play: enum level 1=Macro,2=Micro,3\nSELECT {level:UInt8} AS l, {q:String} AS q, {from:DateTime64(3)} AS f, {to:DateTime64(3)} AS t"

// stateTestApp installs a buffer and the slots its parse would give.
func stateTestApp(t *testing.T) (*PlayLauncher, app.OperationsHandlerI) {
	t.Helper()
	l, h := opsLauncher(t)
	l.inner.swapSql(stateTestSql)
	l.inner.formattedFor = stateTestSql // the debounced preview has caught up
	l.inner.refreshParamSlotsFromParse([]paramSlot{{Name: "level", Type: "UInt8"}, {Name: "q", Type: "String"},
		{Name: "from", Type: "DateTime64(3)"}, {Name: "to", Type: "DateTime64(3)"}},
		map[string]string{"param_level": "2"})
	return l, h
}

func paramByName(st PlayState, name string) (ps ParamState) {
	for _, p := range st.Params {
		if p.Name == name {
			return p
		}
	}
	return
}

func signalByName(st PlayState, name string) (ss SignalState, ok bool) {
	for _, s := range st.Signals {
		if s.Name == name {
			return s, true
		}
	}
	return
}

// get_state reports each parameter as the block draws it: its control, an
// enum's options, the folded pair, the default Reset restores, and whether a
// run still needs it.
func TestGetStateReportsTheParametersControls(t *testing.T) {
	l, h := stateTestApp(t)
	st := queryOp[PlayState](t, h, opGetState, nil)

	level := paramByName(st, "level")
	assert.Equal(t, "enum", level.Widget)
	assert.Equal(t, "pinned", level.Tier)
	assert.Equal(t, []ParamOption{{Value: "1", Label: "Macro"}, {Value: "2", Label: "Micro"}, {Value: "3"}}, level.Options)
	require.NotNil(t, level.Default)
	assert.Equal(t, "2", *level.Default)
	assert.False(t, level.Moved)
	assert.False(t, level.Unfilled)

	q := paramByName(st, "q")
	assert.Equal(t, "text", q.Widget)
	assert.Equal(t, "live", q.Tier)
	assert.True(t, q.Unfilled)
	assert.Nil(t, q.Default, "only a prelude value has a default")

	from := paramByName(st, "from")
	assert.Contains(t, []string{"range", "datetime pair"}, from.Widget)
	assert.Equal(t, "to", from.With)
	assert.Equal(t, "from", paramByName(st, "to").With)

	// The buffer reads q and nothing holds it: a signal row says so.
	qs, ok := signalByName(st, "q")
	require.True(t, ok)
	assert.False(t, qs.Held)
	assert.True(t, qs.Read)
	assert.True(t, qs.Unfilled)
	assert.Equal(t, []string{"String"}, qs.Types)
	ls, ok := signalByName(st, "level")
	require.True(t, ok)
	assert.True(t, ls.Pinned, "a SET line shadows the store")

	*l.inner.paramDrafts["level"] = "3"
	st = queryOp[PlayState](t, h, opGetState, nil)
	assert.True(t, paramByName(st, "level").Moved)
}

// set_param refuses an agent's value an enum does not offer, naming the
// options; the person's dropdown is not second-guessed.
func TestSetParamRefusesAValueTheEnumDoesNotOffer(t *testing.T) {
	l, h := stateTestApp(t)
	err := applyOp(t, h, opSetParam, SetParamArgs{Name: "level", Value: "7"})
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Reason, "'1' (Macro)")
	assert.Equal(t, "2", *l.inner.paramDrafts["level"], "nothing was written")

	require.NoError(t, applyOp(t, h, opSetParam, SetParamArgs{Name: "level", Value: "3"}))
	assert.Equal(t, "3", *l.inner.paramDrafts["level"])
	require.NoError(t, applyOp(t, h, opSetParam, SetParamArgs{Name: "q", Value: "anything"}), "a text field takes any value")

	_, err = h.ApplyCommand(app.OperationCall{Writer: opwire.WriterPerson}, opSetParam, mustEncode(t, SetParamArgs{Name: "level", Value: "9"}))
	require.NoError(t, err)
	assert.Equal(t, "9", *l.inner.paramDrafts["level"])
}

// The status line's override, the statement run would ship, and a pane's
// output signal reach get_state.
func TestGetStateReportsTheNoticeStatementsAndRefusedSignals(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	p.runBlockedReason = "the class ceiling refuses it"
	st := queryOp[PlayState](t, h, opGetState, nil)
	assert.Equal(t, "Run blocked: the class ceiling refuses it", st.Result.Notice)
	p.liveSuspendReason = "Live paused: tl_from kept moving"
	st = queryOp[PlayState](t, h, opGetState, nil)
	assert.Equal(t, "Live paused: tl_from kept moving", st.Result.Notice, "the breaker outranks a refusal")
	assert.Empty(t, st.Result.Truncated)
	assert.Empty(t, st.Result.Progress)

	assert.Zero(t, st.Statements, "one statement")
	p.sql = "SELECT 7 AS q;\nSELECT 8 AS r"
	p.caretByte = len(p.sql) - 2
	st = queryOp[PlayState](t, h, opGetState, nil)
	assert.Equal(t, int32(2), st.Statements)
	assert.Equal(t, int32(2), st.RunStatement)

	p.graph.setSignalRawFrom("tl_from", "2026-10-01 00:00:00.000", "timeline")
	st = queryOp[PlayState](t, h, opGetState, nil)
	tl, ok := signalByName(st, "tl_from")
	require.True(t, ok)
	assert.True(t, tl.Held)
	assert.Equal(t, "timeline", tl.Pane)
	assert.Contains(t, tl.Refused, "set_timeline_window")
	assert.NotZero(t, tl.Revision)
}

// get_query_graph reads the split: kinds, dependencies, the panes each node
// feeds, and the system graph's edges.
func TestGetQueryGraphReadsTheSplit(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	p.sql = bindTestSQL
	split, err := splitGraph(bindTestSQL)
	require.NoError(t, err)
	p.currentSplit = split
	// An observed node runs on the intermediate lane; serve it without an
	// endpoint.
	p.intermediateLane.close()
	p.intermediateLane = newNodeLane(labelledFakeExec{rec: func() arrow.RecordBatch { return int64Rec("n", 1) }},
		memory.NewGoAllocator(), 0)
	t.Cleanup(p.intermediateLane.close)

	g := queryOp[QueryGraph](t, h, opGetQueryGraph, nil)
	assert.Equal(t, string(split.Sink), g.Sink)
	ids := make([]string, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		ids = append(ids, n.Id)
	}
	assert.Equal(t, []string{"recent", "by_kind", string(split.Sink)}, ids)
	assert.Equal(t, "cte", g.Nodes[0].Kind)
	assert.Equal(t, []string{"recent"}, g.Nodes[1].DependsOn)
	assert.Equal(t, "sink", g.Nodes[2].Kind)
	assert.Contains(t, g.Nodes[2].Panes, "table", "the unbound panels draw the sink")
	assert.Contains(t, g.Nodes[0].Sql, "SELECT 1")
	assert.Contains(t, g.Edges, QueryGraphEdge{From: "node/recent", To: "node/by_kind", Kind: "data"})
	assert.Contains(t, g.Edges, QueryGraphEdge{From: "node/" + string(split.Sink), To: "tab/table", Kind: "feeds"})
	assert.Empty(t, g.Observed)

	// observe_node moves every unbound panel and the panes resource.
	before := h.ResourceValue(opsResPanes)
	require.NoError(t, applyOp(t, h, opObserveNode, ObserveNodeArgs{Node: "recent"}))
	assert.Equal(t, NodeID("recent"), p.observedNode)
	assert.NotEqual(t, before, h.ResourceValue(opsResPanes))
	g = queryOp[QueryGraph](t, h, opGetQueryGraph, nil)
	assert.Equal(t, "recent", g.Observed)
	assert.Contains(t, g.Nodes[0].Panes, "table")

	err = applyOp(t, h, opObserveNode, ObserveNodeArgs{Node: "nope"})
	var conflict *app.OperationRefusal
	require.ErrorAs(t, err, &conflict)
	assert.True(t, conflict.Conflict)
	assert.Contains(t, conflict.Reason, "by_kind")
	assert.Equal(t, NodeID("recent"), p.observedNode, "a refused call changes nothing")

	require.NoError(t, applyOp(t, h, opObserveNode, ObserveNodeArgs{}))
	assert.Empty(t, p.observedNode)
}

// The Graph pane's observe button goes through observe_node.
func TestObserveButtonGoesThroughObserveNode(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	split, err := splitGraph(bindTestSQL)
	require.NoError(t, err)
	p.currentSplit = split

	p.personObserveNode("by_kind")
	e := lastEntry(t, eng)
	assert.Equal(t, opObserveNode, e.Op)
	assert.Equal(t, opwire.WriterPerson, eng.Writer(opsResPanes))
	assert.Equal(t, NodeID("by_kind"), p.observedNode)

	// Without a host the button applies directly.
	bare, _ := opsLauncher(t)
	bare.inner.personObserveNode("recent")
	assert.Equal(t, NodeID("recent"), bare.inner.observedNode)
}

// list_history reads the session's runs newest first, with the buffer a
// run came from and the signals it sent, and pages.
func TestListHistoryReadsTheSessionNewestFirst(t *testing.T) {
	l, h := opsLauncher(t)
	lane := l.inner.graph.mainLane
	lane.finish("SELECT 1", nil, time.Now(), nil, nil, 1, Summary{}, nil, runstream.Terminal{})
	lane.sourceBuffer = "SELECT 0;\nSELECT 2"
	lane.finish("SELECT 2", map[string]string{"param_k": "v"}, time.Now(), nil, nil, 0, Summary{}, assert.AnError, runstream.Terminal{})

	out := queryOp[HistoryList](t, h, opListHistory, nil)
	require.Len(t, out.Runs, 2)
	assert.Equal(t, int32(2), out.Total)
	assert.False(t, out.More)
	newest := out.Runs[0]
	assert.Equal(t, int32(0), newest.Index)
	assert.Equal(t, "SELECT 2", newest.Sql)
	assert.Equal(t, "SELECT 0;\nSELECT 2", newest.Buffer)
	assert.Equal(t, map[string]string{"param_k": "v"}, newest.Signals)
	assert.NotEmpty(t, newest.Error)
	assert.Equal(t, "SELECT 1", out.Runs[1].Sql)
	assert.Equal(t, int64(1), out.Runs[1].Rows)

	page := queryOp[HistoryList](t, h, opListHistory, HistoryArgs{Limit: 1})
	require.Len(t, page.Runs, 1)
	assert.True(t, page.More)
	page = queryOp[HistoryList](t, h, opListHistory, HistoryArgs{Offset: 1, Limit: 1})
	require.Len(t, page.Runs, 1)
	assert.Equal(t, "SELECT 1", page.Runs[0].Sql)
	assert.Equal(t, int32(1), page.Runs[0].Index)
}
