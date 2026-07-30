package implot

import (
	"fmt"
	"math"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// End resolves fits, lays the plot out, emits every paint command and the
// canvas, stores this frame's transform for next frame's gestures, and
// closes the id scope opened by Begin.
func (p *Plot) End() {
	st := p.st
	defer func() {
		p.ids.PopIdFromStackChecked(p.scopeId)
		p.series = nil
	}()

	// --- Fit: explicit request (double-click), AutoFit flag, or a plot that
	// has never had ranges. ImPlot fit is exact to the data extents.
	fitX := st.x.fitNext || st.x.flags&AxisFlagsAutoFit != 0 || (!st.initialized && !st.x.hasRange)
	fitY := st.y.fitNext || st.y.flags&AxisFlagsAutoFit != 0 || (!st.initialized && !st.y.hasRange)
	if p.dataOk {
		if fitX {
			st.x.rng = Range{p.dataXMin, p.dataXMax}.sanitize()
		}
		if fitY {
			st.y.rng = Range{p.dataYMin, p.dataYMax}.sanitize()
		}
	}
	st.x.fitNext, st.y.fitNext = false, false
	st.x.rng = st.x.rng.sanitize()
	st.y.rng = st.y.rng.sanitize()
	st.initialized = true
	st.onceApplied = true

	// --- Layout. Vertical gutters are label-independent, so the plot-area
	// height is final immediately; the y ticks computed against it size the
	// left gutter, and only then are the x ticks located against the final
	// width (no iteration needed, see core.go locateTicks).
	topGutter := float32(6.0)
	if p.title != "" {
		topGutter = 24
	}
	bottomGutter := float32(6 + tickLen + 14)
	if st.x.label != "" {
		bottomGutter += 16
	}
	areaH := p.h - topGutter - bottomGutter
	if areaH < 16 {
		areaH = 16
	}
	st.ticksY = locateTicks(st.y.rng, areaH, st.ticksY)
	maxYChars := 1
	for i := range st.ticksY {
		if n := len(st.ticksY[i].label); n > maxYChars {
			maxYChars = n
		}
	}
	leftGutter := float32(maxYChars)*charW + tickLen + 10
	if st.y.label != "" {
		leftGutter += 16
	}
	rightGutter := float32(10.0)
	areaW := p.w - leftGutter - rightGutter
	if areaW < 16 {
		areaW = 16
	}
	st.ticksX = locateTicks(st.x.rng, areaW, st.ticksX)
	areaX, areaY := leftGutter, topGutter
	tr := newTransform(st.x.rng, st.y.rng, areaX, areaY, areaW, areaH)

	// --- Chrome: area fill, grid, tick marks + labels, axis labels, title.
	c.PaintRectFilled(areaX, areaY, areaX+areaW, areaY+areaH, 0, color.Hex(colAreaBg)).Send()
	if st.x.flags&AxisFlagsNoGrid == 0 {
		for i := range st.ticksX {
			t := &st.ticksX[i]
			px := tr.pxX(t.value)
			col := uint32(colGridMinor)
			if t.major {
				col = colGridMajor
			}
			c.PaintLine(px, areaY, px, areaY+areaH, color.Hex(col), 1.0).Send()
		}
	}
	if st.y.flags&AxisFlagsNoGrid == 0 {
		for i := range st.ticksY {
			t := &st.ticksY[i]
			py := tr.pxY(t.value)
			col := uint32(colGridMinor)
			if t.major {
				col = colGridMajor
			}
			c.PaintLine(areaX, py, areaX+areaW, py, color.Hex(col), 1.0).Send()
		}
	}
	for i := range st.ticksX {
		t := &st.ticksX[i]
		if !t.major {
			continue
		}
		px := tr.pxX(t.value)
		c.PaintLine(px, areaY+areaH, px, areaY+areaH+tickLen, color.Hex(colBorder), 1.0).Send()
		if st.x.flags&AxisFlagsNoTickLabels == 0 {
			c.PaintText(px, areaY+areaH+tickLen+2, 1, 0, t.label, tickFontSize, color.Hex(colTickLabel)).Monospace().Send()
		}
	}
	for i := range st.ticksY {
		t := &st.ticksY[i]
		if !t.major {
			continue
		}
		py := tr.pxY(t.value)
		c.PaintLine(areaX-tickLen, py, areaX, py, color.Hex(colBorder), 1.0).Send()
		if st.y.flags&AxisFlagsNoTickLabels == 0 {
			c.PaintText(areaX-tickLen-2, py, 2, 1, t.label, tickFontSize, color.Hex(colTickLabel)).Monospace().Send()
		}
	}
	if p.title != "" {
		c.PaintText(areaX+areaW/2, 4, 1, 0, p.title, titleFontSize, color.Hex(colTitle)).Send()
	}
	if st.x.label != "" {
		c.PaintText(areaX+areaW/2, p.h-16, 1, 0, st.x.label, labelFontSize, color.Hex(colAxisLabel)).Send()
	}
	if st.y.label != "" {
		// The painter lane has no rotated text yet; the y label sits
		// horizontally above the tick column (deviation noted in doc.go).
		c.PaintText(2, topGutter-2, 0, 2, st.y.label, labelFontSize, color.Hex(colAxisLabel)).Send()
	}

	// --- Series, clipped to the plot area (the M0 clip stack).
	c.PaintClipPush(areaX, areaY, areaX+areaW, areaY+areaH).Send()
	for si := range p.series {
		s := &p.series[si]
		colHex := paletteDeep[si%len(paletteDeep)]
		p.emitLine(s, tr, colHex)
	}
	if st.dragging && st.dragBox {
		x0, y0 := st.boxStart[0], st.boxStart[1]
		x1, y1 := st.boxCur[0], st.boxCur[1]
		if x1 < x0 {
			x0, x1 = x1, x0
		}
		if y1 < y0 {
			y0, y1 = y1, y0
		}
		c.PaintRectFilled(x0, y0, x1, y1, 0, color.Hex(colBoxFill)).Send()
		c.PaintRectStroke(x0, y0, x1, y1, 0, color.Hex(colBoxStroke), 1.0).Send()
	}
	c.PaintClipPop().Send()
	c.PaintRectStroke(areaX, areaY, areaX+areaW, areaY+areaH, 0, color.Hex(colBorder), 1.0).Send()

	// --- Hover readout, ImPlot's mouse-position text, bottom-right corner.
	if st.hoverOk {
		hx := tr.plotX(st.hoverPos[0])
		hy := tr.plotY(st.hoverPos[1])
		c.PaintText(areaX+areaW-4, areaY+areaH-3, 2, 2,
			fmt.Sprintf("%.6g, %.6g", hx, hy), tickFontSize, color.Hex(colReadout)).Monospace().Send()
	}

	// --- Interaction surfaces + the canvas drain.
	c.PaintSenseRegion(p.ids.PrepareStr("implot-area"), areaX, areaY, areaW, areaH).Send()
	c.PaintCanvas(p.ids.PrepareStr("implot-canvas"), p.w, p.h).
		Background(color.Hex(colPlotBg)).
		Sense(true, true, true).
		CaptureZoom().
		CaptureScroll().
		Send()

	st.prev = tr
	st.prevOk = tr.valid()
}

// emitLine projects one series through the transform (f64 → f32 at this
// boundary, SD4) and emits one polyline per NaN-free run.
func (p *Plot) emitLine(s *seriesFrame, tr transform, colHex uint32) {
	st := p.st
	st.scratchX = st.scratchX[:0]
	st.scratchY = st.scratchY[:0]
	flush := func() {
		if len(st.scratchX) == 1 {
			// A single surviving point still deserves pixels: a dot.
			c.PaintCircleFilled(st.scratchX[0], st.scratchY[0], 1.5, color.Hex(colHex)).Send()
		} else if len(st.scratchX) > 1 {
			c.PaintPolyline(st.scratchX, st.scratchY, color.Hex(colHex), 1.5).Send()
		}
		st.scratchX = st.scratchX[:0]
		st.scratchY = st.scratchY[:0]
	}
	for i := range s.xs {
		x, y := s.xs[i], s.ys[i]
		if math.IsNaN(x) || math.IsNaN(y) {
			flush()
			continue
		}
		st.scratchX = append(st.scratchX, tr.pxX(x))
		st.scratchY = append(st.scratchY, tr.pxY(y))
	}
	flush()
}
