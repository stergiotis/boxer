package gauge

import (
	"math"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
)

// Layout fractions (of the dial radius r, except padFrac which is of the
// diameter). Tuned for the 270° dial; calibrate against a real consumer per
// ADR-0068 §SD4. anchorCenter is the PaintText center anchor (0=left/top,
// 1=center, 2=right/bottom).
const (
	padFrac           float32 = 0.07 // outer margin (of diameter) so the band + brim are not clipped
	bandThicknessFrac float32 = 0.13 // zone/track band thickness
	brimWidthFrac     float32 = 0.35 // decorative neutral brim width per edge, as a fraction of the band thickness
	arcStepDeg        float32 = 2.0  // arc polyline sampling step
	majorTickLenFrac  float32 = 0.10
	minorTickLenFrac  float32 = 0.06
	tickLabelGapFrac  float32 = 0.10
	needleGapFrac     float32 = 0.04 // needle tip stops this far short of the band
	// Rhomboid needle silhouette (a neutral monochrome kite — the value is
	// encoded by angle and shape, never color): tip at tipR, widest at the
	// shoulders just past the hub, a short counterweight tail behind it.
	needleHalfWidthFrac float32 = 0.05 // half-width at the shoulders (widest point)
	needleShoulderFrac  float32 = 0.13 // shoulder (widest point) distance ahead of the hub
	needleTailFrac      float32 = 0.14 // counterweight tail length behind the hub
	hubRFrac            float32 = 0.08
	readoutYFrac        float32 = 0.30 // value readout, below the hub (inside the dial) — kept high enough to clear the lower-left/right endpoint tick labels

	// labelBottomInsetFrac places the metric label's baseline this far (of the
	// canvas height) above the bottom edge — below the dial, clear of the
	// readout and arc ends at every size.
	labelBottomInsetFrac float32 = 0.03

	anchorCenter      uint8 = 1
	anchorBottom      uint8 = 2
	defaultMajorTicks int   = 5
)

// resolveDiameter returns the explicit Diameter override when set, else the
// density-scaled Size preset.
func (inst Renderer) resolveDiameter() float32 {
	if inst.diameter > 0 {
		return inst.diameter
	}
	return diameterFor(inst.size, inst.density)
}

// fonts returns the readout and the tick/label font sizes for the configured
// size and density, drawn from the IDS type scale (ADR-0030).
func (inst Renderer) fonts() (value, label float32) {
	d := inst.density
	switch inst.size {
	case SizeSm:
		return styletokens.ScaledPt(styletokens.HeadingPt, d), styletokens.ScaledPt(styletokens.MicroPt, d)
	default: // SizeMd, SizeLg
		return styletokens.ScaledPt(styletokens.DisplayPt, d), styletokens.ScaledPt(styletokens.CaptionPt, d)
	}
}

// formatValue applies the formatter (defaulting if nil) and appends the suffix.
func (inst Renderer) formatValue(v float64) string {
	f := inst.formatFunc
	if f == nil {
		f = defaultFormat
	}
	return f(v) + inst.suffix
}

// diameterFor maps a Size preset and density to a diameter in logical points
// (ADR-0068 §SD4). Out-of-range inputs fall back to SizeMd / Standard.
func diameterFor(size SizeE, density styletokens.DensityE) float32 {
	ladder := [3][3]float32{
		{88, 96, 112},   // SizeSm: tight / standard / roomy
		{132, 144, 168}, // SizeMd
		{192, 208, 240}, // SizeLg
	}
	si := int(size)
	if si < 0 || si >= len(ladder) {
		si = int(SizeMd)
	}
	di := int(density)
	if di < 0 || di >= len(ladder[si]) {
		di = int(styletokens.DensityStandard)
	}
	return ladder[si][di]
}

// valueToAngle maps v on [min,max] to an angle on [startDeg,endDeg], clamping
// v into range so the needle never leaves the sweep. A degenerate range parks
// at startDeg.
func valueToAngle(v, lo, hi float64, startDeg, endDeg float32) float32 {
	if hi <= lo {
		return startDeg
	}
	t := min(1, max(0, (v-lo)/(hi-lo)))
	return startDeg + float32(t)*(endDeg-startDeg)
}

// polar converts an angle (degrees; 0° = three o'clock, counter-clockwise
// positive) at radius r about (cx,cy) to a canvas point. Screen y is down, so
// the sine is negated to keep counter-clockwise positive.
func polar(cx, cy, r, angleDeg float32) (x, y float32) {
	a := float64(angleDeg) * math.Pi / 180
	x = cx + r*float32(math.Cos(a))
	y = cy - r*float32(math.Sin(a))
	return
}

// needlePolygon builds the rhomboid needle silhouette pointing along angleDeg
// (same angle convention as polar): a long thin kite with its tip at tipR, the
// two shoulders (widest point) at shoulderR offset ±halfWidth perpendicular to
// the needle axis, and a short counterweight tail at tailR behind the hub.
// Returned as parallel xs/ys for a single PaintPolygonFilled. The shape is the
// sole value cue the needle carries — it is painted in neutral ink, never a
// zone color.
func needlePolygon(cx, cy, angleDeg, tipR, shoulderR, tailR, halfWidth float32) (xs, ys []float32) {
	a := float64(angleDeg) * math.Pi / 180
	ca := float32(math.Cos(a))
	sa := float32(math.Sin(a))
	// Outward unit vector (ca,-sa); its perpendicular (sa,ca) spreads the shoulders.
	tipX, tipY := cx+tipR*ca, cy-tipR*sa
	tailX, tailY := cx-tailR*ca, cy+tailR*sa
	leftX, leftY := cx+shoulderR*ca+halfWidth*sa, cy-shoulderR*sa+halfWidth*ca
	rightX, rightY := cx+shoulderR*ca-halfWidth*sa, cy-shoulderR*sa-halfWidth*ca
	xs = []float32{tipX, leftX, tailX, rightX}
	ys = []float32{tipY, leftY, tailY, rightY}
	return
}

// arcPoints samples the arc from startDeg to endDeg (inclusive) at ~stepDeg
// resolution into parallel x/y slices for a stroked PaintPolyline.
func arcPoints(cx, cy, r, startDeg, endDeg, stepDeg float32) (xs, ys []float32) {
	if stepDeg <= 0 {
		stepDeg = arcStepDeg
	}
	span := endDeg - startDeg
	n := max(1, int(math.Ceil(math.Abs(float64(span))/float64(stepDeg))))
	xs = make([]float32, 0, n+1)
	ys = make([]float32, 0, n+1)
	for i := 0; i <= n; i++ {
		a := startDeg + span*float32(i)/float32(n)
		x, y := polar(cx, cy, r, a)
		xs = append(xs, x)
		ys = append(ys, y)
	}
	return
}

// resolveZones converts zones to absolute [min,max] bounds, expanding
// percentage-mode fractions. Returns nil for an empty input.
func resolveZones(zones []Zone, mode ZoneModeE, lo, hi float64) []Zone {
	if len(zones) == 0 {
		return nil
	}
	out := make([]Zone, 0, len(zones))
	span := hi - lo
	for _, z := range zones {
		if mode == ZonePercentage {
			z.From = lo + z.From*span
			z.To = lo + z.To*span
		}
		out = append(out, z)
	}
	return out
}

// zoneAt returns the first zone whose (absolute) range contains v.
func zoneAt(v float64, zones []Zone) (Zone, bool) {
	for _, z := range zones {
		lo, hi := z.From, z.To
		if lo > hi {
			lo, hi = hi, lo
		}
		if v >= lo && v <= hi {
			return z, true
		}
	}
	return Zone{}, false
}

// tickValues returns major tick values (evenly spaced, both ends inclusive)
// and the minor subdivisions between them. major < 2 falls back to the
// derived default. Even spacing (not "nice" rounding) is the v1 behavior;
// callers wanting round numbers set an explicit range and major count.
func tickValues(lo, hi float64, major, minor int) (majors, minors []float64) {
	if major < 2 {
		major = defaultMajorTicks
	}
	span := hi - lo
	for i := 0; i < major; i++ {
		t := float64(i) / float64(major-1)
		majors = append(majors, lo+t*span)
	}
	if minor > 0 {
		for i := 0; i+1 < len(majors); i++ {
			a, b := majors[i], majors[i+1]
			for j := 1; j <= minor; j++ {
				f := float64(j) / float64(minor+1)
				minors = append(minors, a+f*(b-a))
			}
		}
	}
	return
}

// defaultFormat prints an integer when v is integral, else one decimal place.
// Compact for the common 0..100 / percentage gauges; override via Format for
// units or wider ranges.
func defaultFormat(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'f', 1, 64)
}
