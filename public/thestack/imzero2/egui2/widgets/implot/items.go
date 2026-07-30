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

// addSeries locks setup, accumulates fit extents (skipping a series the
// legend has hidden), and records the item for End's render pass.
func (p *Plot) addSeries(s seriesFrame, fitXs bool, fitYs bool) {
	p.setupLocked = true
	if !p.st.hidden[s.label] {
		n := len(s.xs)
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
