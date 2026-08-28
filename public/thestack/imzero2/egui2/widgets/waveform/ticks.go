package waveform

import (
	"fmt"
	"strings"
	"time"
)

// durationLadder is the tick-step ladder for an offset axis: the steps a
// listener reads naturally, from a microsecond at sample-level zoom to a day.
// It is not the calendar ladder in timeticks — an offset has no months.
var durationLadder = []time.Duration{
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

// pickDurationStep returns the smallest ladder step that puts at least
// targetPx pixels between ticks across a span drawn over widthPx pixels. A
// span too wide for the ladder falls back to the largest step.
func pickDurationStep(span time.Duration, widthPx float32, targetPx float32) (step time.Duration) {
	if span <= 0 || widthPx <= 0 {
		return durationLadder[0]
	}
	pxPerNs := float64(widthPx) / float64(span)
	for _, s := range durationLadder {
		if float64(s)*pxPerNs >= float64(targetPx) {
			return s
		}
	}
	return durationLadder[len(durationLadder)-1]
}

// durationTicks lists the multiples of step inside [from, to].
func durationTicks(from, to, step time.Duration, dst []time.Duration) (ticks []time.Duration) {
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

// formatOffset renders an offset from the start of the track at the
// precision the tick step needs: "h:mm:ss" once an hour is on the axis,
// "m:ss" for whole seconds, and fractional seconds with as many decimals as
// the step has, so neighbouring labels never read the same.
func formatOffset(d time.Duration, step time.Duration) (s string) {
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
	decimals := fracDecimals(step)
	if decimals > 0 {
		// Truncate, never round: a tick at 1.9995 s labelled "2.000" would
		// sit left of the 2 s tick that carries the same label.
		digits := fmt.Sprintf("%09d", int64(frac))
		b.WriteByte('.')
		b.WriteString(digits[:decimals])
	}
	return b.String()
}

// fracDecimals is how many decimal places of a second a step needs: none at
// or above a second, three down to a millisecond, six down to a microsecond.
func fracDecimals(step time.Duration) (n int) {
	switch {
	case step >= time.Second:
		return 0
	case step >= time.Millisecond:
		return 3
	default:
		return 6
	}
}

// formatClock renders a wall-clock instant for an absolute time base at the
// precision the step needs.
func formatClock(t time.Time, step time.Duration) (s string) {
	switch fracDecimals(step) {
	case 0:
		return t.Format("15:04:05")
	case 3:
		return t.Format("15:04:05.000")
	default:
		return t.Format("15:04:05.000000")
	}
}
