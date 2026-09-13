package graphview

import (
	"context"
	"fmt"
	"math"
	"math/rand/v2"
	"runtime"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stergiotis/boxer/public/analytics/graph/knn"
)

func TestNeighborEmbeddingBHApproximatesTheExactSum(t *testing.T) {
	const n = 2000
	x, y := scatter(n)
	const invK2 = 1.0 / (30 * 30)
	ex, ey, ez := make([]float32, n), make([]float32, n), make([]float32, n)
	repulsionRowsNE(x, y, ex, ey, ez, invK2, 0, n)
	var q quadtree
	q.build(x, y)
	for _, tc := range []struct {
		theta  float32
		maxErr float64
		maxZ   float64
	}{{0.5, 0.03, 0.01}, {defaultTheta, 0.08, 0.04}} {
		bx, by, bz := make([]float32, n), make([]float32, n), make([]float32, n)
		q.repulsionNE(x, y, bx, by, bz, invK2, tc.theta*tc.theta, 0, n, nil)
		var errSum, refSum, zErr, zRef float64
		for i := range x {
			errSum += math.Hypot(float64(bx[i]-ex[i]), float64(by[i]-ey[i]))
			refSum += math.Hypot(float64(ex[i]), float64(ey[i]))
			zErr += math.Abs(float64(bz[i] - ez[i]))
			zRef += float64(ez[i])
		}
		require.Less(t, errSum/refSum, tc.maxErr, "theta %v: mean relative force error %.4f", tc.theta, errSum/refSum)
		require.Less(t, zErr/zRef, tc.maxZ, "theta %v: normaliser error %.4f", tc.theta, zErr/zRef)
	}
}

func TestNeighborEmbeddingParallelMatchesSerial(t *testing.T) {
	const n = parallelMinNodes + 101
	x, y := scatter(n)
	var q quadtree
	q.build(x, y)
	sx, sy, sz := make([]float32, n), make([]float32, n), make([]float32, n)
	px, py, pz := make([]float32, n), make([]float32, n), make([]float32, n)
	q.repulsionNE(x, y, sx, sy, sz, 1e-3, 0.81, 0, n, nil)
	stacks := make([][]int32, 8)
	parallelRows(n, 8, func(w, lo, hi int) {
		stacks[w] = q.repulsionNE(x, y, px, py, pz, 1e-3, 0.81, lo, hi, stacks[w])
	})
	require.Equal(t, sx, px)
	require.Equal(t, sy, py)
	require.Equal(t, sz, pz)
}

func TestExaggerationSchedule(t *testing.T) {
	p := ForceParams{Exaggeration: 1}.withDefaults()
	require.Equal(t, float32(1), exaggerationAt(p, 0))
	require.Equal(t, float32(1), exaggerationAt(p, 1000))
	p = ForceParams{Exaggeration: 4, ExaggerationStart: 32, ExaggerationSteps: 100}.withDefaults()
	require.InDelta(t, 32, exaggerationAt(p, 0), 1e-5)
	require.InDelta(t, math.Sqrt(32*4), exaggerationAt(p, 50), 1e-3, "geometric midpoint")
	require.Equal(t, float32(4), exaggerationAt(p, 100))
	require.Equal(t, float32(4), exaggerationAt(p, 101))
	prev := exaggerationAt(p, 0)
	for s := uint64(1); s <= 100; s++ {
		cur := exaggerationAt(p, s)
		require.LessOrEqual(t, cur, prev)
		prev = cur
	}
}

// blobs draws n points in d dimensions from c well-separated Gaussians and
// returns the matrix and each point's blob.
func blobs(n, d, c int, seed uint64) ([]float32, []int) {
	rng := rand.New(rand.NewPCG(seed, seed+1))
	x := make([]float32, n*d)
	lab := make([]int, n)
	for i := range n {
		b := i % c
		lab[i] = b
		for j := range d {
			v := rng.NormFloat64()
			if j == b%d {
				v += 12
			}
			x[i*d+j] = float32(v)
		}
	}
	return x, lab
}

// helix draws n points along a helix in three dimensions, a one-dimensional
// manifold whose global structure is its arc-length order.
func helix(n int) ([]float32, []float64) {
	x := make([]float32, n*3)
	tt := make([]float64, n)
	for i := range n {
		tp := float64(i) / float64(n) * 6 * math.Pi
		tt[i] = tp
		x[i*3+0] = float32(3 * math.Cos(tp))
		x[i*3+1] = float32(3 * math.Sin(tp))
		x[i*3+2] = float32(tp)
	}
	return x, tt
}

// embed lays out the neighbour graph of x under the model with the given
// exaggeration for steps iterations and returns the positions by row.
func embed(t *testing.T, x []float32, d int, k int, exag float32, steps int) (px, py []float32, r knn.Result) {
	t.Helper()
	n := len(x) / d
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(i + 1)
	}
	var err error
	r, err = knn.Build(context.Background(), engine.New(1), x, d, ids, knn.Options{K: k})
	require.NoError(t, err)
	nodes := make([]NodeSpec, n)
	for i := range nodes {
		nodes[i].Id = ids[i]
	}
	var edges []EdgeSpec
	g := r.Graph
	for s := range g.NumVertices() {
		out := g.Out(int32(s))
		w := g.OutWeights(int32(s))
		for a, dd := range out {
			if dd > int32(s) {
				edges = append(edges, EdgeSpec{From: g.ID(int32(s)), To: g.ID(dd), Strength: w[a]})
			}
		}
	}
	var gr graph
	gr.reconcile(nodes, edges)
	placeRandom(&gr, gr.allSlots())
	fs := forceState{lastDisp: nan32}
	p := ForceParams{Model: ForceModelNeighborEmbedding, Exaggeration: exag}.withDefaults()
	for range steps {
		fs.step(&gr, 800, 800, p, 0)
	}
	px = make([]float32, n)
	py = make([]float32, n)
	for i := range n {
		s := gr.slot[ids[i]]
		px[i], py[i] = gr.x[s], gr.y[s]
	}
	return
}

// recall is the mean fraction of each row's k input neighbours found among
// its k nearest neighbours in the plane.
func recall(px, py []float32, r knn.Result, ids []uint64) float64 {
	n := len(px)
	k := r.K
	var hits float64
	for i := range n {
		type cand struct {
			j int
			d float64
		}
		cs := make([]cand, 0, n-1)
		for j := range n {
			if j != i {
				cs = append(cs, cand{j, math.Hypot(float64(px[i]-px[j]), float64(py[i]-py[j]))})
			}
		}
		sort.Slice(cs, func(a, b int) bool { return cs[a].d < cs[b].d })
		near := map[int]bool{}
		for _, c := range cs[:k] {
			near[c.j] = true
		}
		s, _ := r.Graph.Slot(ids[i])
		for _, ns := range r.Indices[int(s)*k : (int(s)+1)*k] {
			if near[int(r.Graph.ID(ns))-1] {
				hits++
			}
		}
	}
	return hits / float64(n*k)
}

// distanceCorrelation is Pearson's correlation between input-space and
// plane distances over all pairs — the global-structure score.
func distanceCorrelation(x []float32, d int, px, py []float32) float64 {
	n := len(px)
	var a, b []float64
	for i := range n {
		for j := i + 1; j < n; j++ {
			var s float64
			for c := range d {
				t := float64(x[i*d+c] - x[j*d+c])
				s += t * t
			}
			a = append(a, math.Sqrt(s))
			b = append(b, math.Hypot(float64(px[i]-px[j]), float64(py[i]-py[j])))
		}
	}
	mean := func(v []float64) float64 {
		var s float64
		for _, e := range v {
			s += e
		}
		return s / float64(len(v))
	}
	ma, mb := mean(a), mean(b)
	var cov, va, vb float64
	for i := range a {
		cov += (a[i] - ma) * (b[i] - mb)
		va += (a[i] - ma) * (a[i] - ma)
		vb += (b[i] - mb) * (b[i] - mb)
	}
	return cov / math.Sqrt(va*vb)
}

// The trade-off Böhm, Berens & Kobak report: raising the exaggeration
// lowers neighbour recall on clustered data and raises the global distance
// correlation on a continuous manifold.
func TestNeighborEmbeddingExaggerationTradeOff(t *testing.T) {
	const n, d, k, steps = 300, 4, 10, 600
	x, _ := blobs(n, d, 3, 5)
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(i + 1)
	}
	px1, py1, r1 := embed(t, x, d, k, 1, steps)
	px30, py30, r30 := embed(t, x, d, k, 30, steps)
	rec1 := recall(px1, py1, r1, ids)
	rec30 := recall(px30, py30, r30, ids)
	t.Logf("blobs: recall ρ=1 %.3f, ρ=30 %.3f", rec1, rec30)
	require.Greater(t, rec1, rec30, "stronger repulsion keeps more neighbours")
	require.Greater(t, rec1, 0.3)

	hx, _ := helix(n)
	hx1, hy1, _ := embed(t, hx, 3, k, 1, steps)
	hx30, hy30, _ := embed(t, hx, 3, k, 30, steps)
	c1 := distanceCorrelation(hx, 3, hx1, hy1)
	c30 := distanceCorrelation(hx, 3, hx30, hy30)
	t.Logf("helix: distance correlation ρ=1 %.3f, ρ=30 %.3f", c1, c30)
	require.Greater(t, c30, c1, "stronger attraction keeps the manifold's global order")
}

func TestNeighborEmbeddingStepIsDeterministic(t *testing.T) {
	const n, d = 600, 3
	x, _ := helix(n)
	a1, b1, _ := embed(t, x, d, 8, 4, 30)
	a2, b2, _ := embed(t, x, d, 8, 4, 30)
	require.Equal(t, a1, a2)
	require.Equal(t, b1, b2)
}

func BenchmarkRepulsionNE(b *testing.B) {
	for _, n := range []int{1000, 5000, 20000} {
		x, y := scatter(n)
		dx, dy, zi := make([]float32, n), make([]float32, n), make([]float32, n)
		if n <= 5000 {
			b.Run(fmt.Sprintf("exact/%d", n), func(b *testing.B) {
				for b.Loop() {
					repulsionRowsNE(x, y, dx, dy, zi, 1e-3, 0, n)
				}
			})
		}
		b.Run(fmt.Sprintf("bh-parallel/%d", n), func(b *testing.B) {
			var q quadtree
			workers := runtime.GOMAXPROCS(0)
			stacks := make([][]int32, workers)
			for b.Loop() {
				q.build(x, y)
				parallelRows(n, workers, func(w, lo, hi int) {
					stacks[w] = q.repulsionNE(x, y, dx, dy, zi, 1e-3, 0.81, lo, hi, stacks[w])
				})
			}
		})
	}
}

// BenchmarkNeighborEmbeddingStep is one full step — tree, repulsion,
// attraction, integration — on the neighbour graph of ten thousand rows,
// the Projection lane's cap.
func BenchmarkNeighborEmbeddingStep(b *testing.B) {
	const n, d, k = 10_000, 8, 15
	x, _ := blobs(n, d, 5, 3)
	ids := make([]uint64, n)
	for i := range ids {
		ids[i] = uint64(i + 1)
	}
	r, err := knn.Build(context.Background(), nil, x, d, ids, knn.Options{K: k})
	if err != nil {
		b.Fatal(err)
	}
	nodes := make([]NodeSpec, n)
	for i := range nodes {
		nodes[i].Id = ids[i]
	}
	var edges []EdgeSpec
	g := r.Graph
	for s := range g.NumVertices() {
		out := g.Out(int32(s))
		w := g.OutWeights(int32(s))
		for a, dd := range out {
			if dd > int32(s) {
				edges = append(edges, EdgeSpec{From: g.ID(int32(s)), To: g.ID(dd), Strength: w[a]})
			}
		}
	}
	var gr graph
	gr.reconcile(nodes, edges)
	placeRandom(&gr, gr.allSlots())
	fs := forceState{lastDisp: nan32}
	p := ForceParams{Model: ForceModelNeighborEmbedding, Exaggeration: 4}.withDefaults()
	b.ReportAllocs()
	for b.Loop() {
		fs.step(&gr, 1000, 1000, p, 0)
	}
}
