// Package gauge renders a read-only radial dial: one scalar value mapped onto
// a bounded [min,max] range, drawn as a ~270° needle dial with optional
// colored zones, ticks, and a center value readout. It is the observability
// "single scalar judged against thresholds" widget (cf. metricsoverlay /
// runtimestatus / taskmonitor) — not progress-over-time (a sparkline) and not
// a bare number. See ADR-0068.
//
// It is painted on the egui2 painter substrate (the treemap / colorscale
// idiom): a sequence of Paint* commands in canvas-relative coordinates,
// flushed into an inline canvas with PaintCanvas. The painter has no native
// arc or filled-polygon primitive, so each zone band and the unzoned track is
// a thick stroked PaintPolyline sampled along the arc; the needle, hub, ticks,
// and text are lines / a circle / PaintText.
//
// Every color, type size, and stroke width comes from the IDS design system
// (styletokens) — nothing is hardcoded. Zones are semantic styletokens.Tone
// values, each carrying a Label so color is never the sole encoding channel
// (ADR-0031 §SD5). The widget is read-only and stateless: Render takes the
// value by copy, keeps nothing across frames, and mutates nothing.
//
// Usage follows the distsummary value-receiver idiom — a New(idPrefix)
// constructor, fluent copy-returning setters, and Render(idGen, value):
//
//	gauge.New("cpu").Range(0, 100).Suffix("%").Label("CPU").
//	    Zones(gauge.TrafficLight(0, 100)...).
//	    Render(ids.PrepareSeq(seq), 72)
package gauge

import (
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// FormatFunc converts the gauge value into the center readout string (before
// the optional Suffix is appended). The default ([defaultFormat]) prints an
// integer when the value is integral and one decimal otherwise. Override via
// [Renderer.Format] for units or domain-specific precision.
type FormatFunc func(v float64) string

// SizeE is a density-scaled diameter preset (the badge SizeSm/Md/Lg idiom).
// The concrete diameters live in [diameterFor]; an explicit [Renderer.Diameter]
// overrides the preset (ADR-0068 §SD4).
type SizeE uint8

const (
	SizeSm SizeE = iota
	SizeMd
	SizeLg
)

// ZoneModeE selects how [Zone] From/To are interpreted.
type ZoneModeE uint8

const (
	// ZoneAbsolute (default) reads Zone.From/To as values on the [min,max]
	// scale.
	ZoneAbsolute ZoneModeE = iota
	// ZonePercentage reads Zone.From/To as fractions in [0,1] of the
	// [min,max] span (the Grafana "Percentage" threshold mode).
	ZonePercentage
)

// Default sweep — the field-standard 270° arc (ECharts default): 0° at three
// o'clock, counter-clockwise positive, so the dial opens at the bottom with
// min at lower-left and max at lower-right (ADR-0068 §SD3).
const (
	defaultStartDeg float32 = 225
	defaultEndDeg   float32 = -45
)

// Zone is a colored qualitative band over a sub-range of the scale. Tone is an
// IDS semantic role (resolved to a color at paint time); Label is the
// redundant text encoding surfaced for accessibility (ADR-0031 §SD5).
type Zone struct {
	From, To float64
	Tone     styletokens.Tone
	Label    string
}

// Renderer is the configured gauge. Values are immutable after construction;
// fluent setters return modified copies. The widget is stateless across
// frames — no package-level instance map — because a read-only dial keeps
// nothing between frames.
type Renderer struct {
	idPrefix string

	min, max float64
	startDeg float32
	endDeg   float32

	size     SizeE
	diameter float32 // explicit override in logical points; 0 = derive from size

	zones    []Zone
	zoneMode ZoneModeE

	majorTicks int
	minorTicks int
	showTicks  bool

	label             string
	formatFunc        FormatFunc
	suffix            string
	showValue         bool
	needleFollowsZone bool

	density styletokens.DensityE
}

// New returns a gauge with documented defaults: range 0..100, the 270° sweep,
// SizeMd, ticks shown, value shown, the humanizing default formatter, and the
// active IDS density. idPrefix scopes the canvas id; distinct instances on one
// idGen should use distinct prefixes.
func New(idPrefix string) (inst Renderer) {
	inst = Renderer{
		idPrefix:   idPrefix,
		min:        0,
		max:        100,
		startDeg:   defaultStartDeg,
		endDeg:     defaultEndDeg,
		size:       SizeMd,
		showTicks:  true,
		showValue:  true,
		formatFunc: defaultFormat,
		density:    styletokens.DensityFromEnv(),
	}
	return
}

// Range sets the scale bounds. A degenerate range (max <= min) parks the
// needle at the start.
func (inst Renderer) Range(min, max float64) (out Renderer) {
	inst.min, inst.max = min, max
	out = inst
	return
}

// Sweep sets the arc start/end angles in degrees (0° = three o'clock,
// counter-clockwise positive). Defaults to 225 → -45 (a 270° bottom-gap dial).
func (inst Renderer) Sweep(startDeg, endDeg float32) (out Renderer) {
	inst.startDeg, inst.endDeg = startDeg, endDeg
	out = inst
	return
}

// Size selects a density-scaled diameter preset. Ignored when Diameter is set.
func (inst Renderer) Size(s SizeE) (out Renderer) {
	inst.size = s
	out = inst
	return
}

// Diameter overrides the size preset with an explicit diameter in logical
// points. A non-positive value clears the override (back to the Size preset).
func (inst Renderer) Diameter(px float32) (out Renderer) {
	if px < 0 {
		px = 0
	}
	inst.diameter = px
	out = inst
	return
}

// Zones sets the colored bands. Empty (default) draws a single neutral track.
func (inst Renderer) Zones(z ...Zone) (out Renderer) {
	inst.zones = z
	out = inst
	return
}

// ZoneMode selects absolute vs percentage interpretation of zone bounds.
func (inst Renderer) ZoneMode(m ZoneModeE) (out Renderer) {
	inst.zoneMode = m
	out = inst
	return
}

// Ticks sets the number of major tick marks (including both ends) and the
// number of minor subdivisions between adjacent majors. major < 2 falls back
// to the derived default; minor < 0 is treated as 0.
func (inst Renderer) Ticks(major, minor int) (out Renderer) {
	if minor < 0 {
		minor = 0
	}
	inst.majorTicks, inst.minorTicks = major, minor
	out = inst
	return
}

// ShowTicks toggles the tick marks and tick labels.
func (inst Renderer) ShowTicks(b bool) (out Renderer) {
	inst.showTicks = b
	out = inst
	return
}

// Label sets the metric name drawn under the dial.
func (inst Renderer) Label(s string) (out Renderer) {
	inst.label = s
	out = inst
	return
}

// Format sets the value formatter. A nil argument is a no-op (keeps the
// current formatter) so callers cannot accidentally clear it.
func (inst Renderer) Format(fn FormatFunc) (out Renderer) {
	if fn != nil {
		inst.formatFunc = fn
	}
	out = inst
	return
}

// Suffix sets a string appended to the formatted readout (e.g. "%", " ms").
func (inst Renderer) Suffix(s string) (out Renderer) {
	inst.suffix = s
	out = inst
	return
}

// ShowValue toggles the center value readout. Disabling it removes the only
// textual encoding of the value; keep it on for accessibility-critical
// surfaces (ADR-0031 §SD5).
func (inst Renderer) ShowValue(b bool) (out Renderer) {
	inst.showValue = b
	out = inst
	return
}

// NeedleFollowsZone colors the needle with the active zone's tone (color by
// value) instead of the neutral foreground. Default off.
func (inst Renderer) NeedleFollowsZone(b bool) (out Renderer) {
	inst.needleFollowsZone = b
	out = inst
	return
}

// TrafficLight returns three equal Success/Warning/Error bands across
// [min,max], each carrying a default label so the classic dial stays
// WCAG-safe (color is not the sole signal). Low reads as "ok"; pass reversed
// zones explicitly when high is the good end.
func TrafficLight(min, max float64) []Zone {
	third := (max - min) / 3
	return []Zone{
		{From: min, To: min + third, Tone: styletokens.ToneSuccess, Label: "ok"},
		{From: min + third, To: min + 2*third, Tone: styletokens.ToneWarning, Label: "warn"},
		{From: min + 2*third, To: max, Tone: styletokens.ToneError, Label: "critical"},
	}
}

// Render draws the dial for value at the current layout cursor, allocating a
// square canvas sized by the Size preset (or the Diameter override). It
// consumes idGen.Derive() exactly once. The needle clamps to the sweep; the
// readout shows the true value.
func (inst Renderer) Render(idGen c.WidgetIdCreatorI, value float64) {
	callId := idGen.Derive()
	d := inst.resolveDiameter()
	if d <= 0 {
		return
	}

	cx := d / 2
	cy := d / 2
	pad := d * padFrac
	r := cx - pad
	if r <= 0 {
		return
	}
	bandT := r * bandThicknessFrac
	zoneR := r - bandT/2 // band centerline; outer edge sits at r

	zones := resolveZones(inst.zones, inst.zoneMode, inst.min, inst.max)
	inst.paintBands(cx, cy, zoneR, bandT, zones)
	if inst.showTicks {
		inst.paintTicks(cx, cy, zoneR-bandT/2, r)
	}
	inst.paintNeedleHub(cx, cy, r, bandT, value, zones)
	inst.paintValue(cx, cy, r, value)
	// Metric label below the dial, anchored to the canvas bottom so it never
	// overlaps the readout or the arc ends (even at the small size preset).
	if inst.label != "" {
		_, labelFont := inst.fonts()
		c.PaintText(cx, d-d*labelBottomInsetFrac, anchorCenter, anchorBottom, inst.label, labelFont,
			color.Hex(styletokens.NeutralTextSecondary.AsHex())).Send()
	}

	canvasID := c.MakeAbsoluteIdStr(inst.idPrefix + "#" + strconv.FormatUint(callId, 16) + "-gauge")
	c.PaintCanvas(canvasID, d, d).Send()
}

// paintBands draws the zone arcs (or a single neutral track when no zones are
// configured) as thick stroked polylines.
func (inst Renderer) paintBands(cx, cy, zoneR, bandT float32, zones []Zone) {
	// Decorative neutral brim, drawn first (behind the range): a slightly wider
	// arc so a neutral bezel frames the colored band on both edges — and serves
	// as the track across any uncovered part of the sweep.
	bxs, bys := arcPoints(cx, cy, zoneR, inst.startDeg, inst.endDeg, arcStepDeg)
	c.PaintPolyline(bxs, bys, color.Hex(styletokens.NeutralBorderDefault.AsHex()), bandT+2*bandT*brimWidthFrac).Send()

	if len(zones) == 0 {
		c.PaintPolyline(bxs, bys, color.Hex(styletokens.NeutralBorderFaint.AsHex()), bandT).Send()
		return
	}
	for _, z := range zones {
		a0 := valueToAngle(z.From, inst.min, inst.max, inst.startDeg, inst.endDeg)
		a1 := valueToAngle(z.To, inst.min, inst.max, inst.startDeg, inst.endDeg)
		xs, ys := arcPoints(cx, cy, zoneR, a0, a1, arcStepDeg)
		c.PaintPolyline(xs, ys, color.Hex(z.Tone.Fill().AsHex()), bandT).Send()
	}
}

// paintTicks draws major tick marks (with labels) and minor tick marks,
// radially inside the band. innerR is the band's inner edge; r is the outer
// radius (used only to scale tick lengths).
func (inst Renderer) paintTicks(cx, cy, innerR, r float32) {
	majors, minors := tickValues(inst.min, inst.max, inst.majorTicks, inst.minorTicks)
	minorCol := color.Hex(styletokens.NeutralBorderFaint.AsHex())
	majorCol := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	labelCol := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	_, labelFont := inst.fonts()

	for _, mv := range minors {
		a := valueToAngle(mv, inst.min, inst.max, inst.startDeg, inst.endDeg)
		x0, y0 := polar(cx, cy, innerR, a)
		x1, y1 := polar(cx, cy, innerR-r*minorTickLenFrac, a)
		c.PaintLine(x0, y0, x1, y1, minorCol, styletokens.StrokeHair).Send()
	}
	labelR := innerR - r*majorTickLenFrac - r*tickLabelGapFrac
	for i, mv := range majors {
		a := valueToAngle(mv, inst.min, inst.max, inst.startDeg, inst.endDeg)
		x0, y0 := polar(cx, cy, innerR, a)
		x1, y1 := polar(cx, cy, innerR-r*majorTickLenFrac, a)
		c.PaintLine(x0, y0, x1, y1, majorCol, styletokens.StrokeRegular).Send()
		// Drop the min/max endpoint labels when a centre readout is shown: they
		// sit at the bottom corners — exactly where the wide readout lives — so
		// on medium dials the readout crowds them. The arc ends and zones still
		// imply the range, and the endpoint tick marks remain.
		if inst.showValue && (i == 0 || i == len(majors)-1) {
			continue
		}
		lx, ly := polar(cx, cy, labelR, a)
		c.PaintText(lx, ly, anchorCenter, anchorCenter, inst.formatFunc(mv), labelFont, labelCol).Send()
	}
}

// paintNeedleHub draws the needle from the hub to the value angle, then the
// hub cap on top.
func (inst Renderer) paintNeedleHub(cx, cy, r, bandT float32, value float64, zones []Zone) {
	a := valueToAngle(value, inst.min, inst.max, inst.startDeg, inst.endDeg)
	tipR := r - bandT - r*needleGapFrac
	tx, ty := polar(cx, cy, tipR, a)

	needleCol := color.Hex(styletokens.NeutralTextPrimary.AsHex())
	if inst.needleFollowsZone {
		if z, ok := zoneAt(value, zones); ok {
			needleCol = color.Hex(z.Tone.Fill().AsHex())
		}
	}
	c.PaintLine(cx, cy, tx, ty, needleCol, styletokens.StrokeStrong).Send()

	hubR := r * hubRFrac
	c.PaintCircleFilled(cx, cy, hubR, color.Hex(styletokens.NeutralBgSurface.AsHex())).Send()
	c.PaintCircleStroke(cx, cy, hubR, color.Hex(styletokens.NeutralBorderDefault.AsHex()), styletokens.StrokeRegular).Send()
}

// paintValue draws the center value readout inside the dial. The metric label
// is drawn separately at the canvas bottom (see Render) so it cannot overlap
// the readout or arc ends on small dials.
func (inst Renderer) paintValue(cx, cy, r float32, value float64) {
	if !inst.showValue {
		return
	}
	valueFont, _ := inst.fonts()
	c.PaintText(cx, cy+r*readoutYFrac, anchorCenter, anchorCenter, inst.formatValue(value), valueFont,
		color.Hex(styletokens.NeutralTextExtreme.AsHex())).Send()
}
