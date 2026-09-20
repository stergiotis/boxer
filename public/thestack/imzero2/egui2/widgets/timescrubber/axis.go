package timescrubber

import (
	"sort"
	"time"
)

// timeAxis maps between the three coordinates of the strip: a position (a
// fractional step index), an instant, and a pixel. Steps sit at their own
// times, so a series that is hourly and then three-hourly looks it
// (ADR-0251 §SD1). Between two steps a position is linear in time.
//
// Steps whose times do not strictly increase have no time axis; they are laid
// out by index instead, which is also what a single step gets.
type timeAxis struct {
	ms      []int64
	byIndex bool
	x0, x1  float32
}

func newTimeAxis(steps []Step, x0, x1 float32) (a timeAxis) {
	a.x0, a.x1 = x0, x1
	a.ms = make([]int64, len(steps))
	for i := range steps {
		a.ms[i] = steps[i].At.UnixMilli()
		if i > 0 && a.ms[i] <= a.ms[i-1] {
			a.byIndex = true
		}
	}
	if len(steps) < 2 {
		a.byIndex = true
	}
	return
}

func (inst timeAxis) last() float64 { return float64(max(len(inst.ms)-1, 0)) }

// posToMS is the instant of a position.
func (inst timeAxis) posToMS(pos float64) (ms float64) {
	n := len(inst.ms)
	if n == 0 {
		return 0
	}
	pos = min(max(pos, 0), inst.last())
	k := min(int(pos), n-2)
	if k < 0 {
		return float64(inst.ms[0])
	}
	return float64(inst.ms[k]) + (pos-float64(k))*float64(inst.ms[k+1]-inst.ms[k])
}

// msToPos is the position of an instant, clamped to the steps.
func (inst timeAxis) msToPos(ms float64) (pos float64) {
	n := len(inst.ms)
	if n < 2 {
		return 0
	}
	if ms <= float64(inst.ms[0]) {
		return 0
	}
	if ms >= float64(inst.ms[n-1]) {
		return float64(n - 1)
	}
	k := sort.Search(n, func(i int) bool { return float64(inst.ms[i]) > ms }) - 1
	return float64(k) + (ms-float64(inst.ms[k]))/float64(inst.ms[k+1]-inst.ms[k])
}

// posToX is the pixel of a position.
func (inst timeAxis) posToX(pos float64) (x float32) {
	n := len(inst.ms)
	if n < 2 {
		return (inst.x0 + inst.x1) / 2
	}
	t := pos / inst.last()
	if !inst.byIndex {
		span := float64(inst.ms[n-1] - inst.ms[0])
		t = (inst.posToMS(pos) - float64(inst.ms[0])) / span
	}
	return inst.x0 + float32(min(max(t, 0), 1))*(inst.x1-inst.x0)
}

// xToPos is the position under a pixel, clamped to the steps.
func (inst timeAxis) xToPos(x float32) (pos float64) {
	n := len(inst.ms)
	if n < 2 || !(inst.x1 > inst.x0) {
		return 0
	}
	t := float64(min(max((x-inst.x0)/(inst.x1-inst.x0), 0), 1))
	if inst.byIndex {
		return t * inst.last()
	}
	return inst.msToPos(float64(inst.ms[0]) + t*float64(inst.ms[n-1]-inst.ms[0]))
}

// timeAt is the instant of a position as a time.
func (inst timeAxis) timeAt(pos float64, loc *time.Location) time.Time {
	return time.UnixMilli(int64(inst.posToMS(pos))).In(loc)
}
