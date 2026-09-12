package graphview

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// stepN runs the force step n times over the declaration and returns the
// position of id.
func stepN(t *testing.T, v *View, nodes []NodeSpec, edges []EdgeSpec, n int, id uint64) (x, y float32) {
	t.Helper()
	fp := ForceParams{}.withDefaults()
	for range n {
		v.reconcileAndPlace(nodes, edges, 800, 600, fp, HierParams{}.withDefaults(), RadialParams{}.withDefaults())
		v.fs.step(&v.g, 800, 600, fp, 0)
	}
	s, ok := v.g.slot[id]
	require.True(t, ok)
	return v.g.x[s], v.g.y[s]
}

func TestAPullClosesTheGapAtTheRateItsStrengthSays(t *testing.T) {
	// A lone node has nothing else acting on it, so the pull's dynamics are
	// visible on their own: each step moves it strength·Dt·Damping of the way,
	// which for the defaults and strength 0.5 is 0.75% a step. The closed form
	// is what this asserts, because a vaguer "it gets closer" would pass with
	// the term applied at the wrong scale.
	const strength = 0.5
	fp := ForceParams{}.withDefaults()
	v := New(nil, "t", Options{Layout: LayoutForceDirected})
	nodes := []NodeSpec{{Id: 1, Pull: Pull{X: 400, Y: 300, StrengthX: strength, StrengthY: strength}}}
	v.reconcileAndPlace(nodes, nil, 800, 600, fp, HierParams{}.withDefaults(), RadialParams{}.withDefaults())
	d0 := math.Hypot(float64(400-v.g.x[0]), float64(300-v.g.y[0]))
	require.Positive(t, d0, "it starts somewhere else")

	const steps = 200
	x, y := stepN(t, v, nodes, nil, steps, 1)
	d := math.Hypot(float64(400-x), float64(300-y))

	perStep := float64(strength * fp.Dt * fp.Damping)
	want := d0 * math.Pow(1-perStep, steps)
	require.InDelta(t, want, d, want*0.02, "the gap decays geometrically at the declared rate")
	require.Less(t, d, d0, "and toward the target, not away")
}

func TestAPullIsPerAxis(t *testing.T) {
	// Pulling on X alone holds the column and leaves Y to the layout — the
	// posture a level wants (ADR-0224 §SD16).
	v := New(nil, "t", Options{Layout: LayoutForceDirected})
	nodes := []NodeSpec{
		{Id: 1, Pull: Pull{X: 100, StrengthX: 4}},
		{Id: 2, Pull: Pull{X: 100, StrengthX: 4}},
	}
	edges := []EdgeSpec{{From: 1, To: 2}}
	x1, y1 := stepN(t, v, nodes, edges, 2000, 1)
	s2 := v.g.slot[2]
	x2, y2 := v.g.x[s2], v.g.y[s2]

	spreadX := math.Abs(float64(x1-100)) + math.Abs(float64(x2-100))
	spreadY := math.Abs(float64(y1 - y2))
	require.Greater(t, spreadY, 4*spreadX,
		"held near the column on X, and far freer on Y: %.1f vs %.1f", spreadY, spreadX)
	require.Greater(t, spreadY, 20.0, "repulsion does separate them on the free axis")
}

func TestNoPullLeavesTheStepUnchangedToTheBit(t *testing.T) {
	// The pass is skipped whole when nothing declares a pull, so an existing
	// caller's layout is bit-identical.
	nodes := []NodeSpec{{Id: 1}, {Id: 2}, {Id: 3}}
	edges := []EdgeSpec{{From: 1, To: 2}, {From: 2, To: 3}}
	run := func() []float32 {
		v := New(nil, "t", Options{Layout: LayoutForceDirected})
		stepN(t, v, nodes, edges, 50, 1)
		out := make([]float32, 0, 6)
		for i := range v.g.ids {
			out = append(out, v.g.x[i], v.g.y[i])
		}
		require.False(t, v.g.anyPull)
		return out
	}
	require.Equal(t, run(), run())
}

func TestAPinnedNodeIgnoresItsPull(t *testing.T) {
	// The step skips a fixed node, so a pin wins over a pull without either
	// knowing about the other.
	v := New(nil, "t", Options{Layout: LayoutForceDirected})
	nodes := []NodeSpec{
		{Id: 1, Pinned: true, PinX: 10, PinY: 20, Pull: Pull{X: 500, Y: 500, StrengthX: 1, StrengthY: 1}},
	}
	x, y := stepN(t, v, nodes, nil, 100, 1)
	require.Equal(t, [2]float32{10, 20}, [2]float32{x, y})
}

func TestPullSurvivesTheSlotSwapAndIsRecomputedPerFrame(t *testing.T) {
	v := New(nil, "t", Options{Layout: LayoutForceDirected})
	fp := ForceParams{}.withDefaults()
	decl := func(ns []NodeSpec) {
		v.reconcileAndPlace(ns, nil, 800, 600, fp, HierParams{}.withDefaults(), RadialParams{}.withDefaults())
	}
	decl([]NodeSpec{{Id: 1, Pull: Pull{X: 5, StrengthX: 0.2}}, {Id: 2}, {Id: 3, Pull: Pull{X: 9, StrengthY: 0.3}}})
	require.True(t, v.g.anyPull)
	// Dropping id 2 swap-removes it, moving id 3 into its slot.
	decl([]NodeSpec{{Id: 1, Pull: Pull{X: 5, StrengthX: 0.2}}, {Id: 3, Pull: Pull{X: 9, StrengthY: 0.3}}})
	require.Equal(t, Pull{X: 5, StrengthX: 0.2}, v.g.pull[v.g.slot[1]])
	require.Equal(t, Pull{X: 9, StrengthY: 0.3}, v.g.pull[v.g.slot[3]])

	// A frame that declares none clears the flag, so the pass goes away again.
	decl([]NodeSpec{{Id: 1}, {Id: 3}})
	require.False(t, v.g.anyPull)
	require.True(t, v.g.pull[v.g.slot[1]].IsZero())
}

func TestPullEquilibriumIsNearTheTargetNotOnIt(t *testing.T) {
	// A pull is a spring: two connected nodes pulled to the same point rest
	// where the pull balances their repulsion, which is near it and not on
	// it. Pinned is what "exactly at" means.
	v := New(nil, "t", Options{Layout: LayoutForceDirected})
	nodes := []NodeSpec{
		{Id: 1, Pull: Pull{X: 200, Y: 200, StrengthX: 0.4, StrengthY: 0.4}},
		{Id: 2, Pull: Pull{X: 200, Y: 200, StrengthX: 0.4, StrengthY: 0.4}},
	}
	stepN(t, v, nodes, nil, 400, 1)
	s1, s2 := v.g.slot[1], v.g.slot[2]
	d1 := math.Hypot(float64(v.g.x[s1]-200), float64(v.g.y[s1]-200))
	apart := math.Hypot(float64(v.g.x[s1]-v.g.x[s2]), float64(v.g.y[s1]-v.g.y[s2]))
	require.Positive(t, d1, "neither sits exactly on the target")
	require.Positive(t, apart, "because repulsion still holds them apart")
	require.Less(t, d1, float64(apart), "each is nearer the target than to the other")
}
