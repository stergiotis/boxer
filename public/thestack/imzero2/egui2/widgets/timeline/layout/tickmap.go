//go:build llm_generated_opus47

package layout

import (
	"math"
	"time"

	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
)

// TickAtX is a tick value with its precomputed screen-x coordinate.
type TickAtX struct {
	T     time.Time
	X     float64
	Label string
}

// RolloverRunAtX is one context-label run with its precomputed screen-x range.
// Half-open: the run covers pixels [StartX, EndX).
type RolloverRunAtX struct {
	StartX float64
	EndX   float64
	Label  string
}

// RolloverRowAtX is one row of context labels with precomputed pixel ranges.
type RolloverRowAtX struct {
	Boundary timeticks.BoundaryE
	Runs     []RolloverRunAtX
}

// TickMap is the renderer-facing tick layout for a time-axis viewport.
//
// AxisStartPx / AxisEndPx are the pixel bounds the caller passed in; every
// TickAtX.X and RolloverRunAtX.{StartX,EndX} is already mapped into that
// range using the timeticks layout's MapToScreen. The renderer needs no
// further math.
type TickMap struct {
	ViewMin      time.Time
	ViewMax      time.Time
	AxisStartPx  float64
	AxisEndPx    float64
	Step         timeticks.TimeStep
	Ticks        []TickAtX
	RolloverRows []RolloverRowAtX
}

// ComputeTickMap calls boxer's timeticks.TimeTicks for the [viewMin,viewMax]
// range and precomputes a screen-x for every tick and every rollover run.
//
// The pixel width must be positive (AxisEndPx > AxisStartPx) and the view
// span non-degenerate (viewMax after viewMin), otherwise the returned map is
// empty (Ticks==nil, RolloverRows==nil) — the renderer should skip drawing.
//
// loc defaults to time.UTC when nil. prevStep enables hysteresis on
// continuous zoom (see timeticks.TimeTickOptions); pass timeticks.TimeStep{}
// to disable.
func ComputeTickMap(viewMin, viewMax time.Time, axisStartPx, axisEndPx float64, loc *time.Location, prevStep timeticks.TimeStep) (tm TickMap) {
	tm.ViewMin = viewMin
	tm.ViewMax = viewMax
	tm.AxisStartPx = axisStartPx
	tm.AxisEndPx = axisEndPx

	width := axisEndPx - axisStartPx
	if width <= 0 || !viewMax.After(viewMin) {
		return
	}

	axis := timeticks.TimeTicks(viewMin, viewMax, timeticks.TimeTickOptions{
		PanelWidthPx: int32(width),
		Location:     loc,
		PrevStep:     prevStep,
	})
	tm.Step = axis.Step

	tm.Ticks = make([]TickAtX, len(axis.TickValues))
	for i, t := range axis.TickValues {
		tm.Ticks[i] = TickAtX{
			T:     t,
			X:     axis.MapToScreen(t, axisStartPx, axisEndPx),
			Label: axis.TickLabels[i],
		}
	}

	tm.RolloverRows = make([]RolloverRowAtX, len(axis.RolloverRows))
	for r, row := range axis.RolloverRows {
		runs := make([]RolloverRunAtX, len(row.Labels))
		for j, lbl := range row.Labels {
			startTick := axis.TickValues[lbl.StartIdx]
			startX := axis.MapToScreen(startTick, axisStartPx, axisEndPx)
			var endX float64
			if int(lbl.EndIdx) < len(axis.TickValues) {
				endTick := axis.TickValues[lbl.EndIdx]
				endX = axis.MapToScreen(endTick, axisStartPx, axisEndPx)
			} else {
				endX = axisEndPx
			}
			runs[j] = RolloverRunAtX{
				StartX: startX,
				EndX:   endX,
				Label:  lbl.Label,
			}
		}
		tm.RolloverRows[r] = RolloverRowAtX{
			Boundary: row.Boundary,
			Runs:     runs,
		}
	}
	return
}

// MapTimeToX maps an arbitrary time onto the tick map's pixel axis using
// the original [ViewMin, ViewMax] → [AxisStartPx, AxisEndPx] linear scale.
// Useful for renderers placing point / interval events at the same pixel
// scale the ticks used. Returns AxisStartPx for a degenerate view; the
// caller is responsible for clipping t outside [ViewMin, ViewMax].
func (inst TickMap) MapTimeToX(t time.Time) (px float64) {
	span := float64(inst.ViewMax.Sub(inst.ViewMin))
	if span <= 0 {
		px = inst.AxisStartPx
		return
	}
	norm := float64(t.Sub(inst.ViewMin)) / span
	px = inst.AxisStartPx + norm*(inst.AxisEndPx-inst.AxisStartPx)
	return
}

// MapMSToX is the int64-epoch-milliseconds form of MapTimeToX. Use when
// driving the renderer directly from PointEvent.TMS / IntervalEvent.FromMS
// without going through time.Time.
func (inst TickMap) MapMSToX(tMS int64) (px float64) {
	px = inst.MapTimeToX(time.UnixMilli(tMS).UTC())
	return
}

// MapXToMS is the inverse of MapMSToX: given a screen-x coordinate
// (already in the axis pixel range), return the corresponding epoch-ms
// time. Useful for cursor-driven readouts. Returns the view minimum's
// epoch-ms for a degenerate axis; the caller should clamp px to
// [AxisStartPx, AxisEndPx] before calling if extrapolation is undesirable.
//
// Uses math.Floor on the millisecond offset to extrapolate consistently
// in both directions: int64(...) truncates toward zero, which for px
// LEFT of AxisStartPx produces a value AT or RIGHT of ViewMin instead of
// left of it — wrong direction. math.Floor fixes that without affecting
// the in-range case.
func (inst TickMap) MapXToMS(px float64) (tMS int64) {
	width := inst.AxisEndPx - inst.AxisStartPx
	viewMinMS := inst.ViewMin.UnixMilli()
	if width <= 0 {
		tMS = viewMinMS
		return
	}
	spanMS := inst.ViewMax.UnixMilli() - viewMinMS
	frac := (px - inst.AxisStartPx) / width
	tMS = viewMinMS + int64(math.Floor(frac*float64(spanMS)))
	return
}
