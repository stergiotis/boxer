package play

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Slice-6c regression tests (ADR-0097 "Slice 6 (design)"): per-panel node
// binding — the binding lifecycle and lane GC, the per-frame resolution and
// frame swap, the dangling-binding fallback, and selection coherence across
// differently-bound panels (selection_node / selection_id stamping, the
// node-scoped read gate, the node-aware clamp, Detail's follow).

// leewayRec builds a two-column record with a leeway primary id column.
func leewayRec(t *testing.T, ids []uint64) arrow.RecordBatch {
	t.Helper()
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id:id:u64:2k:0:0:", Type: arrow.PrimitiveTypes.Uint64},
		{Name: "n", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	ib := array.NewUint64Builder(mem)
	nb := array.NewInt64Builder(mem)
	for i, v := range ids {
		ib.Append(v)
		nb.Append(int64(i))
	}
	ia, na := ib.NewArray(), nb.NewArray()
	rec := array.NewRecord(schema, []arrow.Array{ia, na}, int64(len(ids)))
	ib.Release()
	nb.Release()
	ia.Release()
	na.Release()
	return rec
}

func TestLeewayIdValue(t *testing.T) {
	rec := leewayRec(t, []uint64{500001, 500002})
	defer rec.Release()

	raw, found := leewayIdValue(rec, 1)
	require.True(t, found)
	assert.Equal(t, "500002", raw)

	_, found = leewayIdValue(rec, 7)
	assert.False(t, found, "out-of-range row")
	_, found = leewayIdValue(nil, 0)
	assert.False(t, found, "nil record")

	plain := int64Rec("n", 3)
	defer plain.Release()
	_, found = leewayIdValue(plain, 0)
	assert.False(t, found, "no id:id column")
}

// The dispatcher stamps every panel selection write with the primary
// channel's node and, for leeway results, the row's id value.
type selectionProbePanel struct{}

func (selectionProbePanel) ID() PanelID { return "sel-probe" }
func (selectionProbePanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true}}
}
func (selectionProbePanel) AcceptForChannel(ChannelID, *arrow.Schema, SignalEnvI) (ChannelClaim, string) {
	return true, ""
}
func (selectionProbePanel) Render(_ map[ChannelID]ChannelResult, emit SignalEmitterI) {
	emit.Emit(signalSelection, int64(1))
}

func TestDispatcherStampsSelectionNodeAndId(t *testing.T) {
	g := newQueryGraph(nil, nil)
	rec := leewayRec(t, []uint64{500001, 500002})
	defer rec.Release()

	reject := dispatchPanel(selectionProbePanel{}, map[ChannelID]channelInput{
		chMain: {node: "by_kind", rec: rec, sig: g.signals()},
	}, graphEmitter{graph: g})
	require.Empty(t, reject)

	sig := g.signals()
	sel, _ := sig.Get(signalSelection)
	node, _ := sig.Get(signalSelectionNode)
	id, _ := sig.Get(signalSelectionID)
	assert.Equal(t, "1", sel.Raw)
	assert.Equal(t, "by_kind", node.Raw, "the cursor names its node")
	assert.Equal(t, "500002", id.Raw, "the leeway id rides as a value, not an ordinal")

	rows := g.signalRows()
	for _, r := range rows {
		assert.Equal(t, "sel-probe", r.Writer, "companion writes keep the panel's provenance (%s)", r.Name)
	}
}

// The read gate: a panel sees `selection` only when the cursor indexes its
// own node; an unset selection_node (bootstrap/history) matches everything.
func TestNodeScopedSelectionGate(t *testing.T) {
	g := newQueryGraph(nil, nil)
	g.setSignalRaw(string(signalSelection), "3")

	scoped := nodeScopedSelection{SignalEnvI: g.signals(), node: "recent"}
	row, ok := readSelection(scoped)
	require.True(t, ok, "unset selection_node matches (pre-6c behaviour)")
	assert.Equal(t, int64(3), row)

	g.setSignalRaw(string(signalSelectionNode), "by_kind")
	scoped = nodeScopedSelection{SignalEnvI: g.signals(), node: "recent"}
	_, ok = readSelection(scoped)
	assert.False(t, ok, "cursor on another node is invisible here")

	scoped = nodeScopedSelection{SignalEnvI: g.signals(), node: "by_kind"}
	row, ok = readSelection(scoped)
	require.True(t, ok)
	assert.Equal(t, int64(3), row)

	other, ok := scoped.Get(signalSelectionNode)
	require.True(t, ok, "only `selection` is gated")
	assert.Equal(t, "by_kind", other.Raw)
}

const bindTestSQL = "WITH recent AS (SELECT 1 AS n), by_kind AS (SELECT n FROM recent) SELECT * FROM by_kind"

func bindTestApp(t *testing.T) (*PlayApp, func() []int) {
	t.Helper()
	srv, got := captureServer(t)
	t.Cleanup(srv.Close)
	client := NewClient(ClientConfig{URL: srv.URL}, srv.Client())
	app := NewPlayApp(client, newLiveQueryGraph(client, memory.NewGoAllocator(), 10), bindTestSQL)
	t.Cleanup(func() { app.graph.close(); app.closeBoundLanes() })
	split, err := splitGraph(bindTestSQL)
	require.NoError(t, err)
	app.currentSplit = split
	app.observedNode = split.Sink
	app.frameSig = app.graph.signals()
	return app, func() []int { return []int{len(got())} }
}

// Binding lifecycle: a bound node gets a lane and a per-frame view; frameFor
// swaps the tab's result; unbinding closes the orphaned lane; a dangling
// binding stays registered but resolves to the active result.
func TestBindingLifecycleAndFrameSwap(t *testing.T) {
	app, _ := bindTestApp(t)

	app.bindTab("table", "recent")
	require.Eventually(t, func() bool {
		release := app.demandBoundNodes()
		defer release()
		v, ok := app.boundViews["recent"]
		return ok && v.rec != nil && !v.loading
	}, 2*time.Second, time.Millisecond, "the bound node executes on its own lane")

	release := app.demandBoundNodes()
	defer release()
	require.Equal(t, NodeID("recent"), app.resolvedTabNode("table"))
	require.Equal(t, app.activeNodeID(), app.resolvedTabNode("projection"), "unbound tabs render the active result")

	base := TabFrame{Rec: nil, Loading: true}
	swapped := app.frameFor("table", &base)
	require.NotNil(t, swapped.Rec, "the bound tab's frame carries the lane view")
	assert.Equal(t, int64(1), swapped.NumRows)
	assert.False(t, swapped.Loading, "per-node loading rides the swap")
	plain := app.frameFor("projection", &base)
	assert.Nil(t, plain.Rec, "unbound tabs keep the base frame")

	assert.Contains(t, app.boundTabTitle(&TabSpec{ID: "table", Title: "Table"}), "· recent")
	assert.Equal(t, "bindings — table→recent", app.bindingSummary())

	app.unbindTab("table")
	assert.Empty(t, app.boundLanes, "orphaned lane closed on unbind")
}

func TestDanglingBindingIsInert(t *testing.T) {
	app, _ := bindTestApp(t)
	app.bindTab("table", "ghost")
	release := app.demandBoundNodes()
	defer release()
	assert.Equal(t, app.activeNodeID(), app.resolvedTabNode("table"), "dangling binding falls back to the active result")
	assert.Empty(t, app.boundViews, "no lane demand for an absent node")
	assert.Contains(t, app.boundTabTitle(&TabSpec{ID: "table", Title: "Table"}), "(ghost absent)")
	_, still := app.tabBindings["table"]
	assert.True(t, still, "the binding survives to revive when the name returns")
}

// Detail follows the selection's node when that node is on screen; an
// explicit Detail binding wins over the follow.
func TestDetailFollowsSelectionNode(t *testing.T) {
	app, _ := bindTestApp(t)
	app.bindTab("table", "recent")
	require.Eventually(t, func() bool {
		release := app.demandBoundNodes()
		defer release()
		v, ok := app.boundViews["recent"]
		return ok && v.rec != nil
	}, 2*time.Second, time.Millisecond)

	app.graph.setSignalRaw(string(signalSelectionNode), "recent")
	app.frameSig = app.graph.signals()
	release := app.demandBoundNodes()
	defer release()
	assert.Equal(t, NodeID("recent"), app.resolvedTabNode("detail"), "Detail follows the cursor's node")

	app.bindTab("detail", "by_kind")
	release2 := app.demandBoundNodes()
	defer release2()
	assert.Equal(t, NodeID("by_kind"), app.resolvedTabNode("detail"), "an explicit binding wins over the follow")
}

// The clamp is node-aware: a cursor on a bound node clamps against that
// node's view; a cursor whose node vanished retargets home to the active
// node.
func TestClampNodeAware(t *testing.T) {
	app, _ := bindTestApp(t)
	app.bindTab("table", "recent")
	require.Eventually(t, func() bool {
		release := app.demandBoundNodes()
		defer release()
		v, ok := app.boundViews["recent"]
		return ok && v.rec != nil
	}, 2*time.Second, time.Millisecond)

	base := leewayRec(t, []uint64{1, 2, 3})
	defer base.Release()

	// Cursor on the bound node, out of range for its 1-row view → reset to
	// 0, node kept.
	app.graph.setSignalRaw(string(signalSelection), "5")
	app.graph.setSignalRaw(string(signalSelectionNode), "recent")
	app.frameSig = app.graph.signals()
	release := app.demandBoundNodes()
	app.syncSelectionClamp(base)
	release()
	sig := app.graph.signals()
	sel, _ := sig.Get(signalSelection)
	node, _ := sig.Get(signalSelectionNode)
	assert.Equal(t, "0", sel.Raw)
	assert.Equal(t, "recent", node.Raw, "in-view bound node keeps the cursor")

	// Cursor on a node that is not on screen → retarget home.
	app.graph.setSignalRaw(string(signalSelection), "2")
	app.graph.setSignalRaw(string(signalSelectionNode), "ghost")
	app.frameSig = app.graph.signals()
	release = app.demandBoundNodes()
	app.syncSelectionClamp(base)
	release()
	sig = app.graph.signals()
	sel, _ = sig.Get(signalSelection)
	node, _ = sig.Get(signalSelectionNode)
	assert.Equal(t, "0", sel.Raw)
	assert.Equal(t, string(app.activeNodeID()), node.Raw, "vanished node sends the cursor home")

	// In-range cursor on the active node: untouched.
	app.graph.setSignalRawFrom(string(signalSelection), "2", "test")
	app.graph.setSignalRawFrom(string(signalSelectionNode), string(app.activeNodeID()), "test")
	app.frameSig = app.graph.signals()
	release = app.demandBoundNodes()
	app.syncSelectionClamp(base)
	release()
	sel, _ = app.graph.signals().Get(signalSelection)
	assert.Equal(t, "2", sel.Raw, "a valid cursor is never rewritten")
}
