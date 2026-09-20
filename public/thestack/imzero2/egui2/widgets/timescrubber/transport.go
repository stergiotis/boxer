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

	// Buffering says the last Advance waited for data.
	Buffering bool

	backward bool
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
// person who moves the playhead wants to look at where they put it.
func (inst *Transport) Seek(pos float64, steps int) {
	inst.Pos = clampPos(pos, 0, max(steps-1, 0))
	inst.Playing = false
	inst.Buffering = false
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
	inst.Buffering = false
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
// a step wants that step only.
func (inst *Transport) Advance(dt float64, steps int, ready func(step int) bool) {
	inst.Buffering = false
	if !inst.Playing || steps < 2 || !(dt > 0) {
		return
	}
	lo, hi := inst.Bounds(steps)
	flo, fhi := float64(lo), float64(hi)
	inst.Pos = clampPos(inst.Pos, lo, hi)
	rate := inst.Rate
	if !(rate > 0) {
		rate = defaultRate
	}
	left := rate * min(dt, maxAdvanceSeconds)
	isReady := func(step int) bool { return ready == nil || ready(step) }

	for turns := 0; left > 0 && turns < 4*steps+8; turns++ {
		if !inst.backward {
			if inst.Pos >= fhi {
				switch inst.Mode {
				case ModeLoop:
					inst.Pos = flo
				case ModeBounce:
					inst.backward = true
				default:
					inst.Playing = false
					return
				}
				continue
			}
			k := math.Floor(inst.Pos)
			if inst.Pos-k < entrance && !(isReady(int(k)) && isReady(int(k)+1)) {
				inst.Pos = k + holdOffset
				inst.Buffering = true
				return
			}
			move := min(left, k+1-inst.Pos)
			inst.Pos += move
			left -= move
			continue
		}
		if inst.Pos <= flo {
			// Only bounce plays backward, and it turns round again here.
			inst.backward = false
			continue
		}
		k := math.Ceil(inst.Pos)
		if k-inst.Pos < entrance && !(isReady(int(k)) && isReady(int(k)-1)) {
			inst.Pos = k - holdOffset
			inst.Buffering = true
			return
		}
		move := min(left, inst.Pos-(k-1))
		inst.Pos -= move
		left -= move
	}
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
