package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	sankeyview "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey/view"
)

// ---- Network ----

// networkPane is the build of a → b, b → c, a → c as the Network pane's
// last frame left it.
func networkPane(t *testing.T, p *PlayApp) {
	t.Helper()
	m := gvModel(t, []string{"a", "b", "a"}, []string{"b", "c", "c"})
	b := layeredBuild(&m)
	d := p.networkDriver
	d.model, d.declaredSel, d.capped = b.model, b.declaredSel, b.capped
	d.nodeCount, d.edgeCount = len(b.model.Nodes), len(b.model.Edges)
	drawnPane(p, networkPaneId, 0, nil)
}

func TestGetNetworkReadsTheBuild(t *testing.T) {
	l, h := opsLauncher(t)
	networkPane(t, l.inner)
	r := queryOp[NetworkReading](t, h, opGetNetwork, GetGraphArgs{})
	assert.Zero(t, r.Drawn.ResultId, "a frameless pane names no frame result")
	assert.Equal(t, "top-down", r.RankDir)
	assert.Equal(t, int32(3), r.Nodes)
	assert.Equal(t, int32(3), r.Edges)
	require.Len(t, r.Vertices, 3)
	require.Len(t, r.EdgeList, 3)
	r = queryOp[NetworkReading](t, h, opGetNetwork, GetGraphArgs{Limit: 1, EdgeLimit: 2})
	assert.Len(t, r.Vertices, 1)
	assert.Equal(t, int32(2), r.MoreVertices)
	assert.Equal(t, int32(1), r.MoreEdges)
	require.Error(t, queryErr(t, h, opGetNetwork, GetGraphArgs{Limit: 501}))
}

func TestNetworkSelectAndOptions(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	networkPane(t, p)
	require.NoError(t, applyOp(t, h, opSelectNetworkNode, SelectNetworkNodeArgs{Id: "b"}))
	assert.Equal(t, "b", p.networkDriver.selectedID)
	key, writer := signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "b", key)
	assert.Equal(t, "task:t", writer)
	r := queryOp[NetworkReading](t, h, opGetNetwork, GetGraphArgs{})
	assert.Equal(t, "b", r.Selected)

	require.Error(t, applyOp(t, h, opSelectNetworkNode, SelectNetworkNodeArgs{Id: "zz"}))
	require.Error(t, applyOp(t, h, opSelectNetworkNode, SelectNetworkNodeArgs{}))
	require.Error(t, applyOp(t, h, opSelectNetworkNode, SelectNetworkNodeArgs{Id: "a", Clear: true}))
	assert.Equal(t, "b", p.networkDriver.selectedID, "a refused call applies nothing")
	require.NoError(t, applyOp(t, h, opSelectNetworkNode, SelectNetworkNodeArgs{Clear: true}))
	key, _ = signalOf(t, p, signalSelectionKey)
	assert.Empty(t, key)

	before := h.ResourceValue(opsResNetwork)
	lr, bad := "left-right", "bottom-up"
	require.NoError(t, applyOp(t, h, opSetNetworkOptions, SetNetworkOptionsArgs{RankDir: &lr}))
	assert.Equal(t, layeredgraph.RankDirLeftRight, p.networkDriver.rankDir)
	assert.NotEqual(t, before, h.ResourceValue(opsResNetwork))
	require.Error(t, applyOp(t, h, opSetNetworkOptions, SetNetworkOptionsArgs{RankDir: &bad}))
	require.Error(t, applyOp(t, h, opSetNetworkOptions, SetNetworkOptionsArgs{}))
}

// ---- Graphview ----

// graphviewPane gives the driver the graph a → b, b → c, c → d with slots,
// as a render leaves it.
func graphviewPane(t *testing.T, p *PlayApp) *netModel {
	t.Helper()
	t.Cleanup(scenetest.Install())
	m := gvModel(t, []string{"a", "b", "c"}, []string{"b", "c", "d"})
	d := p.graphviewDriver
	d.rebuild(&m)
	d.lastModel = &m
	gvDriverScene(t, d)
	drawnPane(p, graphviewPaneId, 0, nil)
	return &m
}

func TestGetGraphviewReadsSettingsAndMetrics(t *testing.T) {
	l, h := opsLauncher(t)
	graphviewPane(t, l.inner)
	r := queryOp[GraphviewReading](t, h, opGetGraphview, GetGraphviewArgs{Metrics: []string{"degree", "distance"}})
	assert.Equal(t, int32(4), r.Nodes)
	assert.Equal(t, int32(3), r.Edges)
	assert.Equal(t, GraphviewSetting{Value: "gravity", From: "default"}, r.Layout)
	assert.Equal(t, GraphviewSetting{Value: networkWeightCol, From: "default"}, r.SizeBy)
	assert.Equal(t, []string{"degree"}, r.Metrics)
	require.Len(t, r.MetricNotes, 1)
	assert.Contains(t, r.MetricNotes[0], "nothing is selected")
	require.Len(t, r.Vertices, 4)
	degrees := map[string]float64{}
	for _, v := range r.Vertices {
		require.Len(t, v.Metrics, 1)
		degrees[v.Id] = *v.Metrics[0].Value
	}
	assert.Equal(t, 1.0, degrees["a"])
	assert.Equal(t, 2.0, degrees["b"])

	require.Error(t, queryErr(t, h, opGetGraphview, GetGraphviewArgs{Metrics: []string{"fame"}}))
	require.Error(t, queryErr(t, h, opGetGraphview, GetGraphviewArgs{Metrics: []string{"degree", "pagerank", "kcore", "triangles", "clique"}}))
}

func TestSelectGraphviewNodesWritesTheSelectionSignals(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	graphviewPane(t, p)
	require.NoError(t, applyOp(t, h, opSelectGraphviewNodes, SelectGraphviewNodesArgs{Ids: []string{"c", "a"}}))
	sel, writer := signalOf(t, p, signalGvSelection)
	assert.Equal(t, "task:t", writer)
	assert.Contains(t, sel, "'a'")
	assert.Contains(t, sel, "'c'")
	key, _ := signalOf(t, p, signalSelectionKey)
	assert.Equal(t, p.graphviewDriver.selectedID, key)
	assert.NotEmpty(t, key)

	r := queryOp[GraphviewReading](t, h, opGetGraphview, GetGraphviewArgs{Metrics: []string{"distance"}})
	assert.ElementsMatch(t, []string{"a", "c"}, r.Selected)
	assert.Equal(t, []string{"distance"}, r.Metrics, "a seeded metric measures from the selection")

	// The pane's own publish finds the values in place and leaves them the
	// task's.
	p.graphviewDriver.publishGestures(p.sigEmit.as(graphviewPaneId))
	_, writer = signalOf(t, p, signalGvSelection)
	assert.Equal(t, "task:t", writer)
	_, writer = signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "task:t", writer)

	require.Error(t, applyOp(t, h, opSelectGraphviewNodes, SelectGraphviewNodesArgs{Ids: []string{"zz"}}))
	require.Error(t, applyOp(t, h, opSelectGraphviewNodes, SelectGraphviewNodesArgs{}))
	require.NoError(t, applyOp(t, h, opSelectGraphviewNodes, SelectGraphviewNodesArgs{Clear: true}))
	key, _ = signalOf(t, p, signalSelectionKey)
	assert.Empty(t, key)
}

func TestSetGraphviewOptionsSetsAndHandsBack(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	graphviewPane(t, p)
	d := p.graphviewDriver
	before := h.ResourceValue(opsResGraphview)
	tree, lr, size, on := "tree", "left-right", "pagerank", "on"
	paused := true
	require.NoError(t, applyOp(t, h, opSetGraphviewOptions, SetGraphviewOptionsArgs{Layout: &tree, Orientation: &lr,
		SizeBy: &size, HoldDropped: &on, Paused: &paused, Fit: true}))
	assert.Equal(t, graphviewLayoutTree, d.layout)
	assert.True(t, d.orientSet)
	assert.Equal(t, "pagerank", d.sizeBy)
	assert.False(t, d.keyOk, "a size channel change rebuilds the declaration")
	assert.True(t, d.pinOnDrag && d.pinOnDragSet)
	assert.True(t, d.paused)
	assert.NotEqual(t, before, h.ResourceValue(opsResGraphview))
	r := queryOp[GraphviewReading](t, h, opGetGraphview, GetGraphviewArgs{})
	assert.Equal(t, GraphviewSetting{Value: "tree", From: "pane"}, r.Layout)
	assert.Equal(t, GraphviewSetting{Value: "on", From: "pane"}, r.HoldDropped)

	auto := "auto"
	require.NoError(t, applyOp(t, h, opSetGraphviewOptions, SetGraphviewOptionsArgs{Layout: &auto, SizeBy: &auto}))
	assert.Equal(t, graphviewLayoutAuto, d.layout)
	assert.False(t, d.sizeBySet, "auto hands the channel back to the query")

	bad, basemap := "spiral", "on"
	require.Error(t, applyOp(t, h, opSetGraphviewOptions, SetGraphviewOptionsArgs{Layout: &bad}))
	err := applyOp(t, h, opSetGraphviewOptions, SetGraphviewOptionsArgs{Basemap: &basemap})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "located")
	fame := "fame"
	require.Error(t, applyOp(t, h, opSetGraphviewOptions, SetGraphviewOptionsArgs{SizeBy: &fame, Layout: &tree}))
	assert.Equal(t, graphviewLayoutAuto, d.layout, "a refused call applies nothing")
	require.Error(t, applyOp(t, h, opSetGraphviewOptions, SetGraphviewOptionsArgs{}))
}

// The size-by combo and the action buttons go through
// set_graphview_options; a selection the widget makes is taken back and made
// again through select_graphview_nodes, as the person, with the pane as the
// signals' writer.
func TestGraphviewGesturesGoThroughTheirCommands(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	m := graphviewPane(t, p)
	d := p.graphviewDriver
	d.requestOptions(SetGraphviewOptionsArgs{SizeBy: new("degree")})
	e := lastEntry(t, eng)
	assert.Equal(t, opSetGraphviewOptions, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.Equal(t, "degree", d.sizeBy)

	before := d.selectedKeys()
	require.True(t, d.view.SelectNode(m.Key[netRowOf(t, m, "b")]))
	d.replaySelection(before)
	assert.Empty(t, signalRaw(t, p, signalSelectionKey), "no select event: the widget pruning, not a gesture")
	d.events = graphview.Events{{Kind: graphview.EventKindNodeSelect}}
	d.replaySelection(before)
	e = lastEntry(t, eng)
	assert.Equal(t, opSelectGraphviewNodes, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.True(t, d.view.IsNodeSelected(m.Key[netRowOf(t, m, "b")]))
	key, writer := signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "b", key)
	assert.Equal(t, graphviewPaneId, writer)
}

func signalRaw(t *testing.T, p *PlayApp, name SignalID) string {
	t.Helper()
	raw, _ := signalOf(t, p, name)
	return raw
}

// ---- Sankey ----

// sankeyPane lays out the canonical a → c (3), a → d (1), b → c (2).
func sankeyPane(t *testing.T, p *PlayApp) {
	t.Helper()
	rec, fc := flowsFixture(t)
	t.Cleanup(rec.Release)
	d := p.sankeyDriver
	b := buildSankeyDiagram(rec, fc, nil, noNodesClaim(), d.choice)
	d.stats = b.stats
	d.layout, d.modeUsed, d.modeFallback, d.layoutErr = computeSankeyLayout(b.diagram)
	require.NoError(t, d.layoutErr)
	drawnPane(p, sankeyPaneId, 0, nil)
}

func TestGetSankeyReadsTheConservedTotal(t *testing.T) {
	l, h := opsLauncher(t)
	sankeyPane(t, l.inner)
	r := queryOp[SankeyReading](t, h, opGetSankey, GetSankeyArgs{})
	assert.Equal(t, "auto", r.Mode)
	assert.Equal(t, "sankey", r.ModeUsed)
	assert.Equal(t, int32(4), r.Nodes)
	assert.Equal(t, int32(3), r.Flows)
	assert.Equal(t, int32(2), r.Stages)
	assert.Equal(t, 6.0, *r.Total)
	require.Len(t, r.NodeList, 4)
	assert.Equal(t, int32(0), r.NodeList[0].Stage)
	require.Len(t, r.FlowList, 3)
	assert.Equal(t, "a", r.FlowList[0].Source, "largest first")
	assert.Equal(t, "c", r.FlowList[0].Target)
	assert.InDelta(t, 0.5, *r.FlowList[0].Share, 1e-9)
	r = queryOp[SankeyReading](t, h, opGetSankey, GetSankeyArgs{Limit: 1, FlowLimit: 1})
	assert.Equal(t, int32(3), r.MoreNodes)
	assert.Equal(t, int32(2), r.MoreFlows)
}

func TestSankeySelectAndOptions(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	sankeyPane(t, p)
	require.NoError(t, applyOp(t, h, opSelectSankeyNode, SelectSankeyNodeArgs{Node: "c"}))
	key, writer := signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "c", key)
	assert.Equal(t, "task:t", writer)
	r := queryOp[SankeyReading](t, h, opGetSankey, GetSankeyArgs{})
	require.NotNil(t, r.Pinned)
	assert.Equal(t, "c", r.Pinned.Node)

	require.NoError(t, applyOp(t, h, opSelectSankeyNode, SelectSankeyNodeArgs{Source: "a", Target: "d"}))
	key, _ = signalOf(t, p, signalSelectionKey)
	assert.Empty(t, key, "a ribbon publishes no key")
	assert.Equal(t, sankeyview.HitLink, p.sankeyDriver.selected.Kind)

	require.Error(t, applyOp(t, h, opSelectSankeyNode, SelectSankeyNodeArgs{Source: "d", Target: "a"}))
	require.Error(t, applyOp(t, h, opSelectSankeyNode, SelectSankeyNodeArgs{Node: "zz"}))
	require.Error(t, applyOp(t, h, opSelectSankeyNode, SelectSankeyNodeArgs{Node: "a", Source: "a", Target: "c"}))
	require.Error(t, applyOp(t, h, opSelectSankeyNode, SelectSankeyNodeArgs{}))
	require.NoError(t, applyOp(t, h, opSelectSankeyNode, SelectSankeyNodeArgs{Clear: true}))
	assert.True(t, p.sankeyDriver.selected.None())

	before := h.ResourceValue(opsResSankey)
	alluvial, bad := "alluvial", "chord"
	require.NoError(t, applyOp(t, h, opSetSankeyOptions, SetSankeyOptionsArgs{Mode: &alluvial}))
	assert.Equal(t, sankeyChoiceAlluvial, p.sankeyDriver.choice)
	assert.NotEqual(t, before, h.ResourceValue(opsResSankey))
	require.Error(t, applyOp(t, h, opSetSankeyOptions, SetSankeyOptionsArgs{Mode: &bad}))
	require.Error(t, applyOp(t, h, opSetSankeyOptions, SetSankeyOptionsArgs{}))
}

// A pin survives a re-layout by its ids, and a pinned node that leaves
// the diagram empties selection_key.
func TestSankeyRepinsByIdAcrossALayout(t *testing.T) {
	l, _ := opsLauncher(t)
	p := l.inner
	sankeyPane(t, p)
	d := p.sankeyDriver
	require.NoError(t, p.selectSankeyNode(SelectSankeyNodeArgs{Node: "d"}, sankeyPaneId))
	prev := d.layout
	rec := sankeyTestRec(t,
		sankeyTestCol{name: "source", str: []string{"x", "a"}},
		sankeyTestCol{name: "target", str: []string{"a", "d"}},
		sankeyTestCol{name: "value", num: []float64{5, 5}},
	)
	defer rec.Release()
	fc, reason := resolveSankeyFlows(rec.Schema())
	require.Empty(t, reason)
	b := buildSankeyDiagram(rec, fc, nil, noNodesClaim(), d.choice)
	d.layout, d.modeUsed, d.modeFallback, d.layoutErr = computeSankeyLayout(b.diagram)
	em := &recordingEmitter{}
	d.repin(prev, em)
	assert.Equal(t, "d", d.selectedNodeID(), "kept by id")

	prev = d.layout
	rec3 := sankeyTestRec(t,
		sankeyTestCol{name: "source", str: []string{"x"}},
		sankeyTestCol{name: "target", str: []string{"y"}},
		sankeyTestCol{name: "value", num: []float64{1}},
	)
	defer rec3.Release()
	fc3, _ := resolveSankeyFlows(rec3.Schema())
	b = buildSankeyDiagram(rec3, fc3, nil, noNodesClaim(), d.choice)
	d.layout, d.modeUsed, d.modeFallback, d.layoutErr = computeSankeyLayout(b.diagram)
	em = &recordingEmitter{}
	d.repin(prev, em)
	assert.True(t, d.selected.None())
	v, emitted := em.scalars[signalSelectionKey]
	assert.True(t, emitted)
	assert.Empty(t, v)
}

func TestFramelessClicksGoThroughTheirCommands(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	networkPane(t, p)
	p.networkDriver.requestSelect(SelectNetworkNodeArgs{Id: "c"}, nil)
	e := lastEntry(t, eng)
	assert.Equal(t, opSelectNetworkNode, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	key, writer := signalOf(t, p, signalSelectionKey)
	assert.Equal(t, "c", key)
	assert.Equal(t, networkPaneId, writer)

	sankeyPane(t, p)
	p.sankeyDriver.requestSelect(p.sankeyDriver.selectArgsOf(sankeyview.NodeHit(0)), nil)
	e = lastEntry(t, eng)
	assert.Equal(t, opSelectSankeyNode, e.Op)
	assert.Equal(t, sankeyview.HitNode, p.sankeyDriver.selected.Kind)
}
