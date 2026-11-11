package changepoint

import (
	"container/heap"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// BinSeg segments by binary segmentation (Scott and Knott, Biometrics 30,
// 1974; Vostrikova, 1981): find the single split that lowers the cost most,
// keep it if the gain exceeds penalty, and repeat inside each half.
//
// It is greedy, so it is approximate where [Series.Pelt] is exact: every
// split is chosen as if it were the only change in its stretch. Two changes
// that cancel in the mean of their stretch — up then down again, a short
// excursion — can hide each other, and the first split is then misplaced or
// not made at all. Its uses are speed, an answer in a fixed number of
// changes, and being the method a reader may already know.
//
// maxChanges stops after that many splits, taken in order of decreasing gain;
// zero means no limit beyond the penalty.
func (inst *Series) BinSeg(penalty float64, maxChanges int32) (seg Segmentation, err error) {
	err = checkPenalty(penalty)
	if err != nil {
		return
	}
	if maxChanges < 0 {
		err = eb.Build().Int32("maxChanges", maxChanges).Errorf("the change limit is negative")
		return
	}
	queue := make(splitQueue, 0, 16)
	push := func(start int32, end int32) {
		split, gain, ok := inst.bestSplit(int(start), int(end))
		if ok && gain > penalty {
			heap.Push(&queue, pendingSplit{start: start, end: end, split: split, gain: gain})
		}
	}
	push(0, int32(inst.n))
	for len(queue) > 0 {
		if maxChanges > 0 && int32(len(seg.Starts)) >= maxChanges {
			break
		}
		p := heap.Pop(&queue).(pendingSplit)
		seg.Starts = append(seg.Starts, p.split)
		push(p.start, p.split)
		push(p.split, p.end)
	}
	slices.Sort(seg.Starts)
	seg.Penalty = penalty
	seg.Cost = inst.costOf(seg.Starts)
	return
}

// pendingSplit is a stretch whose best split is known and not yet taken.
type pendingSplit struct {
	start, end, split int32
	gain              float64
}

// splitQueue orders pending splits by decreasing gain.
type splitQueue []pendingSplit

func (inst splitQueue) Len() int           { return len(inst) }
func (inst splitQueue) Less(i, j int) bool { return inst[i].gain > inst[j].gain }
func (inst splitQueue) Swap(i, j int)      { inst[i], inst[j] = inst[j], inst[i] }
func (inst *splitQueue) Push(x any)        { *inst = append(*inst, x.(pendingSplit)) }
func (inst *splitQueue) Pop() (x any) {
	old := *inst
	x = old[len(old)-1]
	*inst = old[:len(old)-1]
	return
}

// bestSplit finds the split of [start, end) that lowers the cost most.
func (inst *Series) bestSplit(start int, end int) (split int32, gain float64, ok bool) {
	m := inst.minSegment
	if end-start < 2*m {
		return
	}
	table := &inst.table
	whole := table.cost(start, end)
	for k := start + m; k <= end-m; k++ {
		g := whole - table.cost(start, k) - table.cost(k, end)
		if !ok || g > gain {
			split, gain, ok = int32(k), g, true
		}
	}
	return
}
