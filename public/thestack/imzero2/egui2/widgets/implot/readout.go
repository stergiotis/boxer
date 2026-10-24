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

// formatTimeCursor prints Unix seconds v as a UTC instant at the precision a
// tick step of stepSec warrants. A value no time.Time can hold prints as a
// number.
func formatTimeCursor(v float64, stepSec float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e15 {
		return strconv.FormatFloat(v, 'g', 6, 64)
	}
	secs := math.Floor(v)
	t := time.Unix(int64(secs), int64((v-secs)*1e9)).UTC()
	layout, trunc := timeCursorLayout(stepSec)
	return t.Truncate(trunc).Format(layout)
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
		return formatTimeCursor(v, majorTickStep(ticks, ax.rng))
	}
	return strconv.FormatFloat(v, 'g', 6, 64)
}

// axisTagText labels a TagX/TagY: a dated instant on a ScaleTime axis, the
// tick formatter at two decimals otherwise.
func axisTagText(ax *axisState, ticks []tick, v float64) string {
	if ax.scale == ScaleTime {
		return formatTimeCursor(v, majorTickStep(ticks, ax.rng))
	}
	return formatTick(v, 2)
}

// readoutText is the hover readout for plot-space position (x, y).
func (st *plotState) readoutText(x float64, y float64) string {
	return axisValueText(&st.x, st.ticksX, x) + ", " + axisValueText(&st.y, st.ticksY, y)
}
