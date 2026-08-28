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
	// Unit, ViewMinU and ViewMaxU are set by [ComputeOffsetTickMap]: the
	// axis counts Unit from a zero that is not a calendar instant, and the
	// x mapping uses these counts. Unit == 0 is the calendar map, whose
	// mapping goes through ViewMin and ViewMax in milliseconds.
	Unit     time.Duration
	ViewMinU int64
	ViewMaxU int64
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

// MapMSToX maps an axis value — milliseconds since the epoch on a calendar
// map, a count of Unit on an offset map — to a pixel.
func (inst TickMap) MapMSToX(tMS int64) (px float64) {
	if inst.Unit > 0 {
		span := float64(inst.ViewMaxU - inst.ViewMinU)
		if span <= 0 {
			return inst.AxisStartPx
		}
		norm := float64(tMS-inst.ViewMinU) / span
		return inst.AxisStartPx + norm*(inst.AxisEndPx-inst.AxisStartPx)
	}
	px = inst.MapTimeToX(time.UnixMilli(tMS).UTC())
	return
}

// MapXToMS is the inverse of [TickMap.MapMSToX]; a pixel left of the axis
// maps below the view's start, so a brush that leaves the axis keeps its
// direction.
func (inst TickMap) MapXToMS(px float64) (tMS int64) {
	width := inst.AxisEndPx - inst.AxisStartPx
	viewMinMS := inst.ViewMin.UnixMilli()
	spanMS := inst.ViewMax.UnixMilli() - viewMinMS
	if inst.Unit > 0 {
		viewMinMS = inst.ViewMinU
		spanMS = inst.ViewMaxU - inst.ViewMinU
	}
	if width <= 0 {
		tMS = viewMinMS
		return
	}
	frac := (px - inst.AxisStartPx) / width
	tMS = viewMinMS + int64(math.Floor(frac*float64(spanMS)))
	return
}
