package graphview

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// scatter places n bodies deterministically: most in a 1000×1000 square,
// a tenth of them clustered in a small corner, so the tree has both deep and
// shallow regions.
func scatter(n int) (x, y []float32) {
	x = make([]float32, n)
	y = make([]float32, n)
	for i := range x {
		h := mix64(uint64(i) + 1)
		x[i] = unit01(h) * 1000
		y[i] = unit01(mix64(h)) * 1000
		if i%10 == 0 {
			x[i] = x[i]/50 + 20
			y[i] = y[i]/50 + 20
		}
	}
	return
}

func TestBarnesHutApproximatesTheExactSum(t *testing.T) {
	const n = 2000
	x, y := scatter(n)
	ex, ey := make([]float32, n), make([]float32, n)
	repulsionRows(x, y, ex, ey, 100, 1e-6, 0, n)
	var q quadtree
	q.build(x, y)
	for _, tc := range []struct {
		theta  float32
		maxErr float64
	}{{0.5, 0.02}, {defaultTheta, 0.06}} {
		bx, by := make([]float32, n), make([]float32, n)
		q.repulsionBH(x, y, bx, by, 100, 1e-6, tc.theta*tc.theta, 0, n, nil)
		var errSum, refSum float64
		for i := range x {
			errSum += math.Hypot(float64(bx[i]-ex[i]), float64(by[i]-ey[i]))
			refSum += math.Hypot(float64(ex[i]), float64(ey[i]))
		}
		rel := errSum / refSum
		require.Less(t, rel, tc.maxErr, "theta %v: mean relative force error %.4f", tc.theta, rel)
	}
}

func TestBarnesHutParallelMatchesSerial(t *testing.T) {
	const n = parallelMinNodes + 101
	x, y := scatter(n)
	var q quadtree
	q.build(x, y)
	s1x, s1y := make([]float32, n), make([]float32, n)
	p1x, p1y := make([]float32, n), make([]float32, n)
	q.repulsionBH(x, y, s1x, s1y, 100, 1e-6, 0.81, 0, n, nil)
	q.repulsionBHParallel(x, y, p1x, p1y, 100, 1e-6, 0.81)
	require.Equal(t, s1x, p1x)
	require.Equal(t, s1y, p1y)
}

func TestBarnesHutCoincidentBodiesTerminate(t *testing.T) {
	const n = 300
	x := make([]float32, n)
	y := make([]float32, n)
	for i := range x {
		x[i], y[i] = 10, 20 // every body on one point
	}
	x[0], y[0] = 500, 500 // one apart, so the root is not degenerate
	var q quadtree
	q.build(x, y)
	dx, dy := make([]float32, n), make([]float32, n)
	q.repulsionBH(x, y, dx, dy, 100, 1e-6, 0.81, 0, n, nil)
	for i := range dx {
		require.False(t, math.IsNaN(float64(dx[i])) || math.IsInf(float64(dx[i]), 0), "body %d", i)
	}
	// The lone body is pushed away from the pile, along the (490, 480) line
	// that joins them.
	require.Greater(t, dx[0], float32(0))
	require.InDelta(t, float64(dx[0])*480/490, float64(dy[0]), 1e-3*math.Abs(float64(dx[0])))
	require.Equal(t, float32(n), q.mass[0], "the root aggregates every body once")
}

func TestForceStepUsesTheTreeAboveTheThreshold(t *testing.T) {
	n := barnesHutMinNodes + 10
	nodes := make([]NodeSpec, n)
	for i := range nodes {
		nodes[i].Id = uint64(i + 1)
	}
	var exact, approx graph
	exact.reconcile(nodes, nil)
	approx.reconcile(nodes, nil)
	placeRandom(&exact, allSlots(n))
	placeRandom(&approx, allSlots(n))
	fe := forceState{lastDisp: nan32}
	fa := forceState{lastDisp: nan32}
	pe := ForceParams{Exact: true}.withDefaults()
	pa := ForceParams{}.withDefaults()
	for range 5 {
		fe.step(&exact, 500, 500, pe, 0)
		fa.step(&approx, 500, 500, pa, 0)
	}
	require.NotEmpty(t, fa.tree.internal, "the approximate run built a tree")
	require.Empty(t, fe.tree.internal, "the exact run did not")
	var drift float64
	for i := range exact.ids {
		drift += math.Hypot(float64(exact.x[i]-approx.x[i]), float64(exact.y[i]-approx.y[i]))
	}
	require.Less(t, drift/float64(n), 1.0, "positions stay within a world unit of the exact run after five steps")
}

func BenchmarkRepulsion(b *testing.B) {
	for _, n := range []int{256, 512, 1000, 5000, 20000} {
		x, y := scatter(n)
		dx, dy := make([]float32, n), make([]float32, n)
		if n <= 5000 {
			b.Run(fmt.Sprintf("exact/%d", n), func(b *testing.B) {
				for b.Loop() {
					repulsionRows(x, y, dx, dy, 100, 1e-6, 0, n)
				}
			})
		}
		b.Run(fmt.Sprintf("bh/%d", n), func(b *testing.B) {
			var q quadtree
			var stack []int32
			for b.Loop() {
				q.build(x, y)
				stack = q.repulsionBH(x, y, dx, dy, 100, 1e-6, 0.81, 0, n, stack)
			}
		})
		b.Run(fmt.Sprintf("bh-parallel/%d", n), func(b *testing.B) {
			var q quadtree
			for b.Loop() {
				q.build(x, y)
				q.repulsionBHParallel(x, y, dx, dy, 100, 1e-6, 0.81)
			}
		})
	}
}
