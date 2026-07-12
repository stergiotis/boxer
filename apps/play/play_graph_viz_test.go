package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// System-graph model tests (the Graph tab's layered drawing): the model is a
// pure projection of frame state — split nodes, reads split into constants
// vs signals, tab feeds (bindings), provenance write-backs, the unfilled
// marking — and its fingerprint must ignore value churn.

func modelIDs(t *testing.T, app *PlayApp) (nodes map[string]vizKindE, edges map[[2]string]string) {
	t.Helper()
	m, meta := app.buildSystemGraphModel()
	nodes = meta
	edges = make(map[[2]string]string, len(m.Edges))
	for _, e := range m.Edges {
		edges[[2]string{e.From, e.To}] = e.Label
	}
	for _, n := range m.Nodes {
		_, ok := meta[n.ID]
		require.True(t, ok, "every model node carries a kind: %s", n.ID)
	}
	return
}

func TestSystemGraphModelSpine(t *testing.T) {
	app, _ := bindTestApp(t)
	nodes, edges := modelIDs(t, app)

	// Split nodes with the sink marked.
	require.Equal(t, vizQuery, nodes["node/recent"])
	require.Equal(t, vizQuery, nodes["node/by_kind"])
	require.Equal(t, vizSink, nodes["node/main"])
	_, dep := edges[[2]string{"node/recent", "node/by_kind"}]
	assert.True(t, dep, "CTE dependency edge")
	_, sinkDep := edges[[2]string{"node/by_kind", "node/main"}]
	assert.True(t, sinkDep)

	// Every result panel tab is fed by the active node (main) when unbound.
	for _, tab := range []string{"table", "projection", "timeline", "detail", "world", "schema"} {
		require.Equal(t, vizTab, nodes["tab/"+tab], tab)
		_, fed := edges[[2]string{"node/main", "tab/" + tab}]
		assert.True(t, fed, "unbound tab %s is fed by the sink", tab)
	}
	assert.Equal(t, "events", edges[[2]string{"node/main", "tab/timeline"}], "the timeline feed is the events channel")
}

func TestSystemGraphModelBindingAndWriteBack(t *testing.T) {
	app, _ := bindTestApp(t)
	app.bindTab("table", "recent")
	release := app.demandBoundNodes()
	defer release()
	// A provenance write-back: the Table panel wrote the selection triple.
	app.graph.setSignalRawFrom(string(signalSelection), "2", "table")
	app.graph.setSignalRawFrom(string(signalSelectionNode), "recent", "table")
	app.frameSig = app.graph.signals()

	nodes, edges := modelIDs(t, app)
	_, boundFeed := edges[[2]string{"node/recent", "tab/table"}]
	assert.True(t, boundFeed, "the bound tab is fed by its node")
	_, sinkFeed := edges[[2]string{"node/main", "tab/table"}]
	assert.False(t, sinkFeed, "not double-fed by the sink")

	require.Equal(t, vizSignal, nodes["sig/selection"], "held signal")
	_, wb := edges[[2]string{"tab/table", "sig/selection"}]
	assert.True(t, wb, "provenance write-back edge")
	_, wbNode := edges[[2]string{"tab/table", "sig/selection_node"}]
	assert.True(t, wbNode)
}

func TestSystemGraphModelConstantsSignalsUnfilled(t *testing.T) {
	app, _ := bindTestApp(t)
	sql := "SET param_a = 1;\nWITH c AS (SELECT {a:Int64} AS x, {b:Int64} AS y) SELECT * FROM c"
	split, err := splitGraph(sql)
	require.NoError(t, err)
	app.currentSplit = split
	app.paramSyncedValues = map[string]string{"a": "1"}

	nodes, edges := modelIDs(t, app)
	require.Equal(t, vizConst, nodes["const/a"], "SET-bound read is a constant")
	_, constEdge := edges[[2]string{"const/a", "node/c"}]
	assert.True(t, constEdge)
	require.Equal(t, vizUnfilled, nodes["sig/b"], "unbound, unheld read is unfilled (D3)")

	app.graph.setSignalRaw("b", "7")
	app.frameSig = app.graph.signals()
	nodes, _ = modelIDs(t, app)
	assert.Equal(t, vizSignal, nodes["sig/b"], "held now — colour-only flip")
}

func TestSystemGraphModelSynthesizesMainWithoutSplit(t *testing.T) {
	app := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
	defer app.graph.close()
	app.frameSig = app.graph.signals()
	nodes, edges := modelIDs(t, app)
	require.Equal(t, vizSink, nodes["node/main"], "no split ⇒ the degenerate single-node graph")
	_, fed := edges[[2]string{"node/main", "tab/table"}]
	assert.True(t, fed)
}

// The layout-cache key tracks topology, not values: a moved signal value
// leaves it unchanged; a new binding (a feed-edge flip) changes it.
func TestSystemGraphKeyIgnoresValueChurn(t *testing.T) {
	app, _ := bindTestApp(t)
	app.graph.setSignalRawFrom(string(signalSelection), "1", "table")
	app.frameSig = app.graph.signals()
	m1, _ := app.buildSystemGraphModel()
	k1 := systemGraphKey(m1)

	app.graph.setSignalRawFrom(string(signalSelection), "9", "table")
	app.frameSig = app.graph.signals()
	m2, _ := app.buildSystemGraphModel()
	assert.Equal(t, k1, systemGraphKey(m2), "value churn must not relayout")

	app.bindTab("timeline", "recent")
	release := app.demandBoundNodes()
	defer release()
	m3, _ := app.buildSystemGraphModel()
	assert.NotEqual(t, k1, systemGraphKey(m3), "a binding is a topology change")
}
