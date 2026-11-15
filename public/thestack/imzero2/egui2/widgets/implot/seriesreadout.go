package implot

import (
	"math"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// The per-series hover readout and the crosshair shared across plots —
// house extensions; upstream has neither (its hover is the mouse-position
// text alone). The readout is Grafana's "multi" tooltip: at the hovered x,
// the value of every visible series, each at its own nearest sample.

// ReadoutRow is what the per-series readout found for one series: the
// sample nearest the read x, as an index into the slices the series was
// declared with and the sample's values, and the colour it drew in.
type ReadoutRow struct {
	Label  string
	Index  int
	X, Y   float64
	ColHex uint32
}

// CrosshairLink shares a hovered x between plots, as SetupAxisLinks shares
// a range: plots that join one link through SetupCrosshair draw a vertical
// line at the x the pointer is over in any of them, and — with HoverSeries —
// their readouts at that x. The caller holds the link; it must stay at one
// address across frames. A plot drawn before the hovered one in a frame sees
// the previous frame's x, one frame late.
//
// A plot that stops being drawn while hovered cannot withdraw its x; a link
// not refreshed for a second reads as empty, so the line does not stay
// behind.
type CrosshairLink struct {
	x    float64
	ok   bool
	src  uint64 // the publishing plot's ScopeId; 0 for Set
	when time.Time
}

// crosshairStale is how long a published x holds without being refreshed.
const crosshairStale = time.Second

// At reports the x the link holds, in plot space; ok is false when no plot
// of the link is hovered.
func (inst *CrosshairLink) At() (x float64, ok bool) {
	if inst == nil || !inst.ok || clockNow().Sub(inst.when) > crosshairStale {
		return 0, false
	}
	return inst.x, true
}

// Set puts x on the link from outside any plot — a table row hovered, a
// slider — so the linked plots draw it. It holds until Clear, or a plot of
// the link is hovered, or a second without a refresh passes.
func (inst *CrosshairLink) Set(x float64) {
	inst.x, inst.ok, inst.src, inst.when = x, true, 0, clockNow()
}

// Clear empties the link.
func (inst *CrosshairLink) Clear() { inst.ok = false }

// publish is a hovered plot's frame: the link takes its x.
func (inst *CrosshairLink) publish(x float64, src uint64, now time.Time) {
	inst.x, inst.ok, inst.src, inst.when = x, true, src, now
}

// foreign is a plot's frame without the pointer: the x another plot (or the
// caller) put on the link, or nothing. The plot that published last withdraws
// its own x here, so the line goes when the pointer leaves it.
func (inst *CrosshairLink) foreign(src uint64, now time.Time) (x float64, ok bool) {
	if !inst.ok {
		return 0, false
	}
	if inst.src == src {
		inst.ok = false
		return 0, false
	}
	if now.Sub(inst.when) > crosshairStale {
		return 0, false
	}
	return inst.x, true
}

// HoverSeries turns on the per-series readout for the frame: while the
// pointer is over the plot area — or a plot sharing this one's crosshair
// link is hovered — every visible, labelled line, scatter, stairs, stems or
// shaded series is marked at its sample nearest the x read, and a box lists
// each one's swatch, label and value, the value through the y axis's
// formatter under a header naming the x through the x axis's. A vertical
// line marks the x. While the box shows in the hovered plot it replaces the
// corner's mouse text. NoHoverSeriesBox keeps the marks and drops the box,
// for a caller that shows the values itself through SeriesReadout.
//
// The nearest sample is found by a scan of the series, not a binary search:
// whether x is sorted is itself an O(n) question, and drawing the series
// already walks every sample each frame. A stairs series reads the last
// sample at or before x — the value its step shows there — rather than the
// nearest one. NaN samples are skipped.
func (p *Plot) HoverSeries() *Plot {
	p.hoverSeries = true
	return p
}

// NoHoverSeriesBox draws HoverSeries' marks and line without its box.
func (p *Plot) NoHoverSeriesBox() *Plot {
	p.noSeriesBox = true
	return p
}

// SetupCrosshair joins the plot to a crosshair link (see CrosshairLink). A
// nil link leaves it. Per-frame Setup state; must precede the first item.
func (p *Plot) SetupCrosshair(link *CrosshairLink) *Plot {
	if p.warnIfLocked("SetupCrosshair") {
		return p
	}
	p.crosshair = link
	return p
}

// SeriesReadout reports what the per-series readout found at the most
// recent End — this frame's once End has run, the previous frame's before:
// the x it read at and, per series, the nearest sample. ok is false when
// nothing was read (HoverSeries off, the pointer elsewhere and no linked
// plot hovered, or no series to read). The rows are reused by the next End;
// copy what must outlive it.
func (p *Plot) SeriesReadout() (x float64, rows []ReadoutRow, ok bool) {
	if p == nil || p.st == nil || !p.st.readoutOk {
		return 0, nil, false
	}
	return p.st.readoutX, p.st.readout, true
}

// nearestSample is the index of the sample whose x is nearest to x —
// stepAfter: the last one at or before x — skipping NaN samples; -1 when
// there is none. Ties go to the earlier sample.
func nearestSample(xs []float64, ys []float64, x float64, stepAfter bool) int {
	best, bestD := -1, math.Inf(1)
	n := min(len(xs), len(ys))
	for i := range n {
		xi := xs[i]
		if math.IsNaN(xi) || math.IsNaN(ys[i]) {
			continue
		}
		var d float64
		if stepAfter {
			if xi > x {
				continue
			}
			d = x - xi
		} else {
			d = math.Abs(xi - x)
		}
		if d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// readsSeries reports whether a series kind has one y per x for the readout
// to read.
func readsSeries(k seriesKind) bool {
	switch k {
	case kindLine, kindScatter, kindStairs, kindStems, kindShaded:
		return true
	}
	return false
}

// hoverX resolves the x this frame reads at: the pointer's while it is over
// the plot area, else the link's. It also keeps the link: a hovered plot
// publishes, an unhovered one withdraws its own x.
func (p *Plot) hoverX(tr transform) (x float64, hovered bool, ok bool) {
	st := p.st
	if st.hoverOk {
		x = tr.plotX(st.hoverPos[0])
		if p.crosshair != nil {
			p.crosshair.publish(x, p.scopeId, clockNow())
		}
		return x, true, true
	}
	if p.crosshair != nil {
		x, ok = p.crosshair.foreign(p.scopeId, clockNow())
		return x, false, ok
	}
	return 0, false, false
}

// collectReadout fills the plot state's readout rows at x and returns the
// x the guide line goes at: the sample x nearest the read x among the rows,
// so the line sits on data rather than between it; x itself without rows.
func (p *Plot) collectReadout(x float64) (lineX float64) {
	st := p.st
	st.readout = st.readout[:0]
	lineX = x
	if !p.hoverSeries {
		return
	}
	nearest := math.Inf(1)
	for si := range p.series {
		s := &p.series[si]
		if s.label == "" || st.hidden[s.label] || !readsSeries(s.kind) || hasReadoutRow(st.readout, s.label) {
			continue
		}
		i := nearestSample(s.xs, s.ys, x, s.kind == kindStairs)
		if i < 0 {
			continue
		}
		colHex := seriesColor(s.slot)
		if s.colOk {
			colHex = s.colHex
		}
		st.readout = append(st.readout, ReadoutRow{Label: s.label, Index: i, X: s.xs[i], Y: s.ys[i], ColHex: colHex})
		if d := math.Abs(s.xs[i] - x); d < nearest {
			nearest, lineX = d, s.xs[i]
		}
	}
	return
}

func hasReadoutRow(rows []ReadoutRow, label string) bool {
	for i := range rows {
		if rows[i].Label == label {
			return true
		}
	}
	return false
}

// emitHoverGuide draws the vertical line at lineX and a mark on each row's
// sample. Runs inside the plot-area clip.
func (p *Plot) emitHoverGuide(tr transform, lineX float64, areaY, areaH float32) {
	px := tr.pxX(lineX)
	c.PaintLine(px, areaY, px, areaY+areaH, color.Hex(colReadout), styletokens.StrokeHair).Send()
	for _, r := range p.st.readout {
		mx, my := tr.pxX(r.X), tr.pxY(r.Y)
		c.PaintCircleFilled(mx, my, 4.5, color.Hex(colAreaBg)).Send()
		c.PaintCircleFilled(mx, my, 3, color.Hex(r.ColHex)).Send()
	}
}

// Readout box metrics, in the tick font.
const (
	readoutPad    = 6
	readoutRowH   = 15
	readoutSwatch = 9
	readoutOffset = 14 // from the pointer or the guide line to the box
)

// emitSeriesBox draws the readout box beside the guide line: at the pointer
// in the hovered plot, at the top of the plot area in a linked one, on
// whichever side of the line keeps it inside the canvas. Runs outside the
// plot-area clip, so the box may overhang the area's edge.
func (p *Plot) emitSeriesBox(tr transform, lineX float64, hovered bool, areaY float32) {
	st := p.st
	rows := st.readout
	if len(rows) == 0 || p.noSeriesBox {
		return
	}
	header := axisValueText(&st.x, st.ticksX, lineX)
	values := make([]string, len(rows))
	labelW, valueW := 0, 0
	for i, r := range rows {
		values[i] = axisValueText(&st.y, st.ticksY, r.Y)
		labelW = max(labelW, len([]rune(r.Label)))
		valueW = max(valueW, len([]rune(values[i])))
	}
	textW := max(float32(labelW+2+valueW)*charW+readoutSwatch+readoutPad, EstimateTextWidth(header, tickFontSize))
	w := textW + 2*readoutPad
	h := float32(len(rows)+1)*readoutRowH + 2*readoutPad

	lx := tr.pxX(lineX)
	x := lx + readoutOffset
	if x+w > p.w {
		x = lx - readoutOffset - w
	}
	x = max(x, 0)
	y := areaY + 8
	if hovered {
		y = st.hoverPos[1] + readoutOffset
	}
	y = max(min(y, p.h-h), 0)

	c.PaintRectFilled(x, y, x+w, y+h, 3, color.Hex(colLegendBg)).Send()
	c.PaintRectStroke(x, y, x+w, y+h, 3, color.Hex(colBorder), styletokens.StrokeHair).Send()
	tx := x + readoutPad
	ty := y + readoutPad + readoutRowH/2
	c.PaintText(tx, ty, 0, 1, header, tickFontSize, color.Hex(colTickLabel)).Monospace().Send()
	for i, r := range rows {
		ry := ty + float32(i+1)*readoutRowH
		c.PaintRectFilled(tx, ry-readoutSwatch/2, tx+readoutSwatch, ry+readoutSwatch/2, 2, color.Hex(r.ColHex)).Send()
		line := r.Label + strings.Repeat(" ", labelW-len([]rune(r.Label))+2+valueW-len([]rune(values[i]))) + values[i]
		c.PaintText(tx+readoutSwatch+readoutPad, ry, 0, 1, line, tickFontSize, color.Hex(colTickLabel)).Monospace().Send()
	}
}
