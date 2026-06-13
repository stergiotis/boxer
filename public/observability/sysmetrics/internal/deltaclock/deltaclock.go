package deltaclock

import "time"

// Tick records a counter sample at a specific monotonic time.
type Tick struct {
	At    time.Time
	Value uint64
}

// Diff returns the delta value and elapsed duration between prev and now.
//
// When now.Value < prev.Value:
//   - if rolloverMax > 0, a counter rollover at rolloverMax is assumed and
//     the delta is computed as (rolloverMax - prev.Value) + now.Value + 1.
//   - otherwise the function returns deltaValue = 0 (the conservative
//     default — the counter went backwards, but no rollover hint was
//     declared, so we clamp rather than fabricate a delta).
func Diff(prev, now Tick, rolloverMax uint64) (deltaValue uint64, elapsed time.Duration) {
	elapsed = now.At.Sub(prev.At)
	if now.Value >= prev.Value {
		deltaValue = now.Value - prev.Value
		return
	}
	if rolloverMax > 0 {
		deltaValue = (rolloverMax - prev.Value) + now.Value + 1
		return
	}
	return
}

// RatePerSecond returns deltaValue / elapsed.Seconds(). Returns 0 when
// elapsed is zero or negative — callers must treat the first sample of a
// counter as having no rate.
func RatePerSecond(deltaValue uint64, elapsed time.Duration) (rate float64) {
	if elapsed <= 0 {
		return 0
	}
	return float64(deltaValue) / elapsed.Seconds()
}

// Counter accumulates a single monotonic counter and computes delta+rate
// against the previous Sample. Zero value is not directly usable —
// instantiate via [NewCounter].
type Counter struct {
	rolloverMax uint64
	prev        Tick
	primed      bool
}

// NewCounter returns a Counter with the given rollover boundary.
func NewCounter(rolloverMax uint64) (inst *Counter) {
	return &Counter{rolloverMax: rolloverMax}
}

// Sample records a new (now, value) and returns the delta+elapsed since
// the previous Sample. The first call returns deltaValue = 0,
// elapsed = 0 — callers should treat that as "no rate yet".
func (inst *Counter) Sample(now time.Time, value uint64) (deltaValue uint64, elapsed time.Duration) {
	if !inst.primed {
		inst.prev = Tick{At: now, Value: value}
		inst.primed = true
		return
	}
	deltaValue, elapsed = Diff(inst.prev, Tick{At: now, Value: value}, inst.rolloverMax)
	inst.prev = Tick{At: now, Value: value}
	return
}

// Primed reports whether the Counter has recorded at least one Sample —
// i.e. whether subsequent Samples will return non-zero deltas.
func (inst *Counter) Primed() (primed bool) {
	return inst.primed
}

// Reset discards any prior Sample state, restoring zero-sample behavior.
func (inst *Counter) Reset() {
	inst.prev = Tick{}
	inst.primed = false
}
