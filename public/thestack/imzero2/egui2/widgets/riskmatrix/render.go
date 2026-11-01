package riskmatrix

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// ModeE is the matrix's reading.
type ModeE uint8

const (
	// ModeBanded is the familiar grid: each risk placed in the cell its
	// interval midpoints fall in, cells shaded by expected loss.
	ModeBanded ModeE = iota
	// ModeContinuous drops the bands: log–log axes, each risk's range as a
	// box, lines of equal expected annual loss.
	ModeContinuous
)

// State is the host-owned part of the matrix that survives a frame: the
// reading, whether inherent ratings are drawn, and the selected risk. The zero
// value is the banded reading with nothing selected. Keep it at one address —
// the inherent toggle binds to it.
type State struct {
	Mode         ModeE
	ShowInherent bool
	// sel is the selected risk's index + 1, so the zero value selects none.
	sel int
}

// SetSelected selects risk i; a negative i clears the selection.
func (inst *State) SetSelected(i int) { inst.sel = max(i, -1) + 1 }

// Selected is the selected risk's index, -1 when none.
func (inst *State) Selected() int { return inst.sel - 1 }

// Input is one frame of the matrix.
type Input struct {
	// Ids is the host's id stack and ScopeKey this matrix's scope under it;
	// empty is "riskmatrix".
	Ids      *c.WidgetIdStack
	ScopeKey string

	Model *Model
	State *State

	// Bands bins the banded reading; the zero value is DefaultBands.
	Bands Bands
	// FormatImpact formats an amount — an impact, an expected loss — for
	// labels and tooltips; nil is FormatCompact, a number with no unit. The
	// widget assumes no unit of its own: the host that knows one says it here.
	FormatImpact func(v float64) string
	// ImpactUnit names the unit on the impact axis's title ("EUR", "h"); empty
	// is a title without one.
	ImpactUnit string

	// Width and Height are the canvas in points; zero is 760 × 470. The
	// control row, when shown, sits above the canvas and is not counted.
	Width, Height float32
	// HideControls drops the reading buttons and the inherent toggle, for a
	// host that sets State itself.
	HideControls bool
	// HideKey drops the key at the right. A canvas narrower than 620 points
	// draws none either way.
	HideKey bool
}

// Result is what a frame drew and what the pointer did, one frame behind
// like every canvas readback.
type Result struct {
	// Hovered is the risk under the pointer, Clicked the one clicked; -1 for
	// none.
	Hovered, Clicked int
	// Width and Height are the canvas drawn.
	Width, Height float32
	// Err is set when the input cannot be drawn; a short message is drawn in
	// its place.
	Err error
}

const (
	defaultScopeKey         = "riskmatrix"
	defaultWidth    float32 = 760
	defaultHeight   float32 = 470
	narrowWidth     float32 = 620

	anchorStart  uint8 = 0
	anchorMiddle uint8 = 1
	anchorEnd    uint8 = 2
	anchorTop    uint8 = 0
	anchorCenter uint8 = 1
	anchorBottom uint8 = 2

	dotR       float32 = 6.5
	keyMinW    float32 = 230
	keyMaxW    float32 = 300
	keyRowH    float32 = 19
	growFactor         = 1.15 // each band 15% larger than the one before, toward the high-risk corner
)

func (in Input) scopeKey() string {
	if in.ScopeKey == "" {
		return defaultScopeKey
	}
	return in.ScopeKey
}

// Render draws the matrix.
func Render(in Input) (res Result) {
	res.Hovered, res.Clicked = -1, -1
	switch {
	case in.Ids == nil:
		res.Err = eh.Errorf("riskmatrix: no id stack")
		return
	case in.State == nil:
		res.Err = eh.Errorf("riskmatrix: no State")
	}
	if res.Err == nil {
		res.Err = in.Model.Validate()
	}
	var bands Bands
	if res.Err == nil {
		bands, res.Err = in.Bands.forModel(in.Model).resolved()
	}
	if res.Err != nil {
		for rt := range c.RichTextLabel(res.Err.Error()) {
			rt.Small().Weak()
		}
		return
	}
	for range c.IdScope(in.Ids.PrepareStr(in.scopeKey())) {
		if !in.HideControls {
			in.renderControls(bands)
		}
		f := newFrame(in, bands)
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

// renderControls is the reading switch and the inherent toggle. The toggle is
// absent when the register keeps no inherent rating — a checkbox that can
// change nothing is noise.
func (in Input) renderControls(bands Bands) {
	st := in.State
	nl, ni := len(bands.LikelihoodEdges)-1, len(bands.ImpactEdges)-1
	banded := "Banded " + num(float64(nl)) + "×" + num(float64(ni))
	for range c.HorizontalTop().KeepIter() {
		if c.Button(in.Ids.PrepareStr("mode-banded"), c.Atoms().Text(banded).Keep()).
			Selected(st.Mode == ModeBanded).SendResp().HasPrimaryClicked() {
			st.Mode = ModeBanded
		}
		if c.Button(in.Ids.PrepareStr("mode-continuous"), c.Atoms().Text("Continuous with ranges").Keep()).
			Selected(st.Mode == ModeContinuous).SendResp().HasPrimaryClicked() {
			st.Mode = ModeContinuous
		}
		if in.Model.HasInherent() {
			c.AddSpace(styletokens.GapSections(styletokens.ActiveDensity()))
			c.Checkbox(in.Ids.PrepareStr("inherent"), st.ShowInherent, "Show inherent → residual").SendRespVal(&st.ShowInherent)
		}
	}
	c.AddSpace(styletokens.GapItems(styletokens.ActiveDensity()))
}

// palette is a frame's colours, every one from the design system.
type palette struct {
	bg, area, grid, axis, border color.Color
	ink, ink2                    color.Color
	ramp                         []color.Color
	// rawRamp and rawMark are ramp and mark as tokens, for measuring.
	rawRamp                    []styletokens.RGBA8
	rawMark                    styletokens.RGBA8
	mark, markFill, markStroke color.Color
}

func tok(t styletokens.RGBA8) color.Color { return color.Hex(t.AsHex()) }

func tokA(t styletokens.RGBA8, a uint8) color.Color { return color.Hex(t.AsHex()&^0xff | uint32(a)) }

// newPalette resolves the colours for n shade classes.
//
// The risk dots are the theme's extreme ink — white on the dark canvas,
// near-black on the light one — ringed in the canvas colour. No qualitative
// slot will do: the dots must stay disjoint from every semantic Default in
// every theme (ADR-0029 §SD4, measured as the colour generators' cross-palette
// check does), and each Okabe-Ito entry comes within that check's advisory
// distance of one theme's status colours or other. The extreme ink clears it
// in both themes, and selectRamp keeps every class 3:1 from it.
//
// The shade classes come from the IDS sequential palette rampSequential
// resolves, cut to what selectRamp measures against the canvas: magnitude maps
// to contrast against it, the lowest class nearest and the highest farthest.
func newPalette(n int) (p palette) {
	p.bg = tok(styletokens.NeutralBgFaint)
	p.area = tok(styletokens.NeutralBgSurface)
	p.grid = tokA(styletokens.NeutralBorderFaint, 0x50)
	p.axis = tok(styletokens.NeutralBorderFaint)
	p.border = tokA(styletokens.NeutralBorderDefault, 0x60)
	p.ink = tok(styletokens.NeutralTextPrimary)
	p.ink2 = tok(styletokens.NeutralTextSecondary)
	p.rawRamp = rampFor(rampSequential(), n)
	p.ramp = make([]color.Color, n)
	for i, r := range p.rawRamp {
		p.ramp[i] = tok(r)
	}
	p.rawMark = styletokens.NeutralTextExtreme
	p.mark = tok(p.rawMark)
	p.markFill = tokA(p.rawMark, 0x1a)
	p.markStroke = tokA(p.rawMark, 0x8c)
	return
}

// frame is one frame's geometry: the canvas, the plot rect, every risk's
// place, and the hit regions' marks. Built before anything is emitted.
type frame struct {
	in     Input
	bands  Bands
	pal    palette
	format func(float64) string
	w, h   float32
	narrow bool
	key    bool
	keyW   float32 // the key's width, when there is one
	// the plot rect
	ml, mt, pw, ph float32
	// banded: band boundaries in canvas points, xs left to right, ys bottom
	// to top.
	xs, ys []float32
	// continuous: the log axes.
	xMin, xMax, yMin, yMax float64
	res, inh               [][2]float32
	hasInh                 []bool
	marks                  []mark
}

func newFrame(in Input, bands Bands) (f *frame) {
	f = &frame{in: in, bands: bands, format: in.FormatImpact,
		w: in.Width, h: in.Height}
	if f.format == nil {
		f.format = FormatCompact
	}
	if f.w <= 0 {
		f.w = defaultWidth
	}
	if f.h <= 0 {
		f.h = defaultHeight
	}
	f.pal = newPalette(len(bands.ShadeEdges) + 1)
	f.narrow = f.w < narrowWidth
	f.key = !in.HideKey && !f.narrow
	f.ml = 112
	mr := float32(16)
	if f.narrow {
		f.ml = 92
	}
	if f.key {
		f.keyW = min(max(keyMinW, f.w*0.22), keyMaxW)
		mr = f.keyW
	}
	f.mt = 30
	mb := float32(62)
	f.pw, f.ph = max(f.w-f.ml-mr, 60), max(f.h-f.mt-mb, 60)

	m := in.Model
	n := m.Len()
	f.res, f.inh, f.hasInh = make([][2]float32, n), make([][2]float32, n), make([]bool, n)
	if in.State.Mode == ModeContinuous {
		f.layoutContinuous()
	} else {
		f.layoutBanded()
	}
	f.declareRiskMarks()
	return
}

// showInherent reports whether the inherent arrows are drawn this frame.
func (f *frame) showInherent() bool {
	return f.in.State.ShowInherent && f.in.Model.HasInherent()
}

func (f *frame) layoutBanded() {
	b := f.bands
	nl, ni := len(b.LikelihoodEdges)-1, len(b.ImpactEdges)-1
	f.xs = growingSteps(f.ml, f.pw, ni, 1)
	f.ys = growingSteps(f.mt+f.ph, f.ph, nl, -1)
	pos := func(p, imp float64) [2]float32 {
		li, ii := bandIndex(b.LikelihoodEdges, p), bandIndex(b.ImpactEdges, imp)
		fy := fracIn(b.LikelihoodEdges[li], b.LikelihoodEdges[li+1], p)
		fx := fracIn(b.ImpactEdges[ii], b.ImpactEdges[ii+1], imp)
		return [2]float32{f.xs[ii] + float32(fx)*(f.xs[ii+1]-f.xs[ii]), f.ys[li] - float32(fy)*(f.ys[li]-f.ys[li+1])}
	}
	m := f.in.Model
	for i := range m.Len() {
		f.res[i] = pos(gmean(m.PLo[i], m.PHi[i]), gmean(m.ImpactLo[i], m.ImpactHi[i]))
		if p, imp, ok := m.inherent(i); ok {
			f.inh[i], f.hasInh[i] = pos(gmean(p[0], p[1]), gmean(imp[0], imp[1])), true
		}
	}
	labL, labI := b.likelihoodLabels(), b.impactLabels(f.format)
	shades := b.shadeLabels(f.format)
	for li := range nl {
		for ii := range ni {
			f.marks = append(f.marks, mark{risk: -1,
				x0: f.xs[ii], y0: f.ys[li+1], x1: f.xs[ii+1], y1: f.ys[li],
				label: labL[li] + " × " + labI[ii],
				tip:   "Expected loss at the cell centre " + shades[b.cellShade(li, ii)] + " / yr"})
		}
	}
}

// growingSteps splits span into n steps that grow by growFactor, from origin
// in direction dir (+1 right, -1 up). It returns the n+1 boundaries.
func growingSteps(origin, span float32, n int, dir float32) (out []float32) {
	sum := 0.0
	for i := range n {
		sum += math.Pow(growFactor, float64(i))
	}
	out = make([]float32, n+1)
	out[0] = origin
	for i := range n {
		out[i+1] = out[i] + dir*span*float32(math.Pow(growFactor, float64(i))/sum)
	}
	return
}

// fracIn is v's place within [lo, hi] on a log scale, kept off the cell's
// edges so a mark never sits on a boundary it does not belong to.
func fracIn(lo, hi, v float64) float64 {
	t := (math.Log(v) - math.Log(lo)) / (math.Log(hi) - math.Log(lo))
	return math.Min(0.85, math.Max(0.15, t))
}

func (f *frame) layoutContinuous() {
	m := f.in.Model
	b := f.bands
	xLo, xHi := b.ImpactEdges[0], b.ImpactEdges[len(b.ImpactEdges)-1]
	yLo, yHi := b.LikelihoodEdges[0], b.LikelihoodEdges[len(b.LikelihoodEdges)-1]
	for i := range m.Len() {
		xLo, xHi = math.Min(xLo, m.ImpactLo[i]), math.Max(xHi, m.ImpactHi[i])
		yLo, yHi = math.Min(yLo, m.PLo[i]), math.Max(yHi, m.PHi[i])
		if p, imp, ok := m.inherent(i); ok && f.showInherent() {
			xLo, xHi = math.Min(xLo, imp[0]), math.Max(xHi, imp[1])
			yLo, yHi = math.Min(yLo, p[0]), math.Max(yHi, p[1])
		}
	}
	f.xMin, f.xMax = decadeFloor(xLo), decadeCeil(xHi)
	f.yMin, f.yMax = decadeFloor(yLo), decadeCeil(yHi)
	for i := range m.Len() {
		f.res[i] = [2]float32{f.px(gmean(m.ImpactLo[i], m.ImpactHi[i])), f.py(gmean(m.PLo[i], m.PHi[i]))}
		if p, imp, ok := m.inherent(i); ok {
			f.inh[i], f.hasInh[i] = [2]float32{f.px(gmean(imp[0], imp[1])), f.py(gmean(p[0], p[1]))}, true
		}
	}
}

func decadeFloor(v float64) float64 { return math.Pow(10, math.Floor(math.Log10(v)+1e-9)) }
func decadeCeil(v float64) float64  { return math.Pow(10, math.Ceil(math.Log10(v)-1e-9)) }

// px / py place an impact and a likelihood on the continuous axes.
func (f *frame) px(v float64) float32 {
	return f.ml + float32((math.Log(v)-math.Log(f.xMin))/(math.Log(f.xMax)-math.Log(f.xMin)))*f.pw
}

func (f *frame) py(v float64) float32 {
	return f.mt + f.ph - float32((math.Log(v)-math.Log(f.yMin))/(math.Log(f.yMax)-math.Log(f.yMin)))*f.ph
}

// declareRiskMarks adds one mark per risk dot, named "R1 · name": the id the
// dot is labelled with, then the risk's name when it has one.
func (f *frame) declareRiskMarks() {
	m := f.in.Model
	for i := range m.Len() {
		tip := "Likelihood " + pctText(m.PLo[i]) + "–" + pctText(m.PHi[i]) + " / yr" +
			"\nImpact " + f.format(m.ImpactLo[i]) + "–" + f.format(m.ImpactHi[i])
		if p, imp, ok := m.inherent(i); ok {
			tip += "\nInherent " + pctText(p[0]) + "–" + pctText(p[1]) + " / yr, " + f.format(imp[0]) + "–" + f.format(imp[1])
		}
		label := m.Ids[i]
		if name := m.name(i); name != label {
			label += " · " + name
		}
		f.marks = append(f.marks, mark{risk: i, circle: true, cx: f.res[i][0], cy: f.res[i][1], r: dotR,
			label: label, tip: tip})
	}
}

// paint emits every paint opcode of the frame; the caller emits the canvas.
func (f *frame) paint(hovered int) {
	if f.in.State.Mode == ModeContinuous {
		f.paintContinuous()
	} else {
		f.paintBanded()
	}
	p := f.pal
	text(f.ml+f.pw/2, f.h-8, anchorMiddle, anchorBottom, impactAxisTitle(f.in.ImpactUnit), styletokens.CaptionPt, p.ink2)
	text(4, 4, anchorStart, anchorTop, "Likelihood per year", styletokens.CaptionPt, p.ink2)
	f.paintRisks(hovered)
	if f.key {
		f.paintKey()
	}
}

// impactAxisTitle is the impact axis's title, with the unit when one is known.
func impactAxisTitle(unit string) string {
	if unit == "" {
		return "Impact if it happens (range midpoint)"
	}
	return "Impact if it happens (" + unit + ", range midpoint)"
}

func (f *frame) paintBanded() {
	b, p := f.bands, f.pal
	nl, ni := len(b.LikelihoodEdges)-1, len(b.ImpactEdges)-1
	for li := range nl {
		for ii := range ni {
			c.PaintRectFilled(f.xs[ii]+1, f.ys[li+1]+1, f.xs[ii+1]-1, f.ys[li]-1, 3, p.ramp[b.cellShade(li, ii)]).Send()
		}
	}
	labL, labI := b.likelihoodLabels(), b.impactLabels(f.format)
	for li, l := range labL {
		mono(f.ml-8, (f.ys[li]+f.ys[li+1])/2, anchorEnd, anchorCenter, implot.Elide(l, f.ml-12, styletokens.CaptionPt), p.ink2)
	}
	for ii, l := range labI {
		mono((f.xs[ii]+f.xs[ii+1])/2, f.mt+f.ph+12, anchorMiddle, anchorTop, implot.Elide(l, f.xs[ii+1]-f.xs[ii]-4, styletokens.CaptionPt), p.ink2)
	}
}

func (f *frame) paintContinuous() {
	p := f.pal
	x0, y0, x1, y1 := f.ml, f.mt, f.ml+f.pw, f.mt+f.ph
	c.PaintRectFilled(x0, y0, x1, y1, 0, p.area).Send()
	for v := f.xMin; v <= f.xMax*1.0001; v *= 10 {
		x := f.px(v)
		c.PaintLine(x, y0, x, y1, p.grid, 1).Send()
		mono(x, y1+12, anchorMiddle, anchorTop, f.format(v), p.ink2)
	}
	for v := f.yMin; v <= f.yMax*1.0001; v *= 10 {
		y := f.py(v)
		c.PaintLine(x0, y, x1, y, p.grid, 1).Send()
		mono(x0-8, y, anchorEnd, anchorCenter, returnPeriodText(v), p.ink2)
	}
	c.PaintClipPush(x0, y0, x1, y1).Send()
	// Lines of equal expected annual loss: p·I = e is a straight line of
	// slope -1 on log–log axes. Drawn at the decades the shade classes span.
	isos := f.isoLosses()
	for _, e := range isos {
		c.PaintLine(f.px(e/f.yMax), f.py(f.yMax), f.px(e/f.yMin), f.py(f.yMin), p.axis, 1).Send()
	}
	m := f.in.Model
	for i := range m.Len() {
		bx0, bx1 := f.px(m.ImpactLo[i]), f.px(m.ImpactHi[i])
		by0, by1 := f.py(m.PHi[i]), f.py(m.PLo[i])
		c.PaintRectFilled(bx0, by0, bx1, by1, 2, p.markFill).Send()
		c.PaintRectStroke(bx0, by0, bx1, by1, 2, p.markStroke, 1).Send()
	}
	c.PaintClipPop().Send()
	for _, e := range isos {
		// Label each line where it leaves the plot: the right edge when it
		// reaches it, the top edge otherwise.
		if pr := e / f.xMax; pr >= f.yMin && pr <= f.yMax {
			mono(x1-4, f.py(pr)-4, anchorEnd, anchorBottom, f.format(e)+"/yr", p.ink2)
		} else {
			mono(f.px(e/f.yMax)+4, y0+4, anchorStart, anchorTop, f.format(e)+"/yr", p.ink2)
		}
	}
	if !f.narrow {
		mono(x1-4, y0-4, anchorEnd, anchorBottom, "Diagonal lines: equal expected annual loss", p.ink2)
	}
}

// isoLosses are the decades of expected annual loss drawn as lines in the
// continuous reading: those from the first shade boundary to three times the
// last, so the lines and the banded shades describe the same losses.
func (f *frame) isoLosses() (out []float64) {
	s := f.bands.ShadeEdges
	lo, hi := s[0], s[len(s)-1]*3
	for e := decadeCeil(lo); e <= hi; e *= 10 {
		out = append(out, e)
	}
	return
}

// returnPeriodText labels a likelihood gridline: "1 in 100 yr", "every yr".
func returnPeriodText(p float64) string {
	switch {
	case p >= 0.9999 && p <= 1.0001:
		return "every yr"
	case p > 1:
		return num(roundSig2(p)) + " per yr"
	}
	return "1 in " + num(roundSig2(1/p)) + " yr"
}

func (f *frame) paintRisks(hovered int) {
	p := f.pal
	m := f.in.Model
	if f.showInherent() {
		for i := range m.Len() {
			if !f.hasInh[i] {
				continue
			}
			a, r := f.inh[i], f.res[i]
			dx, dy := r[0]-a[0], r[1]-a[1]
			d := float32(math.Hypot(float64(dx), float64(dy)))
			c.PaintCircleFilled(a[0], a[1], 5, p.bg).Send()
			// The mark's ink, which selectRamp keeps 3:1 from every class; the
			// secondary ink has no such floor and sinks into the high classes.
			c.PaintCircleStroke(a[0], a[1], 5, p.mark, 1.5).Send()
			if d > 14 {
				paintArrow(a[0], a[1], r[0], r[1], d, p.mark)
			}
		}
	}
	type place struct {
		x, y   float32
		ah, av uint8
	}
	labels := make([]place, m.Len())
	for i := range m.Len() {
		l := place{x: f.res[i][0] + dotR + 4, y: f.res[i][1], ah: anchorStart, av: anchorCenter}
		if f.showInherent() && f.hasInh[i] {
			l.x, l.y, l.ah, l.av = labelAwayFromArrow(f.inh[i], f.res[i])
		}
		labels[i] = l
		if f.in.State.Mode != ModeContinuous {
			f.paintLabelChip(l.x, l.y, l.ah, l.av, m.Ids[i])
		}
	}
	sel := f.in.State.Selected()
	for i := range m.Len() {
		x, y := f.res[i][0], f.res[i][1]
		if i == sel {
			c.PaintCircleStroke(x, y, dotR+4, p.ink, 2).Send()
		} else if i == hovered {
			c.PaintCircleStroke(x, y, dotR+3, p.ink2, 1.5).Send()
		}
		c.PaintCircleFilled(x, y, dotR, p.mark).Send()
		c.PaintCircleStroke(x, y, dotR, p.bg, 2).Send()
	}
	for i, l := range labels {
		mono(l.x, l.y, l.ah, l.av, m.Ids[i], p.ink)
	}
}

// paintLabelChip backs a risk's label with the canvas colour, as the dot's ring
// does the dot. The label is small text on whichever cell the dot fell in, and
// holding every class 4.5:1 from the ink would cost the ramp about a third of
// its lightness room, so the label does not rest on the ramp at all.
func (f *frame) paintLabelChip(x, y float32, ah, av uint8, s string) {
	const padX, padY float32 = 2, 1
	w := implot.EstimateTextWidth(s, styletokens.CaptionPt)
	h := float32(styletokens.CaptionPt) + 2
	x0, y0 := x, y
	switch ah {
	case anchorMiddle:
		x0 -= w / 2
	case anchorEnd:
		x0 -= w
	}
	switch av {
	case anchorCenter:
		y0 -= h / 2
	case anchorBottom:
		y0 -= h
	}
	c.PaintRectFilled(x0-padX, y0-padY, x0+w+padX, y0+h+padY, 2, f.pal.bg).Send()
}

// Arrow-head size in points, fixed: egui's own arrow scales its head with
// the arrow's length, so a risk that moved far got a head that covered
// its neighbours.
const (
	arrowHeadLen   float32 = 7
	arrowHeadHalfW float32 = 3.5
)

// paintArrow draws the inherent → residual arrow from the inherent ring at
// a to the residual dot at r, d apart: a shaft and a small filled head,
// both kept clear of the two marks.
func paintArrow(ax, ay, rx, ry, d float32, col color.Color) {
	ux, uy := (rx-ax)/d, (ry-ay)/d
	ox, oy := ax+ux*6, ay+uy*6
	tx, ty := rx-ux*(dotR+3), ry-uy*(dotR+3)
	bx, by := tx-ux*arrowHeadLen, ty-uy*arrowHeadLen
	c.PaintLine(ox, oy, bx, by, col, 1.5).Send()
	c.PaintPolygonFilled(
		[]float32{tx, bx - uy*arrowHeadHalfW, bx + uy*arrowHeadHalfW},
		[]float32{ty, by + ux*arrowHeadHalfW, by - ux*arrowHeadHalfW},
		col).Send()
}

// labelAwayFromArrow places a residual dot's label on the side facing away
// from its inherent position, so the arrow arriving at the dot never runs
// through the label: the label continues the arrow's direction past the
// dot, anchored so the text grows away from it.
func labelAwayFromArrow(inh, res [2]float32) (x, y float32, ah, av uint8) {
	dx, dy := res[0]-inh[0], res[1]-inh[1]
	d := float32(math.Hypot(float64(dx), float64(dy)))
	if d < 1e-3 {
		return res[0] + dotR + 4, res[1], anchorStart, anchorCenter
	}
	ux, uy := dx/d, dy/d
	x, y = res[0]+ux*(dotR+4), res[1]+uy*(dotR+4)
	switch {
	case ux > 0.38:
		ah = anchorStart
	case ux < -0.38:
		ah = anchorEnd
	default:
		ah = anchorMiddle
	}
	switch {
	case uy > 0.38:
		av = anchorTop
	case uy < -0.38:
		av = anchorBottom
	default:
		av = anchorCenter
	}
	return
}

// paintKey lists the risks and explains the encoding, at the right of the
// plot. Rows past the room the canvas has collapse into a "+n more" line.
func (f *frame) paintKey() {
	p := f.pal
	m := f.in.Model
	kx, ky := f.w-f.keyW+24, f.mt+6
	text(kx, ky, anchorStart, anchorCenter, "Residual risks", styletokens.BodyPt, p.ink)
	explain := float32(66)
	if f.in.State.Mode != ModeContinuous {
		explain = 46 + float32(len(f.pal.ramp))*18
	}
	room := int((f.h - ky - 22 - 14 - explain) / keyRowH)
	rows := m.Len()
	if rows > room {
		rows = max(room-1, 0)
	}
	idW := float32(28)
	for i := range rows {
		y := ky + 22 + float32(i)*keyRowH
		mono(kx, y, anchorStart, anchorCenter, implot.Elide(m.Ids[i], idW-4, styletokens.CaptionPt), p.ink)
		text(kx+idW, y, anchorStart, anchorCenter, implot.Elide(m.name(i), f.keyW-24-idW-8, styletokens.CaptionPt), styletokens.CaptionPt, p.ink2)
	}
	shown := rows
	if rows < m.Len() {
		text(kx, ky+22+float32(rows)*keyRowH, anchorStart, anchorCenter, "+"+num(float64(m.Len()-rows))+" more", styletokens.CaptionPt, p.ink2)
		shown++
	}
	y0 := ky + 22 + float32(shown)*keyRowH + 14
	if f.in.State.Mode == ModeContinuous {
		text(kx, y0, anchorStart, anchorCenter, "Boxes", styletokens.BodyPt, p.ink)
		text(kx, y0+18, anchorStart, anchorCenter, "Ranges for likelihood and", styletokens.CaptionPt, p.ink2)
		text(kx, y0+34, anchorStart, anchorCenter, "impact. Dots mark the", styletokens.CaptionPt, p.ink2)
		text(kx, y0+50, anchorStart, anchorCenter, "geometric midpoint.", styletokens.CaptionPt, p.ink2)
		return
	}
	text(kx, y0, anchorStart, anchorCenter, "Cell shade", styletokens.BodyPt, p.ink)
	text(kx, y0+18, anchorStart, anchorCenter, "Expected annual loss at the", styletokens.CaptionPt, p.ink2)
	text(kx, y0+34, anchorStart, anchorCenter, "cell centre, one ramp:", styletokens.CaptionPt, p.ink2)
	for i, l := range f.bands.shadeLabels(f.format) {
		yy := y0 + 46 + float32(i)*18
		c.PaintRectFilled(kx, yy, kx+14, yy+12, 2, p.ramp[i]).Send()
		c.PaintRectStroke(kx, yy, kx+14, yy+12, 2, p.border, 1).Send()
		mono(kx+22, yy+6, anchorStart, anchorCenter, l, p.ink2)
	}
}

func text(x, y float32, ah, av uint8, s string, fs float32, col color.Color) {
	c.PaintText(x, y, ah, av, s, fs, col).Send()
}

// mono is the face numbers are set in: axis ticks, ids, band labels.
func mono(x, y float32, ah, av uint8, s string, col color.Color) {
	c.PaintText(x, y, ah, av, s, styletokens.CaptionPt, col).Monospace().Send()
}
