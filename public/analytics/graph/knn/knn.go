package knn

import (
	"context"
	"math"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// MetricE selects the distance between rows.
type MetricE uint8

const (
	// MetricEuclidean is the L2 distance.
	MetricEuclidean MetricE = iota
	// MetricCosine is 1 − cos(x, y); a zero-norm row is at distance 1 from
	// everything.
	MetricCosine
)

// Options configures [Build]. Zero values take the defaults noted.
type Options struct {
	// K is the number of neighbours per row, the row itself excluded. The
	// umap-learn n_neighbors, which counts the row, is K+1; the membership
	// sum targets log₂(K+1) so a given n_neighbors keeps its meaning. It is
	// clamped to the row count minus one. Default 15.
	K      int
	Metric MetricE
	// LocalConnectivity is the number of neighbours assumed connected at
	// full strength, umap-learn's local_connectivity; it may be fractional.
	// Default 1.
	LocalConnectivity float32
	// SetOpMixRatio blends the fuzzy union (1) and intersection (0) when the
	// directed graph is symmetrised. Default 1.
	SetOpMixRatio float32
	// MaxRows is the row budget; above it the input is cut to a uniform
	// subsample that keeps the first and last row, and the result says so
	// (ADR-0229 §SD4). Zero means no budget.
	MaxRows int
}

func (o Options) withDefaults() Options {
	if o.K <= 0 {
		o.K = 15
	}
	if o.LocalConnectivity <= 0 {
		o.LocalConnectivity = 1
	}
	if o.SetOpMixRatio <= 0 {
		o.SetOpMixRatio = 1
	}
	return o
}

// Result is the neighbour graph and the per-row values that produced it.
// Every per-slot column is aligned with Graph's slots, which are in
// ascending id order; Rows maps a slot to the input row it came from.
type Result struct {
	// Graph is undirected and weighted by fuzzy membership in (0, 1].
	Graph *csr.Graph
	// Rows is the input row index at each slot. With no truncation it is a
	// permutation of the row indices; with truncation it names the kept rows.
	Rows []int32
	// K is the neighbour count actually used, after clamping.
	K int
	// Sigmas and Rhos are UMAP's per-row bandwidth and offset.
	Sigmas, Rhos []float32
	// CoreDist is the distance to the K-th neighbour, the input HDBSCAN's
	// core distance is read from (ADR-0230 §SD3).
	CoreDist []float32
	// Indices and Dists are the raw neighbour lists, K per slot, nearest
	// first, as slot indices and distances; row i occupies [i*K, (i+1)*K).
	Indices []int32
	Dists   []float32
	// Truncation names the budget that cut the input, if one did.
	Truncation algo.Truncation

	// pair list in slot terms, each undirected edge once, for DistanceGraph.
	pairSrc, pairDst []uint64
	pairDist         []float32
}

// DistanceGraph builds the same topology weighted by distance rather than
// membership: the input of the density-based algorithms.
func (r *Result) DistanceGraph() (*csr.Graph, error) {
	return csr.BuildE(r.pairSrc, r.pairDst, r.pairDist, csr.Options{})
}

// smoothKTolerance and minKDistScale are umap-learn's SMOOTH_K_TOLERANCE
// and MIN_K_DIST_SCALE.
const (
	smoothKTolerance = 1e-5
	minKDistScale    = 1e-3
	smoothKIters     = 64
)

// Build produces the neighbour graph of the n×d row-major matrix x, whose
// rows carry ids. Ids must be unique. e at nil means every core.
//
// The exact k-NN is brute force: each row against every other, O(n²·d),
// which is the interactive budget at the row counts the consumers cap at
// (ADR-0230 §SD1); an approximate index is deferred there. A cancelled
// context returns its error rather than a partial graph, since a partial
// neighbour graph is not a result.
func Build(ctx context.Context, e *engine.Engine, x []float32, d int, ids []uint64, opts Options) (r Result, err error) {
	opts = opts.withDefaults()
	if e == nil {
		e = engine.New(0)
	}
	if d <= 0 {
		err = eb.Build().Int("d", d).Errorf("knn: dimension must be positive")
		return
	}
	if len(x)%d != 0 {
		err = eb.Build().Int("len", len(x)).Int("d", d).Errorf("knn: matrix length is not a multiple of the dimension")
		return
	}
	n := len(x) / d
	if len(ids) != n {
		err = eb.Build().Int("rows", n).Int("ids", len(ids)).Errorf("knn: one id per row required")
		return
	}
	for i, v := range x {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			err = eb.Build().Int("row", i/d).Int("col", i%d).Errorf("knn: non-finite value")
			return
		}
	}

	// Row budget: a uniform stride keeping the first and last row.
	rows := make([]int32, 0, n)
	if opts.MaxRows > 0 && n > opts.MaxRows {
		m := opts.MaxRows
		if m < 2 {
			m = 2
		}
		for i := range m {
			rows = append(rows, int32(int64(i)*int64(n-1)/int64(m-1)))
		}
		r.Truncation = algo.Truncation{Truncated: true, By: algo.LimitRows}
	} else {
		for i := range n {
			rows = append(rows, int32(i))
		}
	}
	n = len(rows)
	if n < 2 {
		err = eb.Build().Int("rows", n).Errorf("knn: at least two rows required")
		return
	}
	k := min(opts.K, n-1)
	r.K = k

	// Gather the kept rows contiguously so the inner loop indexes one array.
	xs := make([]float32, n*d)
	kept := make([]uint64, n)
	for i, row := range rows {
		copy(xs[i*d:(i+1)*d], x[int(row)*d:(int(row)+1)*d])
		kept[i] = ids[row]
	}
	var norms []float32
	if opts.Metric == MetricCosine {
		norms = make([]float32, n)
		for i := range n {
			var s float64
			for _, v := range xs[i*d : (i+1)*d] {
				s += float64(v) * float64(v)
			}
			norms[i] = float32(math.Sqrt(s))
		}
	}

	// Exact k-NN in row order: per worker a distance buffer and a bounded
	// heap; each row's list is sorted by (distance, index) so ties are a
	// function of the input.
	idxRow := make([]int32, n*k)
	distRow := make([]float32, n*k)
	workers := e.Workers()
	scratch := make([][]float32, workers)
	heaps := make([]knnHeap, workers)
	var cancelled atomic.Bool
	e.ParallelFor(n, func(w, lo, hi int) {
		if cap(scratch[w]) < n {
			scratch[w] = make([]float32, n)
		}
		buf := scratch[w][:n]
		h := &heaps[w]
		for i := lo; i < hi; i++ {
			if i%1024 == 0 && ctxDone(ctx) {
				cancelled.Store(true)
				return
			}
			rowDistances(xs, d, i, opts.Metric, norms, buf)
			h.selectK(buf, i, k)
			h.sortedInto(idxRow[i*k:(i+1)*k], distRow[i*k:(i+1)*k])
		}
	})
	if cancelled.Load() {
		err = eh.Errorf("knn: %w", ctx.Err())
		return
	}

	// Smooth-kNN bandwidths and offsets, umap-learn's smooth_knn_dist with
	// the row's own entry (distance 0) counted in the means as the reference
	// does. The global mean is a fixed-order reduction.
	sigmas := make([]float32, n)
	rhos := make([]float32, n)
	target := math.Log2(float64(k + 1))
	var meanAll float64
	{
		var sum float64
		for _, v := range distRow {
			sum += float64(v)
		}
		meanAll = sum / float64(n*(k+1))
	}
	e.ParallelFor(n, func(_, lo, hi int) {
		for i := lo; i < hi; i++ {
			s, rho := smoothKNN(distRow[i*k:(i+1)*k], target, float64(opts.LocalConnectivity), meanAll)
			sigmas[i] = float32(s)
			rhos[i] = float32(rho)
		}
	})

	// Membership strengths, then the fuzzy union, each undirected pair once.
	// A pair (i, j) is emitted from the side whose index is smaller when
	// both directions exist, and from the only side otherwise.
	mix := float64(opts.SetOpMixRatio)
	src := make([]uint64, 0, n*k)
	dst := make([]uint64, 0, n*k)
	wgt := make([]float32, 0, n*k)
	pdist := make([]float32, 0, n*k)
	member := func(i int, j int32, dist float32) float64 {
		dd := float64(dist) - float64(rhos[i])
		if dd <= 0 || sigmas[i] == 0 {
			return 1
		}
		return math.Exp(-dd / float64(sigmas[i]))
	}
	for i := range n {
		li := idxRow[i*k : (i+1)*k]
		di := distRow[i*k : (i+1)*k]
		for a, j := range li {
			wij := member(i, j, di[a])
			// Does j list i?
			var wji float64
			back := false
			lj := idxRow[int(j)*k : (int(j)+1)*k]
			dj := distRow[int(j)*k : (int(j)+1)*k]
			for b, jj := range lj {
				if int(jj) == i {
					wji = member(int(j), int32(i), dj[b])
					back = true
					break
				}
			}
			if back && int(j) < i {
				continue // emitted from j's side
			}
			prod := wij * wji
			w := float32(mix*(wij+wji-prod) + (1-mix)*prod)
			if w <= 0 {
				continue // underflowed: no arc, as the reference eliminates zeros
			}
			src = append(src, kept[i])
			dst = append(dst, kept[j])
			wgt = append(wgt, w)
			pdist = append(pdist, di[a])
		}
	}
	g, err := csr.BuildE(src, dst, wgt, csr.Options{})
	if err != nil {
		err = eh.Errorf("knn: build graph: %w", err)
		return
	}
	if g.NumVertices() != n {
		err = eb.Build().Int("rows", n).Int("vertices", g.NumVertices()).Errorf("knn: ids are not unique")
		return
	}

	// Permute the row-ordered columns into slot order.
	slotOf := make([]int32, n)
	for i := range n {
		s, ok := g.Slot(kept[i])
		if !ok {
			err = eb.Build().Uint64("id", kept[i]).Errorf("knn: id lost in build")
			return
		}
		slotOf[i] = s
	}
	r.Graph = g
	r.Rows = make([]int32, n)
	r.Sigmas = make([]float32, n)
	r.Rhos = make([]float32, n)
	r.CoreDist = make([]float32, n)
	r.Indices = make([]int32, n*k)
	r.Dists = make([]float32, n*k)
	for i := range n {
		s := int(slotOf[i])
		r.Rows[s] = rows[i]
		r.Sigmas[s] = sigmas[i]
		r.Rhos[s] = rhos[i]
		r.CoreDist[s] = distRow[i*k+k-1]
		for a := range k {
			r.Indices[s*k+a] = slotOf[idxRow[i*k+a]]
			r.Dists[s*k+a] = distRow[i*k+a]
		}
	}
	r.pairSrc, r.pairDst, r.pairDist = src, dst, pdist
	return
}

// rowDistances fills buf[j] with the distance from row i to every row j;
// buf[i] is left at 0 and skipped by the caller.
func rowDistances(xs []float32, d, i int, metric MetricE, norms []float32, buf []float32) {
	n := len(buf)
	xi := xs[i*d : (i+1)*d]
	switch metric {
	case MetricCosine:
		ni := norms[i]
		for j := range n {
			if ni == 0 || norms[j] == 0 {
				buf[j] = 1
				continue
			}
			xj := xs[j*d : (j+1)*d]
			var dot float32
			for c := range xi {
				dot += xi[c] * xj[c]
			}
			v := 1 - dot/(ni*norms[j])
			if v < 0 {
				v = 0
			}
			buf[j] = v
		}
	default:
		for j := range n {
			xj := xs[j*d : (j+1)*d]
			var s float32
			for c := range xi {
				t := xi[c] - xj[c]
				s += t * t
			}
			buf[j] = float32(math.Sqrt(float64(s)))
		}
	}
	buf[i] = 0
}

// knnHeap is a bounded max-heap over (distance, index) that keeps the k
// smallest; ties order by index so the selection is a function of the input.
type knnHeap struct {
	idx  []int32
	dist []float32
}

func (h *knnHeap) less(a, b int) bool {
	// "less" for a max-heap: a should be above b when a is larger.
	if h.dist[a] != h.dist[b] {
		return h.dist[a] > h.dist[b]
	}
	return h.idx[a] > h.idx[b]
}

func (h *knnHeap) swap(a, b int) {
	h.idx[a], h.idx[b] = h.idx[b], h.idx[a]
	h.dist[a], h.dist[b] = h.dist[b], h.dist[a]
}

func (h *knnHeap) up(c int) {
	for c > 0 {
		p := (c - 1) / 2
		if !h.less(c, p) {
			return
		}
		h.swap(c, p)
		c = p
	}
}

func (h *knnHeap) down(c, n int) {
	for {
		l := 2*c + 1
		if l >= n {
			return
		}
		m := l
		if rgt := l + 1; rgt < n && h.less(rgt, l) {
			m = rgt
		}
		if !h.less(m, c) {
			return
		}
		h.swap(c, m)
		c = m
	}
}

// selectK keeps the k rows nearest to i, i itself excluded.
func (h *knnHeap) selectK(buf []float32, i, k int) {
	h.idx = h.idx[:0]
	h.dist = h.dist[:0]
	for j := range buf {
		if j == i {
			continue
		}
		dj := buf[j]
		if len(h.idx) < k {
			h.idx = append(h.idx, int32(j))
			h.dist = append(h.dist, dj)
			h.up(len(h.idx) - 1)
			continue
		}
		// Replace the current worst when j is nearer, or equal and lower.
		if dj > h.dist[0] || (dj == h.dist[0] && int32(j) > h.idx[0]) {
			continue
		}
		h.idx[0], h.dist[0] = int32(j), dj
		h.down(0, len(h.idx))
	}
}

// sortedInto writes the heap's contents nearest first, by (distance, index).
func (h *knnHeap) sortedInto(idx []int32, dist []float32) {
	n := len(h.idx)
	for m := n; m > 0; m-- {
		// The root is the current worst: it goes to the back.
		idx[m-1], dist[m-1] = h.idx[0], h.dist[0]
		h.swap(0, m-1)
		h.down(0, m-1)
	}
}

// smoothKNN is umap-learn's smooth_knn_dist for one row. dists are the k
// neighbour distances nearest first, without the row's own zero; the mean
// used for the bandwidth floor counts that zero as the reference does.
func smoothKNN(dists []float32, target, localConnectivity, meanAll float64) (sigma, rho float64) {
	k := len(dists)
	// ρ from the non-zero distances, interpolated for fractional connectivity.
	nz := 0
	for _, v := range dists {
		if v > 0 {
			nz++
		}
	}
	if float64(nz) >= localConnectivity {
		index := int(math.Floor(localConnectivity))
		interp := localConnectivity - float64(index)
		nth := func(m int) float64 { // m-th non-zero distance, 0-based
			c := 0
			for _, v := range dists {
				if v > 0 {
					if c == m {
						return float64(v)
					}
					c++
				}
			}
			return 0
		}
		if index > 0 {
			rho = nth(index - 1)
			if interp > smoothKTolerance {
				rho += interp * (nth(index) - nth(index-1))
			}
		} else {
			rho = interp * nth(0)
		}
	} else if nz > 0 {
		for _, v := range dists {
			if v > 0 {
				rho = max(rho, float64(v))
			}
		}
	}

	lo, hi, mid := 0.0, math.Inf(1), 1.0
	for range smoothKIters {
		var psum float64
		for _, v := range dists {
			dd := float64(v) - rho
			if dd > 0 {
				psum += math.Exp(-dd / mid)
			} else {
				psum += 1
			}
		}
		if math.Abs(psum-target) < smoothKTolerance {
			break
		}
		if psum > target {
			hi = mid
			mid = (lo + hi) / 2
		} else {
			lo = mid
			if math.IsInf(hi, 1) {
				mid *= 2
			} else {
				mid = (lo + hi) / 2
			}
		}
	}
	sigma = mid
	if rho > 0 {
		var sum float64
		for _, v := range dists {
			sum += float64(v)
		}
		meanRow := sum / float64(k+1)
		if sigma < minKDistScale*meanRow {
			sigma = minKDistScale * meanRow
		}
	} else if sigma < minKDistScale*meanAll {
		sigma = minKDistScale * meanAll
	}
	return
}

func ctxDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
