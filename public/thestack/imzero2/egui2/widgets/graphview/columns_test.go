package graphview

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

var nan = float32(math.NaN())

// richSpecs is a declaration that exercises every field of both forms:
// colours, radii, opacity, no-pick, a pin, pulls on one and two axes, aura
// membership, donuts with and without colours and with a track, per-node
// labels, and edges with ids, labels, colours, widths, lengths, strengths,
// opacity, no-pick, a parallel pair and a self-loop.
func richSpecs() (nodes []NodeSpec, edges []EdgeSpec) {
	red := color.RGBA(0xff, 0, 0, 0xff)
	nodes = []NodeSpec{
		{Id: 10, Label: "ten", Color: red, Radius: 8, Auras: []string{"a"}, Donut: Donut{Values: []float32{1, 2}, Colors: color.Colors{0x00ff00ff, 0x0000ffff}, Total: 5}},
		{Id: 20, Label: "twenty", Opacity: 0.5, NoPick: true, Auras: []string{"a", "b"}, LabelAlways: true},
		{Id: 30, Pinned: true, PinX: 40, PinY: 50, Pull: Pull{X: 1, StrengthX: 0.2}},
		{Id: 40, Label: "forty", Donut: Donut{Values: []float32{3, 1, 1}}, Pull: Pull{X: 5, Y: 6, StrengthX: 0.1, StrengthY: 0.3}},
		{Id: 50, Radius: 3, Auras: []string{"b"}},
	}
	edges = []EdgeSpec{
		{From: 10, To: 20, Id: 1, Label: "x", Color: red, Width: 3, Length: 2, Strength: 0.5},
		{From: 10, To: 20, Id: 2, Opacity: 0.3, NoPick: true},
		{From: 20, To: 30},
		{From: 30, To: 30, Label: "loop"},
		{From: 40, To: 50, Strength: 2},
	}
	return
}

// richColumns is richSpecs hand-written in the columnar form, NaN where the
// row form left a numeric field at zero.
func richColumns() (nc NodeColumns, ec EdgeColumns) {
	red := color.RGBA(0xff, 0, 0, 0xff)
	nc = NodeColumns{
		Ids:           []uint64{10, 20, 30, 40, 50},
		Label:         []string{"ten", "twenty", "", "forty", ""},
		Color:         []color.Color{red, {}, {}, {}, {}},
		Radius:        []float32{8, nan, nan, nan, 3},
		Opacity:       []float32{nan, 0.5, nan, nan, nan},
		NoPick:        []bool{false, true, false, false, false},
		LabelAlways:   []bool{false, true, false, false, false},
		PinX:          []float32{nan, nan, 40, nan, nan},
		PinY:          []float32{nan, nan, 50, nan, nan},
		PullX:         []float32{nan, nan, 1, 5, nan},
		PullY:         []float32{nan, nan, nan, 6, nan},
		PullStrengthX: []float32{nan, nan, 0.2, 0.1, nan},
		PullStrengthY: []float32{nan, nan, nan, 0.3, nan},
		AuraOffsets:   []int32{0, 1, 3, 3, 3, 4},
		AuraIds:       []string{"a", "a", "b", "b"},
		DonutOffsets:  []int32{0, 2, 2, 2, 5, 5},
		DonutValues:   []float32{1, 2, 3, 1, 1},
		DonutColors:   color.Colors{0x00ff00ff, 0x0000ffff, 0, 0, 0},
		DonutTotal:    []float32{5, nan, nan, nan, nan},
	}
	ec = EdgeColumns{
		From:     []uint64{10, 10, 20, 30, 40},
		To:       []uint64{20, 20, 30, 30, 50},
		Id:       []uint64{1, 2, 0, 0, 0},
		Label:    []string{"x", "", "", "loop", ""},
		Color:    []color.Color{red, {}, {}, {}, {}},
		Width:    []float32{3, nan, nan, nan, nan},
		Length:   []float32{2, nan, nan, nan, nan},
		Strength: []float32{0.5, nan, nan, nan, 2},
		Opacity:  []float32{nan, 0.3, nan, nan, nan},
		NoPick:   []bool{false, true, false, false, false},
	}
	return
}

// requireSameGraph compares every retained field of two graphs, NaN-aware.
func requireSameGraph(t *testing.T, a, b *graph) {
	t.Helper()
	require.Equal(t, a.ids, b.ids)
	require.Equal(t, a.x, b.x)
	require.Equal(t, a.y, b.y)
	require.Equal(t, a.label, b.label)
	require.Equal(t, a.col, b.col)
	require.Equal(t, len(a.radius), len(b.radius))
	for i := range a.radius {
		require.True(t, sameF32(a.radius[i], b.radius[i]), "radius of slot %d: %v vs %v", i, a.radius[i], b.radius[i])
	}
	require.Equal(t, a.donut, b.donut)
	require.Equal(t, a.opacity, b.opacity)
	require.Equal(t, a.noPick, b.noPick)
	require.Equal(t, a.labelAlways, b.labelAlways)
	require.Equal(t, a.pull, b.pull)
	require.Equal(t, a.anyPull, b.anyPull)
	require.Equal(t, a.pinDecl, b.pinDecl)
	require.Equal(t, a.pinX, b.pinX)
	require.Equal(t, a.pinY, b.pinY)
	require.Equal(t, a.held, b.held)
	require.Equal(t, a.eFrom, b.eFrom)
	require.Equal(t, a.eTo, b.eTo)
	require.Equal(t, a.eId, b.eId)
	require.Equal(t, a.eLabel, b.eLabel)
	require.Equal(t, a.eCol, b.eCol)
	require.Equal(t, len(a.eWidth), len(b.eWidth))
	for i := range a.eWidth {
		require.True(t, sameF32(a.eWidth[i], b.eWidth[i]), "width of edge %d", i)
	}
	require.Equal(t, a.eLen, b.eLen)
	require.Equal(t, a.eStr, b.eStr)
	require.Equal(t, a.eOpacity, b.eOpacity)
	require.Equal(t, a.eNoPick, b.eNoPick)
	require.Equal(t, a.eOrder, b.eOrder)
	require.Equal(t, a.adjStart, b.adjStart)
	require.Equal(t, a.adjList, b.adjList)
	require.Equal(t, a.adjEdge, b.adjEdge)
	require.Equal(t, a.inDeg, b.inDeg)
	require.Equal(t, a.topoHash, b.topoHash)
}

// The same declaration through the row form and the columnar form
// reconciles to identical retained state and paints an identical byte
// stream (ADR-0232 §SD2).
func TestColumnsAndSpecsReconcileAndPaintIdentically(t *testing.T) {
	nodes, edges := richSpecs()
	nc, ec := richColumns()
	require.NoError(t, nc.Validate())
	require.NoError(t, ec.Validate())

	// Each form runs against a fresh runtime, so what the two streams may
	// differ by is the declaration alone.
	opts := Options{Layout: LayoutForceDirected, Auras: AuraParams{Enabled: true, Legend: AuraLegendInside}}
	sum, zero, reset := scenetest.InstallHashing()
	t.Cleanup(reset)
	rows := New(c.NewWidgetIdStack(), "same", opts)
	var rowSum []uint64
	for range 4 {
		zero()
		rows.Render(nodes, edges, 400, 300)
		rowSum = append(rowSum, sum())
		reset()
	}
	sum, zero, reset = scenetest.InstallHashing()
	t.Cleanup(reset)
	cols := New(c.NewWidgetIdStack(), "same", opts)
	var colSum []uint64
	for range 4 {
		zero()
		require.NoError(t, cols.RenderColumns(&nc, &ec, 400, 300))
		colSum = append(colSum, sum())
		reset()
	}
	requireSameGraph(t, &rows.g, &cols.g)
	require.Equal(t, rowSum, colSum, "the two forms paint the same bytes, frame by frame")
	require.Equal(t, []string{"a", "b"}, rows.auraSet.ids)
	require.Equal(t, rows.auraSet.ids, cols.auraSet.ids)
	require.Equal(t, rows.auraSet.start, cols.auraSet.start)
	require.Equal(t, rows.auraSet.list, cols.auraSet.list)
	// The hosted paint takes the columns too.
	require.NoError(t, cols.HostedPaintColumns(&nc, &ec))
}

// NaN is the unset value in every float column and a declared zero is a
// zero (ADR-0232 §SD4).
func TestColumnsNaNIsUnsetAndZeroIsZero(t *testing.T) {
	var g graph
	nc := NodeColumns{
		Ids:           []uint64{1, 2, 3},
		Radius:        []float32{nan, 0, 4},
		Opacity:       []float32{nan, 0, 1.5},
		PinX:          []float32{nan, 7, 8},
		PinY:          []float32{nan, nan, 9},
		PullX:         []float32{3, 3, 3},
		PullStrengthX: []float32{nan, 0, 0.5},
	}
	ec := EdgeColumns{
		From:     []uint64{1, 1, 1, 2},
		To:       []uint64{2, 2, 2, 3},
		Id:       []uint64{1, 2, 3, 0},
		Length:   []float32{nan, 0, 2, -1},
		Strength: []float32{nan, 0, -1, 2},
		Width:    []float32{nan, 0, 1, nan},
		Opacity:  []float32{nan, 0, 0.25, 2},
	}
	created, changed := g.reconcileColumns(&nc, &ec)
	require.Len(t, created, 3)
	require.True(t, changed)
	require.True(t, isNaN32(g.radius[0]), "unset stays unset")
	require.Equal(t, float32(0), g.radius[1], "a declared zero radius is zero")
	require.Equal(t, float32(4), g.radius[2])
	require.Equal(t, float32(5), g.radiusOr(0, 5))
	require.Equal(t, float32(0), g.radiusOr(1, 5))
	require.Equal(t, []float32{1, 0, 1}, g.opacity)
	require.Equal(t, []bool{false, false, true}, g.pinDecl, "a pin needs both coordinates")
	require.Equal(t, [2]float32{8, 9}, [2]float32{g.pinX[2], g.pinY[2]})
	require.Equal(t, Pull{X: 3}, g.pull[0], "a target with no strength pulls nothing")
	require.Equal(t, Pull{X: 3}, g.pull[1], "a zero strength pulls nothing")
	require.Equal(t, Pull{X: 3, StrengthX: 0.5}, g.pull[2])
	require.True(t, g.anyPull)
	require.Equal(t, []float32{1, 1, 2, 1}, g.eLen, "a non-positive length reads as unset")
	require.Equal(t, []float32{1, 0, 0, 2}, g.eStr, "zero pulls nothing, a negative is zero")
	require.True(t, isNaN32(g.eWidth[0]))
	require.Equal(t, float32(0), g.eWidth[1])
	require.Equal(t, []float32{1, 0, 0.25, 1}, g.eOpacity)

	// A strength of zero is an edge that is drawn and does not pull: the
	// attraction pass moves nothing along it.
	var a, b graph
	a.reconcileColumns(&NodeColumns{Ids: []uint64{1, 2}}, &EdgeColumns{From: []uint64{1}, To: []uint64{2}, Strength: []float32{0}})
	b.reconcileColumns(&NodeColumns{Ids: []uint64{1, 2}}, &EdgeColumns{})
	for _, g := range []*graph{&a, &b} {
		g.x[0], g.y[0], g.x[1], g.y[1] = 0, 0, 600, 0
	}
	dxA, dyA := make([]float32, 2), make([]float32, 2)
	dxB, dyB := make([]float32, 2), make([]float32, 2)
	attraction(&a, dxA, dyA, 100, 1e-3, 1)
	attraction(&b, dxB, dyB, 100, 1e-3, 1)
	require.Equal(t, dxB, dxA)
	require.Equal(t, []float32{0, 0}, dxA)

	// A declared zero opacity paints nothing: the fill's alpha is zero.
	v := New(nil, "t", Options{})
	v.style = DefaultStyle()
	require.NoError(t, v.RenderColumns(&NodeColumns{Ids: []uint64{1}, Opacity: []float32{0}}, &EdgeColumns{}, 0, 0))
	v.g.reconcileColumns(&NodeColumns{Ids: []uint64{1}, Opacity: []float32{0}}, &EdgeColumns{})
	require.Equal(t, uint32(0), alphaOf(v.nodeFill(0)))
}

// The slots follow the declaration's order; a reorder, a drop and an
// insertion are topology changes that carry every surviving id's position
// and hold into its new slot (ADR-0232 §SD3).
func TestReorderKeepsPositionsAndHoldsById(t *testing.T) {
	v := New(nil, "t", Options{})
	created, changed := v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}}, []EdgeSpec{{From: 1, To: 2}})
	require.Equal(t, []int32{0, 1, 2}, created)
	require.True(t, changed)
	require.Equal(t, []uint64{1, 2, 3}, v.g.ids)
	for i, id := range v.g.ids {
		v.g.x[i], v.g.y[i] = float32(id)*10, float32(id)*100
	}
	v.PinNode(2, 20, 200)

	// A pure reorder: no slot is created, the topology changed, the state
	// followed the ids.
	created, changed = v.g.reconcile([]NodeSpec{{Id: 3}, {Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2}})
	require.Empty(t, created)
	require.True(t, changed, "a reorder is a topology change")
	require.Equal(t, []uint64{3, 1, 2}, v.g.ids)
	for i, id := range v.g.ids {
		require.Equal(t, int32(i), v.g.slot[id])
		require.Equal(t, [2]float32{float32(id) * 10, float32(id) * 100}, [2]float32{v.g.x[i], v.g.y[i]})
	}
	require.Equal(t, []bool{false, false, true}, v.g.held)
	require.Equal(t, []int32{v.g.slot[1]}, []int32{v.g.eFrom[0]}, "the edges were rebuilt on the new slots")

	// Same order again: nothing changes.
	created, changed = v.g.reconcile([]NodeSpec{{Id: 3}, {Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2}})
	require.Empty(t, created)
	require.False(t, changed)

	// A drop keeps the survivors in order; an insertion creates one slot
	// where the declaration put it.
	created, changed = v.g.reconcile([]NodeSpec{{Id: 3}, {Id: 2}}, nil)
	require.Empty(t, created)
	require.True(t, changed)
	require.Equal(t, []uint64{3, 2}, v.g.ids)
	require.True(t, v.g.held[1])
	require.Equal(t, [2]float32{20, 200}, [2]float32{v.g.x[1], v.g.y[1]})
	created, changed = v.g.reconcile([]NodeSpec{{Id: 3}, {Id: 4}, {Id: 2}}, nil)
	require.Equal(t, []int32{1}, created)
	require.True(t, changed)
	require.Equal(t, []uint64{3, 4, 2}, v.g.ids)
	require.Equal(t, [2]float32{30, 300}, [2]float32{v.g.x[0], v.g.y[0]})
	require.Equal(t, [2]float32{20, 200}, [2]float32{v.g.x[2], v.g.y[2]})
	_, has1 := v.g.slot[1]
	require.False(t, has1)

	// A duplicate id folds into its first row; the later row's attributes
	// win, and the slot is created once.
	created, changed = v.g.reconcile([]NodeSpec{{Id: 5, Label: "first"}, {Id: 2}, {Id: 5, Label: "second"}}, nil)
	require.Equal(t, []int32{0}, created)
	require.True(t, changed)
	require.Equal(t, []uint64{5, 2}, v.g.ids)
	require.Equal(t, "second", v.g.label[0])
	require.Equal(t, [2]float32{20, 200}, [2]float32{v.g.x[1], v.g.y[1]})
}

func TestPositionColumnsFollowTheDeclaration(t *testing.T) {
	v := New(nil, "t", Options{})
	v.g.reconcile([]NodeSpec{{Id: 7}, {Id: 3}, {Id: 5}}, nil)
	for i := range v.g.ids {
		v.g.x[i], v.g.y[i] = float32(i), float32(i)*2
	}
	ids, xs, ys := v.PositionColumns(nil, nil, nil)
	require.Nil(t, ids, "no ids asked for")
	require.Equal(t, []float32{0, 1, 2}, xs)
	require.Equal(t, []float32{0, 2, 4}, ys)
	ids, xs, _ = v.PositionColumns([]uint64{}, []float32{9}, nil)
	require.Equal(t, []uint64{7, 3, 5}, ids)
	require.Equal(t, []float32{9, 0, 1, 2}, xs, "appended to the caller's slice")
}

func TestRenderColumnsRefusesAMalformedDeclaration(t *testing.T) {
	t.Cleanup(scenetest.Install())
	v := New(c.NewWidgetIdStack(), "bad", Options{})
	require.NoError(t, v.RenderColumns(&NodeColumns{Ids: []uint64{1, 2}}, &EdgeColumns{}, 100, 100))
	require.Equal(t, uint32(2), v.Metrics().NodeCount)

	short := NodeColumns{Ids: []uint64{1, 2, 3}, Radius: []float32{1}}
	require.Error(t, short.Validate())
	require.Error(t, v.RenderColumns(&short, &EdgeColumns{}, 100, 100))
	require.Equal(t, uint32(2), v.Metrics().NodeCount, "a refused declaration leaves the state alone")

	offsets := NodeColumns{Ids: []uint64{1, 2}, AuraOffsets: []int32{0, 2, 1}, AuraIds: []string{"a", "b"}}
	require.Error(t, offsets.Validate(), "offsets must be monotone")
	offsets.AuraOffsets = []int32{0, 1, 1}
	require.Error(t, offsets.Validate(), "offsets must span the values")
	offsets.AuraOffsets = []int32{0, 1, 2}
	require.NoError(t, offsets.Validate())

	colours := NodeColumns{Ids: []uint64{1}, DonutOffsets: []int32{0, 2}, DonutValues: []float32{1, 2}, DonutColors: color.Colors{1}}
	require.Error(t, colours.Validate(), "colours pair with values")

	edges := EdgeColumns{From: []uint64{1}, To: []uint64{1, 2}}
	require.Error(t, edges.Validate())
	require.Error(t, v.RenderColumns(&NodeColumns{Ids: []uint64{1, 2}}, &edges, 100, 100))
	require.Error(t, v.HostedPaintColumns(&short, &EdgeColumns{}))
}

// A declared LabelAlways paints the node's label under the label budget:
// one more paint message than the same node without it.
func TestLabelAlwaysPaintsUnderTheBudget(t *testing.T) {
	messages, zero, reset := scenetest.InstallCounting()
	t.Cleanup(reset)
	render := func(always bool) int {
		v := New(c.NewWidgetIdStack(), "label", Options{Layout: LayoutRandom})
		v.Render([]NodeSpec{{Id: 1, Label: "one", LabelAlways: always}, {Id: 2, Label: "two"}}, nil, 400, 300)
		reset()
		zero()
		v.Render([]NodeSpec{{Id: 1, Label: "one", LabelAlways: always}, {Id: 2, Label: "two"}}, nil, 400, 300)
		reset()
		return messages()
	}
	require.Equal(t, render(false)+1, render(true))
}

// An undirected picture paints no arrow heads — fewer messages for the same
// declaration — and picks its edges as before.
func TestSceneUndirectedPaintsNoHeadAndStillPicks(t *testing.T) {
	messages, zero, reset := scenetest.InstallCounting()
	t.Cleanup(reset)
	nodes := []NodeSpec{{Id: 1, Pinned: true, PinX: 0, PinY: 0}, {Id: 2, Pinned: true, PinX: 200, PinY: 0}, {Id: 3, Pinned: true, PinX: 100, PinY: 100}}
	edges := []EdgeSpec{{From: 1, To: 2}, {From: 2, To: 3}, {From: 3, To: 3}}
	count := func(undirected bool) int {
		v := New(c.NewWidgetIdStack(), "heads", Options{Layout: LayoutRandom, Undirected: undirected})
		v.Render(nodes, edges, 400, 300)
		reset()
		zero()
		v.Render(nodes, edges, 400, 300)
		reset()
		return messages()
	}
	require.Equal(t, count(false)-3, count(true), "one polygon per head is gone")

	s := newScene(t, "heads-pick", Options{Layout: LayoutRandom, Undirected: true, EdgeClicking: true}, 400, 300)
	s.frame(nodes, edges)
	x1, y1, _ := s.v.NodeCanvasPosition(1)
	x2, y2, _ := s.v.NodeCanvasPosition(2)
	s.pointerAt((x1+x2)/2, (y1+y2)/2, c.ContainsPointerResponseFlags)
	s.frame(nodes, edges)
	ref, ok := s.v.HoveredEdge()
	require.True(t, ok)
	require.Equal(t, EdgeRef{From: 1, To: 2}, ref, "the ref keeps the declared direction")
}

// After a removal the batches paint in declaration order, not with the
// last slot moved into the hole (ADR-0232 §SD3).
func TestScenePaintOrderFollowsTheDeclarationAfterARemoval(t *testing.T) {
	s := newScene(t, "order", Options{Layout: LayoutRandom}, 400, 300)
	col := func(i uint8) color.Color { return color.RGBA(i, 0, 0, 0xff) }
	nodes := []NodeSpec{{Id: 1, Color: col(1)}, {Id: 2, Color: col(2)}, {Id: 3, Color: col(3)}, {Id: 4, Color: col(4)}}
	s.frame(nodes, nil)
	s.frame([]NodeSpec{nodes[0], nodes[2], nodes[3]}, nil)
	var order []uint64
	for _, b := range s.v.batches {
		for _, slot := range b.slots {
			order = append(order, s.v.g.ids[slot])
		}
	}
	require.Equal(t, []uint64{1, 3, 4}, order)
}
