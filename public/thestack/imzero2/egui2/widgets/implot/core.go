package implot

import (
	"math"
	"strconv"
)

// Range is a closed interval in plot space. Plot space is float64
// throughout (ADR-0149 SD4); pixels appear only at emission.
type Range struct {
	Min float64
	Max float64
}

func (r Range) Size() float64 { return r.Max - r.Min }

func (r Range) Contains(v float64) bool { return v >= r.Min && v <= r.Max }

// Clamp returns v limited to the range.
func (r Range) Clamp(v float64) float64 {
	if v < r.Min {
		return r.Min
	}
	if v > r.Max {
		return r.Max
	}
	return v
}

// sanitize enforces a non-empty, finite, ordered range so the transform
// stays invertible: NaN/Inf endpoints reset to the unit range, inverted
// endpoints swap, and a degenerate range is widened around its center —
// ImPlot's constraint behavior, condensed.
func (r Range) sanitize() Range {
	if math.IsNaN(r.Min) || math.IsInf(r.Min, 0) || math.IsNaN(r.Max) || math.IsInf(r.Max, 0) {
		return Range{0, 1}
	}
	if r.Min > r.Max {
		r.Min, r.Max = r.Max, r.Min
	}
	if r.Size() < minRangeSize {
		c := (r.Min + r.Max) / 2
		return Range{c - minRangeSize/2, c + minRangeSize/2}
	}
	return r
}

const minRangeSize = 1e-12

// AxisFlags configures one axis at Setup time.
type AxisFlags uint32

const (
	AxisFlagsNone AxisFlags = 0
	// AxisFlagsAutoFit refits the axis to the frame's data every frame.
	AxisFlagsAutoFit AxisFlags = 1 << 0
	// AxisFlagsNoGrid suppresses the grid lines for this axis.
	AxisFlagsNoGrid AxisFlags = 1 << 1
	// AxisFlagsNoTickLabels suppresses the tick labels (marks remain).
	AxisFlagsNoTickLabels AxisFlags = 1 << 2
)

// Cond controls when SetupAxisLimits applies, mirroring ImPlot's ImPlotCond.
type Cond uint8

const (
	// CondOnce applies the limits only the first time this plot id is seen.
	CondOnce Cond = iota
	// CondAlways applies the limits every frame (the axis is then not
	// user-navigable).
	CondAlways
)

// tick is one located axis tick in plot space, with its formatted label.
type tick struct {
	value float64
	major bool
	label string // empty for minor ticks
}

// niceNum is Heckbert's nice-number rounding, the same helper ImPlot's
// default locator is built on: the largest "nice" step (1/2/5 × 10^k) not
// exceeding x (round=false), or the nicest step closest to x (round=true).
func niceNum(x float64, round bool) float64 {
	expv := math.Floor(math.Log10(x))
	f := x / math.Pow(10, expv)
	var nf float64
	if round {
		switch {
		case f < 1.5:
			nf = 1
		case f < 3:
			nf = 2
		case f < 7:
			nf = 5
		default:
			nf = 10
		}
	} else {
		switch {
		case f <= 1:
			nf = 1
		case f <= 2:
			nf = 2
		case f <= 5:
			nf = 5
		default:
			nf = 10
		}
	}
	return nf * math.Pow(10, expv)
}

// locateTicks ports ImPlot's default locator: pick a major count from the
// pixel density, snap the step to a nice number, walk from the first
// snapped major below the range, and fill three minor ticks per major
// interval. Ticks outside the (sanitized) range are dropped.
func locateTicks(rng Range, sizePx float32, dst []tick) []tick {
	dst = dst[:0]
	rng = rng.sanitize()
	nMajor := int(math.Round(float64(sizePx) / 90.0))
	if nMajor < 2 {
		nMajor = 2
	}
	if nMajor > 12 {
		nMajor = 12
	}
	const nMinor = 4 // subdivisions per major interval, ImPlot's default of 10 is dense for our label sizes
	niceRange := niceNum(rng.Size()*0.99, false)
	step := niceNum(niceRange/float64(nMajor-1), true)
	first := math.Floor(rng.Min/step) * step
	prec := stepPrecision(step)
	minorStep := step / nMinor
	for major := first; major < rng.Max+0.5*step; major += step {
		if rng.Contains(major) {
			dst = append(dst, tick{value: major, major: true, label: formatTick(major, prec)})
		}
		for k := 1; k < nMinor; k++ {
			mv := major + float64(k)*minorStep
			if rng.Contains(mv) {
				dst = append(dst, tick{value: mv})
			}
		}
	}
	return dst
}

// stepPrecision is the number of fractional digits needed to print
// multiples of step exactly (0 for steps ≥ 1).
func stepPrecision(step float64) int {
	p := int(math.Ceil(-math.Log10(step)))
	if p < 0 {
		return 0
	}
	if p > 12 {
		return 12
	}
	return p
}

// formatTick renders a tick value the way ImPlot's default formatter
// ("%g"-family) reads: fixed decimals at the step's precision in the
// human range, scientific outside it. The value is snapped to its own
// precision first so accumulated float walk error never prints
// (0.30000000000000004 renders as 0.3).
func formatTick(v float64, prec int) string {
	if v == 0 {
		return "0"
	}
	av := math.Abs(v)
	if av >= 1e5 || av < 1e-4 {
		return strconv.FormatFloat(v, 'g', 4, 64)
	}
	return strconv.FormatFloat(v, 'f', prec, 64)
}

// transform maps plot space to canvas-relative pixels. Built once per
// frame from the sanitized ranges and the plot-area rect; float64 until
// the final cast (SD4). y is inverted: plot-space up is pixel-space down.
type transform struct {
	xmin, ymin   float64
	sx, sy       float64 // px per plot unit
	px0, py0     float64 // plot-area origin (canvas px)
	plotW, plotH float64 // plot-area size (canvas px)
}

func newTransform(xr, yr Range, areaX, areaY, areaW, areaH float32) transform {
	xr = xr.sanitize()
	yr = yr.sanitize()
	return transform{
		xmin: xr.Min, ymin: yr.Min,
		sx:  float64(areaW) / xr.Size(),
		sy:  float64(areaH) / yr.Size(),
		px0: float64(areaX), py0: float64(areaY),
		plotW: float64(areaW), plotH: float64(areaH),
	}
}

func (t transform) pxX(v float64) float32 { return float32(t.px0 + (v-t.xmin)*t.sx) }
func (t transform) pxY(v float64) float32 {
	return float32(t.py0 + t.plotH - (v-t.ymin)*t.sy)
}

// plotX / plotY invert the transform (canvas px → plot space), used by
// interactions: pan deltas, zoom anchors, box-zoom corners, hover readout.
func (t transform) plotX(px float32) float64 { return t.xmin + (float64(px)-t.px0)/t.sx }
func (t transform) plotY(px float32) float64 {
	return t.ymin + (t.py0+t.plotH-float64(px))/t.sy
}

func (t transform) valid() bool {
	return t.sx != 0 && t.sy != 0 && !math.IsNaN(t.sx) && !math.IsNaN(t.sy)
}
