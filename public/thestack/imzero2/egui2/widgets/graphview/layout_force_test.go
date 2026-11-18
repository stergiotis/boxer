package graphview

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// starGraph is a hub (id 0) with m leaves.
func starGraph(m int) ([]NodeSpec, []EdgeSpec) {
	nodes := []NodeSpec{{Id: 0}}
	edges := make([]EdgeSpec, 0, m)
	for i := 1; i <= m; i++ {
		nodes = append(nodes, NodeSpec{Id: uint64(i)})
		edges = append(edges, EdgeSpec{From: 0, To: uint64(i)})
	}
	return nodes, edges
}

// completeGraph is K_m.
func completeGraph(m int) ([]NodeSpec, []EdgeSpec) {
	nodes := make([]NodeSpec, 0, m)
	var edges []EdgeSpec
	for i := range m {
		nodes = append(nodes, NodeSpec{Id: uint64(i)})
		for j := range i {
			edges = append(edges, EdgeSpec{From: uint64(j), To: uint64(i)})
		}
	}
	return nodes, edges
}

func TestAStiffNodeDoesNotSwingAcrossItsEquilibrium(t *testing.T) {
	// Under the default step a hub of 80 edges, or every node of K40, is
	// stiffer than the fixed-step integrator can follow: without swing
	// damping each step overshot the equilibrium by more than it closed, the
	// clamp held the overshoot at MaxStep, and the node jumped between two
	// positions for good — every step MaxStep long, every second step back
	// where it was, the layout never settled. The assertion is the settle
	// itself plus the shape of the last step, because a layout that still
	// moved by MaxStep a step would fail the first and a slow creep would
	// not trip the second.
	fp := ForceParams{}.withDefaults()
	for _, tc := range []struct {
		name   string
		layout LayoutE
		graph  func(int) ([]NodeSpec, []EdgeSpec)
		m      int
	}{
		{"star80", LayoutForceDirected, starGraph, 80},
		{"K40", LayoutForceDirected, completeGraph, 40},
		{"K40 centre gravity", LayoutForceDirectedCG, completeGraph, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nodes, edges := tc.graph(tc.m)
			v := New(nil, "t", Options{Layout: tc.layout})
			cg := float32(0)
			if tc.layout == LayoutForceDirectedCG {
				cg = fp.CenterGravity
			}
			var prevX, prevY []float32
			for range 3000 {
				v.reconcileAndPlace(nodes, edges, 800, 600, fp, HierParams{}.withDefaults(), RadialParams{}.withDefaults())
				prevX = append(prevX[:0], v.g.x...)
				prevY = append(prevY[:0], v.g.y...)
				v.fs.step(&v.g, 800, 600, fp, cg)
			}
			var longest float64
			for i := range v.g.x {
				longest = max(longest, math.Hypot(float64(v.g.x[i]-prevX[i]), float64(v.g.y[i]-prevY[i])))
			}
			require.Less(t, longest, float64(fp.MaxStep)/100, "no node still jumps at the clamp")
			require.True(t, v.fs.settled(fp.Epsilon), "the layout settles; last average step %g", v.fs.lastDisp)
		})
	}
}

func TestSwingDampingLeavesASteadyApproachAtFullGain(t *testing.T) {
	// Two nodes and an edge close on their ideal distance without
	// overshooting, so no step reverses and the gain never leaves 1: the
	// damping is inert where the plain step already converges.
	nodes, edges := starGraph(1)
	v := New(nil, "t", Options{Layout: LayoutForceDirected})
	fp := ForceParams{}.withDefaults()
	for range 500 {
		v.reconcileAndPlace(nodes, edges, 800, 600, fp, HierParams{}.withDefaults(), RadialParams{}.withDefaults())
		v.fs.step(&v.g, 800, 600, fp, 0)
		for i, g := range v.fs.gain {
			require.Equal(t, float32(1), g, "slot %d", i)
		}
	}
}

func TestSwingStateResetsWhenTheSlotsAreRenumbered(t *testing.T) {
	// The gain belongs to a node, and a relayout hands its slot to another;
	// rather than carry it across, the state starts afresh.
	nodes, edges := starGraph(80)
	v := New(nil, "t", Options{Layout: LayoutForceDirected})
	fp := ForceParams{}.withDefaults()
	damped := func() bool {
		for _, g := range v.fs.gain {
			if g < 1 {
				return true
			}
		}
		return false
	}
	for range 200 {
		v.reconcileAndPlace(nodes, edges, 800, 600, fp, HierParams{}.withDefaults(), RadialParams{}.withDefaults())
		v.fs.step(&v.g, 800, 600, fp, 0)
		if damped() {
			break
		}
	}
	require.True(t, damped(), "a node swung and its gain dropped")

	grown := append([]NodeSpec{{Id: 1000}}, nodes...)
	v.reconcileAndPlace(grown, edges, 800, 600, fp, HierParams{}.withDefaults(), RadialParams{}.withDefaults())
	v.fs.prepareSwing(&v.g)
	require.Len(t, v.fs.gain, len(grown))
	for i, g := range v.fs.gain {
		require.Equal(t, float32(1), g, fmt.Sprintf("slot %d", i))
	}
}
