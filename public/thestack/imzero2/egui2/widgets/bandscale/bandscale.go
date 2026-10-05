// Package bandscale is an immediate-mode widget (ADR-0267) that draws a
// linear scale of named bands with markers on it: an ordinal ladder —
// severity, authority, readiness — where a position is judged against the
// band it falls in. ADR-0280 decided it, for the scale of what a chat's
// model may do.
//
// The bands are equal in width and read left to right. Each carries a label
// and a semantic tone, and each marker a label, so colour is never the only
// channel (ADR-0031 §SD5): a band is named under the bar, a marker above
// it. Two markers on one scale compare two positions — a limit and a current
// value — and a hollow marker reads as the limit.
//
// It is painted on the painter substrate into one inline canvas, like gauge
// and colorscale, with every colour, type size and stroke from the design
// system. It is read-only and keeps nothing between frames.
//
//	bandscale.Render(bandscale.Input{
//	    Ids: ids, ScopeKey: "authority",
//	    Bands: []bandscale.Band{{Label: "read", Tone: styletokens.ToneSuccess}, …},
//	    Markers: []bandscale.Marker{{Position: 0.8, Label: "may", Hollow: true}, {Position: 0.3, Label: "now"}},
//	})
package bandscale

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// ShadeE picks which of a tone's fills a band takes, so two neighbours of
// one tone still read as two bands.
type ShadeE uint8

const (
	// ShadeFill is the tone's own fill.
	ShadeFill ShadeE = iota
	// ShadeSoft is its subtle fill and ShadeStrong its strong one.
	ShadeSoft
	ShadeStrong
)

// Band is one band of the scale.
type Band struct {
	// Label names the band under the bar.
	Label string
	// Tone colours it, in the Shade given.
	Tone  styletokens.Tone
	Shade ShadeE
}

// fill is the band's colour.
func (inst Band) fill() (rgba styletokens.RGBA8) {
	switch inst.Shade {
	case ShadeSoft:
		return inst.Tone.Soft()
	case ShadeStrong:
		return inst.Tone.Strong()
	}
	return inst.Tone.Fill()
}

// Marker is one position on the scale.
type Marker struct {
	// Position is the place along the whole scale, in [0, 1]; a value
	// outside is drawn at the nearer end.
	Position float64
	// Label names the marker above the bar.
	Label string
	// Hollow draws the pointer as an outline: the limit of two markers.
	Hollow bool
}

// Input is one frame of the scale.
type Input struct {
	// Ids is the host's id stack and ScopeKey this scale's scope under it;
	// empty is "bandscale".
	Ids      *c.WidgetIdStack
	ScopeKey string

	// Bands are the scale, left to right; Markers the positions on it.
	Bands   []Band
	Markers []Marker

	// Width is the scale's width in points; zero is 320.
	Width float32
	// Compact draws the bar and the pointers alone, without labels, for a
	// toolbar row: the host names the position beside it.
	Compact bool
}

// Result is what a frame drew.
type Result struct {
	// Width and Height are the canvas drawn.
	Width, Height float32
	// Err is set when the input cannot be drawn: no id stack, no band.
	Err error
}

const (
	defaultScopeKey         = "bandscale"
	defaultWidth    float32 = 320

	anchorMin    uint8 = 0
	anchorCenter uint8 = 1
	anchorMax    uint8 = 2
)

// layout is a frame's geometry, in canvas points.
type layout struct {
	w, h       float32
	barTop     float32
	barH       float32
	pointer    float32 // the pointer's half width and height
	markerRows int
	rowH       float32
	font       float32
	bandGap    float32
	labelsY    float32
}

func (in Input) scopeKey() string {
	if in.ScopeKey == "" {
		return defaultScopeKey
	}
	return in.ScopeKey
}

func (in Input) layout() (l layout) {
	d := styletokens.ActiveDensity()
	l.w = in.Width
	if l.w <= 0 {
		l.w = defaultWidth
	}
	l.font = styletokens.ScaledPt(styletokens.CaptionPt, d)
	l.pointer = styletokens.ScaledPt(4, d)
	l.bandGap = styletokens.StrokeHair
	if in.Compact {
		l.barH = styletokens.ScaledPt(8, d)
		l.barTop = l.pointer + 1
		l.h = l.barTop + l.barH + 1
		return
	}
	l.barH = styletokens.ScaledPt(12, d)
	l.rowH = l.font + 3
	l.markerRows = len(in.Markers)
	l.barTop = float32(l.markerRows)*l.rowH + l.pointer + 2
	l.labelsY = l.barTop + l.barH + 3
	l.h = l.labelsY + l.font + 2
	return
}

// x is the canvas x of a position, kept inside the bar.
func (inst layout) x(position float64) (x float32) {
	p := min(max(position, 0), 1)
	return min(max(float32(p)*inst.w, inst.pointer), inst.w-inst.pointer)
}

// Render draws the scale.
func Render(in Input) (res Result) {
	switch {
	case in.Ids == nil:
		res.Err = eh.Errorf("bandscale: no id stack")
		return
	case len(in.Bands) == 0:
		c.Label("bandscale: no bands").Send()
		res.Err = eh.Errorf("bandscale: no bands")
		return
	}
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		l := in.layout()
		in.paintBands(l)
		in.paintMarkers(l)
		c.PaintCanvas(in.Ids.PrepareStr("canvas"), l.w, l.h).Send()
		res.Width, res.Height = l.w, l.h
	}
	return
}

func (in Input) paintBands(l layout) {
	n := float32(len(in.Bands))
	bw := l.w / n
	labelCol := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	for i, b := range in.Bands {
		x0 := float32(i) * bw
		x1 := x0 + bw
		if i < len(in.Bands)-1 {
			x1 -= l.bandGap
		}
		c.PaintRectFilled(x0, l.barTop, x1, l.barTop+l.barH, 2, color.Hex(b.fill().AsHex())).Send()
		if !in.Compact && b.Label != "" {
			c.PaintText(x0+bw/2, l.labelsY, anchorCenter, anchorMin, b.Label, l.font, labelCol).Send()
		}
	}
}

func (in Input) paintMarkers(l layout) {
	ink := color.Hex(styletokens.NeutralTextExtreme.AsHex())
	ground := color.Hex(styletokens.NeutralBgSurface.AsHex())
	for i, m := range in.Markers {
		x := l.x(m.Position)
		tipY := l.barTop
		topY := tipY - l.pointer - 1
		// The line through the bar marks the place; a light casing keeps it
		// legible over any tone.
		c.PaintLine(x, tipY, x, l.barTop+l.barH, ground, styletokens.StrokeRegular+2).Send()
		c.PaintLine(x, tipY, x, l.barTop+l.barH, ink, styletokens.StrokeRegular).Send()
		xs := []float32{x - l.pointer, x + l.pointer, x}
		ys := []float32{topY, topY, tipY}
		if m.Hollow {
			c.PaintPolygonFilled(xs, ys, ground).Send()
			c.PaintPolyline(append(xs, xs[0]), append(ys, ys[0]), ink, styletokens.StrokeHair).Send()
		} else {
			c.PaintPolygonFilled(xs, ys, ink).Send()
		}
		if in.Compact || m.Label == "" {
			continue
		}
		// One row per marker, so two near each other never overprint; the
		// label leans away from the nearer end.
		y := float32(i) * l.rowH
		switch {
		case x < l.w*0.2:
			c.PaintText(max(x-l.pointer, 0), y, anchorMin, anchorMin, m.Label, l.font, ink).Send()
		case x > l.w*0.8:
			c.PaintText(min(x+l.pointer, l.w), y, anchorMax, anchorMin, m.Label, l.font, ink).Send()
		default:
			c.PaintText(x, y, anchorCenter, anchorMin, m.Label, l.font, ink).Send()
		}
		if i < l.markerRows-1 {
			// A leader from the label's row down to the pointer.
			c.PaintLine(x, y+l.rowH-1, x, topY, color.Hex(styletokens.NeutralBorderDefault.AsHex()), styletokens.StrokeHair).Send()
		}
	}
}
