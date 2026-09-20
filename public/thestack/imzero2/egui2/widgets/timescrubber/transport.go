package timescrubber

import "math"

// ModeE is what playback does at the end of its span.
type ModeE uint8

const (
	// ModeLoop starts over at the other end.
	ModeLoop ModeE = iota
	// ModeBounce turns round and plays back.
	ModeBounce
	// ModeOnce stops.
	ModeOnce
)

// AllModes lists the modes in the order a control cycles through them.
var AllModes = []ModeE{ModeLoop, ModeBounce, ModeOnce}

func (inst ModeE) String() string {
	switch inst {
	case ModeBounce:
		return "bounce"
	case ModeOnce:
		return "once"
	}
	return "loop"
}

const (
	// holdOffset is how far inside a bracket playback waits for it. It is
	// past a step — so that whoever loads the data sees the bracket wanted
	// and not the single step — and close enough to it that the picture is
	// the step's.
	holdOffset = 5e-4
	// entrance is how close to a step a position counts as entering the
	// bracket beyond it.
	entrance = 2 * holdOffset
	// maxAdvanceSeconds bounds one Advance, so a stalled frame loop resumes
	// where it was and does not leap.
	maxAdvanceSeconds = 0.25
	defaultRate       = 0.5
	// defaultDwell is how long playback holds an end. The loop tools of
	// meteorology all have the knob; the one bound a vendor documents is
	// 2.5 s (ADR-0251 §SD3).
	defaultDwell = 1.0
	// waitHorizon is the time over which WaitShare forgets.
	waitHorizon = 3.0
)

// label is the mode as a control shows it.
func (inst ModeE) label() string {
	switch inst {
	case ModeBounce:
		return "Bounce"
	case ModeOnce:
		return "Once"
	}
	return "Loop"
}

// Transport is the playback state of a stepped series (ADR-0251 §SD3): a
// position as a fractional step index — 2.25 is a quarter of the way from
// step 2 to step 3 — and the rules that move it. It draws nothing and reads
// no clock, so it is driven and tested with plain numbers.
type Transport struct {
	Pos     float64
	Playing bool
	// Rate is steps per second; zero takes 0.5.
	Rate float64
	Mode ModeE

	// RangeOn limits playback, and the first/last controls, to the steps
	// RangeLo through RangeHi.
	RangeOn          bool
	RangeLo, RangeHi int

	// Dwell is how long, in seconds, playback holds the last step before a
	// loop wraps and each end before a bounce turns; without it the last
	// step is shown for one frame. Zero takes 1 s, a negative value none.
	Dwell float64

	// Buffering says the last Advance waited for data; BufferingStep is the
	// step it waited for and BufferingFor for how long, in seconds.
	Buffering     bool
	BufferingStep int
	BufferingFor  float64
	// WaitShare is the share of recent playback spent waiting, so
	// Rate·(1−WaitShare) is the rate achieved.
	WaitShare float64

	backward  bool
	entered   int // the bracket entered whole, by its lower step; -1 is none
	enteredOK bool
	dwelling  bool
	dwellLeft float64
	settled   bool
}

// Bounds is the span playback covers: the range when one is set and valid,
// else every step.
func (inst *Transport) Bounds(steps int) (lo, hi int) {
	lo, hi = 0, max(steps-1, 0)
	if inst.RangeOn {
		a, b := min(inst.RangeLo, inst.RangeHi), max(inst.RangeLo, inst.RangeHi)
		a, b = max(a, 0), min(b, hi)
		if a < b {
			lo, hi = a, b
		}
	}
	return
}

// SetRange limits playback to the steps a through b, in either order. A range
// of one step is no range.
func (inst *Transport) SetRange(a, b int) {
	if a == b {
		inst.ClearRange()
		return
	}
	inst.RangeOn, inst.RangeLo, inst.RangeHi = true, min(a, b), max(a, b)
}

// ClearRange lets playback span every step again.
func (inst *Transport) ClearRange() { inst.RangeOn = false }

// Seek moves to a position, clamped to the steps, and stops playback: a
// person who moves the playhead wants to look at where they put it. The
// position counts as settled (see TakeSettled).
func (inst *Transport) Seek(pos float64, steps int) {
	inst.scrub(pos, steps)
	inst.settled = true
}

// scrub is Seek for a position that is still moving under a drag: it does not
// settle.
func (inst *Transport) scrub(pos float64, steps int) {
	inst.Pos = clampPos(pos, 0, max(steps-1, 0))
	inst.Playing = false
	inst.rest()
}

// rest forgets what playback had going: the wait, the dwell and the bracket
// it had entered.
func (inst *Transport) rest() {
	inst.Buffering, inst.BufferingFor = false, 0
	inst.dwelling, inst.dwellLeft = false, 0
	inst.enteredOK = false
}

// TakeSettled says whether the position came to rest since the last call: a
// seek, a step, and each whole step playback reached. A consumer whose work
// per position is a query reads this and needs no debounce of its own
// (ADR-0251 §SD3).
func (inst *Transport) TakeSettled() (settled bool) {
	settled, inst.settled = inst.settled, false
	return
}

// Dwelling says playback is holding an end.
func (inst *Transport) Dwelling() bool { return inst.dwelling }

// SetIn and SetOut put an end of the range on a step, keeping the other end
// where it is — at the series' own end when there is no range. An end set on
// or past the other one sends that one back to the series' end, which is what
// an editor's in and out points do.
func (inst *Transport) SetIn(step, steps int) {
	last := max(steps-1, 0)
	lo, hi := inst.Bounds(steps)
	if !inst.RangeOn {
		hi = last
	}
	lo = min(max(step, 0), last)
	if lo >= hi {
		hi = last
	}
	inst.SetRange(lo, hi)
}

func (inst *Transport) SetOut(step, steps int) {
	last := max(steps-1, 0)
	lo, hi := inst.Bounds(steps)
	if !inst.RangeOn {
		lo = 0
	}
	hi = min(max(step, 0), last)
	if hi <= lo {
		lo = 0
	}
	inst.SetRange(lo, hi)
}

// SlideRange moves the range by delta steps, whole, as far as the series lets
// it.
func (inst *Transport) SlideRange(delta, steps int) {
	if !inst.RangeOn {
		return
	}
	lo, hi := inst.Bounds(steps)
	delta = min(max(delta, -lo), max(steps-1, 0)-hi)
	inst.SetRange(lo+delta, hi+delta)
}

// MoveRangeEdge puts the lower (or upper) edge of the range on a step. An
// edge stops one step short of the other and does not pass it.
func (inst *Transport) MoveRangeEdge(upper bool, step, steps int) {
	if !inst.RangeOn {
		return
	}
	lo, hi := inst.Bounds(steps)
	if upper {
		hi = min(max(step, lo+1), max(steps-1, 0))
	} else {
		lo = min(max(step, 0), hi-1)
	}
	inst.SetRange(lo, hi)
}

// StepBy moves to the n-th whole step after (or before) the position. From
// between two steps, one step forward is the next whole step and one step
// back the previous one.
func (inst *Transport) StepBy(n int, steps int) {
	pos := inst.Pos
	switch {
	case n > 0:
		pos = math.Floor(pos+1e-9) + float64(n)
	case n < 0:
		pos = math.Ceil(pos-1e-9) + float64(n)
	}
	inst.Seek(pos, steps)
}

// First and Last move to the ends of the playback span.
func (inst *Transport) First(steps int) {
	lo, _ := inst.Bounds(steps)
	inst.Seek(float64(lo), steps)
}

func (inst *Transport) Last(steps int) {
	_, hi := inst.Bounds(steps)
	inst.Seek(float64(hi), steps)
}

// Toggle starts or stops playback. Starting at the end of a once-through
// starts over.
func (inst *Transport) Toggle(steps int) {
	inst.Playing = !inst.Playing
	inst.rest()
	inst.WaitShare = 0
	if !inst.Playing {
		return
	}
	lo, hi := inst.Bounds(steps)
	if inst.Mode == ModeOnce && inst.Pos >= float64(hi) {
		inst.Pos = float64(lo)
	}
	inst.backward = false
}

// Advance plays for dt seconds. ready says whether a step's data is there;
// nil means always.
//
// Playback enters the bracket between two steps only when both are ready, and
// otherwise waits just inside it (ADR-0251 §SD3). Showing half a bracket
// would draw the one step alone and then jump when the other arrived; waiting
// on the step itself would never ask for the next one, because a position on
// a step wants that step only. The rule holds wherever playback starts: from
// the middle of a bracket it waits where it is.
func (inst *Transport) Advance(dt float64, steps int, ready func(step int) bool) {
	inst.Buffering = false
	if !inst.Playing || steps < 2 || !(dt > 0) {
		inst.BufferingFor = 0
		return
	}
	lo, hi := inst.Bounds(steps)
	flo, fhi := float64(lo), float64(hi)
	inst.Pos = clampPos(inst.Pos, lo, hi)
	rate := inst.Rate
	if !(rate > 0) {
		rate = defaultRate
	}
	left := min(dt, maxAdvanceSeconds) // seconds
	isReady := func(step int) bool { return ready == nil || ready(step) }
	defer func() {
		// The share of the time that was there to play in, dwell excluded.
		share := 1 - math.Exp(-dt/waitHorizon)
		w := 0.0
		if inst.Buffering {
			w = 1
			inst.BufferingFor += dt
		} else {
			inst.BufferingFor = 0
		}
		inst.WaitShare += (w - inst.WaitShare) * share
	}()

	// wait holds at pos for the bracket [a, a+1], naming the step that is
	// not there.
	wait := func(pos float64, a int) {
		near, far := a, a+1
		if inst.backward {
			near, far = far, near
		}
		inst.Pos = pos
		inst.Buffering = true
		inst.BufferingStep = far
		if isReady(far) {
			inst.BufferingStep = near
		}
	}
	// dwell spends time at an end; it returns false when the time ran out.
	dwell := func() bool {
		d := inst.Dwell
		if d == 0 {
			d = defaultDwell
		}
		if !(d > 0) {
			return true
		}
		if !inst.dwelling {
			inst.dwelling, inst.dwellLeft = true, d
		}
		if left < inst.dwellLeft {
			inst.dwellLeft -= left
			left = 0
			return false
		}
		left -= inst.dwellLeft
		inst.dwelling, inst.dwellLeft = false, 0
		return true
	}

	for turns := 0; left > 0 && turns < 4*steps+8; turns++ {
		if !inst.backward {
			if inst.Pos >= fhi {
				switch inst.Mode {
				case ModeLoop:
					if !dwell() {
						return
					}
					inst.Pos = flo
					inst.enteredOK = false
					inst.settled = true
				case ModeBounce:
					if !dwell() {
						return
					}
					inst.backward = true
					inst.enteredOK = false
				default:
					inst.Playing = false
					return
				}
				continue
			}
			k := math.Floor(inst.Pos)
			if !(inst.enteredOK && inst.entered == int(k)) {
				if !(isReady(int(k)) && isReady(int(k)+1)) {
					pos := inst.Pos
					if pos-k < entrance {
						pos = k + holdOffset
					}
					wait(pos, int(k))
					return
				}
				inst.entered, inst.enteredOK = int(k), true
			}
			move := min(left*rate, k+1-inst.Pos)
			inst.Pos += move
			left -= move / rate
			if inst.Pos >= k+1 {
				inst.Pos = k + 1
				inst.settled = true
			}
			continue
		}
		if inst.Pos <= flo {
			// Only bounce plays backward, and it turns round again here.
			if !dwell() {
				return
			}
			inst.backward = false
			inst.enteredOK = false
			continue
		}
		k := math.Ceil(inst.Pos)
		if !(inst.enteredOK && inst.entered == int(k)-1) {
			if !(isReady(int(k)) && isReady(int(k)-1)) {
				pos := inst.Pos
				if k-pos < entrance {
					pos = k - holdOffset
				}
				wait(pos, int(k)-1)
				return
			}
			inst.entered, inst.enteredOK = int(k)-1, true
		}
		move := min(left*rate, inst.Pos-(k-1))
		inst.Pos -= move
		left -= move / rate
		if inst.Pos <= k-1 {
			inst.Pos = k - 1
			inst.settled = true
		}
	}
}

// Ahead appends to dst the next n steps playback will reach beyond the
// bracket it is in, in the order it will reach them: through a loop's wrap
// and a bounce's turn, and no further than a once-through goes. It walks the
// rules Advance does, so whoever loads data ahead of the playhead and the
// playhead cannot disagree (ADR-0251 §SD3). It does not ask whether playback
// is on: a loader that reads it at rest has the next step there when play is
// pressed.
func (inst *Transport) Ahead(steps, n int, dst []int) []int {
	dst = dst[:0]
	if steps < 2 || n <= 0 {
		return dst
	}
	lo, hi := inst.Bounds(steps)
	if hi <= lo {
		return dst
	}
	pos := clampPos(inst.Pos, lo, hi)
	backward := inst.backward && inst.Playing && inst.Mode == ModeBounce
	a, b := int(math.Floor(pos)), int(math.Ceil(pos)) // the bracket in hand
	next := b + 1
	if backward {
		next = a - 1
	}
	listed := func(step int) bool {
		for _, s := range dst {
			if s == step {
				return true
			}
		}
		return false
	}
	for turns := 0; len(dst) < n && turns < 2*steps+4; turns++ {
		switch {
		case !backward && next > hi:
			switch inst.Mode {
			case ModeLoop:
				next = lo
			case ModeBounce:
				backward, next = true, hi-1
			default:
				return dst
			}
			continue
		case backward && next < lo:
			backward, next = false, lo+1
			continue
		}
		if listed(next) {
			return dst // come round to what is already listed
		}
		if next != a && next != b { // the bracket in hand is passed through, not listed
			dst = append(dst, next)
		}
		if backward {
			next--
		} else {
			next++
		}
	}
	return dst
}

func clampPos(pos float64, lo, hi int) float64 {
	if !(pos > float64(lo)) {
		return float64(lo)
	}
	if pos > float64(hi) {
		return float64(hi)
	}
	return pos
}
