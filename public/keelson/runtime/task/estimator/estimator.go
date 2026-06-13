package estimator

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
)

// UnitE mirrors task.UnitE one level down so the estimator stays free of
// the task package import (avoids a cycle if task ever wants to reuse
// estimator internals). Callers pass through the matching constant.
type UnitE uint8

const (
	UnitUnspecified UnitE = 0
	UnitItems       UnitE = 1
	UnitBytes       UnitE = 2
	UnitSteps       UnitE = 3
)

func (inst UnitE) String() (s string) {
	switch inst {
	case UnitItems:
		s = "items"
	case UnitBytes:
		s = "bytes"
	case UnitSteps:
		s = "steps"
	default:
		s = "unspecified"
	}
	return
}

// DefaultWindowMs is the sliding-window size for throughput averaging.
// Long enough to smooth single-step jitter; short enough that a stalled
// producer's ETA reflects the stall quickly.
const DefaultWindowMs int64 = 2_000

// DefaultMaxSamples caps the per-task sample buffer. A 2 s window with a
// hot producer reporting at 1 kHz yields 2 000 samples; cap at 256 to
// bound memory while keeping the throughput estimate well-conditioned.
const DefaultMaxSamples int32 = 256

type sample struct {
	current uint64
	atMs    int64
}

// Inst holds the sliding-window state for one in-flight task. Not
// goroutine-safe — the caller (task.Handle) guards access with its own
// mutex.
type Inst struct {
	samples    []sample
	head       int32
	filled     int32
	maxSamples int32
	windowMs   int64
}

// New returns an Inst sized at DefaultMaxSamples with DefaultWindowMs.
func New() (inst *Inst) {
	inst = NewWith(DefaultWindowMs, DefaultMaxSamples)
	return
}

// NewWith returns an Inst configured for the given window + buffer size.
// Tests use this to dial both knobs.
func NewWith(windowMs int64, maxSamples int32) (inst *Inst) {
	if windowMs <= 0 {
		windowMs = DefaultWindowMs
	}
	if maxSamples <= 0 {
		maxSamples = DefaultMaxSamples
	}
	inst = &Inst{
		samples:    make([]sample, maxSamples),
		maxSamples: maxSamples,
		windowMs:   windowMs,
	}
	return
}

// Add records a (current, atMs) sample. Samples are kept in insertion
// order in a ring buffer; throughput/ETA are computed lazily from the
// oldest sample still inside the window.
func (inst *Inst) Add(current uint64, atMs int64) {
	inst.samples[inst.head] = sample{current: current, atMs: atMs}
	inst.head = (inst.head + 1) % inst.maxSamples
	if inst.filled < inst.maxSamples {
		inst.filled++
	}
}

// ThroughputPerSec returns the windowed rate-of-change of Current in
// units per second. Zero when fewer than two samples or when the window
// span is degenerate (single timestamp). Negative deltas (a producer
// resetting its counter) yield zero — a stalled-or-restarted task should
// not show negative throughput to a user.
func (inst *Inst) ThroughputPerSec() (rate float64) {
	if inst.filled < 2 {
		return
	}
	oldest, newest, ok := inst.windowBounds()
	if !ok {
		return
	}
	dCur := int64(newest.current) - int64(oldest.current)
	if dCur <= 0 {
		return
	}
	dMs := newest.atMs - oldest.atMs
	if dMs <= 0 {
		return
	}
	rate = float64(dCur) * 1000.0 / float64(dMs)
	return
}

// EtaMs returns the estimated milliseconds-remaining for a task with the
// given total, or -1 when unknown (insufficient samples, zero throughput,
// indeterminate total, or current already past total). Caller passes the
// most recent Current value alongside total.
func (inst *Inst) EtaMs(current, total uint64) (etaMs int64) {
	etaMs = -1
	if total == 0 || current >= total {
		return
	}
	rate := inst.ThroughputPerSec()
	if rate <= 0 {
		return
	}
	remaining := total - current
	etaMs = int64(float64(remaining) * 1000.0 / rate)
	return
}

// Reset clears the sample history. Tests use it to simulate a paused
// task resuming; production code does not call it.
func (inst *Inst) Reset() {
	inst.head = 0
	inst.filled = 0
}

func (inst *Inst) windowBounds() (oldest, newest sample, ok bool) {
	if inst.filled < 2 {
		return
	}
	newestIdx := (inst.head - 1 + inst.maxSamples) % inst.maxSamples
	newest = inst.samples[newestIdx]
	cutoff := newest.atMs - inst.windowMs
	for i := int32(0); i < inst.filled; i++ {
		idx := (newestIdx - i + inst.maxSamples) % inst.maxSamples
		s := inst.samples[idx]
		if s.atMs < cutoff && i > 0 {
			break
		}
		oldest = s
	}
	ok = true
	return
}

// Humanize formats the (current, total, unit, throughput, etaMs) tuple
// into a stable visible string. Callers compare the returned string to
// the previously emitted one to gate publication on humanized-change.
//
// Examples:
//
//	(items, 470, 1000, 240, 2200)   -> "47% · 240 items/s · 2s left"
//	(bytes, 1_300_000_000, 3_400_000_000, 18_900_000, 110_000) ->
//	  "1.3 GB / 3.4 GB · 19 MB/s · 1m50s left"
//	(items, 47, 0, 240, -1)         -> "47 items · 240 items/s"
//	(steps, 3, 5, 0, -1)            -> "step 3 of 5"
//	(items, 0, 0, 0, -1)            -> "starting"
func Humanize(current, total uint64, unit UnitE, throughput float64, etaMs int64) (s string) {
	if unit == UnitSteps {
		s = humanizeSteps(current, total)
		return
	}

	if current == 0 && throughput == 0 {
		s = "starting"
		return
	}

	progress := humanizeProgress(current, total, unit)
	rate := humanizeRate(throughput, unit)
	eta := humanizeEta(etaMs)

	switch {
	case rate == "" && eta == "":
		s = progress
	case eta == "":
		s = progress + " · " + rate
	case rate == "":
		s = progress + " · " + eta
	default:
		s = progress + " · " + rate + " · " + eta
	}
	return
}

func humanizeSteps(current, total uint64) (s string) {
	switch {
	case total == 0:
		s = fmt.Sprintf("step %d", current)
	default:
		s = fmt.Sprintf("step %d of %d", current, total)
	}
	return
}

func humanizeProgress(current, total uint64, unit UnitE) (s string) {
	switch unit {
	case UnitBytes:
		if total == 0 {
			s = humanize.IBytes(current)
			return
		}
		// Round percent for the visible string; raw fraction lives on
		// the wire. Distinct integer-percent values trigger emission;
		// fractional drift between them does not.
		pct := int(float64(current) * 100.0 / float64(total))
		s = fmt.Sprintf("%s / %s · %d%%", humanize.IBytes(current), humanize.IBytes(total), pct)
	default:
		unitLabel := unit.String()
		if total == 0 {
			s = fmt.Sprintf("%s %s", humanize.Comma(int64(current)), unitLabel)
			return
		}
		pct := int(float64(current) * 100.0 / float64(total))
		s = fmt.Sprintf("%d%%", pct)
	}
	return
}

func humanizeRate(throughput float64, unit UnitE) (s string) {
	if throughput <= 0 {
		return
	}
	switch unit {
	case UnitBytes:
		s = humanize.IBytes(uint64(throughput)) + "/s"
	default:
		unitLabel := unit.String()
		if throughput >= 100 {
			s = fmt.Sprintf("%s %s/s", humanize.Comma(int64(throughput)), unitLabel)
		} else {
			s = fmt.Sprintf("%.1f %s/s", throughput, unitLabel)
		}
	}
	return
}

func humanizeEta(etaMs int64) (s string) {
	if etaMs < 0 {
		return
	}
	if etaMs < 1000 {
		s = "<1s left"
		return
	}
	d := time.Duration(etaMs) * time.Millisecond
	s = formatDuration(d) + " left"
	return
}

func formatDuration(d time.Duration) (s string) {
	switch {
	case d < time.Minute:
		s = fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		m := int(d.Minutes())
		sec := int(d.Seconds()) - m*60
		if sec == 0 {
			s = fmt.Sprintf("%dm", m)
		} else {
			s = fmt.Sprintf("%dm%ds", m, sec)
		}
	default:
		h := int(d.Hours())
		m := int(d.Minutes()) - h*60
		if m == 0 {
			s = fmt.Sprintf("%dh", h)
		} else {
			s = fmt.Sprintf("%dh%dm", h, m)
		}
	}
	return
}
