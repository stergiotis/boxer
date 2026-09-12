package graphview

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRadialRingsByHopAndSectorsByLeaves(t *testing.T) {
	// 1 at the centre; 2 and 3 on ring one; 2 has three leaves (4, 5, 6),
	// 3 has one (7), so 2's sector is three quarters of the turn.
	var g graph
	g.reconcile(
		[]NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}, {Id: 4}, {Id: 5}, {Id: 6}, {Id: 7}},
		[]EdgeSpec{{From: 1, To: 2}, {From: 1, To: 3}, {From: 2, To: 4}, {From: 2, To: 5}, {From: 2, To: 6}, {From: 3, To: 7}},
	)
	p := RadialParams{Centers: []uint64{1}, RingDist: 10}
	layoutRadial(&g, p.withDefaults())
	// The centre is at the system's origin, which sits at x = maxRing·dist.
	s1 := g.slot[1]
	require.InDelta(t, 20, g.x[s1], 1e-4)
	require.InDelta(t, 0, g.y[s1], 1e-4)
	cx := float64(g.x[s1])
	ring := func(id uint64) float64 {
		s := g.slot[id]
		return math.Hypot(float64(g.x[s])-cx, float64(g.y[s]))
	}
	for _, id := range []uint64{2, 3} {
		require.InDelta(t, 10, ring(id), 1e-3, "ring one")
	}
	for _, id := range []uint64{4, 5, 6, 7} {
		require.InDelta(t, 20, ring(id), 1e-3, "ring two")
	}
	angle := func(id uint64) float64 { // in [0, 2π)
		s := g.slot[id]
		a := math.Atan2(float64(g.y[s]), float64(g.x[s])-cx)
		if a < 0 {
			a += 2 * math.Pi
		}
		return a
	}
	// Node 2's sector is [0, 3π/2), so it sits at 3π/4; node 3's is the
	// remaining quarter, centred at 7π/4 (−π/4).
	require.InDelta(t, 3*math.Pi/4, angle(2), 1e-3)
	require.InDelta(t, 7*math.Pi/4, angle(3), 1e-3)
	// The leaves under 2 split its sector in thirds and stay inside it.
	for _, id := range []uint64{4, 5, 6} {
		a := angle(id)
		require.Greater(t, a, 0.0)
		require.Less(t, a, 3*math.Pi/2)
	}
	require.InDelta(t, 7*math.Pi/4, angle(7), 1e-3, "an only child sits under its parent")
}

func TestRadialSeveralCentresSitOnRingOne(t *testing.T) {
	var g graph
	g.reconcile(
		[]NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}, {Id: 4}},
		[]EdgeSpec{{From: 1, To: 3}, {From: 2, To: 4}, {From: 3, To: 4}},
	)
	p := RadialParams{Centers: []uint64{1, 2, 99}, RingDist: 10}
	layoutRadial(&g, p.withDefaults())
	s1, s2 := g.slot[1], g.slot[2]
	cx := (float64(g.x[s1]) + float64(g.x[s2])) / 2
	require.InDelta(t, 20, cx, 1e-3, "the system origin sits at max ring · dist")
	for _, id := range []uint64{1, 2} {
		s := g.slot[id]
		require.InDelta(t, 10, math.Hypot(float64(g.x[s])-cx, float64(g.y[s])), 1e-3, "centres on ring one")
	}
	for _, id := range []uint64{3, 4} {
		s := g.slot[id]
		require.InDelta(t, 20, math.Hypot(float64(g.x[s])-cx, float64(g.y[s])), 1e-3)
	}
	// Two centres of equal weight sit opposite each other.
	require.InDelta(t, -float64(g.x[s1])+2*cx, float64(g.x[s2]), 1e-3)
	require.InDelta(t, -float64(g.y[s1]), float64(g.y[s2]), 1e-3)
}

func TestRadialUnreachedComponentsPackBeside(t *testing.T) {
	var g graph
	g.reconcile(
		[]NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}, {Id: 4}, {Id: 5}},
		[]EdgeSpec{{From: 1, To: 2}, {From: 3, To: 4}, {From: 3, To: 5}},
	)
	p := RadialParams{RingDist: 10}
	layoutRadial(&g, p.withDefaults())
	// No centre given: node 3 has the highest degree and heads the first
	// system; the {1, 2} component follows to the right around node 1.
	s3, s1 := g.slot[3], g.slot[1]
	require.InDelta(t, 10, g.x[s3], 1e-3)
	require.InDelta(t, 0, g.y[s3], 1e-3)
	require.Greater(t, g.x[s1], g.x[s3]+10, "the second system starts past the first's extent")
	require.InDelta(t, 0, g.y[s1], 1e-3, "its centre is on the axis")
	require.InDelta(t, 10, math.Hypot(float64(g.x[g.slot[2]]-g.x[s1]), float64(g.y[g.slot[2]])), 1e-3)

	// Declaration order does not change the picture.
	var h graph
	h.reconcile(
		[]NodeSpec{{Id: 5}, {Id: 4}, {Id: 3}, {Id: 2}, {Id: 1}},
		[]EdgeSpec{{From: 3, To: 5}, {From: 3, To: 4}, {From: 1, To: 2}},
	)
	layoutRadial(&h, p.withDefaults())
	for _, id := range []uint64{1, 2, 3, 4, 5} {
		require.InDelta(t, g.x[g.slot[id]], h.x[h.slot[id]], 1e-4)
		require.InDelta(t, g.y[g.slot[id]], h.y[h.slot[id]], 1e-4)
	}
}

func TestRadialParamsEqualAndClone(t *testing.T) {
	a := RadialParams{Centers: []uint64{1, 2}, RingDist: 5}
	b := a.clone()
	require.True(t, a.equal(b))
	b.Centers[0] = 9
	require.False(t, a.equal(b), "the clone does not alias")
	require.Equal(t, float32(60), RadialParams{}.withDefaults().RingDist)
	require.False(t, LayoutRadial.IsAnimated())
}
