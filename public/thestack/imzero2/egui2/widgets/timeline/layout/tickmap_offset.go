package layout

import (
	"time"

	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
)

// ComputeOffsetTickMap is [ComputeTickMap] for an offset axis (ADR-0043
// update 2026-08-28): the view is [fromU, toU] counted in unit from a zero
// that is not a calendar instant, ticks come from [timeticks.OffsetLadder]
// and are labelled as offsets, and there are no rollover rows. ViewMin and
// ViewMax are set through the Unix epoch so the time.Time surface stays
// usable; the x mapping uses the units directly.
func ComputeOffsetTickMap(fromU, toU int64, unit time.Duration, axisStartPx, axisEndPx float64) (tm TickMap) {
	if unit <= 0 {
		unit = time.Millisecond
	}
	tm.Unit = unit
	tm.ViewMinU, tm.ViewMaxU = fromU, toU
	tm.ViewMin = unitsToUnixTime(fromU, unit)
	tm.ViewMax = unitsToUnixTime(toU, unit)
	tm.AxisStartPx = axisStartPx
	tm.AxisEndPx = axisEndPx

	width := axisEndPx - axisStartPx
	if width <= 0 || toU <= fromU {
		return
	}
	fromD := time.Duration(fromU) * unit
	toD := time.Duration(toU) * unit
	step := timeticks.PickOffsetStep(toD-fromD, float32(width), offsetTargetPx)
	ticks := timeticks.OffsetTicks(fromD, toD, step, nil)
	tm.Ticks = make([]TickAtX, 0, len(ticks))
	for _, d := range ticks {
		u := int64(d / unit)
		tm.Ticks = append(tm.Ticks, TickAtX{
			T:     unitsToUnixTime(u, unit),
			X:     tm.MapMSToX(u),
			Label: timeticks.FormatOffset(d, step),
		})
	}
	return
}

// offsetTargetPx is the tick spacing the offset ladder aims for.
const offsetTargetPx float32 = 90

// unitsToUnixTime maps an offset count to the time.Time surface through the
// Unix epoch.
func unitsToUnixTime(v int64, unit time.Duration) (t time.Time) {
	return time.Unix(0, 0).UTC().Add(time.Duration(v) * unit)
}
