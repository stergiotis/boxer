package implot

import "math"

// seriesKind discriminates the frame-declared items (M2 breadth).
type seriesKind uint8

const (
	kindLine seriesKind = iota
	kindScatter
	kindBars
	kindShaded
	kindStairs
	kindStems
	kindInfV
	kindInfH
	kindHeatmap
	kindErrV
	kindErrH
	kindDigital
	kindPieSlice
	kindImage
)

// MarkerE selects a scatter glyph; the numbering is the paintMarkers wire
// contract (= ImPlot's marker numbering).
type MarkerE uint8

const (
	MarkerCircle MarkerE = iota
	MarkerSquare
	MarkerDiamond
	MarkerUp
	MarkerDown
	MarkerLeft
	MarkerRight
	MarkerCross
	MarkerPlus
	MarkerAsterisk
)

// Scatter declares a marker series (one paintMarkers opcode per series).
func (p *Plot) Scatter(label string, xs []float64, ys []float64, marker MarkerE, radius float32) *Plot {
	p.addSeries(seriesFrame{kind: kindScatter, label: label, xs: xs, ys: ys,
		marker: marker, radius: radius}, true, true)
	return p
}

// Bars declares vertical bars centered on xs with heights ys and the given
// bar width in plot units (one paintRectsFilled opcode per series). Bars
// are drawn from y=0 to ys[i], like ImPlot's default.
func (p *Plot) Bars(label string, xs []float64, ys []float64, width float64) *Plot {
	s := seriesFrame{kind: kindBars, label: label, xs: xs, ys: ys, width: width}
	p.addSeries(s, true, true)
	// The bar body spans x±width/2 and always includes the y=0 baseline.
	n := min(len(xs), len(ys))
	for i := range n {
		if !math.IsNaN(xs[i]) {
			p.fitX(xs[i] - width/2)
			p.fitX(xs[i] + width/2)
		}
	}
	p.fitY(0)
	return p
}

// Shaded declares a filled region between the curve and yref (per-segment
// convex quads — a whole-polygon fill would be concave).
func (p *Plot) Shaded(label string, xs []float64, ys []float64, yref float64) *Plot {
	p.addSeries(seriesFrame{kind: kindShaded, label: label, xs: xs, ys: ys, yref: yref}, true, true)
	p.fitY(yref)
	return p
}

// Stairs declares a step-after series (one polyline opcode).
func (p *Plot) Stairs(label string, xs []float64, ys []float64) *Plot {
	p.addSeries(seriesFrame{kind: kindStairs, label: label, xs: xs, ys: ys}, true, true)
	return p
}

// Stems declares vertical stems from yref to ys with a circle head.
func (p *Plot) Stems(label string, xs []float64, ys []float64, yref float64) *Plot {
	p.addSeries(seriesFrame{kind: kindStems, label: label, xs: xs, ys: ys, yref: yref}, true, true)
	p.fitY(yref)
	return p
}

// InfLinesV declares vertical reference lines at xs, spanning the plot
// height. Contributes only x to auto-fit, per ImPlot.
func (p *Plot) InfLinesV(label string, xs []float64) *Plot {
	p.addSeries(seriesFrame{kind: kindInfV, label: label, xs: xs}, true, false)
	return p
}

// InfLinesH declares horizontal reference lines at ys, spanning the plot
// width. Contributes only y to auto-fit.
func (p *Plot) InfLinesH(label string, ys []float64) *Plot {
	p.addSeries(seriesFrame{kind: kindInfH, label: label, ys: ys}, false, true)
	return p
}

// ErrorBars declares vertical error whiskers about (xs, ys): bar i spans
// ys[i]-neg[i] to ys[i]+pos[i]. Pass the same slice twice for symmetric
// errors. Reusing the label of the series the bars decorate merges them
// into that series' legend entry and visibility toggle; the whiskers
// themselves draw in a fixed foreground color, as upstream's error-bar
// style color does.
func (p *Plot) ErrorBars(label string, xs []float64, ys []float64, neg []float64, pos []float64) *Plot {
	p.addSeries(seriesFrame{kind: kindErrV, label: label, xs: xs, ys: ys, neg: neg, pos: pos}, true, false)
	if !p.st.hidden[label] {
		n := min(len(xs), len(ys), len(neg), len(pos))
		for i := range n {
			if math.IsNaN(xs[i]) || math.IsNaN(ys[i]) {
				continue
			}
			p.fitY(ys[i] - neg[i])
			p.fitY(ys[i] + pos[i])
		}
	}
	return p
}

// ErrorBarsH declares horizontal error whiskers: bar i spans xs[i]-neg[i]
// to xs[i]+pos[i] at height ys[i].
func (p *Plot) ErrorBarsH(label string, xs []float64, ys []float64, neg []float64, pos []float64) *Plot {
	p.addSeries(seriesFrame{kind: kindErrH, label: label, xs: xs, ys: ys, neg: neg, pos: pos}, false, true)
	if !p.st.hidden[label] {
		n := min(len(xs), len(ys), len(neg), len(pos))
		for i := range n {
			if math.IsNaN(xs[i]) || math.IsNaN(ys[i]) {
				continue
			}
			p.fitX(xs[i] - neg[i])
			p.fitX(xs[i] + pos[i])
		}
	}
	return p
}

// Digital declares a digital channel: y > 0 is high (the value scales the
// bit height, so 0/1 data reads as a logic trace), rendered as filled
// runs pinned to the bottom of the plot area in pixel space — digital
// channels never scale or pan with the y axis, per upstream. Visible
// digital series stack upward in declaration order. Contributes only x to
// auto-fit.
func (p *Plot) Digital(label string, xs []float64, ys []float64) *Plot {
	p.addSeries(seriesFrame{kind: kindDigital, label: label, xs: xs, ys: ys}, true, false)
	return p
}

// digitalRuns walks (xs, ys) pairwise and emits one run per stretch of
// equal y: emit(x0, x1, v) covers x0..x1 at value v, ending at the next
// transition sample. NaN in either coordinate ends the current run before
// it and starts fresh past it, like the upstream digital renderer.
func digitalRuns(xs []float64, ys []float64, emit func(x0 float64, x1 float64, v float64)) {
	n := min(len(xs), len(ys))
	i := 0
	for i < n {
		if math.IsNaN(xs[i]) || math.IsNaN(ys[i]) {
			i++
			continue
		}
		j := i + 1
		for j < n && !math.IsNaN(xs[j]) && !math.IsNaN(ys[j]) && ys[j] == ys[i] {
			j++
		}
		if j < n && !math.IsNaN(xs[j]) && !math.IsNaN(ys[j]) {
			emit(xs[i], xs[j], ys[i])
		} else if j-1 > i {
			emit(xs[i], xs[j-1], ys[i])
		}
		i = j
	}
}

// addSeries locks setup, assigns the palette slot, accumulates fit extents
// (skipping a series the legend has hidden), and records the item for
// End's render pass.
func (p *Plot) addSeries(s seriesFrame, fitXs bool, fitYs bool) {
	p.setupLocked = true
	s.slot = p.assignSlot(s.label)
	s.colHex, s.colOk, s.weight = p.takeNextStyle()
	if !p.st.hidden[s.label] {
		n := max(len(s.xs), len(s.ys))
		if fitXs && fitYs {
			n = min(len(s.xs), len(s.ys))
		}
		if fitXs {
			for i := 0; i < n && i < len(s.xs); i++ {
				if !math.IsNaN(s.xs[i]) {
					p.fitX(s.xs[i])
				}
			}
		}
		if fitYs {
			for i := 0; i < n && i < len(s.ys); i++ {
				if !math.IsNaN(s.ys[i]) {
					p.fitY(s.ys[i])
				}
			}
		}
	}
	p.series = append(p.series, s)
}

func (p *Plot) fitX(v float64) {
	p.dataXMin = math.Min(p.dataXMin, v)
	p.dataXMax = math.Max(p.dataXMax, v)
	p.dataOk = true
}

func (p *Plot) fitY(v float64) {
	p.dataYMin = math.Min(p.dataYMin, v)
	p.dataYMax = math.Max(p.dataYMax, v)
	p.dataOk = true
}
