package implot

import (
	"math"
	"strconv"
	"time"
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
	// AxisFlagsFollow keeps refitting the axis to the data until the user
	// pans or zooms it; a double-click (or context-menu) fit resumes
	// following. The egui_plot auto-bounds model, carried over for the
	// monitoring panels whose data is a rolling window — not an upstream
	// ImPlot flag.
	AxisFlagsFollow AxisFlags = 1 << 3
	// AxisFlagsNoPan stops gestures translating this axis: a drag moves
	// only the other one. The axis still zooms, about its own centre.
	AxisFlagsNoPan AxisFlags = 1 << 4
	// AxisFlagsNoZoom stops gestures scaling this axis: the wheel, box-zoom
	// and a double-click fit leave its span alone, so a caller that derives
	// the span from the plot-area height keeps a constant pixels-per-unit.
	// Panning still slides the window, which is how a depth axis scrolls.
	AxisFlagsNoZoom AxisFlags = 1 << 5
	// AxisFlagsLock is NoPan|NoZoom: the range is the caller's alone, and
	// no gesture changes it — upstream's ImPlotAxisFlags_Lock, which locks
	// both ends together.
	AxisFlagsLock = AxisFlagsNoPan | AxisFlagsNoZoom
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

// ScaleE selects an axis scale, mirroring ImPlotScale. Time is a linear
// transform with the time locator/formatter; Log10 and SymLog change the
// value↔pixel mapping itself.
type ScaleE uint8

const (
	ScaleLinear ScaleE = iota
	// ScaleTime treats values as Unix seconds (UTC labels, ImPlot's
	// default; a UseLocalTime knob can follow when needed).
	ScaleTime
	ScaleLog10
	// ScaleSymLog is ImPlot's asinh-based symmetric log: linear near
	// zero, logarithmic in both tails.
	ScaleSymLog
)

// scaleFwd / scaleInv are the plot-value ↔ transformed-space maps; nil
// means identity (linear and time). Log10 guards non-positive input the
// way ImPlot's TransformForward_Log10 does.
func scaleFwd(s ScaleE) func(float64) float64 {
	switch s {
	case ScaleLog10:
		return func(v float64) float64 {
			if v <= 0 {
				v = 5e-324
			}
			return math.Log10(v)
		}
	case ScaleSymLog:
		return func(v float64) float64 { return 2 * math.Asinh(v/2) }
	}
	return nil
}

func scaleInv(s ScaleE) func(float64) float64 {
	switch s {
	case ScaleLog10:
		return func(t float64) float64 { return math.Pow(10, t) }
	case ScaleSymLog:
		return func(t float64) float64 { return 2 * math.Sinh(t/2) }
	}
	return nil
}

// sanitizeScaled is sanitize plus the per-scale domain constraint: a log
// axis must stay strictly positive (ImPlot constrains the same way).
func sanitizeScaled(r Range, s ScaleE) Range {
	r = r.sanitize()
	if s == ScaleLog10 {
		if r.Max <= 0 {
			return Range{1e-3, 1}
		}
		if r.Min <= 0 {
			r.Min = r.Max * 1e-4
		}
	}
	return r
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
//
// The major walk indexes (first + i·step) rather than accumulating
// (major += step), and settles each value with snapTickDecimal before it is
// labelled. Accumulation is not what a compensated sum would fix here: first
// and step are each the nearest double to a decimal with no binary form, so
// on an axis crossing zero their exact sum at the crossing is -5.55e-17
// rather than 0 — which formatTick's scientific branch then prints. Upstream
// guards the same case by snapping the straddling major to zero ("combat zero
// formatting issues"); indexing costs no more and also keeps the tick one
// step below zero, which that guard swallows on a tiny-magnitude axis.
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
	// The accumulating walk's bound, resolved to a count up front — an indexed
	// walk needs one. A non-finite first or step (reachable only for a range up
	// against the float64 ceiling) yields a non-finite count, where the walk
	// would otherwise never advance past its first value.
	count := math.Floor((rng.Max + 0.5*step - first) / step)
	if math.IsNaN(count) || math.IsInf(count, 0) {
		return dst
	}
	if count > maxLocatedMajors {
		count = maxLocatedMajors
	}
	for i := 0; i <= int(count); i++ {
		major := snapTickDecimal(first+float64(i)*step, step)
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

// maxLocatedMajors bounds the located walk. nMajor caps at 12, so a legitimate
// axis stays far below this; the cap is only here so a degenerate range cannot
// spin the loop.
const maxLocatedMajors = 64

// snapTickDecimal settles one located major. Indexing bounds the walk's error
// at a single rounding instead of letting it accumulate, but it does not
// remove it — 3·0.4 is 1.2000000000000002 — so the value is rounded onto a
// decimal grid ten decades below step's own decade: fine enough to leave every
// digit a reader could care about untouched (the grid sits far below the step's
// last significant digit, and far below one pixel on any axis), coarse enough
// to erase the binary noise. The scale factor is a power of ten, so both the
// multiply and the divide are single correctly rounded operations; outside the
// exactly-representable range the value is returned as it came rather than
// snapped onto a grid that is itself inexact.
//
// The zero case is the one that shows: a major only drift away from zero must
// become zero, or formatTick's exact-equality guard misses it and the
// scientific branch prints the drift. Snapping also normalises the negative
// zero the rounding can produce. Both steps follow
// finddivisions.GenerateTicksRobust, which answers the same question for the
// standalone axis solvers.
func snapTickDecimal(v float64, step float64) float64 {
	if math.Abs(v) < step*1e-10 {
		return 0
	}
	g := 10 - math.Floor(math.Log10(step))
	if g < 0 || g > 22 {
		return v
	}
	scale := math.Pow(10, g)
	sv := v * scale
	if math.IsInf(sv, 0) {
		return v
	}
	return math.Round(sv) / scale
}

// filterTicksInRange copies the caller-supplied SetupAxisTicks ticks
// that fall inside the (sanitized) visible range into dst.
func filterTicksInRange(rng Range, src []tick, dst []tick) []tick {
	dst = dst[:0]
	rng = rng.sanitize()
	for _, t := range src {
		if rng.Contains(t.value) {
			dst = append(dst, t)
		}
	}
	return dst
}

// locateTicksScaled dispatches to the scale's locator. SymLog currently
// uses the default locator on raw values — an approximation recorded in
// the package doc; Log10 and Time have faithful ports.
func locateTicksScaled(rng Range, sizePx float32, scale ScaleE, dst []tick) []tick {
	switch scale {
	case ScaleLog10:
		return locateTicksLog10(rng, dst)
	case ScaleTime:
		return locateTicksTime(rng, sizePx, dst)
	}
	return locateTicks(rng, sizePx, dst)
}

// locateTicksLog10 ports ImPlot's log locator: major ticks at decades
// 10^k with minors at 2..9 × 10^k. Inside a single decade (deep zoom)
// the default locator takes over, as upstream does.
func locateTicksLog10(rng Range, dst []tick) []tick {
	dst = dst[:0]
	rng = sanitizeScaled(rng, ScaleLog10)
	kLo := int(math.Floor(math.Log10(rng.Min)))
	kHi := int(math.Ceil(math.Log10(rng.Max)))
	if kHi-kLo < 1 {
		return locateTicks(rng, 300, dst)
	}
	for k := kLo; k <= kHi; k++ {
		dec := math.Pow(10, float64(k))
		if rng.Contains(dec) {
			dst = append(dst, tick{value: dec, major: true, label: formatLogTick(dec)})
		}
		for m := 2; m <= 9; m++ {
			mv := float64(m) * dec
			if rng.Contains(mv) {
				dst = append(dst, tick{value: mv})
			}
		}
	}
	return dst
}

func formatLogTick(v float64) string {
	if v >= 1e-3 && v < 1e5 {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return strconv.FormatFloat(v, 'g', 2, 64)
}

// timeStep is one rung of the time-unit ladder. Month and year rungs walk
// calendar boundaries instead of fixed seconds.
type timeStep struct {
	sec    float64 // nominal length; calendar rungs approximate
	months int     // 0 = fixed-duration rung
	format string  // Go reference-time layout for tick labels
}

var timeSteps = []timeStep{
	{1, 0, "15:04:05"}, {5, 0, "15:04:05"}, {15, 0, "15:04:05"}, {30, 0, "15:04:05"},
	{60, 0, "15:04"}, {300, 0, "15:04"}, {900, 0, "15:04"}, {1800, 0, "15:04"},
	{3600, 0, "15:04"}, {3 * 3600, 0, "15:04"}, {6 * 3600, 0, "Jan 2 15:04"},
	{12 * 3600, 0, "Jan 2 15:04"}, {86400, 0, "Jan 2"}, {7 * 86400, 0, "Jan 2"},
	{2629800, 1, "Jan '06"}, {3 * 2629800, 3, "Jan '06"}, {6 * 2629800, 6, "Jan '06"},
	{31557600, 12, "2006"}, {10 * 31557600, 120, "2006"}, {100 * 31557600, 1200, "2006"},
}

// locateTicksTime ports the shape of ImPlot's time locator: pick the
// smallest unit whose tick count fits the pixel density, snap the first
// tick to that unit's boundary (UTC), and walk. Minor ticks and the
// two-level context labels of upstream are deferred (package doc).
func locateTicksTime(rng Range, sizePx float32, dst []tick) []tick {
	dst = dst[:0]
	rng = rng.sanitize()
	nMax := int(math.Round(float64(sizePx) / 110.0))
	if nMax < 2 {
		nMax = 2
	}
	span := rng.Size()
	step := timeSteps[len(timeSteps)-1]
	for _, s := range timeSteps {
		if span/s.sec <= float64(nMax) {
			step = s
			break
		}
	}
	tMin := time.Unix(int64(math.Floor(rng.Min)), 0).UTC()
	var cur time.Time
	if step.months > 0 {
		monthsSinceZero := (tMin.Year()*12 + int(tMin.Month()) - 1) / step.months * step.months
		cur = time.Date(monthsSinceZero/12, time.Month(monthsSinceZero%12+1), 1, 0, 0, 0, 0, time.UTC)
		for len(dst) < 64 {
			v := float64(cur.Unix())
			if v > rng.Max {
				break
			}
			if rng.Contains(v) {
				dst = append(dst, tick{value: v, major: true, label: cur.Format(step.format)})
			}
			cur = cur.AddDate(0, step.months, 0)
		}
		return dst
	}
	d := time.Duration(step.sec * float64(time.Second))
	cur = tMin.Truncate(d)
	for len(dst) < 128 {
		v := float64(cur.Unix())
		if v > rng.Max {
			break
		}
		if rng.Contains(v) {
			dst = append(dst, tick{value: v, major: true, label: cur.Format(step.format)})
		}
		cur = cur.Add(d)
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
// human range, scientific outside it. Both forms cap their digits, so float
// walk error never reaches a printed one — 0.30000000000000004 renders as 0.3.
//
// The exception is a value that is only drift away from zero: the guard below
// tests exact equality, so a near-zero value falls through to the scientific
// branch and prints its own drift. Deciding that belongs to the caller with
// the step in hand (snapTickDecimal) rather than here — prec cannot stand in
// for it, since stepPrecision saturates at 12, and tools.go shares this
// formatter for data coordinates at a fixed prec, where a value below the last
// printed digit is small rather than zero.
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

// transform maps plot space to canvas-relative pixels through the axis
// scales: value → transformed space (fwd) → affine → pixels. Built once
// per frame; float64 until the final cast (SD4). y is inverted:
// plot-space up is pixel-space down. Nil fwd/inv means identity.
type transform struct {
	fwdX, invX   func(float64) float64
	fwdY, invY   func(float64) float64
	txmin, tymin float64 // transformed-space minimums
	sx, sy       float64 // px per transformed unit
	px0, py0     float64 // plot-area origin (canvas px)
	plotW, plotH float64 // plot-area size (canvas px)
}

func newTransform(xr, yr Range, xs, ys ScaleE, areaX, areaY, areaW, areaH float32) transform {
	xr = sanitizeScaled(xr, xs)
	yr = sanitizeScaled(yr, ys)
	t := transform{
		fwdX: scaleFwd(xs), invX: scaleInv(xs),
		fwdY: scaleFwd(ys), invY: scaleInv(ys),
		px0: float64(areaX), py0: float64(areaY),
		plotW: float64(areaW), plotH: float64(areaH),
	}
	tx0, tx1 := apply(t.fwdX, xr.Min), apply(t.fwdX, xr.Max)
	ty0, ty1 := apply(t.fwdY, yr.Min), apply(t.fwdY, yr.Max)
	t.txmin, t.tymin = tx0, ty0
	t.sx = float64(areaW) / (tx1 - tx0)
	t.sy = float64(areaH) / (ty1 - ty0)
	return t
}

func apply(f func(float64) float64, v float64) float64 {
	if f == nil {
		return v
	}
	return f(v)
}

func (t transform) pxX(v float64) float32 {
	return float32(t.px0 + (apply(t.fwdX, v)-t.txmin)*t.sx)
}

func (t transform) pxY(v float64) float32 {
	return float32(t.py0 + t.plotH - (apply(t.fwdY, v)-t.tymin)*t.sy)
}

// plotX / plotY invert the transform (canvas px → plot space), used by
// interactions: pan deltas, zoom anchors, box-zoom corners, hover
// readout. Because they run through the scale's inverse, pixel-space
// gesture math stays correct on any monotone scale.
func (t transform) plotX(px float32) float64 {
	return apply(t.invX, t.txmin+(float64(px)-t.px0)/t.sx)
}

func (t transform) plotY(px float32) float64 {
	return apply(t.invY, t.tymin+(t.py0+t.plotH-float64(px))/t.sy)
}

func (t transform) valid() bool {
	return t.sx != 0 && t.sy != 0 && !math.IsNaN(t.sx) && !math.IsNaN(t.sy) &&
		!math.IsInf(t.sx, 0) && !math.IsInf(t.sy, 0)
}
