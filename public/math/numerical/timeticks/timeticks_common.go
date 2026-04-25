package timeticks

import (
	"time"
)

// TimeStepUnitE identifies the calendar unit of a tick step.
type TimeStepUnitE uint8

const (
	TimeStepUnitInvalid TimeStepUnitE = iota
	TimeStepUnitMillisecond
	TimeStepUnitSecond
	TimeStepUnitMinute
	TimeStepUnitHour
	TimeStepUnitDay
	TimeStepUnitMonth
	TimeStepUnitYear
)

// TimeStep is a tick step expressed as (Count × Unit). Months and years are
// non-uniform calendar quantities; sub-day units are uniform.
type TimeStep struct {
	Unit  TimeStepUnitE
	Count int64
}

// IsZero reports whether the step is the zero value (no step set).
func (inst TimeStep) IsZero() (zero bool) {
	zero = inst.Unit == TimeStepUnitInvalid && inst.Count == 0
	return
}

// ContextLabel describes a contiguous run of ticks that share a common
// coarser-grained label (e.g. all ticks within "Jan 14 2026" when the inner
// labels are hour-of-day). EndIdx is exclusive.
type ContextLabel struct {
	StartIdx int32
	EndIdx   int32
	Label    string
}

// TimeAxisLayout is the time-axis counterpart of finddivisions.AxisLayout.
//
// TickValues, TickLabels, ContextLabels are the renderer-facing outputs.
// Callers map each TickValues[i] to a pixel x via:
//
//	t := float64(tick.Sub(layout.ViewMin)) / float64(layout.ViewMax.Sub(layout.ViewMin))
//	px := axisStartPx + t*(axisEndPx-axisStartPx)
type TimeAxisLayout struct {
	DataMin time.Time
	DataMax time.Time

	ViewMin time.Time
	ViewMax time.Time

	Step TimeStep

	TickValues    []time.Time
	TickLabels    []string
	ContextLabels []ContextLabel

	Algorithm string
}

// TimeTickOptions configures TimeTicks.
//
// PanelWidthPx and TargetSpacingPx together set how many ticks are produced:
// the chosen step yields approximately (PanelWidthPx / TargetSpacingPx) ticks.
// Defaults: TargetSpacingPx=50 (matches uPlot), Location=time.UTC.
//
// PrevStep enables hysteresis on continuous zoom: if a previously rendered
// step would still produce spacing within HysteresisFrac of the target, it
// is reused even when the natural pick would advance one ladder rung. Pass
// the zero TimeStep{} to disable hysteresis.
type TimeTickOptions struct {
	PanelWidthPx    int32
	TargetSpacingPx int32
	Location        *time.Location
	PrevStep        TimeStep
	HysteresisFrac  float64
}
