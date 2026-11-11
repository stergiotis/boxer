package changepoint

import (
	"math"
	"slices"
)

// Pelt returns the segmentation minimising Σ segment cost + penalty·changes,
// exactly, by the pruned dynamic programme of Killick, Fearnhead and Eckley,
// "Optimal detection of changepoints with a linear computational cost", JASA
// 107 (2012), doi:10.1080/01621459.2012.737745.
//
// The result is the same one an exhaustive search over all segmentations
// would return; pruning only discards last-change candidates that can never
// again be optimal. The running time is linear when changes keep arriving as
// the series grows and degrades towards quadratic when there are none to
// find or the penalty is too large to admit any — the cases where no
// candidate is ever dominated.
//
// "Exact" describes the optimisation, not the answer. The segmentation is
// optimal for the cost model, and the cost model assumes independent noise
// within a segment: a trend is reported as a staircase, and autocorrelated
// noise as changes that are not there.
func (inst *Series) Pelt(penalty float64) (seg Segmentation, err error) {
	err = checkPenalty(penalty)
	if err != nil {
		return
	}
	seg = inst.pelt(penalty)
	return
}

func (inst *Series) pelt(penalty float64) (seg Segmentation) {
	n, m := inst.n, inst.minSegment
	table := &inst.table
	// best[t] is the optimal penalised cost of the prefix [0, t); last[t] the
	// start of that optimum's final segment.
	best := make([]float64, n+1)
	last := make([]int32, n+1)
	for i := range best {
		best[i] = math.Inf(1)
	}
	best[0] = -penalty
	cands := make([]int32, 1, 64)
	cands[0] = 0
	pruneAt := 0
	for t := m; t <= n; t++ {
		bestCost, bestStart := math.Inf(1), int32(0)
		for _, s := range cands {
			c := best[s] + table.cost(int(s), t) + penalty
			if c < bestCost {
				bestCost, bestStart = c, s
			}
		}
		best[t], last[t] = bestCost, bestStart
		// A candidate s whose cost up to some prefix end u already exceeds the
		// optimum at u cannot be the last change of a longer prefix: splitting
		// a segment never raises these costs, so a final segment starting at u
		// does at least as well. That argument needs the segment from u to be
		// admissible, which with a minimum length holds only for prefixes
		// ending at u+m or later — so the test is applied m−1 steps late, at
		// the u whose first admissible prefix is the next one.
		//
		// Pruning only saves work, so it need not run at every step. Testing
		// costs as much as the step itself, and on a stretch with nothing to
		// prune — a long regime — that would double the running time for no
		// candidates removed; the stride grows with the candidate set.
		u := t - m + 1
		if u >= m && t >= pruneAt {
			pruneAt = t + 1 + len(cands)/16
			kept := 0
			for _, s := range cands {
				if int(s) > u-m || best[s]+table.cost(int(s), u) <= best[u] {
					cands[kept] = s
					kept++
				}
			}
			cands = cands[:kept]
		}
		// The next prefix ends at t+1; its final segment may start no later
		// than t+1−m, and that start must itself close a valid prefix.
		next := t + 1 - m
		if next >= m {
			cands = append(cands, int32(next))
		}
	}
	seg.Penalty = penalty
	for t := int32(n); ; {
		s := last[t]
		if s == 0 {
			break
		}
		seg.Starts = append(seg.Starts, s)
		t = s
	}
	slices.Reverse(seg.Starts)
	seg.Cost = inst.costOf(seg.Starts)
	return
}
