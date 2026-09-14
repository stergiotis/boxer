package play

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// The fixes of the 2026-09-14 review of ADR-0231 SD2–SD5's landing.

// A rebuild caused by a signal this panel wrote keeps the camera; one caused
// by anything else does not (ADR-0231 §SD8).
func TestOwnSignalRuleTellsItsOwnRebuildsApart(t *testing.T) {
	prev := netServed{edges: laneServed{sql: "SELECT 1", params: map[string]string{"gv_focus": "a", "lim": "1"}}}
	own := netServed{edges: laneServed{sql: "SELECT 1", params: map[string]string{"gv_focus": "b", "lim": "1"}}}
	assert.True(t, own.divergedOnlyOn(prev, graphviewOwnSignal), "only gv_focus moved")

	other := netServed{edges: laneServed{sql: "SELECT 1", params: map[string]string{"gv_focus": "a", "lim": "2"}}}
	assert.False(t, other.divergedOnlyOn(prev, graphviewOwnSignal), "a human parameter moved")

	edited := netServed{edges: laneServed{sql: "SELECT 2", params: map[string]string{"gv_focus": "a", "lim": "1"}}}
	assert.False(t, edited.divergedOnlyOn(prev, graphviewOwnSignal), "the SQL changed")

	same := netServed{edges: laneServed{sql: "SELECT 1", params: map[string]string{"gv_focus": "a", "lim": "1"}}}
	assert.False(t, same.divergedOnlyOn(prev, graphviewOwnSignal), "nothing moved, so nothing was caused")

	// A signal appearing or vanishing counts as a move of that signal.
	grown := netServed{edges: laneServed{sql: "SELECT 1", params: map[string]string{"gv_focus": "a", "lim": "1", "gv_hover": "x"}}}
	assert.True(t, grown.divergedOnlyOn(prev, graphviewOwnSignal))

	assert.True(t, graphviewOwnSignal("gv_pin_x"))
	assert.False(t, graphviewOwnSignal("selection_key"), "written by other panes too")
	assert.False(t, graphviewOwnSignal("vp_min_x"))
}

// A flag column is a Bool or an 8-bit integer; a wider numeric that shares the
// name is a collision, not a flag.
func TestFlagClaimIsNarrow(t *testing.T) {
	assert.True(t, isBooleanType(arrow.FixedWidthTypes.Boolean))
	assert.True(t, isBooleanType(arrow.PrimitiveTypes.Uint8))
	assert.True(t, isBooleanType(arrow.PrimitiveTypes.Int8))
	assert.False(t, isBooleanType(arrow.PrimitiveTypes.Float64))
	assert.False(t, isBooleanType(arrow.PrimitiveTypes.Int64))
	assert.False(t, isBooleanType(arrow.BinaryTypes.String))
}

// gvDriverScene gives a driver's view slots by rendering it once against the
// headless scene, so the setters that need slots — SelectNode, SetNodePosition
// — can be exercised.
func gvDriverScene(t *testing.T, d *GraphviewDriver) {
	t.Helper()
	require.NoError(t, d.nodes.Validate())
	// The simulation is held: these tests read positions the panel set, not
	// ones the force step moved.
	d.view.Opts.Force.Paused = true
	require.NoError(t, d.view.RenderColumns(&d.nodes, &d.edges, 400, 300))
}

// The seeded channels follow the seed: a `size_by = 'distance'` is derived
// again when the selection moves, without a rebuild.
func TestSeededChannelFollowsTheSelection(t *testing.T) {
	t.Cleanup(scenetest.Install())
	m := gvModel(t, []string{"a", "b", "c"}, []string{"b", "c", "d"})
	d := NewGraphviewDriver(bindings.NewWidgetIdStack(), nil)
	d.sizeBy, d.sizeBySet = "distance", true
	d.rebuild(&m)
	d.lastModel = &m
	for _, r := range d.nodes.Radius {
		assert.True(t, math.IsNaN(float64(r)), "nothing selected: no distance, the style default")
	}
	assert.Contains(t, d.sizeReason, "select or hover")

	gvDriverScene(t, d)
	a := m.Key[netRowOf(t, &m, "a")]
	require.True(t, d.view.SelectNode(a))
	d.syncSeeds()
	assert.Empty(t, d.sizeReason)
	ra := d.nodes.Radius[netRowOf(t, &m, "a")]
	rd := d.nodes.Radius[netRowOf(t, &m, "d")]
	require.False(t, math.IsNaN(float64(ra)))
	require.False(t, math.IsNaN(float64(rd)))
	assert.Less(t, ra, rd, "the farther from the selection, the larger under `distance`")

	// The same seed again re-derives nothing; a different one does.
	before := d.lastSeedKey
	d.syncSeeds()
	assert.Equal(t, before, d.lastSeedKey)
	d.view.DeselectNode(a)
	require.True(t, d.view.SelectNode(m.Key[netRowOf(t, &m, "d")]))
	d.syncSeeds()
	assert.NotEqual(t, before, d.lastSeedKey)
	assert.Greater(t, d.nodes.Radius[netRowOf(t, &m, "a")], d.nodes.Radius[netRowOf(t, &m, "d")])
}

// A declared selection is applied silently, withdrawn on the next
// declaration, and never published as a gesture (§SD8).
func TestDeclaredSelectionIsNeitherPublishedNorAccumulated(t *testing.T) {
	t.Cleanup(scenetest.Install())
	d := NewGraphviewDriver(bindings.NewWidgetIdStack(), nil)
	sel := func(ids ...string) netModel {
		vr := seamVerts(t, []string{"a", "b"}, map[string]any{
			networkSelectedCol: []*bool{boolp(contains(ids, "a")), boolp(contains(ids, "b"))},
		})
		return seamModel(t, vr)
	}
	m1 := sel("a")
	d.rebuild(&m1)
	d.lastModel = &m1
	gvDriverScene(t, d)
	d.applyDeclaredState(&m1)
	a, b := m1.Key[netRowOf(t, &m1, "a")], m1.Key[netRowOf(t, &m1, "b")]
	assert.True(t, d.view.IsNodeSelected(a))

	em := &recordingEmitter{}
	d.publishGestures(em)
	assert.Empty(t, em.arrays["gv_selection"], "a declared selection is not a gesture")
	assert.Equal(t, "", d.selectedID)

	// The reader selects b by hand: that one is published.
	require.True(t, d.view.SelectNode(b))
	em = &recordingEmitter{}
	d.publishGestures(em)
	assert.Equal(t, []string{"b"}, em.arrays["gv_selection"])

	// The next declaration moves `selected` to b: a is withdrawn, b stays
	// (declared now, and the reader's before), and nothing accumulates.
	m2 := sel("b")
	d.rebuild(&m2)
	d.lastModel = &m2
	gvDriverScene(t, d)
	d.applyDeclaredState(&m2)
	assert.False(t, d.view.IsNodeSelected(a), "the previous declaration's selection is withdrawn")
	assert.True(t, d.view.IsNodeSelected(b))
	em = &recordingEmitter{}
	d.publishGestures(em)
	assert.Empty(t, em.arrays["gv_selection"], "b is declared now, so it is not published either")
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// recordingEmitter keeps what a panel published, by name.
type recordingEmitter struct {
	scalars map[SignalID]string
	arrays  map[SignalID][]string
}

func (inst *recordingEmitter) Emit(id SignalID, value any) {
	switch v := value.(type) {
	case []string:
		if inst.arrays == nil {
			inst.arrays = make(map[SignalID][]string)
		}
		inst.arrays[id] = v
	default:
		if inst.scalars == nil {
			inst.scalars = make(map[SignalID]string)
		}
		if raw, ok := encodeSignalValue(value); ok {
			inst.scalars[id] = raw
		}
	}
}

// A start position is spent on a node that was not there before, not on a
// survivor the reader may have moved.
func TestStartPositionIsSpentOnceOnANewNode(t *testing.T) {
	t.Cleanup(scenetest.Install())
	d := NewGraphviewDriver(bindings.NewWidgetIdStack(), nil)
	starts := func(ids []string, xs []float64) netModel {
		x := make([]*float64, len(xs))
		y := make([]*float64, len(xs))
		for i := range xs {
			x[i], y[i] = f64p(xs[i]), f64p(xs[i]*2)
		}
		vr := seamVerts(t, ids, map[string]any{networkStartXCol: x, networkStartYCol: y})
		er := netEdges(t, []string{ids[0]}, []string{ids[len(ids)-1]}, nil)
		t.Cleanup(er.Release)
		ec, _ := resolveNetworkEdges(er.Schema())
		vc, _ := resolveNetworkVertices(vr.Schema())
		return buildNetModel(er, ec, vr, vc, netCaps{vertices: 100, edges: 100})
	}
	m1 := starts([]string{"a", "b"}, []float64{10, 20})
	d.prevKeys = keySet(d.nodes.Ids, d.prevKeys)
	d.rebuild(&m1)
	d.lastModel = &m1
	gvDriverScene(t, d)
	d.applyDeclaredState(&m1)
	a := m1.Key[netRowOf(t, &m1, "a")]
	x, _, ok := d.view.NodePosition(a)
	require.True(t, ok)
	assert.Equal(t, float32(10), x, "a new node takes its start")

	// The reader moves a; the next declaration (a survivor, plus a newcomer
	// c) leaves a where the reader put it and seats c at its start.
	d.view.SetNodePosition(a, 99, 99)
	m2 := starts([]string{"a", "c"}, []float64{10, 30})
	d.prevKeys = keySet(d.nodes.Ids, d.prevKeys)
	d.rebuild(&m2)
	d.lastModel = &m2
	gvDriverScene(t, d)
	d.applyDeclaredState(&m2)
	x, _, _ = d.view.NodePosition(a)
	assert.Equal(t, float32(99), x, "a survivor keeps the reader's position")
	cx, _, _ := d.view.NodePosition(m2.Key[netRowOf(t, &m2, "c")])
	assert.Equal(t, float32(30), cx)
}

// The tone, opacity and aura selectors are spent, over a metric and over a
// column of the query's own (§SD6).
func TestToneOpacityAndAuraSelectorsAreSpent(t *testing.T) {
	// Two components: a–b and c–d, plus a numeric `score` and a label `kind`.
	pool := memory.NewGoAllocator()
	ids := []string{"a", "b", "c", "d"}
	idb := array.NewStringBuilder(pool)
	kb := array.NewStringBuilder(pool)
	sb := array.NewFloat64Builder(pool)
	for i, id := range ids {
		idb.Append(id)
		kb.Append([]string{"x", "x", "y", "y"}[i])
		sb.Append(float64(i + 1))
	}
	vr := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{
		{Name: networkIDCol, Type: arrow.BinaryTypes.String},
		{Name: "kind", Type: arrow.BinaryTypes.String},
		{Name: "score", Type: arrow.PrimitiveTypes.Float64},
	}, nil), []arrow.Array{idb.NewArray(), kb.NewArray(), sb.NewArray()}, 4)
	t.Cleanup(vr.Release)
	er := netEdges(t, []string{"a", "c"}, []string{"b", "d"}, nil)
	t.Cleanup(er.Release)
	ec, _ := resolveNetworkEdges(er.Schema())
	vc, _ := resolveNetworkVertices(vr.Schema())

	d := NewGraphviewDriver(nil, nil)
	d.opts = graphOpts{ToneBy: "component", OpacityBy: "score", AuraBy: "kind"}
	m := buildNetModelWith(er, ec, vr, vc, netCaps{vertices: 100, edges: 100}, d.selectorColumns())
	require.Contains(t, m.Extra, "score")
	require.Contains(t, m.Extra, "kind")
	d.rebuild(&m)
	d.lastModel = &m
	assert.Empty(t, d.chanReasons, "%v", d.chanReasons)

	row := func(id string) int { return netRowOf(t, &m, id) }
	// tone_by = component: a and b share a colour, c and d another.
	assert.Equal(t, d.nodes.Color[row("a")], d.nodes.Color[row("b")])
	assert.Equal(t, d.nodes.Color[row("c")], d.nodes.Color[row("d")])
	assert.NotEqual(t, d.nodes.Color[row("a")], d.nodes.Color[row("c")])
	// opacity_by = score: a ramp from the floor to 1.
	assert.InDelta(t, graphviewOpacityFloor+(1-graphviewOpacityFloor)*0.25, d.nodes.Opacity[row("a")], 1e-6)
	assert.InDelta(t, 1, d.nodes.Opacity[row("d")], 1e-6)
	// aura_by = kind: two auras, membership by label.
	assert.Equal(t, []string{"x", "y"}, d.groups)
	require.NotNil(t, d.nodes.AuraOffsets)
	auraOf := func(id string) []string {
		r := row(id)
		return d.nodes.AuraIds[d.nodes.AuraOffsets[r]:d.nodes.AuraOffsets[r+1]]
	}
	assert.Equal(t, []string{"x"}, auraOf("a"))
	assert.Equal(t, []string{"y"}, auraOf("d"))
	assert.True(t, d.aurasDefault(), "an aura_by is the query asking for auras")

	// A label where a quantity is needed is refused, by name, and the
	// channel keeps its declared value.
	d.opts = graphOpts{OpacityBy: "kind"}
	m2 := buildNetModelWith(er, ec, vr, vc, netCaps{vertices: 100, edges: 100}, d.selectorColumns())
	d.rebuild(&m2)
	require.Len(t, d.chanReasons, 1)
	assert.Contains(t, d.chanReasons[0], "opacity_by = kind")
	assert.True(t, math.IsNaN(float64(d.nodes.Opacity[0])))
}

// A settings change that only moves the widget's options does not re-key the
// model; one that changes a channel does.
func TestDeclKeyIgnoresPerFrameOptions(t *testing.T) {
	d := NewGraphviewDriver(nil, nil)
	d.opts = graphOpts{Layout: graphviewLayoutRadial, LayoutSet: true, HideEdges: true, KScale: 2}
	k1 := d.declKey()
	d.opts = graphOpts{Layout: graphviewLayoutTree, LayoutSet: true, HideEdges: false, KScale: 3}
	assert.Equal(t, k1, d.declKey(), "layout, hide_edges and k_scale are per-frame options")
	d.opts = graphOpts{ToneBy: "degree"}
	assert.NotEqual(t, k1, d.declKey())
	d.opts = graphOpts{Undirected: true, UndirectedSet: true}
	assert.NotEqual(t, k1, d.declKey())
}

// The layered panel fades a declared `opacity` and highlights a declared
// `selected` (§SD10).
func TestLayeredPanelHonoursOpacityAndSelected(t *testing.T) {
	vr := seamVerts(t, []string{"a", "b"}, map[string]any{
		networkOpacityCol:  []*float64{f64p(0.5), nil},
		networkSelectedCol: []*bool{nil, boolp(true)},
	})
	m := seamModel(t, vr)
	b := layeredBuild(&m)
	assert.InDelta(t, 0.5, b.opacityOf["a"], 1e-6)
	_, has := b.opacityOf["b"]
	assert.False(t, has)
	_, declared := b.declaredSel["b"]
	assert.True(t, declared)
}

// The model's projection is the host's: a pin projected here lands where
// portolan's EPSG3857 puts the same coordinate at the reference zoom, which is
// what lets the located graph draw inside the map without a second
// projection (ADR-0231 §SD3 as revised).
func TestModelProjectionMatchesTheHost(t *testing.T) {
	for _, ll := range []portolan.LatLng{portolan.LL(47.37, 8.54), portolan.LL(-33.9, 151.2), portolan.LL(64.1, -21.9)} {
		x, y := netProjectWebMercator(ll.Lat, ll.Lng)
		p := portolan.EPSG3857.LatLngToPoint(ll, netWebMercatorZoom)
		assert.InDelta(t, p.X, x, 1e-6, "x at %v", ll)
		assert.InDelta(t, p.Y, y, 1e-6, "y at %v", ll)
	}
}
