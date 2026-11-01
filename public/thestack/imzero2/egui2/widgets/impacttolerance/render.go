package impacttolerance

import (
	"math"
	"strconv"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// Point names one estimate: a service and a scenario, by index. -1 in either
// is no point.
type Point struct {
	Service, Scenario int
}

// NoPoint is the Point that names nothing.
var NoPoint = Point{Service: -1, Scenario: -1}

// Valid reports whether p names an estimate.
func (inst Point) Valid() bool { return inst.Service >= 0 && inst.Scenario >= 0 }

// State is the host-owned selection. The zero value selects nothing.
type State struct {
	// sel is the selected point with both indices shifted by one, so the
	// zero value selects none.
	sel Point
}

// SetSelected selects p; NoPoint clears the selection.
func (inst *State) SetSelected(p Point) {
	if !p.Valid() {
		inst.sel = Point{}
		return
	}
	inst.sel = Point{Service: p.Service + 1, Scenario: p.Scenario + 1}
}

// Selected is the selected point, NoPoint when none.
func (inst *State) Selected() Point {
	if inst.sel.Service == 0 {
		return NoPoint
	}
	return Point{Service: inst.sel.Service - 1, Scenario: inst.sel.Scenario - 1}
}

// Input is one frame of the chart.
type Input struct {
	// Ids is the host's id stack and ScopeKey this chart's scope under it;
	// empty is "impacttolerance".
	Ids      *c.WidgetIdStack
	ScopeKey string

	Model *Model
	State *State

	// Width is the canvas width in points; zero is 760. The height follows
	// from the rows.
	Width float32
	// RowHeight is one service's row in points; zero is 62.
	RowHeight float32
	// MinHours and MaxHours fix the time axis; zero derives it from the
	// model, from the duration rung below the smallest value to the one
	// above three times the largest.
	MinHours, MaxHours float64
	// HideLegend drops the scenario legend above the rows, for a host that
	// draws its own.
	HideLegend bool
}

// Result is what a frame drew and what the pointer did, one frame behind
// like every canvas readback.
type Result struct {
	// Hovered is the estimate under the pointer, Clicked the one clicked;
	// NoPoint for none.
	Hovered, Clicked Point
	// Width and Height are the canvas drawn.
	Width, Height float32
	// Err is set when the input cannot be drawn; a short message is drawn in
	// its place.
	Err error
}

const (
	defaultScopeKey          = "impacttolerance"
	defaultWidth     float32 = 760
	defaultRowHeight float32 = 62
	narrowWidth      float32 = 480

	anchorStart  uint8 = 0
	anchorMiddle uint8 = 1
	anchorEnd    uint8 = 2
	anchorTop    uint8 = 0
	anchorCenter uint8 = 1
	anchorBottom uint8 = 2

	pointR float32 = 5.5
)

func (in Input) scopeKey() string {
	if in.ScopeKey == "" {
		return defaultScopeKey
	}
	return in.ScopeKey
}

// Render draws the chart.
func Render(in Input) (res Result) {
	res.Hovered, res.Clicked = NoPoint, NoPoint
	switch {
	case in.Ids == nil:
		res.Err = eh.Errorf("impacttolerance: no id stack")
		return
	case in.State == nil:
		res.Err = eh.Errorf("impacttolerance: no State")
	}
	if res.Err == nil {
		res.Err = in.Model.Validate()
	}
	if res.Err == nil && (in.MinHours < 0 || in.MaxHours < 0 || (in.MaxHours > 0 && in.MaxHours <= in.MinHours)) {
		res.Err = eh.Errorf("impacttolerance: MinHours %v / MaxHours %v is not a range", in.MinHours, in.MaxHours)
	}
	if res.Err != nil {
		for rt := range c.RichTextLabel(res.Err.Error()) {
			rt.Small().Weak()
		}
		return
	}
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		f := newFrame(in)
		res.Hovered, res.Clicked = f.declareRegions()
		f.paint(res.Hovered)
		c.PaintCanvas(in.Ids.PrepareStr("canvas"), f.w, f.h).
			Background(f.pal.bg).
			Sense(true, false, true).
			Send()
		res.Width, res.Height = f.w, f.h
	}
	return
}

// palette is a frame's colours, every one from the design system.
type palette struct {
	bg, area, grid, ink, ink2, breach color.Color
	scenario                          []color.Color
}

func tok(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }

func tokA(t styletokens.RGBA8, a uint8) color.Color { return color.Hex(t.AsHex()&^0xff | uint32(a)) }

// newPalette resolves the colours. Scenario k takes qualitative slot k, by
// position: the cycle decides what colour that is.
func newPalette(scenarios int) (p palette) {
	p.bg = tok(styletokens.NeutralBgFaint)
	p.area = tok(styletokens.NeutralBgSurface)
	p.grid = tokA(styletokens.NeutralBorderFaint, 0x50)
	p.ink = tok(styletokens.NeutralTextPrimary)
	p.ink2 = tok(styletokens.NeutralTextSecondary)
	p.breach = tokA(styletokens.ToneError.Fill(), 0x24)
	p.scenario = make([]color.Color, scenarios)
	for k := range scenarios {
		p.scenario[k] = tok(styletokens.QualitativeCycle(k))
	}
	return
}

// legendItem is one entry of the legend row, placed.
type legendItem struct {
	x, y     float32
	scenario int // -1 for the tolerance line
	label    string
}

// frame is one frame's geometry, built before anything is emitted.
type frame struct {
	in               Input
	pal              palette
	w, h             float32
	rowH             float32
	ml, mt, pw       float32
	lo, hi           float64
	legend           []legendItem
	ticks            []float64
	points           [][]pointPos // [service][scenario]
	marks            []mark
	nameFS, detailFS float32
}

type pointPos struct {
	x, y float32
	ok   bool
}

const (
	legendRowH float32 = 20
	legendSw   float32 = 16
	legendGap  float32 = 18
)

func newFrame(in Input) (f *frame) {
	m := in.Model
	f = &frame{in: in, w: in.Width, rowH: in.RowHeight,
		nameFS: styletokens.BodyPt, detailFS: styletokens.CaptionPt}
	if f.w <= 0 {
		f.w = defaultWidth
	}
	if f.rowH <= 0 {
		f.rowH = defaultRowHeight
	}
	f.pal = newPalette(len(m.Scenarios))
	f.lo, f.hi = in.MinHours, in.MaxHours
	if f.lo <= 0 || f.hi <= 0 {
		dlo, dhi := m.hourRange()
		alo, ahi := axisRange(dlo, dhi)
		if f.lo <= 0 {
			f.lo = alo
		}
		if f.hi <= 0 {
			f.hi = ahi
		}
	}

	legendH := float32(0)
	if !in.HideLegend {
		legendH = f.layoutLegend() + 8
	}
	labelW := float32(0)
	for s, name := range m.Services {
		labelW = max(labelW, implot.EstimateTextWidth(name, f.nameFS), implot.EstimateTextWidth(f.summary(s), f.detailFS))
	}
	f.ml = min(max(labelW+16, 96), f.w*0.4)
	mr := float32(18)
	f.mt = legendH + 8
	f.pw = max(f.w-f.ml-mr, 60)
	f.h = f.mt + float32(len(m.Services))*f.rowH + 40

	narrow := f.w < narrowWidth
	minGap := float32(44)
	if narrow {
		minGap = 60
	}
	last := float32(math.Inf(-1))
	for _, r := range hourLadder {
		if r < f.lo*0.999 || r > f.hi*1.001 {
			continue
		}
		if x := f.px(r); x-last >= minGap {
			f.ticks = append(f.ticks, r)
			last = x
		}
	}

	nk := len(m.Scenarios)
	spread := min(9, f.rowH*0.5/float32(max(nk, 1)))
	f.points = make([][]pointPos, len(m.Services))
	for s := range m.Services {
		yc := f.rowY(s)
		f.points[s] = make([]pointPos, nk)
		tol := m.ToleranceHours[s]
		over, of := m.breaches(s)
		tx := f.px(tol)
		f.marks = append(f.marks, mark{x0: tx - 4, y0: yc - f.band(), x1: tx + 4, y1: yc + f.band(), point: NoPoint, service: s,
			label: m.Services[s] + " · tolerance",
			tip:   "Impact tolerance " + FormatHours(tol) + "\n" + breachText(over, of)})
		for k := range nk {
			v := m.Recovery(s, k)
			if math.IsNaN(v) {
				continue
			}
			p := pointPos{x: f.px(v), y: yc + (float32(k)-float32(nk-1)/2)*spread, ok: true}
			f.points[s][k] = p
			tl := "Tolerance " + FormatHours(tol)
			if v > tol {
				tl += " · breached"
			}
			f.marks = append(f.marks, mark{circle: true, cx: p.x, cy: p.y, r: pointR, point: Point{Service: s, Scenario: k}, service: s,
				label: m.Services[s] + " · " + m.Scenarios[k],
				tip:   "Recovery about " + FormatHours(v) + "\n" + tl})
		}
	}
	return
}

// layoutLegend places the legend entries, wrapping at the canvas width, and
// returns the legend's height.
func (f *frame) layoutLegend() float32 {
	x, y := float32(0), float32(0)
	add := func(scenario int, label string) {
		iw := legendSw + 6 + implot.EstimateTextWidth(label, styletokens.CaptionPt)
		if x > 0 && x+iw > f.w {
			x, y = 0, y+legendRowH
		}
		f.legend = append(f.legend, legendItem{x: x, y: y, scenario: scenario, label: label})
		x += iw + legendGap
	}
	for k, name := range f.in.Model.Scenarios {
		add(k, name)
	}
	add(-1, "Impact tolerance")
	return y + legendRowH
}

func breachText(over, of int) string {
	if over == 0 {
		return "all within"
	}
	return strconv.Itoa(over) + " of " + strconv.Itoa(of) + " breach"
}

func (f *frame) summary(s int) string { return breachText(f.in.Model.breaches(s)) }

// px places a duration on the log axis, clamped to the plot.
func (f *frame) px(h float64) float32 {
	h = math.Min(math.Max(h, f.lo), f.hi)
	return f.ml + float32((math.Log(h)-math.Log(f.lo))/(math.Log(f.hi)-math.Log(f.lo)))*f.pw
}

// rowY is the centre line of service s's row.
func (f *frame) rowY(s int) float32 { return f.mt + float32(s)*f.rowH + f.rowH/2 }

// band is the half height of a row's tolerance band.
func (f *frame) band() float32 { return f.rowH * 0.32 }

func (f *frame) paint(hovered Point) {
	m, p := f.in.Model, f.pal
	if !f.in.HideLegend {
		for _, it := range f.legend {
			cy := it.y + legendRowH/2
			if it.scenario >= 0 {
				c.PaintCircleFilled(it.x+5, cy, 4.5, p.scenario[it.scenario]).Send()
			} else {
				c.PaintRectFilled(it.x, cy-1.5, it.x+14, cy+1.5, 2, p.ink).Send()
			}
			text(it.x+legendSw+6, cy, anchorStart, anchorCenter, it.label, styletokens.CaptionPt, p.ink2)
		}
	}
	top, bottom := f.mt, f.mt+float32(len(m.Services))*f.rowH
	right := f.ml + f.pw
	for _, t := range f.ticks {
		x := f.px(t)
		c.PaintLine(x, top, x, bottom, p.grid, 1).Send()
		mono(x, bottom+6, anchorMiddle, anchorTop, FormatHours(t), p.ink2)
	}
	text(f.ml+f.pw/2, f.h-4, anchorMiddle, anchorBottom, "Estimated time to recover (log scale)", styletokens.CaptionPt, p.ink2)
	sel := f.in.State.Selected()
	for s, name := range m.Services {
		yc := f.rowY(s)
		tx := f.px(m.ToleranceHours[s])
		b := f.band()
		c.PaintRectFilled(tx, yc-b, right, yc+b, 0, p.breach).Send()
		c.PaintLine(f.ml, yc, right, yc, p.grid, 1).Send()
		c.PaintLine(tx, yc-b, tx, yc+b, p.ink, 2).Send()
		text(f.ml-10, yc-2, anchorEnd, anchorBottom, name, f.nameFS, p.ink)
		mono(f.ml-10, yc+2, anchorEnd, anchorTop, f.summary(s), p.ink2)
		for k, pt := range f.points[s] {
			if !pt.ok {
				continue
			}
			here := Point{Service: s, Scenario: k}
			if here == sel {
				c.PaintCircleStroke(pt.x, pt.y, pointR+4, p.ink, 2).Send()
			} else if here == hovered {
				c.PaintCircleStroke(pt.x, pt.y, pointR+3, p.ink2, 1.5).Send()
			}
			c.PaintCircleFilled(pt.x, pt.y, pointR, p.scenario[k]).Send()
			c.PaintCircleStroke(pt.x, pt.y, pointR, p.bg, 2).Send()
		}
	}
}

func text(x, y float32, ah, av uint8, s string, fs float32, col color.Color) {
	c.PaintText(x, y, ah, av, s, fs, col).Send()
}

// mono is the face numbers are set in: ticks and the breach summary.
func mono(x, y float32, ah, av uint8, s string, col color.Color) {
	c.PaintText(x, y, ah, av, s, styletokens.CaptionPt, col).Monospace().Send()
}
