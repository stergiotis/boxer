package algo

import (
	"context"
	"math"

	"github.com/stergiotis/boxer/public/analytics/graph/csr"
	"github.com/stergiotis/boxer/public/analytics/graph/engine"
)

// PageRankOptions tunes [PageRank].
type PageRankOptions struct {
	// Damping is the follow probability; zero means 0.85.
	Damping float64
	// Iterations is the sweep budget; zero means 20.
	Iterations int
	// Tolerance stops early once the L1 change of one sweep falls below it;
	// zero runs the whole budget.
	Tolerance float64
	// Teleport restarts the random surfer at these slots rather than
	// uniformly, and returns dangling mass there too — personalised PageRank
	// (ADR-0232 §SD7). Empty teleports uniformly, which is the classic rank
	// and is computed by the arithmetic it always was, bit for bit.
	//
	// Slots out of range are ignored, and a set with no slot left in range
	// teleports uniformly; a duplicate slot counts once. Seeding every slot
	// is uniform teleportation expressed the long way and gives the same
	// ranks to within the arithmetic's own rounding.
	Teleport []int32
}

// PageRankResult holds the ranks, which sum to one.
type PageRankResult struct {
	Rank []float64
	// Iterations counts the sweeps run.
	Iterations int
	// Delta is the L1 change of the last sweep.
	Delta float64
	// Converged reports that Delta fell below the tolerance.
	Converged bool
	Truncation
}

// PageRank runs the pull form (the GAP reference): each vertex sums the rank
// of its in-neighbours divided by their out-degree, in ascending source
// order, and a dangling vertex's mass is spread uniformly. Every write is to
// the destination's own slot, so the sweep is deterministic under
// parallelism; the dangling and delta sums fold in fixed chunk order.
func PageRank(ctx context.Context, e *engine.Engine, g *csr.Graph, opts PageRankOptions) (r PageRankResult) {
	e = engineOrDefault(e)
	n := g.NumVertices()
	if n == 0 {
		return
	}
	damping := opts.Damping
	if damping == 0 {
		damping = 0.85
	}
	budget := opts.Iterations
	if budget == 0 {
		budget = 20
	}
	// tp is the teleport distribution, nil when it is uniform: the uniform
	// path then keeps the exact expression it had, so a classic rank is
	// unchanged to the bit by this option existing.
	tp := teleportDistribution(opts.Teleport, n)
	rank := make([]float64, n)
	next := make([]float64, n)
	if tp == nil {
		fill(rank, 1/float64(n))
	} else {
		// Start at the teleport distribution: the fixed point does not depend
		// on the start vector, and starting where the mass restarts reaches
		// it in fewer sweeps.
		copy(rank, tp)
	}
	invOut := make([]float64, n)
	for v := range n {
		if d := g.OutDegree(int32(v)); d > 0 {
			invOut[v] = 1 / float64(d)
		}
	}
	base := (1 - damping) / float64(n)
	inOff := g.InOffsets()
	inTgt := g.InTargets()
	for r.Iterations < budget {
		if engine.ContextDone(ctx) {
			r.Truncation = truncatedBy(LimitContext)
			break
		}
		dangling := e.ReduceFloat64(n, func(lo, hi int) float64 {
			var s float64
			for v := lo; v < hi; v++ {
				if invOut[v] == 0 {
					s += rank[v]
				}
			}
			return s
		})
		spread := damping * dangling / float64(n)
		e.ParallelFor(n, func(_, lo, hi int) {
			for d := lo; d < hi; d++ {
				var s float64
				for _, src := range inTgt[inOff[d]:inOff[d+1]] {
					s += rank[src] * invOut[src]
				}
				if tp == nil {
					next[d] = base + spread + damping*s
					continue
				}
				// Seeded: the restart mass and the dangling mass both land on
				// the teleport distribution instead of being spread evenly.
				next[d] = (1-damping)*tp[d] + damping*dangling*tp[d] + damping*s
			}
		})
		r.Delta = e.ReduceFloat64(n, func(lo, hi int) float64 {
			var s float64
			for v := lo; v < hi; v++ {
				s += math.Abs(next[v] - rank[v])
			}
			return s
		})
		rank, next = next, rank
		r.Iterations++
		if opts.Tolerance > 0 && r.Delta < opts.Tolerance {
			r.Converged = true
			break
		}
	}
	if opts.Tolerance > 0 && !r.Converged && !r.Truncated {
		r.Truncation = truncatedBy(LimitIterations)
	}
	r.Rank = rank
	return
}

// teleportDistribution builds the restart distribution over n slots: mass
// 1/|T| on each distinct in-range slot of t, zero elsewhere. It returns nil
// when the distribution is the uniform one — an empty set, or one whose
// slots are all out of range — so the caller keeps the uniform arithmetic
// rather than expressing it as a vector.
func teleportDistribution(t []int32, n int) (tp []float64) {
	if len(t) == 0 || n == 0 {
		return nil
	}
	tp = make([]float64, n)
	var count int
	for _, s := range t {
		if s < 0 || int(s) >= n || tp[s] != 0 {
			continue // out of range, or already counted
		}
		tp[s] = 1
		count++
	}
	if count == 0 {
		return nil
	}
	w := 1 / float64(count)
	for i, v := range tp {
		if v != 0 {
			tp[i] = w
		}
	}
	return tp
}
