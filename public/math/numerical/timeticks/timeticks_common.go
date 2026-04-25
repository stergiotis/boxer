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

// BoundaryE identifies which calendar unit's change drives a rollover row.
// Used as informational metadata so a renderer can decide where to place
// each row (e.g. coarsest at top, finest just above the tick row).
type BoundaryE uint8

const (
	BoundaryNone BoundaryE = iota
	BoundarySecond
	BoundaryMinute
	BoundaryHour
	BoundaryDay
	BoundaryMonth
	BoundaryYear
)

// RolloverRow is one row of context labels at a specific calendar boundary.
// Each label spans a contiguous run of ticks that produce the same
// formatted string at this row's level (e.g. all ticks within "Apr 25
// 2026" share one label in the day row). Rows are independent — a renderer
// stacks them at increasing y offset. Each row's label format is
// self-sufficient: it contains the full coarser-than-or-equal-to context
// (the day row carries the year too), so a renderer can display a single
// row at coarse zoom and the full stack at fine zoom.
type RolloverRow struct {
	Boundary BoundaryE
	Labels   []ContextLabel
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

	TickValues   []time.Time
	TickLabels   []string
	RolloverRows []RolloverRow

	Algorithm string
}

// MapToScreen converts a time value to a pixel coordinate using the
// layout's view bounds. Mirrors finddivisions.AxisLayout.MapToScreen for
// the time-axis case. Returns axisStartPx for a degenerate view; the
// caller is responsible for clipping if t falls outside [ViewMin, ViewMax].
func (inst TimeAxisLayout) MapToScreen(t time.Time, axisStartPx, axisEndPx float64) (px float64) {
	span := float64(inst.ViewMax.Sub(inst.ViewMin))
	if span <= 0 {
		px = axisStartPx
		return
	}
	norm := float64(t.Sub(inst.ViewMin)) / span
	px = axisStartPx + norm*(axisEndPx-axisStartPx)
	return
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
