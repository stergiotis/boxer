package graphview

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// scanNode is the pick the grid replaced: every node, exact test, NoPick
// skipped as pickNode skips it (ADR-0224 §SD14).
func (v *View) scanNode(px, py float32) int32 {
	best, bestD := int32(-1), float32(math.MaxFloat32)
	for i := range v.g.ids {
		if v.g.noPick[i] {
			continue
		}
		sx, sy := v.cam.ToScreen(v.g.x[i], v.g.y[i])
		r := max(v.nodeOuterPx(i), pickMinPx)
		dx, dy := px-sx, py-sy
		d2 := dx*dx + dy*dy
		if d2 <= r*r && d2 < bestD {
			best, bestD = int32(i), d2
		}
	}
	return best
}

// scanEdge is pickEdge without the box reject.
func (v *View) scanEdge(px, py float32) int32 {
	best, bestD := int32(-1), float32(math.MaxFloat32)
	for i := range v.g.eFrom {
		if v.g.eNoPick[i] {
			continue
		}
		geo := v.edgeGeometry(i)
		tol := max(geo.width, pickEdgeTolPx)
		var d float32
		switch geo.kind {
		case edgeKindLoop:
			dist := float32(math.Hypot(float64(px-geo.loopCx), float64(py-geo.loopCy)))
			d = float32(math.Abs(float64(dist - geo.loopR)))
		case edgeKindStraight:
			d = distSegment(geo.x[0], geo.y[0], geo.x[3], geo.y[3], px, py)
		default:
			d = distBezier(geo.x, geo.y, px, py)
		}
		if d <= tol && d < bestD {
			best, bestD = int32(i), d
		}
	}
	return best
}

func TestPickGridMatchesTheScan(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(0, 120).Draw(rt, "n")
		nodes := make([]NodeSpec, n)
		for i := range nodes {
			nodes[i] = NodeSpec{Id: uint64(i + 1), Radius: rapid.Float32Range(0, 30).Draw(rt, "r")}
			if rapid.Bool().Draw(rt, "donut") {
				nodes[i].Donut = Donut{Values: []float32{1, 2}}
			}
		}
		var edges []EdgeSpec
		if n >= 2 {
			m := rapid.IntRange(0, 3*n).Draw(rt, "m")
			for range m {
				edges = append(edges, EdgeSpec{
					From: uint64(rapid.IntRange(1, n).Draw(rt, "from")),
					To:   uint64(rapid.IntRange(1, n).Draw(rt, "to")),
				})
			}
		}
		// Some items opt out of the pointer; the oracle skips them too, so
		// the two must still agree (ADR-0224 §SD14).
		for i := range nodes {
			nodes[i].NoPick = rapid.Bool().Draw(rt, "nodeNoPick")
		}
		for i := range edges {
			edges[i].NoPick = rapid.Bool().Draw(rt, "edgeNoPick")
		}
		v := New(nil, "t", Options{})
		v.g.reconcile(nodes, edges)
		// Positions on a wide or a squashed range, so the grid sees both.
		spanX := rapid.Float32Range(1, 5000).Draw(rt, "spanX")
		spanY := rapid.Float32Range(1, 5000).Draw(rt, "spanY")
		for i := range v.g.ids {
			v.g.x[i] = rapid.Float32Range(-spanX, spanX).Draw(rt, "x")
			v.g.y[i] = rapid.Float32Range(-spanY, spanY).Draw(rt, "y")
		}
		v.g.posVer++
		v.cam.Zoom = rapid.Float32Range(0.02, 20).Draw(rt, "zoom")
		v.cam.PanX = rapid.Float32Range(-500, 500).Draw(rt, "panX")
		v.cam.PanY = rapid.Float32Range(-500, 500).Draw(rt, "panY")
		// Points near a node, or anywhere.
		var px, py float32
		if n > 0 && rapid.Bool().Draw(rt, "near") {
			i := rapid.IntRange(0, n-1).Draw(rt, "i")
			sx, sy := v.cam.ToScreen(v.g.x[i], v.g.y[i])
			px = sx + rapid.Float32Range(-40, 40).Draw(rt, "dx")
			py = sy + rapid.Float32Range(-40, 40).Draw(rt, "dy")
		} else {
			px = rapid.Float32Range(-1000, 1000).Draw(rt, "px")
			py = rapid.Float32Range(-1000, 1000).Draw(rt, "py")
		}
		require.Equal(rt, v.scanNode(px, py), v.pickNode(px, py))
		require.Equal(rt, v.scanEdge(px, py), v.pickEdge(px, py))
	})
}

func TestPickGridIsReusedUntilSomethingMoves(t *testing.T) {
	v := twoNodeView(Options{})
	v.g.posVer++
	require.Equal(t, int32(0), v.pickNode(0, 0))
	ver := v.grid.ver
	require.Equal(t, int32(1), v.pickNode(100, 0))
	require.Equal(t, ver, v.grid.ver, "a second pick reuses the grid")

	v.SetNodePosition(1, 300, 300)
	require.Equal(t, int32(-1), v.pickNode(0, 0), "the moved node is gone from the origin")
	require.Equal(t, int32(0), v.pickNode(300, 300))
	require.NotEqual(t, ver, v.grid.ver, "a position set rebuilt it")

	// A radius change alone widens the reach: node 2 grows to cover (130, 0).
	ver = v.grid.ver
	v.g.reconcile([]NodeSpec{{Id: 1}, {Id: 2, Radius: 40}}, []EdgeSpec{{From: 1, To: 2, Id: 7}})
	require.Equal(t, int32(1), v.pickNode(130, 0))
	require.NotEqual(t, ver, v.grid.ver)

	// A force step and a hierarchical layout bump the version too.
	ver = v.g.posVer
	fs := forceState{lastDisp: nan32}
	fs.step(&v.g, 500, 500, ForceParams{}.withDefaults(), 0)
	require.NotEqual(t, ver, v.g.posVer)
	ver = v.g.posVer
	layoutHierarchical(&v.g, HierParams{}.withDefaults())
	require.NotEqual(t, ver, v.g.posVer)

	// An empty graph picks nothing and builds nothing.
	e := New(nil, "e", Options{})
	require.Equal(t, int32(-1), e.pickNode(0, 0))
}

func TestPickGridBoundsDegenerateShapes(t *testing.T) {
	// A long thin line of nodes would want a cell per unit of length; the
	// cap keeps the cell count proportional to the node count.
	v := New(nil, "t", Options{})
	nodes := make([]NodeSpec, 50)
	for i := range nodes {
		nodes[i] = NodeSpec{Id: uint64(i + 1)}
	}
	v.g.reconcile(nodes, nil)
	for i := range v.g.ids {
		v.g.x[i] = float32(i) * 10000
		v.g.y[i] = 0
	}
	v.g.posVer++
	v.grid.build(&v.g, v.style.NodeRadius)
	require.LessOrEqual(t, int(v.grid.cols)*int(v.grid.rows), pickGridMaxCellsPerNode*50+64)
	sx, sy := v.cam.ToScreen(v.g.x[7], v.g.y[7])
	require.Equal(t, int32(7), v.pickNode(sx, sy))
}

func TestEdgeIndexFindsParallelEdgesByRef(t *testing.T) {
	var g graph
	g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 1, To: 2}, {From: 1, To: 2}, {From: 1, To: 2, Id: 4}, {From: 2, To: 1}})
	require.Equal(t, int32(0), g.findEdge(EdgeRef{From: 1, To: 2}), "the first of two id-less parallels")
	require.Equal(t, int32(2), g.findEdge(EdgeRef{From: 1, To: 2, Id: 4}))
	require.Equal(t, int32(3), g.findEdge(EdgeRef{From: 2, To: 1}))
	require.Equal(t, int32(-1), g.findEdge(EdgeRef{From: 2, To: 1, Id: 1}))
	require.Equal(t, int32(-1), g.findEdge(EdgeRef{From: 1, To: 3}))
	g.reconcile([]NodeSpec{{Id: 1}, {Id: 2}}, []EdgeSpec{{From: 2, To: 1}})
	require.Equal(t, int32(-1), g.findEdge(EdgeRef{From: 1, To: 2}), "the index follows the edges")
	require.Equal(t, int32(0), g.findEdge(EdgeRef{From: 2, To: 1}))
}
