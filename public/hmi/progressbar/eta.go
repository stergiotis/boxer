//go:build llm_generated_opus47

package progressbar

import "time"

const (
	desAlpha            = 0.3
	desBeta             = 0.1
	desDampingThreshold = 0.10
)

// Estimator uses Holt's Double Exponential Smoothing to produce a smoothed
// rate estimate that captures both level and trend (acceleration/deceleration),
// plus a display-dampening layer that prevents the shown ETA from oscillating.
//
// It has no I/O and no concurrency story of its own — callers drive it with
// Update and read back SmoothedRate / SmoothedTrend / EstimateETA. The CLI
// renderer and the egui2 demo both use it this way.
//
// Use NewEstimator to construct one with tqdm/Rich-style defaults.
type Estimator struct {
	smoothedRate  float64 // S: level (items/sec)
	smoothedTrend float64 // B: trend (rate-of-change of rate)
	alpha         float64
	beta          float64

	prevCount int64
	prevTime  time.Time
	samples   int

	dampingThreshold float64
	displayedETA     time.Duration
}

// NewEstimator returns an Estimator with tqdm/Rich-style defaults
// (alpha=0.3, beta=0.1, damping=10%). Start must be called before Update.
func NewEstimator() (inst *Estimator) {
	inst = &Estimator{
		alpha:            desAlpha,
		beta:             desBeta,
		dampingThreshold: desDampingThreshold,
	}
	return
}

// Start anchors the estimator at the given time and count. Call before the
// first Update so dt is measured from a meaningful origin.
func (inst *Estimator) Start(now time.Time, count int64) {
	inst.prevTime = now
	inst.prevCount = count
}

// Reset clears all smoothed state and re-anchors at (now, count). Use this
// when the underlying counter is reset (e.g. a new run) — otherwise the
// stale level/trend will bias the first few updates.
func (inst *Estimator) Reset(now time.Time, count int64) {
	inst.smoothedRate = 0
	inst.smoothedTrend = 0
	inst.samples = 0
	inst.displayedETA = 0
	inst.prevTime = now
	inst.prevCount = count
}

// Update folds the instantaneous rate between the previous observation and
// (now, count) into the smoothed level and trend. Observations closer than
// 50 ms apart are ignored to avoid noise from high-frequency sampling.
func (inst *Estimator) Update(now time.Time, count int64) {
	dt := now.Sub(inst.prevTime).Seconds()
	if dt < 0.05 {
		return
	}

	rate := float64(count-inst.prevCount) / dt
	inst.prevCount = count
	inst.prevTime = now

	if inst.samples == 0 {
		inst.smoothedRate = rate
		inst.smoothedTrend = 0
		inst.samples = 1
		return
	}

	prevSmoothed := inst.smoothedRate
	inst.smoothedRate = inst.alpha*rate + (1-inst.alpha)*(inst.smoothedRate+inst.smoothedTrend)
	inst.smoothedTrend = inst.beta*(inst.smoothedRate-prevSmoothed) + (1-inst.beta)*inst.smoothedTrend
	inst.samples++
}

// SmoothedRate returns the current level estimate S (items/sec).
func (inst *Estimator) SmoothedRate() float64 { return inst.smoothedRate }

// SmoothedTrend returns the current trend estimate B (items/sec^2-ish —
// it is the smoothed step-over-step change in the level).
func (inst *Estimator) SmoothedTrend() float64 { return inst.smoothedTrend }

// Samples returns the number of Update calls that produced a sample
// (ignores calls skipped by the 50 ms floor).
func (inst *Estimator) Samples() int { return inst.samples }

// RawETA returns remaining/smoothedRate without damping. Useful for demos
// that want to visualise the damping filter.
func (inst *Estimator) RawETA(remaining float64) (eta time.Duration, valid bool) {
	if inst.samples < 2 || inst.smoothedRate <= 0 {
		return 0, false
	}
	if remaining <= 0 {
		return 0, true
	}
	return time.Duration(remaining/inst.smoothedRate) * time.Second, true
}

// DisplayedETA is the last ETA returned by EstimateETA, after damping.
// Zero if EstimateETA has not yet produced a valid value.
func (inst *Estimator) DisplayedETA() time.Duration { return inst.displayedETA }

// EstimateETA returns the damped ETA for `remaining` units of work.
// Decreases pass through immediately; small increases (within
// dampingThreshold of the displayed value) are suppressed; large increases
// break through. See EXPLANATION.md for the rationale.
func (inst *Estimator) EstimateETA(remaining float64) (eta time.Duration, valid bool) {
	if inst.samples < 2 || inst.smoothedRate <= 0 {
		return 0, false
	}
	if remaining <= 0 {
		inst.displayedETA = 0
		return 0, true
	}

	rawETA := time.Duration(remaining/inst.smoothedRate) * time.Second

	if inst.displayedETA <= 0 {
		inst.displayedETA = rawETA
	} else if rawETA <= inst.displayedETA {
		inst.displayedETA = rawETA
	} else if float64(rawETA) > float64(inst.displayedETA)*(1.0+inst.dampingThreshold) {
		inst.displayedETA = rawETA
	}

	return inst.displayedETA, true
}
