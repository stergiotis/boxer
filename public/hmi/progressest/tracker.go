package progressest

import "time"

// DefaultStallAfter is how long a Tracker waits on an unchanged counter
// before folding the stall in as a zero-rate sample. Shorter, and a producer
// that reports in chunks reads as stalling between chunks; longer, and a
// real stall keeps showing the old rate and ETA.
const DefaultStallAfter = time.Second

// fractionScale is the counter resolution ObserveFraction maps [0,1] onto.
const fractionScale = 1_000_000

// View is what a Tracker makes of one observation.
type View struct {
	// Fraction is current/total clamped to [0,1], or -1 when the total is
	// unknown (zero).
	Fraction float32
	// Rate is the smoothed level in counter units per second; 0 until two
	// samples have landed, and never negative (a stall reads as 0).
	Rate float64
	// Eta is the damped estimate of the time left; EtaValid is false while
	// the estimator warms up or when the total is unknown.
	Eta      time.Duration
	EtaValid bool
}

// EtaMs is Eta in milliseconds, or -1 when it is not valid.
func (inst View) EtaMs() (ms int64) {
	if !inst.EtaValid {
		return -1
	}
	return inst.Eta.Milliseconds()
}

// Tracker drives an [Estimator] from the counters a job reports, sampled
// whenever the caller looks — per frame on a render thread, per report on a
// producer. It anchors on the first observation, re-anchors when the counter
// goes backwards (a new run under the same tracker), and folds a sample in
// only when the counter moved or StallAfter has passed without it moving, so
// re-reading an unchanged counter every frame does not drag the rate to zero.
//
// A change of total does not re-anchor: some producers (a database reporting
// rows to read) refine their total while the rate holds. A caller whose total
// change means a new phase calls Reset.
//
// The zero value is ready to use. Not goroutine-safe.
type Tracker struct {
	// StallAfter overrides DefaultStallAfter when positive.
	StallAfter time.Duration

	est         *Estimator
	tracking    bool
	lastCurrent int64
	lastFold    time.Time
}

// Reset forgets the run; the next Observe anchors afresh.
func (inst *Tracker) Reset() {
	inst.tracking = false
}

// Observe folds (now, current) in and returns the view for total. A total of
// zero means unknown: the view then carries the rate but no fraction or ETA.
func (inst *Tracker) Observe(now time.Time, current, total int64) (v View) {
	if inst.est == nil {
		inst.est = NewEstimator()
	}
	stallAfter := inst.StallAfter
	if stallAfter <= 0 {
		stallAfter = DefaultStallAfter
	}
	switch {
	case !inst.tracking || current < inst.lastCurrent:
		inst.tracking = true
		inst.est.Reset(now, current)
		inst.lastFold = now
	case current != inst.lastCurrent || now.Sub(inst.lastFold) >= stallAfter:
		inst.est.Update(now, current)
		inst.lastFold = now
	}
	inst.lastCurrent = current

	v.Fraction = -1
	if inst.est.Samples() >= 2 {
		// One sample is one interval's raw rate, not yet an estimate; the
		// ETA waits for the same second sample, so both appear together.
		v.Rate = max(inst.est.SmoothedRate(), 0)
	}
	if total > 0 {
		done := min(max(current, 0), total)
		v.Fraction = float32(float64(done) / float64(total))
		v.Eta, v.EtaValid = inst.est.EstimateETA(float64(total - done))
	}
	return
}

// ObserveFraction is Observe for a producer that reports only a fraction in
// [0,1]; a negative fraction means indeterminate. The view's Rate is then in
// fractions per second, which is useful for the ETA and meaningless to show.
func (inst *Tracker) ObserveFraction(now time.Time, fraction float64) (v View) {
	if fraction < 0 {
		v = inst.Observe(now, 0, 0)
		v.Rate = 0
		return
	}
	v = inst.Observe(now, int64(min(fraction, 1)*fractionScale), fractionScale)
	v.Rate /= fractionScale
	return
}
