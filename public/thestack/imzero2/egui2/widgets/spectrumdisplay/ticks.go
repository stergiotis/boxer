package spectrumdisplay

import (
	"math"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/math/numerical/finddivisions"
)

// AxisUnitE selects engineering-unit formatting for an axis.
type AxisUnitE uint8

const (
	AxisUnitHertz   AxisUnitE = iota // Hz -> Hz/kHz/MHz/GHz/THz
	AxisUnitDecibel                  // bare dB numbers (the unit goes in the axis caption)
	AxisUnitSeconds                  // s -> s/ms/µs/ns
	AxisUnitGeneric                  // compact numeric, no SI suffix
)

// defaultDesiredTicks is the tick-count target when AxisSpec.DesiredTicks is unset.
const defaultDesiredTicks = 6

// AxisTicks computes tick positions (in data space) and engineering-formatted labels
// for the axis, picking nice-number positions via finddivisions.Heckbert (clean
// 1/2/5×10ⁿ steps). It is pure (no GUI) so it is unit-tested directly. A degenerate
// range (max <= min) yields the two endpoints.
func AxisTicks(a AxisSpec) (positions []float64, labels []string) {
	desired := a.DesiredTicks
	if desired < 2 {
		desired = defaultDesiredTicks
	}
	lo, hi := a.Min, a.Max
	mag := math.Max(math.Abs(lo), math.Abs(hi))
	if !(hi > lo) {
		return []float64{lo, hi}, []string{engFormat(lo, mag, a.Unit), engFormat(hi, mag, a.Unit)}
	}
	if axis, err := finddivisions.Heckbert(lo, hi, desired); err == nil && len(axis.TickValues) > 0 {
		positions = axis.TickValues
	} else {
		positions = []float64{lo, hi}
	}
	labels = make([]string, len(positions))
	for i, v := range positions {
		labels[i] = engFormat(v, mag, a.Unit)
	}
	return
}

// engFormat renders v under an SI scale chosen from the axis magnitude, so every
// tick on one axis shares a single suffix (e.g. "868.6  869.6  870.6 MHz" rather than
// a mix). dB and generic axes carry no suffix — the unit, if any, is drawn once as the
// axis caption.
func engFormat(v, mag float64, unit AxisUnitE) string {
	switch unit {
	case AxisUnitHertz:
		div, suf := siScaleUp(mag, hertzSuffixes)
		return trimNum(v/div) + " " + suf
	case AxisUnitSeconds:
		div, suf := siScaleDown(mag, secondSuffixes)
		return trimNum(v/div) + " " + suf
	default: // AxisUnitDecibel, AxisUnitGeneric
		return trimNum(v)
	}
}

var (
	hertzSuffixes  = []string{"Hz", "kHz", "MHz", "GHz", "THz"}
	secondSuffixes = []string{"s", "ms", "µs", "ns"}
)

// siScaleUp returns the (divisor, suffix) whose 1000ˣ band contains mag, stepping up
// from the base unit. Saturates at the last suffix.
func siScaleUp(mag float64, suffixes []string) (float64, string) {
	div := 1.0
	for i := range suffixes {
		if mag < div*1000 || i == len(suffixes)-1 {
			return div, suffixes[i]
		}
		div *= 1000
	}
	return div, suffixes[len(suffixes)-1]
}

// siScaleDown returns the (divisor, suffix) whose 1000⁻ˣ band contains mag, stepping
// down from the base unit. Saturates at the last suffix.
func siScaleDown(mag float64, suffixes []string) (float64, string) {
	div := 1.0
	for i := range suffixes {
		if mag >= div || i == len(suffixes)-1 {
			return div, suffixes[i]
		}
		div /= 1000
	}
	return div, suffixes[len(suffixes)-1]
}

// trimNum formats v compactly: an integer when integral, otherwise up to three
// decimals with trailing zeros (and any dangling dot) trimmed.
func trimNum(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	s := strconv.FormatFloat(v, 'f', 3, 64)
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}
