package explain

import (
	"cmp"
	"context"
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Cell is one label's reading of one feature (ADR-0235 §SD2).
type Cell struct {
	// AUC is the probability that a member's value exceeds a non-member's,
	// ties counting half: the Mann–Whitney statistic over the two groups.
	// 0.5 is no separation; 1 every member above every non-member; 0 the
	// reverse. NaN when either side is empty.
	AUC float32
	// Q1, Median and Q3 are the members' quartiles; MedianRest is the
	// non-members' median. All in the matrix's units.
	Q1, Median, Q3 float64
	MedianRest     float64
}

// Effect is how far the cell sits from no separation, in [0, 0.5].
func (inst Cell) Effect() float32 {
	if math.IsNaN(float64(inst.AUC)) {
		return 0
	}
	return float32(math.Abs(float64(inst.AUC) - 0.5))
}

// Contrast holds a Cell per label and feature.
type Contrast struct {
	NumLabels   int
	NumFeatures int
	// Cells is indexed [label*NumFeatures + feature].
	Cells []Cell
	// Rows is the member count per label.
	Rows       []int32
	Truncation algo.Truncation
}

// Cell returns the reading of feature under label.
func (inst *Contrast) Cell(label, feature int32) Cell {
	return inst.Cells[int(label)*inst.NumFeatures+int(feature)]
}

// Ranked returns the features under label ordered by effect, the largest
// first, ties by feature index.
func (inst *Contrast) Ranked(label int32) (features []int32) {
	features = make([]int32, inst.NumFeatures)
	for f := range features {
		features[f] = int32(f)
	}
	cells := inst.Cells[int(label)*inst.NumFeatures : (int(label)+1)*inst.NumFeatures]
	slices.SortStableFunc(features, func(a, b int32) int {
		return cmp.Compare(cells[b].Effect(), cells[a].Effect())
	})
	return
}

// Contrasts reads every label against the rest of the rows over the
// row-major matrix x with d columns. Rows with a negative label belong to
// no label and to every label's rest. Each feature is sorted once; ranks
// tie by averaging. A cancelled context stops at a feature boundary, the
// cells not yet computed stay zero, and the Truncation says so.
func Contrasts(ctx context.Context, x []float64, d int, labels []int32) (c *Contrast, err error) {
	n := len(labels)
	if d <= 0 || len(x) != n*d {
		err = eb.Build().Int("rows", n).Int("cols", d).Int("len", len(x)).Errorf("explain: the matrix does not have rows × cols values")
		return
	}
	k := 0
	for _, lb := range labels {
		k = max(k, int(lb)+1)
	}
	if k == 0 {
		err = eb.Build().Int("rows", n).Errorf("explain: no row carries a label")
		return
	}
	c = &Contrast{
		NumLabels:   k,
		NumFeatures: d,
		Cells:       make([]Cell, k*d),
		Rows:        make([]int32, k),
	}
	for _, lb := range labels {
		if lb >= 0 {
			c.Rows[lb]++
		}
	}
	order := make([]int32, n)
	rank := make([]float64, n)
	members := make([]float64, 0, n)
	rest := make([]float64, 0, n)
	rankSum := make([]float64, k)
	for f := range d {
		if ctx.Err() != nil {
			c.Truncation = algo.Truncation{Truncated: true, By: algo.LimitContext}
			return
		}
		col := func(r int32) float64 { return x[int(r)*d+f] }
		for r := range order {
			order[r] = int32(r)
		}
		slices.SortFunc(order, func(a, b int32) int {
			if v := cmp.Compare(col(a), col(b)); v != 0 {
				return v
			}
			return cmp.Compare(a, b)
		})
		// Average ranks over each run of equal values, 1-based.
		for i := 0; i < n; {
			j := i + 1
			for j < n && col(order[j]) == col(order[i]) {
				j++
			}
			avg := float64(i+1+j) / 2
			for p := i; p < j; p++ {
				rank[order[p]] = avg
			}
			i = j
		}
		for lb := range rankSum {
			rankSum[lb] = 0
		}
		for r, lb := range labels {
			if lb >= 0 {
				rankSum[lb] += rank[r]
			}
		}
		for lb := range k {
			m := float64(c.Rows[lb])
			others := float64(n) - m
			cell := &c.Cells[lb*d+f]
			if m == 0 || others == 0 {
				cell.AUC = float32(math.NaN())
			} else {
				u := rankSum[lb] - m*(m+1)/2
				cell.AUC = float32(u / (m * others))
			}
		}
		for lb := range k {
			members, rest = members[:0], rest[:0]
			for _, r := range order {
				if int(labels[r]) == lb {
					members = append(members, col(r))
				} else {
					rest = append(rest, col(r))
				}
			}
			cell := &c.Cells[lb*d+f]
			cell.Q1 = quantile(members, 0.25)
			cell.Median = quantile(members, 0.5)
			cell.Q3 = quantile(members, 0.75)
			cell.MedianRest = quantile(rest, 0.5)
		}
	}
	return
}

// quantile is the q-th quantile of sorted values by linear interpolation
// between the order statistics (type 7); NaN of no values.
func quantile(sorted []float64, q float64) float64 {
	m := len(sorted)
	switch m {
	case 0:
		return math.NaN()
	case 1:
		return sorted[0]
	}
	pos := q * float64(m-1)
	lo := int(math.Floor(pos))
	hi := min(lo+1, m-1)
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[hi]-sorted[lo])
}
