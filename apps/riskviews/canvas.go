package riskviews

import (
	"math"
	"strings"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// canvas is the small painter-lane helper every hand-drawn view shares: it
// numbers the hit regions a view declares beside its marks, and on finish
// emits the PaintCanvas that drains the frame's paint commands. A mark's
// tooltip is a hit region (c.PaintHitRect / c.PaintHitCircle): the host
// hit-tests it, shows the tooltip at the pointer above the page — not clipped
// by the canvas — and gives the mark an accessibility node named by its
// title.
//
// Must be used inside an IdScope unique to the view: the canvas id is the
// literal "cv" under it, and the regions are numbered under it.
type canvas struct {
	ids  *c.WidgetIdStack
	w, h float32
	n    uint64
	p    palette
}

func newCanvas(ids *c.WidgetIdStack, w, h float32, p palette) *canvas {
	return &canvas{ids: ids, w: w, h: h, p: p}
}

func (cv *canvas) next() *c.WidgetIdStack {
	cv.n++
	return cv.ids.PrepareSeq(cv.n)
}

// tipCircle declares a circular mark's tooltip: title first, then the detail
// lines. Regions declared later win, matching SVG's paint order where the
// topmost mark gets the pointer; 2 px of slack keeps small dots easy to hit.
func (cv *canvas) tipCircle(cx, cy, r float32, title string, detail ...string) {
	c.PaintHitCircle(cv.next(), cx, cy, r+2).Label(title).Tooltip(strings.Join(detail, "\n")).Send()
}

func (cv *canvas) tipRect(x0, y0, x1, y1 float32, title string, detail ...string) {
	c.PaintHitRect(cv.next(), x0, y0, x1, y1).Label(title).Tooltip(strings.Join(detail, "\n")).Send()
}

// finish emits the canvas.
func (cv *canvas) finish() {
	c.PaintCanvas(cv.ids.PrepareStr("cv"), cv.w, cv.h).
		Background(cv.p.bg).
		Sense(false, false, true).
		Send()
}

// PaintText anchors: horizontal 0 left / 1 centre / 2 right; vertical 0 top
// / 1 centre / 2 bottom. The bindings carry them as bare uint8s.
const (
	anchorStart  uint8 = 0
	anchorMiddle uint8 = 1
	anchorEnd    uint8 = 2
	anchorTop    uint8 = 0
	anchorCenter uint8 = 1
	anchorBottom uint8 = 2
)

func text(x, y float32, ah, av uint8, s string, fs float32, col color.Color) {
	c.PaintText(x, y, ah, av, s, fs, col).Send()
}

func mono(x, y float32, ah, av uint8, s string, fs float32, col color.Color) {
	c.PaintText(x, y, ah, av, s, fs, col).Monospace().Send()
}

func line(x0, y0, x1, y1 float32, col color.Color, w float32) {
	c.PaintLine(x0, y0, x1, y1, col, w).Send()
}

// linScale / logScale mirror the prototype's lin() / logs().
func linScale(d0, d1 float64, r0, r1 float32) func(float64) float32 {
	return func(v float64) float32 { return r0 + float32((v-d0)/(d1-d0))*(r1-r0) }
}

func logScale(d0, d1 float64, r0, r1 float32) func(float64) float32 {
	a, b := math.Log(d0), math.Log(d1)
	return func(v float64) float32 { return r0 + float32((math.Log(v)-a)/(b-a))*(r1-r0) }
}

// statusDot paints a filled status disc with its Phosphor glyph — status is
// never colour alone.
func statusDot(cx, cy, r float32, s StatusE) {
	c.PaintCircleFilled(cx, cy, r, statusFill(s)).Send()
	c.PaintText(cx, cy, anchorMiddle, anchorCenter, statusGlyph(s), r*1.25, tok(statusTone(s).TextOnFill())).Send()
}
