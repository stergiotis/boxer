package changepoint

import (
	"iter"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// DefaultPathMaxRuns bounds how many segmentations [Series.PenaltyPath]
// computes when the caller passes zero.
const DefaultPathMaxRuns int32 = 64

// Path is every segmentation that is optimal for some penalty in a searched
// interval, from the most changes to the fewest.
//
// Struct-of-arrays: a reader usually wants one column — the change counts
// against the penalties — and the starts only for the row it settles on.
type Path struct {
	// Found[i] is a penalty at which segmentation i was computed, and so one
	// at which it is optimal. Ascending.
	Found []float64
	// Penalty[i] is the smallest penalty in the searched interval at which
	// segmentation i is optimal; it stays optimal up to Penalty[i+1], or to the
	// interval's upper end for the last one. Ascending. Only meaningful when
	// Complete: the ranges are derived from neighbouring rows, and a missing
	// neighbour makes them too wide.
	Penalty []float64
	// NumChanges[i] is segmentation i's change count. Strictly descending.
	NumChanges []int32
	// Cost[i] is segmentation i's unpenalised cost. Ascending.
	Cost []float64
	// Complete is false when the run budget ended the search early. Each row
	// present is still optimal at its Found penalty, but segmentations are
	// missing between them, so Penalty does not bound where a row holds.
	Complete bool

	offsets []int32
	starts  []int32
}

// Len returns the number of segmentations on the path.
func (inst Path) Len() (count int) {
	return len(inst.Found)
}

// Starts returns segmentation i's change positions, as in
// [Segmentation.Starts]. The slice aliases the path's storage.
func (inst Path) Starts(i int) (starts []int32) {
	starts = inst.starts[inst.offsets[i]:inst.offsets[i+1]]
	return
}

// All iterates the path's segmentations, each carrying the penalty it was
// found at.
func (inst Path) All() iter.Seq2[int, Segmentation] {
	return func(yield func(int, Segmentation) bool) {
		for i := range inst.Found {
			if !yield(i, Segmentation{Starts: inst.Starts(i), Cost: inst.Cost[i], Penalty: inst.Found[i]}) {
				return
			}
		}
	}
}

// PenaltyPath finds the segmentations optimal for the penalties in
// [penaltyLo, penaltyHi], by the CROPS search of Haynes, Eckley and
// Fearnhead, "Computationally efficient changepoint detection for a range of
// penalties", JCGS 26 (2017), doi:10.1080/10618600.2015.1116445.
//
// The number of changes is a step function of the penalty, and the search
// runs [Series.Pelt] only at the penalties where two known steps would meet,
// so it costs about two runs per distinct segmentation rather than a grid.
//
// This is the diagnostic the choice of penalty should be read against: a
// change count that holds over a wide range of penalties is a feature of the
// data, and one that moves with every small change of penalty is a feature of
// the penalty. The path does not pick a row.
//
// maxRuns bounds the Pelt runs; zero accepts [DefaultPathMaxRuns]. Small
// penalties are the expensive end: pruning is least effective there and each
// run approaches quadratic time.
func (inst *Series) PenaltyPath(penaltyLo float64, penaltyHi float64, maxRuns int32) (path Path, err error) {
	err = checkPenalty(penaltyLo)
	if err != nil {
		return
	}
	err = checkPenalty(penaltyHi)
	if err != nil {
		return
	}
	if penaltyHi <= penaltyLo {
		err = eb.Build().Float64("penaltyLo", penaltyLo).Float64("penaltyHi", penaltyHi).
			Errorf("the penalty interval is empty")
		return
	}
	if maxRuns < 0 {
		err = eb.Build().Int32("maxRuns", maxRuns).Errorf("the run limit is negative")
		return
	}
	if maxRuns == 0 {
		maxRuns = DefaultPathMaxRuns
	}
	if maxRuns < 2 {
		maxRuns = 2
	}
	lo, hi := inst.pelt(penaltyLo), inst.pelt(penaltyHi)
	runs := int32(2)
	found := make([]Segmentation, 0, 16)
	found = append(found, lo, hi)
	path.Complete = true
	// Each pending pair brackets a stretch of penalties whose optimal
	// segmentations are not all known yet.
	type bracket struct{ a, b Segmentation }
	todo := make([]bracket, 0, 16)
	todo = append(todo, bracket{lo, hi})
	for len(todo) > 0 {
		br := todo[len(todo)-1]
		todo = todo[:len(todo)-1]
		da := br.a.NumChanges() - br.b.NumChanges()
		if da <= 1 {
			continue
		}
		if runs >= maxRuns {
			path.Complete = false
			break
		}
		// The penalty at which the two known segmentations cost the same. If
		// anything else is optimal between them, it is optimal here.
		mid := (br.b.Cost - br.a.Cost) / float64(da)
		if !(mid > br.a.Penalty && mid < br.b.Penalty) {
			continue
		}
		r := inst.pelt(mid)
		runs++
		if r.NumChanges() == br.b.NumChanges() || r.NumChanges() == br.a.NumChanges() {
			continue
		}
		found = append(found, r)
		todo = append(todo, bracket{br.a, r}, bracket{r, br.b})
	}
	slices.SortFunc(found, func(x, y Segmentation) int {
		if d := int(y.NumChanges() - x.NumChanges()); d != 0 {
			return d
		}
		switch {
		case x.Penalty < y.Penalty:
			return -1
		case x.Penalty > y.Penalty:
			return 1
		}
		return 0
	})
	found = slices.CompactFunc(found, func(x, y Segmentation) bool {
		return x.NumChanges() == y.NumChanges()
	})
	path.offsets = make([]int32, 1, len(found)+1)
	for i, s := range found {
		from := penaltyLo
		if i > 0 {
			prev := found[i-1]
			from = (s.Cost - prev.Cost) / float64(prev.NumChanges()-s.NumChanges())
			from = min(max(from, penaltyLo), penaltyHi)
		}
		path.Found = append(path.Found, s.Penalty)
		path.Penalty = append(path.Penalty, from)
		path.NumChanges = append(path.NumChanges, s.NumChanges())
		path.Cost = append(path.Cost, s.Cost)
		path.starts = append(path.starts, s.Starts...)
		path.offsets = append(path.offsets, int32(len(path.starts)))
	}
	return
}
