package implot

import (
	"math"
	"strconv"
	"time"
)

// Text for one value on an axis: the hover readout and the TagX/TagY labels.
// Tick labels leave out what their neighbours share (Oct '26, 15:04); a
// readout or a tag names a single instant and carries the date, at the
// precision of the tick ladder's current unit — upstream's mouse-cursor time
// format, except that the date is never left out.

// timeCursorLayout picks the layout and truncation for an instant on a
// ScaleTime axis whose major ticks step by stepSec seconds.
func timeCursorLayout(stepSec float64) (layout string, trunc time.Duration) {
	switch {
	case stepSec < 60:
		return "2006-01-02 15:04:05", time.Second
	case stepSec < 86400:
		return "2006-01-02 15:04", time.Minute
	case stepSec < 7*86400:
		return "2006-01-02 15:04", time.Hour
	}
	return "2006-01-02", 24 * time.Hour
}

// formatTimeCursor prints Unix seconds v as an instant in loc (nil is UTC)
// at the precision a tick step of stepSec warrants. Outside UTC an instant
// with a time of day carries the zone's abbreviation, which also tells apart
// the two readings of an hour the clocks go back over. A value no time.Time
// can hold prints as a number.
func formatTimeCursor(v float64, stepSec float64, loc *time.Location) string {
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e15 {
		return strconv.FormatFloat(v, 'g', 6, 64)
	}
	if loc == nil {
		loc = time.UTC
	}
	secs := math.Floor(v)
	t := time.Unix(int64(secs), 0).In(loc)
	layout, trunc := timeCursorLayout(stepSec)
	if trunc == time.Hour {
		// The layout prints minutes; take the wall clock's off. Truncate
		// would round in absolute time, off the hour in a zone whose offset
		// is not whole hours.
		t = t.Add(-time.Duration(t.Minute())*time.Minute - time.Duration(t.Second())*time.Second)
	}
	s := t.Format(layout)
	if trunc < 24*time.Hour && loc != time.UTC && loc.String() != "UTC" {
		s += " " + t.Format("MST")
	}
	return s
}

// majorTickStep is the spacing of the first two major ticks, which on a
// located time axis is the ladder's current unit. With fewer than two the
// visible span stands in: the axis then shows less than two units.
func majorTickStep(ticks []tick, rng Range) float64 {
	first := math.NaN()
	for i := range ticks {
		if !ticks[i].major {
			continue
		}
		if math.IsNaN(first) {
			first = ticks[i].value
			continue
		}
		return ticks[i].value - first
	}
	return rng.Size()
}

// axisValueText prints v for the axis with the given state and this frame's
// ticks: a dated instant on a ScaleTime axis, %.6g otherwise.
func axisValueText(ax *axisState, ticks []tick, v float64) string {
	if ax.scale == ScaleTime {
		return formatTimeCursor(v, majorTickStep(ticks, ax.rng), ax.loc)
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}

// axisTagText labels a TagX/TagY: a dated instant on a ScaleTime axis, the
// tick formatter at two decimals otherwise.
func axisTagText(ax *axisState, ticks []tick, v float64) string {
	if ax.scale == ScaleTime {
		return formatTimeCursor(v, majorTickStep(ticks, ax.rng), ax.loc)
	}
	return formatTick(v, 2)
}

// readoutText is the hover readout for plot-space position (x, y).
func (st *plotState) readoutText(x float64, y float64) string {
	return axisValueText(&st.x, st.ticksX, x) + ", " + axisValueText(&st.y, st.ticksY, y)
}
