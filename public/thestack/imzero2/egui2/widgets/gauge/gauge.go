// Package gauge is an immediate-mode widget (ADR-0267) that renders a
// read-only radial dial: one scalar value mapped onto a bounded [min,max]
// range, drawn as a ~270° needle dial with optional colored zones, ticks, and
// a center value readout. It is the observability "single scalar judged
// against thresholds" widget (cf. metricsoverlay / runtimestatus /
// taskmonitor) — not progress-over-time (a sparkline) and not a bare number.
// See ADR-0068.
//
// It is painted on the egui2 painter substrate (the treemap / colorscale
// idiom): a sequence of Paint* commands in canvas-relative coordinates,
// flushed into an inline canvas with PaintCanvas. The painter has no native
// arc primitive, so each zone band and the unzoned track is a thick stroked
// PaintPolyline sampled along the arc; the needle is a filled rhomboid polygon
// (PaintPolygonFilled), and the hub, ticks, and text are a circle / lines /
// PaintText.
//
// Every color, type size, and stroke width comes from the IDS design system
// (styletokens) — nothing is hardcoded. Zones are semantic styletokens.Tone
// values, each carrying a Label so color is never the sole encoding channel
// (ADR-0031 §SD5); the needle itself is a neutral monochrome silhouette,
// encoding the value by its angle alone. The widget is read-only — Render
// takes the value by copy and mutates none of its inputs. The one piece of
// state that outlives a frame is the readout auto-fit measurement (fit.go),
// which the host keeps in a [State] so the center value shrinks to fit the
// dial; without one the fit is an approximation.
//
//	gauge.Render(gauge.Input{
//	    Ids: ids, ScopeKey: "cpu", Value: 72,
//	    Min: 0, Max: 100, Suffix: "%", Label: "CPU",
//	    Zones: gauge.TrafficLight(0, 100), State: &st.cpuFit,
//	})
package gauge

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// FormatFunc converts the gauge value into the center readout string (before
// the optional Suffix is appended). The default ([defaultFormat]) prints an
// integer when the value is integral and one decimal otherwise. Override via
// [Input.Format] for units or domain-specific precision.
type FormatFunc func(v float64) string

// SizeE is a density-scaled diameter preset (the badge SizeSm/Md/Lg idiom).
// The concrete diameters live in [diameterFor]; an explicit [Input.Diameter]
// overrides the preset (ADR-0068 §SD4). The zero value is SizeMd.
type SizeE uint8

const (
	SizeMd SizeE = iota
	SizeSm
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
	defaultMin      float64 = 0
	defaultMax      float64 = 100
	defaultScopeKey         = "gauge"
)

// Zone is a colored qualitative band over a sub-range of the scale. Tone is an
// IDS semantic role (resolved to a color at paint time); Label is the
// redundant text encoding surfaced for accessibility (ADR-0031 §SD5).
type Zone struct {
	From, To float64
	Tone     styletokens.Tone
	Label    string
}

// Input is one frame's dial. Every zero value is the documented default, so
// a dial needs only Ids, ScopeKey and Value.
type Input struct {
	// Ids is the host's widget id stack. Render opens its own IdScope under
	// it, so two dials in one frame need only differ in ScopeKey.
	Ids *c.WidgetIdStack
	// ScopeKey names this dial within the host's id space; empty uses "gauge".
	ScopeKey string

	// Value is the scalar the needle points at; the readout shows it as is,
	// the needle clamps to the sweep.
	Value float64
	// Min and Max bound the scale. Both zero takes 0..100; a degenerate range
	// (Max <= Min) parks the needle at the start.
	Min, Max float64
	// StartDeg and EndDeg are the arc's start and end angles in degrees (0° =
	// three o'clock, counter-clockwise positive). Both zero takes 225 → -45,
	// a 270° bottom-gap dial.
	StartDeg, EndDeg float32

	// Size is the density-scaled diameter preset; the zero value is SizeMd.
	// Ignored when Diameter is set.
	Size SizeE
	// Diameter overrides the Size preset with an explicit diameter in logical
	// points when positive.
	Diameter float32

	// Zones are the colored bands; none draws a single neutral track.
	Zones []Zone
	// ZoneMode selects absolute (the default) or percentage zone bounds.
	ZoneMode ZoneModeE

	// MajorTicks is the number of major tick marks including both ends; below
	// 2 the count is derived from the range. MinorTicks is the number of
	// subdivisions between adjacent majors; 0 draws none.
	MajorTicks, MinorTicks int
	// HideTicks drops the tick marks and tick labels.
	HideTicks bool

	// Label is the metric name drawn under the dial.
	Label string
	// Format renders the value for the readout and the tick labels; nil takes
	// the default (an integer when integral, else one decimal).
	Format FormatFunc
	// Suffix is appended to the formatted readout (e.g. "%", " ms").
	Suffix string
	// HideValue drops the center value readout. That removes the only textual
	// encoding of the value; keep it shown for accessibility-critical
	// surfaces (ADR-0031 §SD5).
	HideValue bool

	// State is the host-owned readout-fit memo, one per dial the host draws.
	// Optional: without it the readout's width is estimated every frame
	// instead of measured, so a long readout may sit a little wide or narrow.
	State *State
}

// Result is what one Render reports.
type Result struct {
	// Diameter is the side of the square canvas the dial took, in logical
	// points; zero when nothing was drawn.
	Diameter float32
}

// State is the host-owned cross-frame state of one dial: the readout's
// measured width, which lands one frame after it is asked for (fit.go). The
// zero value is usable.
type State struct {
	fit fitState
}

// dial is one frame's Input with every default resolved; the paint helpers
// hang off it.
type dial struct {
	min, max float64
	startDeg float32
	endDeg   float32

	size     SizeE
	diameter float32

	zones    []Zone
	zoneMode ZoneModeE

	majorTicks int
	minorTicks int
	showTicks  bool

	label      string
	formatFunc FormatFunc
	suffix     string
	showValue  bool

	density styletokens.DensityE
}

// resolve fills the Input's defaults. The density preset is re-read every
// frame because it is runtime-switchable (Layout ▸ Density).
func (in Input) resolve() (d dial) {
	d = dial{
		min:        in.Min,
		max:        in.Max,
		startDeg:   in.StartDeg,
		endDeg:     in.EndDeg,
		size:       in.Size,
		diameter:   max(in.Diameter, 0),
		zones:      in.Zones,
		zoneMode:   in.ZoneMode,
		majorTicks: in.MajorTicks,
		minorTicks: max(in.MinorTicks, 0),
		showTicks:  !in.HideTicks,
		label:      in.Label,
		formatFunc: in.Format,
		suffix:     in.Suffix,
		showValue:  !in.HideValue,
		density:    styletokens.ActiveDensity(),
	}
	if d.min == 0 && d.max == 0 {
		d.min, d.max = defaultMin, defaultMax
	}
	if d.startDeg == 0 && d.endDeg == 0 {
		d.startDeg, d.endDeg = defaultStartDeg, defaultEndDeg
	}
	if d.formatFunc == nil {
		d.formatFunc = defaultFormat
	}
	return
}

func (in Input) scopeKey() string {
	if in.ScopeKey == "" {
		return defaultScopeKey
	}
	return in.ScopeKey
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

// Render draws the dial at the current layout cursor, allocating a square
// canvas sized by the Size preset (or the Diameter override), inside one
// IdScope under Input.Ids. A nil Ids draws nothing.
func Render(in Input) (res Result) {
	if in.Ids == nil {
		return
	}
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		res = in.resolve().render(in)
	}
	return
}

func (inst dial) render(in Input) (res Result) {
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
	inst.paintNeedleHub(cx, cy, r, bandT, in.Value)
	var fit *fitState
	if in.State != nil {
		fit = &in.State.fit
	}
	inst.paintValue(cx, cy, r, bandT, in.Value, fit, in.Ids.ProbeSeq("readout"))
	// Metric label below the dial, anchored to the canvas bottom so it never
	// overlaps the readout or the arc ends (even at the small size preset).
	if inst.label != "" {
		_, labelFont := inst.fonts()
		c.PaintText(cx, d-d*labelBottomInsetFrac, anchorCenter, anchorBottom, inst.label, labelFont,
			color.Hex(styletokens.NeutralTextSecondary.AsHex())).Send()
	}

	// Drain into the canvas, whose id is relative under the dial's scope.
	c.PaintCanvas(in.Ids.PrepareStr("canvas"), d, d).Send()
	res.Diameter = d
	return
}

// paintBands draws the zone arcs (or a single neutral track when no zones are
// configured) as thick stroked polylines.
func (inst dial) paintBands(cx, cy, zoneR, bandT float32, zones []Zone) {
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

// paintTicks draws major tick marks (labeled — except the min/max endpoints
// when a readout is shown, see below) and minor tick marks, radially inside
// the band. innerR is the band's inner edge; r is the outer radius (used only
// to scale tick lengths).
func (inst dial) paintTicks(cx, cy, innerR, r float32) {
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

// paintNeedleHub draws the rhomboid needle pointing at the value angle, then
// the hub cap on top of its base. The needle is a neutral monochrome silhouette
// (PaintPolygonFilled in NeutralTextPrimary): the value is encoded by angle and
// shape, never by color — the colored zone bands carry the qualitative reading.
func (inst dial) paintNeedleHub(cx, cy, r, bandT float32, value float64) {
	a := valueToAngle(value, inst.min, inst.max, inst.startDeg, inst.endDeg)
	tipR := r - bandT - r*needleGapFrac
	xs, ys := needlePolygon(cx, cy, a, tipR, r*needleShoulderFrac, r*needleTailFrac, r*needleHalfWidthFrac)
	c.PaintPolygonFilled(xs, ys, color.Hex(styletokens.NeutralTextPrimary.AsHex())).Send()

	hubR := r * hubRFrac
	c.PaintCircleFilled(cx, cy, hubR, color.Hex(styletokens.NeutralBgSurface.AsHex())).Send()
	c.PaintCircleStroke(cx, cy, hubR, color.Hex(styletokens.NeutralBorderDefault.AsHex()), styletokens.StrokeRegular).Send()
}

// paintValue draws the center value readout inside the dial, shrinking its font
// so the string fits the dial's inner opening (see fit.go): a wide readout — a
// multi-digit value plus a unit suffix, e.g. "8500 mAh" — at the full display
// size would otherwise overrun the arc and collide with the interior tick
// labels. The metric label is drawn separately at the canvas bottom (see
// render) so it cannot overlap the readout or arc ends on small dials.
func (inst dial) paintValue(cx, cy, r, bandT float32, value float64, fit *fitState, measureId uint64) {
	if !inst.showValue {
		return
	}
	baseFont, _ := inst.fonts()
	text := inst.formatValue(value)
	yOff := r * readoutYFrac
	font := fitReadoutFont(fit, measureId, text, baseFont, readoutAvailWidth(r-bandT, yOff))
	c.PaintText(cx, cy+yOff, anchorCenter, anchorCenter, text, font,
		color.Hex(styletokens.NeutralTextExtreme.AsHex())).Send()
}
