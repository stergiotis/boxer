package timeticks

import (
	"fmt"
	"strings"
	"time"
)

// OffsetLadder is the tick-step ladder for an axis of offsets from a start —
// a recording's position, an elapsed time — as opposed to the calendar
// ladder of [TimeTicks]: from a microsecond at sample-level zoom to a day,
// through the steps a listener reads naturally. An offset has no months.
var OffsetLadder = []time.Duration{
	time.Microsecond, 2 * time.Microsecond, 5 * time.Microsecond,
	10 * time.Microsecond, 20 * time.Microsecond, 50 * time.Microsecond,
	100 * time.Microsecond, 200 * time.Microsecond, 500 * time.Microsecond,
	time.Millisecond, 2 * time.Millisecond, 5 * time.Millisecond,
	10 * time.Millisecond, 20 * time.Millisecond, 50 * time.Millisecond,
	100 * time.Millisecond, 200 * time.Millisecond, 500 * time.Millisecond,
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second, 15 * time.Second, 30 * time.Second,
	time.Minute, 2 * time.Minute, 5 * time.Minute, 10 * time.Minute, 15 * time.Minute, 30 * time.Minute,
	time.Hour, 2 * time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

// PickOffsetStep returns the smallest ladder step that puts at least targetPx
// pixels between ticks across span drawn over widthPx pixels. A span too wide
// for the ladder falls back to the largest step; a degenerate span to the
// smallest.
func PickOffsetStep(span time.Duration, widthPx float32, targetPx float32) (step time.Duration) {
	if span <= 0 || widthPx <= 0 {
		return OffsetLadder[0]
	}
	pxPerNs := float64(widthPx) / float64(span)
	for _, s := range OffsetLadder {
		if float64(s)*pxPerNs >= float64(targetPx) {
			return s
		}
	}
	return OffsetLadder[len(OffsetLadder)-1]
}

// OffsetTicks lists the multiples of step inside [from, to], appending to
// dst[:0]. The count is capped so a bad step cannot allocate without bound.
func OffsetTicks(from, to, step time.Duration, dst []time.Duration) (ticks []time.Duration) {
	ticks = dst[:0]
	if step <= 0 || to < from {
		return ticks
	}
	first := from / step * step
	if first < from {
		first += step
	}
	for t := first; t <= to; t += step {
		ticks = append(ticks, t)
		if len(ticks) > 4096 {
			break
		}
	}
	return ticks
}

// FormatOffset renders an offset at the precision the tick step needs:
// "h:mm:ss" once an hour is on the axis, "m:ss" for whole seconds, and
// fractional seconds with as many decimals as the step has, so neighbouring
// labels never read the same. The fraction is truncated, not rounded: a tick
// at 1.9995 s labelled "2.000" would sit left of the 2 s tick carrying the
// same label.
func FormatOffset(d time.Duration, step time.Duration) (s string) {
	neg := d < 0
	if neg {
		d = -d
	}
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	sec := d / time.Second
	frac := d - sec*time.Second

	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	if h > 0 {
		fmt.Fprintf(&b, "%d:%02d:%02d", h, m, sec)
	} else {
		fmt.Fprintf(&b, "%d:%02d", m, sec)
	}
	if decimals := OffsetDecimals(step); decimals > 0 {
		digits := fmt.Sprintf("%09d", int64(frac))
		b.WriteByte('.')
		b.WriteString(digits[:decimals])
	}
	return b.String()
}

// OffsetDecimals is how many decimal places of a second a step needs: none at
// or above a second, three down to a millisecond, six below.
func OffsetDecimals(step time.Duration) (n int) {
	switch {
	case step >= time.Second:
		return 0
	case step >= time.Millisecond:
		return 3
	default:
		return 6
	}
}

// FormatClock renders a wall-clock instant at the precision the step needs,
// for an offset axis whose zero is a known instant.
func FormatClock(t time.Time, step time.Duration) (s string) {
	switch OffsetDecimals(step) {
	case 0:
		return t.Format("15:04:05")
	case 3:
		return t.Format("15:04:05.000")
	default:
		return t.Format("15:04:05.000000")
	}
}
