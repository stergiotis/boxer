package timescrubber

import "math"

// column is what one pixel column of a crowded strip shows: the largest value
// and the largest peak among its steps, and the worst of their states. A mark
// keeps its meaning — the bar is still a value, the cap still a peak — and
// nothing is subsampled, so the step a reader scans for is not the one left
// out (ADR-0251 §SD10).
type column struct {
	x           float32
	value, peak float32
	state       StepStateE
	first, last int
}

const columnW = 3

// crowded says the steps are closer than a bar is wide. It depends on the
// axis — the width and the steps — and on nothing that moves.
func crowded(axis timeAxis) bool {
	n := len(axis.ms)
	if n < 3 {
		return false
	}
	return (axis.x1-axis.x0)/float32(n-1) < columnW || barWidth(axis) <= minBarW
}

// stateRank orders states by how much a reader needs to know of them.
func stateRank(s StepStateE) int {
	switch s {
	case StepStateMissing:
		return 3
	case StepStateLoading:
		return 2
	case StepStateIdle:
		return 1
	}
	return 0
}

// columnsOf groups the steps by the pixel column they fall in.
func columnsOf(axis timeAxis, steps []Step, dst []column) []column {
	dst = dst[:0]
	nan := float32(math.NaN())
	slot := -1
	for i := range steps {
		x := axis.posToX(float64(i))
		k := int((x - axis.x0) / columnW)
		if k != slot {
			slot = k
			dst = append(dst, column{
				x: axis.x0 + (float32(k)+0.5)*columnW, value: nan, peak: nan,
				state: StepStateHeld, first: i, last: i,
			})
		}
		col := &dst[len(dst)-1]
		col.last = i
		if v := steps[i].Value; v == v && !(col.value >= v) {
			col.value = v
		}
		if p := steps[i].Peak; p == p && !(col.peak >= p) {
			col.peak = p
		}
		if stateRank(steps[i].State) > stateRank(col.state) {
			col.state = steps[i].State
		}
	}
	return dst
}
