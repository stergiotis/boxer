package graphview

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

func TestReconcileKeepsSurvivingPositionsAndDropsVanished(t *testing.T) {
	var g graph
	created, changed := g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}}, []EdgeSpec{{From: 1, To: 2}})
	require.Len(t, created, 3)
	require.True(t, changed)
	g.x[g.slot[1]], g.y[g.slot[1]] = 10, 20
	g.x[g.slot[3]], g.y[g.slot[3]] = 30, 40

	created, changed = g.reconcile([]NodeSpec{{Id: 1}, {Id: 3}, {Id: 4}}, []EdgeSpec{{From: 3, To: 4}})
	require.Equal(t, 1, len(created), "only id 4 is new")
	require.True(t, changed)
	require.Equal(t, 3, g.n())
	_, has2 := g.slot[2]
	require.False(t, has2)
	require.Equal(t, float32(10), g.x[g.slot[1]])
	require.Equal(t, float32(20), g.y[g.slot[1]])
	require.Equal(t, float32(30), g.x[g.slot[3]])
	require.Equal(t, float32(40), g.y[g.slot[3]])
	// Slot map and ids agree after the swap-remove.
	for id, s := range g.slot {
		require.Equal(t, id, g.ids[s])
	}
	// Same declaration again: nothing new, no topology change.
	created, changed = g.reconcile([]NodeSpec{{Id: 1}, {Id: 3}, {Id: 4}}, []EdgeSpec{{From: 3, To: 4}})
	require.Empty(t, created)
	require.False(t, changed)
}

func TestReconcileBuildsUndirectedAdjacencyAndOrders(t *testing.T) {
	var g graph
	g.reconcile(
		[]NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}},
		[]EdgeSpec{{From: 1, To: 2}, {From: 1, To: 2}, {From: 2, To: 3}, {From: 3, To: 3}, {From: 9, To: 1}},
	)
	require.Equal(t, 4, len(g.eFrom), "the edge to an undeclared node is dropped")
	require.Equal(t, []uint8{0, 1, 0, 0}, g.eOrder)
	s1, s2, s3 := g.slot[1], g.slot[2], g.slot[3]
	require.ElementsMatch(t, []int32{s2, s2}, g.neighbors(s1))
	require.ElementsMatch(t, []int32{s1, s1, s3}, g.neighbors(s2))
	require.ElementsMatch(t, []int32{s2}, g.neighbors(s3), "a self-loop adds no neighbour")
	require.Equal(t, int32(2), g.inDeg[s2])
	require.Equal(t, int32(0), g.inDeg[s1])
}

func TestHierarchicalPlacesRowsAndPacksColumns(t *testing.T) {
	var g graph
	g.reconcile(
		[]NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}, {Id: 4}},
		[]EdgeSpec{{From: 1, To: 2}, {From: 1, To: 3}, {From: 2, To: 4}},
	)
	p := HierParams{RowDist: 10, ColDist: 100}
	layoutHierarchical(&g, p.withDefaults())
	pos := func(id uint64) (float32, float32) { s := g.slot[id]; return g.x[s], g.y[s] }
	x, y := pos(1)
	require.Equal(t, [2]float32{0, 0}, [2]float32{x, y})
	x, y = pos(2)
	require.Equal(t, [2]float32{0, 10}, [2]float32{x, y})
	x, y = pos(4)
	require.Equal(t, [2]float32{0, 20}, [2]float32{x, y})
	x, y = pos(3)
	require.Equal(t, [2]float32{100, 10}, [2]float32{x, y}, "a sibling subtree starts past the previous one's span")

	p.CenterParent = true
	layoutHierarchical(&g, p.withDefaults())
	x, _ = pos(1)
	require.Equal(t, float32(50), x, "a centred parent sits over the middle of its children's span")

	p.Orientation = OrientationLeftRight
	layoutHierarchical(&g, p.withDefaults())
	x, y = pos(4)
	require.Equal(t, [2]float32{20, 0}, [2]float32{x, y}, "left-right swaps the axes")
}

func TestHierarchicalReachesCycles(t *testing.T) {
	var g graph
	g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2}, {From: 2, To: 1}})
	layoutHierarchical(&g, HierParams{}.withDefaults())
	// No root exists; the fallback walk still places both, one level apart.
	require.NotEqual(t, g.y[g.slot[1]], g.y[g.slot[2]])
}

func TestForceStepPullsAnEdgeTogether(t *testing.T) {
	var g graph
	g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2}})
	// Farther apart than the ideal length k = sqrt(500·500/2) ≈ 354, so
	// attraction (d²/k) outweighs repulsion (k²/d).
	g.x[g.slot[1]], g.y[g.slot[1]] = 0, 0
	g.x[g.slot[2]], g.y[g.slot[2]] = 600, 0
	fs := forceState{lastDisp: nan32}
	p := ForceParams{}.withDefaults()
	for range 20 {
		fs.step(&g, 500, 500, p, 0)
	}
	after := g.x[g.slot[2]] - g.x[g.slot[1]]
	require.Less(t, after, float32(600), "attraction shortens the edge")
	require.Greater(t, after, float32(300), "and does not overshoot the ideal length in 20 damped steps")
	require.Equal(t, uint64(20), fs.steps)
	require.False(t, math.IsNaN(float64(fs.lastDisp)))
	require.False(t, fs.settled(p.Epsilon), "still moving after 20 steps")
}

func TestForceStepPushesOverlapApartAndSettles(t *testing.T) {
	var g graph
	g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, nil)
	g.x[g.slot[2]] = 0.5
	fs := forceState{lastDisp: nan32}
	p := ForceParams{}.withDefaults()
	fs.step(&g, 500, 500, p, 0)
	d := g.x[g.slot[2]] - g.x[g.slot[1]]
	require.Greater(t, d, float32(0.5), "repulsion separates the overlapping pair")
	require.InDelta(t, 2*p.MaxStep+0.5, d, 1e-3, "each node moves the clamped maximum on a near-singular step")
	// Two unconnected nodes fly apart; with centre gravity they come to rest.
	for range 2000 {
		fs.step(&g, 500, 500, p, 0.3)
	}
	require.True(t, fs.settled(0.5), "centre gravity balances repulsion")
}

func TestForceStepLeavesFixedNodesAlone(t *testing.T) {
	var g graph
	g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2}})
	g.x[g.slot[2]] = 300
	g.fixed[g.slot[1]] = true
	fs := forceState{lastDisp: nan32}
	fs.step(&g, 500, 500, ForceParams{}.withDefaults(), 0.3)
	require.Equal(t, float32(0), g.x[g.slot[1]])
	require.Equal(t, float32(0), g.y[g.slot[1]])
	require.NotEqual(t, float32(300), g.x[g.slot[2]])
}

func TestRepulsionParallelMatchesSerial(t *testing.T) {
	const n = parallelMinNodes + 37
	x := make([]float32, n)
	y := make([]float32, n)
	for i := range x {
		h := mix64(uint64(i) + 1)
		x[i] = unit01(h) * 1000
		y[i] = unit01(mix64(h)) * 1000
	}
	dx1, dy1 := make([]float32, n), make([]float32, n)
	dx2, dy2 := make([]float32, n), make([]float32, n)
	repulsionRows(x, y, dx1, dy1, 100, 1e-6, 0, n)
	repulsionParallel(x, y, dx2, dy2, 100, 1e-6)
	require.Equal(t, dx1, dx2, "each row is computed whole by one goroutine, so the split changes nothing")
	require.Equal(t, dy1, dy2)
}

func TestRandomPlacementIsAFunctionOfTheId(t *testing.T) {
	var a, b graph
	a.reconcile([]NodeSpec{{Id: 7}, {Id: 8}}, nil)
	b.reconcile([]NodeSpec{{Id: 8}, {Id: 7}}, nil)
	placeRandom(&a, allSlots(2))
	placeRandom(&b, allSlots(2))
	require.Equal(t, a.x[a.slot[7]], b.x[b.slot[7]])
	require.Equal(t, a.y[a.slot[8]], b.y[b.slot[8]])
	require.NotEqual(t, a.x[a.slot[7]], a.x[a.slot[8]])
	for i := range a.ids {
		require.GreaterOrEqual(t, a.x[i], float32(0))
		require.Less(t, a.x[i], float32(spawnSize))
	}
}

func TestCameraFitCentresTheBox(t *testing.T) {
	var cam camera
	cam.fit(100, 100, 300, 200, 800, 400, 0.1)
	sx, sy := cam.toScreen(200, 150)
	require.InDelta(t, 400, sx, 1e-3)
	require.InDelta(t, 200, sy, 1e-3)
	// The tighter axis decides the zoom: 200 wide into 640, 100 tall into 320.
	require.InDelta(t, 3.2, cam.zoom, 1e-5)
}

func TestCameraZoomAroundKeepsTheAnchorFixed(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		cam := camera{
			zoom: rapid.Float32Range(0.05, 20).Draw(t, "zoom"),
			panX: rapid.Float32Range(-1000, 1000).Draw(t, "panX"),
			panY: rapid.Float32Range(-1000, 1000).Draw(t, "panY"),
		}
		ax := rapid.Float32Range(0, 800).Draw(t, "ax")
		ay := rapid.Float32Range(0, 600).Draw(t, "ay")
		wx, wy := cam.toWorld(ax, ay)
		cam.zoomAround(rapid.Float32Range(0.5, 2).Draw(t, "factor"), ax, ay)
		sx, sy := cam.toScreen(wx, wy)
		tol := float64(1e-2 * max(1, math.Abs(float64(ax))+math.Abs(float64(cam.panX))))
		require.InDelta(t, float64(ax), float64(sx), tol)
		require.InDelta(t, float64(ay), float64(sy), tol)
	})
}

func TestDistSegmentAndBezier(t *testing.T) {
	require.InDelta(t, 3, distSegment(0, 0, 10, 0, 5, 3), 1e-6)
	require.InDelta(t, 5, distSegment(0, 0, 10, 0, 15, 0), 1e-6, "past the end measures to the endpoint")
	x := [4]float32{0, 0, 10, 10}
	y := [4]float32{0, 0, 0, 0}
	require.InDelta(t, 2, distBezier(x, y, 5, 2), 1e-3, "a degenerate straight cubic behaves like the segment")
}

func TestForceParamsDefaultsMatchTheBinding(t *testing.T) {
	p := ForceParams{}.withDefaults()
	require.Equal(t, ForceParams{Dt: 0.05, Damping: 0.3, Epsilon: 1e-3, MaxStep: 10, KScale: 1, CAttract: 1, CRepulse: 1, CenterGravity: 0.3, Theta: defaultTheta}, p)
	require.Equal(t, float32(0.02), ForceParams{Dt: 0.02}.withDefaults().Dt, "a set field is kept")
}

func TestZoomAnchorFallsBackFromTheWheelRowToThePointerToTheCentre(t *testing.T) {
	nan := float32(math.NaN())
	ax, ay := zoomAnchor(c.CanvasWheelValue{Zoom: 1.1, HoverX: 10, HoverY: 20}, 300, 400, true, 800, 600)
	require.Equal(t, [2]float32{10, 20}, [2]float32{ax, ay}, "the row's own hover wins")
	ax, ay = zoomAnchor(c.CanvasWheelValue{Zoom: 1.1, HoverX: nan, HoverY: nan}, 300, 400, true, 800, 600)
	require.Equal(t, [2]float32{300, 400}, [2]float32{ax, ay}, "a sense region on top leaves the row's hover NaN; the pointer anchors")
	ax, ay = zoomAnchor(c.CanvasWheelValue{Zoom: 1.1, HoverX: nan, HoverY: nan}, 0, 0, false, 800, 600)
	require.Equal(t, [2]float32{400, 300}, [2]float32{ax, ay}, "no pointer at all anchors on the centre")
}
