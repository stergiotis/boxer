package estimator

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/hmi/progressest"
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

// Inst estimates one in-flight task's throughput and ETA from its reported
// counter. It is a thin millisecond-clock face on progressest.Tracker —
// Holt smoothing with a damped ETA, the estimator every progress readout in
// boxer shares — so a task's wire figures and a widget's own agree. Not
// goroutine-safe — the caller (task.Handle) guards access with its own
// mutex.
type Inst struct {
	tracker progressest.Tracker
	view    progressest.View
	atMs    int64
}

// New returns an Inst with no samples.
func New() (inst *Inst) {
	inst = &Inst{}
	return
}

// Add records a (current, atMs) sample. A counter that goes backwards
// starts the estimate afresh.
func (inst *Inst) Add(current uint64, atMs int64) {
	inst.atMs = atMs
	inst.view = inst.tracker.Observe(time.UnixMilli(atMs), int64(current), 0)
}

// ThroughputPerSec returns the smoothed rate of change of Current in units
// per second. Zero until two samples have landed, and never negative — a
// stalled-or-restarted task should not show negative throughput to a user.
func (inst *Inst) ThroughputPerSec() (rate float64) {
	rate = inst.view.Rate
	return
}

// EtaMs returns the estimated milliseconds-remaining for a task with the
// given total, or -1 when unknown (insufficient samples, zero throughput,
// indeterminate total, or current already past total). Caller passes the
// most recent Current value alongside total; it re-observes the last sample
// against that total without folding a new one in.
func (inst *Inst) EtaMs(current, total uint64) (etaMs int64) {
	etaMs = -1
	if total == 0 || current >= total || inst.view.Rate <= 0 {
		return
	}
	v := inst.tracker.Observe(time.UnixMilli(inst.atMs), int64(current), int64(total))
	etaMs = v.EtaMs()
	return
}

// Reset clears the sample history. Tests use it to simulate a paused
// task resuming; production code does not call it.
func (inst *Inst) Reset() {
	inst.tracker.Reset()
	inst.view = progressest.View{}
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
	s = progressest.FormatRate(throughput, unit.String())
	return
}

func humanizeEta(etaMs int64) (s string) {
	if etaMs < 0 {
		return
	}
	s = progressest.FormatRemaining(time.Duration(etaMs) * time.Millisecond)
	return
}
