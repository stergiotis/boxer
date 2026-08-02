package implot

import (
	"fmt"
	"math"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// pieFrame carries one pie slice's geometry to the render pass: center and
// radius in plot units, the slice's angular span in radians (plot-space
// CCW from +x), the raw value and the label format.
type pieFrame struct {
	cx, cy, r float64
	a0, a1    float64
	value     float64
	labelFmt  string
}

// Pie declares a pie chart at center (x, y) with the given radius, all in
// plot units — like upstream, the disc goes elliptical when the two axes'
// pixel densities differ. Each slice is its own legend entry (click to
// hide; a hidden slice keeps its angular span). Slices start at angle0
// (degrees, plot-space CCW from +x; 90 = top) and advance counter-
// clockwise. Values are normalized to a full circle when they sum past 1;
// a smaller sum leaves the pie partial, per upstream's auto-normalize.
// labelFmt ("" = none) formats each slice's value at the slice centroid.
func (p *Plot) Pie(labels []string, values []float64, x float64, y float64, radius float64, angle0Deg float64, labelFmt string) *Plot {
	p.setupLocked = true
	p.takeNextStyle() // slices cycle the palette; an override must not leak
	n := min(len(labels), len(values))
	if n == 0 || radius <= 0 {
		return p
	}
	spans := pieSpans(values[:n], angle0Deg)
	if spans == nil {
		return p
	}
	for i := range n {
		p.series = append(p.series, seriesFrame{kind: kindPieSlice, label: labels[i],
			slot: p.assignSlot(labels[i]),
			pie:  &pieFrame{cx: x, cy: y, r: radius, a0: spans[i][0], a1: spans[i][1], value: values[i], labelFmt: labelFmt}})
	}
	p.fitX(x - radius)
	p.fitX(x + radius)
	p.fitY(y - radius)
	p.fitY(y + radius)
	return p
}

// pieSpans converts values to per-slice angular spans [a0, a1) in radians
// starting at angle0: shares of the full circle when the sum exceeds 1
// (upstream's auto-normalize), raw fractions of the circle otherwise. NaN
// or non-positive values yield zero-span slices. Returns nil when no
// value is positive.
func pieSpans(values []float64, angle0Deg float64) [][2]float64 {
	sum := 0.0
	for _, v := range values {
		if !math.IsNaN(v) && v > 0 {
			sum += v
		}
	}
	if sum <= 0 {
		return nil
	}
	norm := 1.0
	if sum > 1 {
		norm = 1 / sum
	}
	spans := make([][2]float64, len(values))
	a := angle0Deg * math.Pi / 180
	for i, v := range values {
		if math.IsNaN(v) || v < 0 {
			v = 0
		}
		a1 := a + 2*math.Pi*v*norm
		spans[i] = [2]float64{a, a1}
		a = a1
	}
	return spans
}

// arcChunks splits [a0, a1] into spans no wider than a half circle, so
// the center-fan polygon over each chunk stays convex for the painter's
// convex-only fill (upstream hands >180° fans to AddConvexPolyFilled and
// accepts the artifact; splitting renders them correctly).
func arcChunks(a0 float64, a1 float64, dst [][2]float64) [][2]float64 {
	dst = dst[:0]
	const maxSpan = math.Pi * 0.9999
	for a0 < a1 {
		e := math.Min(a0+maxSpan, a1)
		dst = append(dst, [2]float64{a0, e})
		a0 = e
	}
	return dst
}

// pieSegPerCircle matches upstream RenderPieSlice's arc resolution.
const pieSegPerCircle = 50

// emitPieSlice renders one slice as center-fan polygon chunks plus the
// optional value label at the slice centroid.
func (p *Plot) emitPieSlice(s *seriesFrame, tr transform, colHex uint32) {
	pf := s.pie
	if pf.a1 <= pf.a0 {
		return
	}
	st := p.st
	var buf [3][2]float64
	for _, ch := range arcChunks(pf.a0, pf.a1, buf[:0]) {
		chSpan := ch[1] - ch[0]
		nSeg := max(3, int(math.Ceil(chSpan/(2*math.Pi)*pieSegPerCircle)))
		st.scratchX = st.scratchX[:0]
		st.scratchY = st.scratchY[:0]
		st.scratchX = append(st.scratchX, tr.pxX(pf.cx))
		st.scratchY = append(st.scratchY, tr.pxY(pf.cy))
		for j := 0; j <= nSeg; j++ {
			a := ch[0] + chSpan*float64(j)/float64(nSeg)
			st.scratchX = append(st.scratchX, tr.pxX(pf.cx+pf.r*math.Cos(a)))
			st.scratchY = append(st.scratchY, tr.pxY(pf.cy+pf.r*math.Sin(a)))
		}
		c.PaintPolygonFilled(st.scratchX, st.scratchY, color.Hex(colHex)).Send()
	}
	if pf.labelFmt != "" {
		mid := (pf.a0 + pf.a1) / 2
		lx := tr.pxX(pf.cx + 0.5*pf.r*math.Cos(mid))
		ly := tr.pxY(pf.cy + 0.5*pf.r*math.Sin(mid))
		c.PaintText(lx, ly, 1, 1, fmt.Sprintf(pf.labelFmt, pf.value), tickFontSize,
			color.Hex(contrastText(colHex))).Monospace().Send()
	}
}

// contrastText picks a dark or light text color against the given fill by
// perceived luminance — the shape of upstream's CalcTextColor: Rec.601
// weights on the gamma-encoded bytes, switching at 140 of 255.
//
// It is kept rather than rewritten over RelativeLuminance so a ported ImPlot
// chart keeps the ink upstream would have given it. The two rules disagree on
// about 7% of the colour space, and near the switch the accurate one is the
// better pick — see contrast.go. A widget that is not reproducing an upstream
// look should use RelativeLuminance and choose its own switch point.
func contrastText(colHex uint32) uint32 {
	r := float64(colHex >> 24 & 0xff)
	g := float64(colHex >> 16 & 0xff)
	b := float64(colHex >> 8 & 0xff)
	if 0.299*r+0.587*g+0.114*b > 140 {
		return colContrastDark
	}
	return colContrastLite
}
