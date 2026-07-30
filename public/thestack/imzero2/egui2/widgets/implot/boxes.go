package implot

import (
	"math"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// boxFrame carries one Boxes declaration to the render pass.
type boxFrame struct {
	args, q1s, medians, q3s []float64
	wmins, wmaxs, widths    []float64
	fills                   []uint32
	strokeHex               uint32
	strokeW                 float32
}

// textFrame carries one Text declaration to the render pass.
type textFrame struct {
	x, y   float64
	colHex uint32
	text   string
}

// Boxes declares a box / letter-value series: box i centers on args[i],
// its body spans q1s[i]..q3s[i] with a median line at medians[i],
// whiskers extend to wmins[i]/wmaxs[i] where those reach beyond the
// body, the box is widths[i] wide in plot units and filled with
// fills[i] (0xRRGGBBAA). One stroke color/width serves the series. A
// house extension: upstream ImPlot has no box item, and the
// letter-value widgets need per-box fills.
func (p *Plot) Boxes(label string, args []float64, q1s []float64, medians []float64, q3s []float64, wmins []float64, wmaxs []float64, widths []float64, fills []uint32, strokeHex uint32, strokeW float32) *Plot {
	p.setupLocked = true
	p.takeNextStyle() // per-box fills own the color; an override must not leak
	slot := p.assignSlot(label)
	n := min(len(args), len(q1s), len(medians), len(q3s),
		len(wmins), len(wmaxs), len(widths), len(fills))
	if !p.st.hidden[label] {
		for i := range n {
			if math.IsNaN(args[i]) {
				continue
			}
			p.fitX(args[i] - widths[i]/2)
			p.fitX(args[i] + widths[i]/2)
			if !math.IsNaN(wmins[i]) {
				p.fitY(wmins[i])
			}
			if !math.IsNaN(wmaxs[i]) {
				p.fitY(wmaxs[i])
			}
		}
	}
	p.series = append(p.series, seriesFrame{kind: kindBoxes, label: label, slot: slot,
		boxes: &boxFrame{args: args[:n], q1s: q1s[:n], medians: medians[:n], q3s: q3s[:n],
			wmins: wmins[:n], wmaxs: wmaxs[:n], widths: widths[:n], fills: fills[:n],
			strokeHex: strokeHex, strokeW: strokeW}})
	return p
}

// Text declares an inlay text annotation centered at the plot point —
// upstream's PlotText. No legend entry; the point contributes to
// auto-fit like upstream.
func (p *Plot) Text(x float64, y float64, colHex uint32, text string) *Plot {
	p.setupLocked = true
	p.takeNextStyle()
	if !math.IsNaN(x) && !math.IsNaN(y) {
		p.fitX(x)
		p.fitY(y)
	}
	p.series = append(p.series, seriesFrame{kind: kindText,
		txt: &textFrame{x: x, y: y, colHex: colHex, text: text}})
	return p
}

// emitBoxes renders one box series: fill, outline, median line, and the
// whisker stems where the whisker range reaches beyond the body.
func (p *Plot) emitBoxes(s *seriesFrame, tr transform) {
	b := s.boxes
	for i := range b.args {
		if math.IsNaN(b.args[i]) || math.IsNaN(b.q1s[i]) || math.IsNaN(b.q3s[i]) {
			continue
		}
		hw := b.widths[i] / 2
		x0, x1 := tr.pxX(b.args[i]-hw), tr.pxX(b.args[i]+hw)
		yLo, yHi := tr.pxY(b.q1s[i]), tr.pxY(b.q3s[i])
		if yHi > yLo {
			yLo, yHi = yHi, yLo
		}
		c.PaintRectFilled(x0, yHi, x1, yLo, 0, color.Hex(b.fills[i])).Send()
		if b.strokeW > 0 {
			c.PaintRectStroke(x0, yHi, x1, yLo, 0, color.Hex(b.strokeHex), b.strokeW).Send()
		}
		if !math.IsNaN(b.medians[i]) {
			ym := tr.pxY(b.medians[i])
			c.PaintLine(x0, ym, x1, ym, color.Hex(b.strokeHex), max(b.strokeW, 1.0)).Send()
		}
		cx := tr.pxX(b.args[i])
		if !math.IsNaN(b.wmaxs[i]) && b.wmaxs[i] > b.q3s[i] {
			yw := tr.pxY(b.wmaxs[i])
			c.PaintLine(cx, yHi, cx, yw, color.Hex(b.strokeHex), max(b.strokeW, 1.0)).Send()
			c.PaintLine(x0, yw, x1, yw, color.Hex(b.strokeHex), max(b.strokeW, 1.0)).Send()
		}
		if !math.IsNaN(b.wmins[i]) && b.wmins[i] < b.q1s[i] {
			yw := tr.pxY(b.wmins[i])
			c.PaintLine(cx, yLo, cx, yw, color.Hex(b.strokeHex), max(b.strokeW, 1.0)).Send()
			c.PaintLine(x0, yw, x1, yw, color.Hex(b.strokeHex), max(b.strokeW, 1.0)).Send()
		}
	}
}

// emitText renders one inlay text declaration, centered at its point.
func (p *Plot) emitText(s *seriesFrame, tr transform) {
	t := s.txt
	c.PaintText(tr.pxX(t.x), tr.pxY(t.y), 1, 1, t.text, tickFontSize,
		color.Hex(t.colHex)).Monospace().Send()
}
