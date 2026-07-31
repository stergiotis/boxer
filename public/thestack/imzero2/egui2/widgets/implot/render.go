package implot

import (
	"fmt"
	"math"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// End resolves fits, lays the plot out, emits every paint command and the
// canvas, stores this frame's transform for next frame's gestures, and
// closes the id scope opened by Begin. Idempotent: a second call is a
// debug-logged no-op, so an explicit End inside a Scoped body cannot
// double-pop the id scope.
func (p *Plot) End() {
	if p.ended {
		log.Debug().Str("plot", p.title).
			Msg("implot: End called twice (a Scoped body needs no explicit End)")
		return
	}
	p.ended = true
	st := p.st
	defer func() {
		p.ids.PopIdFromStackChecked(p.scopeId)
		p.series = nil
		p.tools = nil
	}()

	// --- Fit: explicit request (double-click), AutoFit flag, Follow (auto
	// until a gesture, resumed by an explicit fit), or a plot that has
	// never had ranges. ImPlot fit is exact to the data extents.
	if st.x.fitNext {
		st.x.touched = false
	}
	if st.y.fitNext {
		st.y.touched = false
	}
	fitX := st.x.fitNext || st.x.flags&AxisFlagsAutoFit != 0 || (!st.initialized && !st.x.hasRange) ||
		(st.x.flags&AxisFlagsFollow != 0 && !st.x.touched)
	fitY := st.y.fitNext || st.y.flags&AxisFlagsAutoFit != 0 || (!st.initialized && !st.y.hasRange) ||
		(st.y.flags&AxisFlagsFollow != 0 && !st.y.touched)
	if p.dataOk {
		if fitX {
			st.x.rng = Range{p.dataXMin, p.dataXMax}.sanitize()
		}
		if fitY {
			st.y.rng = Range{p.dataYMin, p.dataYMax}.sanitize()
		}
	}
	st.x.fitNext, st.y.fitNext = false, false
	st.x.rng = sanitizeScaled(st.x.rng, st.x.scale)
	st.y.rng = sanitizeScaled(st.y.rng, st.y.scale)
	st.x.constrain()
	st.y.constrain()
	// Constraints are per-frame Setup state: consumed here, re-declared
	// by the caller next frame.
	st.x.consOk, st.y.consOk = false, false
	st.initialized = true
	st.onceApplied = true
	st.writeLinks()

	// --- Layout. Vertical gutters are label-independent, so the plot-area
	// height is final immediately; the y ticks computed against it size the
	// left gutter, and only then are the x ticks located against the final
	// width (no iteration needed, see core.go locateTicks).
	topGutter := float32(6.0)
	if p.titleShown != "" {
		topGutter = 24
	} else if st.y.label != "" {
		// The horizontal y label sits in the top gutter; without a title
		// it still needs the band, or it clips above the canvas.
		topGutter = 20
	}
	bottomGutter := float32(6 + tickLen + 14)
	if st.x.flags&AxisFlagsNoTickLabels != 0 {
		bottomGutter = 6 // no label row to reserve (sparklines)
	}
	if st.x.label != "" {
		bottomGutter += 16
	}
	areaH := p.h - topGutter - bottomGutter
	if areaH < 16 {
		areaH = 16
	}
	if len(p.yCustomTicks) > 0 {
		st.ticksY = filterTicksInRange(st.y.rng, p.yCustomTicks, st.ticksY)
	} else {
		st.ticksY = locateTicksScaled(st.y.rng, areaH, st.y.scale, st.ticksY)
	}
	maxYChars := 1
	for i := range st.ticksY {
		if n := len(st.ticksY[i].label); n > maxYChars {
			maxYChars = n
		}
	}
	leftGutter := float32(maxYChars)*charW + tickLen + 10
	if st.y.flags&AxisFlagsNoTickLabels != 0 {
		leftGutter = 8
	}
	if st.y.label != "" {
		leftGutter += 16
	}
	rightGutter := float32(10.0)
	areaW := p.w - leftGutter - rightGutter
	if areaW < 16 {
		areaW = 16
	}
	if len(p.xCustomTicks) > 0 {
		st.ticksX = filterTicksInRange(st.x.rng, p.xCustomTicks, st.ticksX)
	} else {
		st.ticksX = locateTicksScaled(st.x.rng, areaW, st.x.scale, st.ticksX)
	}
	areaX, areaY := leftGutter, topGutter
	tr := newTransform(st.x.rng, st.y.rng, st.x.scale, st.y.scale, areaX, areaY, areaW, areaH)

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
	if p.titleShown != "" {
		c.PaintText(areaX+areaW/2, 4, 1, 0, p.titleShown, titleFontSize, color.Hex(colTitle)).Send()
	}
	if st.x.label != "" {
		c.PaintText(areaX+areaW/2, p.h-16, 1, 0, st.x.label, labelFontSize, color.Hex(colAxisLabel)).Send()
	}
	if st.y.label != "" {
		// The painter lane has no rotated text yet; the y label sits
		// horizontally above the tick column (deviation noted in doc.go).
		c.PaintText(2, topGutter-2, 0, 2, st.y.label, labelFontSize, color.Hex(colAxisLabel)).Send()
	}

	// --- Legend interaction: last frame's flags for each entry's sense
	// region, read before the draw so a toggle applies this frame. One
	// entry per distinct label — same-label items share it.
	st.legendHover = ""
	sm := c.CurrentApplicationState.StateManager
	var leg []int
	if !p.noLegend {
		leg = legendIndices(p.series)
	}
	for _, si := range leg {
		s := &p.series[si]
		h := widgethandle.Make(p.ids.PrepareStr("legend-" + s.label).Derive())
		lf := sm.GetResponse(h)
		if lf.HasPrimaryClicked() {
			st.hidden[s.label] = !st.hidden[s.label]
		}
		if lf.HasHovered() {
			st.legendHover = s.label
		}
	}

	// --- Series, clipped to the plot area (the M0 clip stack). emitting
	// arms the declaration guard for the span in which Custom closures run.
	c.PaintClipPush(areaX, areaY, areaX+areaW, areaY+areaH).Send()
	p.emitting = true
	for si := range p.series {
		s := &p.series[si]
		if st.hidden[s.label] {
			continue
		}
		colHex := paletteDeep[s.slot%len(paletteDeep)]
		if s.colOk {
			colHex = s.colHex
		}
		weight := float32(1.5)
		if s.weight > 0 {
			weight = s.weight
		}
		if s.label != "" && s.label == st.legendHover {
			weight *= 2
		}
		p.emitSeries(s, tr, areaX, areaY, areaW, areaH, colHex, weight)
	}
	p.emitting = false
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
	p.emitToolsClipped(tr, areaX, areaY, areaW, areaH)
	c.PaintClipPop().Send()
	c.PaintRectStroke(areaX, areaY, areaX+areaW, areaY+areaH, 0, color.Hex(colBorder), 1.0).Send()

	// --- Hover readout, ImPlot's mouse-position text, bottom-right
	// corner. Suppressed on NoInputs plots (sparklines, inert popups):
	// hover state stays readable through HoverPlotPos, the inlay text
	// would just be noise there.
	if st.hoverOk && !p.noInputs {
		hx := tr.plotX(st.hoverPos[0])
		hy := tr.plotY(st.hoverPos[1])
		c.PaintText(areaX+areaW-4, areaY+areaH-3, 2, 2,
			fmt.Sprintf("%.6g, %.6g", hx, hy), tickFontSize, color.Hex(colReadout)).Monospace().Send()
	}

	// --- Interaction surfaces + the canvas drain. Sense-region emission
	// order is hit-test priority (later wins): plot area first, then the
	// drag tools, then the legend rows on top of everything — a legend
	// click must never fall through and start a pan (upstream's legend
	// hover blocks plot interaction the same way). The legend was emitted
	// before the area region until M7; its rows rendered but the area
	// swallowed every click. Under NoInputs no region is stamped and the
	// canvas neither senses nor captures the wheel — the legend still
	// paints, it just is not clickable.
	if !p.noInputs {
		c.PaintSenseRegion(p.ids.PrepareStr("implot-area"), areaX, areaY, areaW, areaH).Send()
		p.emitToolChrome(tr, areaX, areaY, areaW, areaH, true)
		p.emitLegend(leg, areaX, areaY, true)
		c.PaintCanvas(p.ids.PrepareStr("implot-canvas"), p.w, p.h).
			Background(color.Hex(colPlotBg)).
			Sense(true, true, true).
			CaptureZoom().
			CaptureScroll().
			Send()
	} else {
		p.emitToolChrome(tr, areaX, areaY, areaW, areaH, false)
		p.emitLegend(leg, areaX, areaY, false)
		// Hover sense stays on: HoverPlotPos consumers (crosshairs) keep
		// working on an inert plot; only gestures and the wheel are gone.
		c.PaintCanvas(p.ids.PrepareStr("implot-canvas"), p.w, p.h).
			Background(color.Hex(colPlotBg)).
			Sense(false, false, true).
			Send()
	}

	st.prev = tr
	st.prevOk = tr.valid()

	p.emitContextMenu()
}

// emitSeries dispatches one frame-declared item to its renderer. All
// coordinates pass the f64 → f32 boundary here (SD4).
func (p *Plot) emitSeries(s *seriesFrame, tr transform, areaX, areaY, areaW, areaH float32, colHex uint32, weight float32) {
	st := p.st
	switch s.kind {
	case kindHeatmap:
		p.emitHeatmap(s, tr)
	case kindLine:
		p.emitLineWeighted(s, tr, colHex, weight)
	case kindScatter:
		n := min(len(s.xs), len(s.ys))
		st.scratchX = st.scratchX[:0]
		st.scratchY = st.scratchY[:0]
		for i := range n {
			if math.IsNaN(s.xs[i]) || math.IsNaN(s.ys[i]) {
				continue
			}
			st.scratchX = append(st.scratchX, tr.pxX(s.xs[i]))
			st.scratchY = append(st.scratchY, tr.pxY(s.ys[i]))
		}
		ra := s.radius
		if ra <= 0 {
			ra = 3.5
		}
		c.PaintMarkers(st.scratchX, st.scratchY, uint8(s.marker), ra, color.Hex(colHex), weight).Send()
	case kindBars:
		n := min(len(s.xs), len(s.ys))
		minXs := make([]float32, 0, n)
		minYs := make([]float32, 0, n)
		maxXs := make([]float32, 0, n)
		maxYs := make([]float32, 0, n)
		cols := make([]uint32, 0, n)
		hw := s.width / 2
		y0px := tr.pxY(0)
		for i := range n {
			if math.IsNaN(s.xs[i]) || math.IsNaN(s.ys[i]) {
				continue
			}
			x0 := tr.pxX(s.xs[i] - hw)
			x1 := tr.pxX(s.xs[i] + hw)
			y1 := tr.pxY(s.ys[i])
			top, bot := y1, y0px
			if top > bot {
				top, bot = bot, top
			}
			minXs = append(minXs, x0)
			minYs = append(minYs, top)
			maxXs = append(maxXs, x1)
			maxYs = append(maxYs, bot)
			cols = append(cols, colHex)
		}
		c.PaintRectsFilled(minXs, minYs, maxXs, maxYs, color.ColorsFromU32(cols)).Send()
	case kindShaded:
		// Per-segment convex quads: curve segment + its baseline mirror.
		// A single whole-run polygon would be concave, which the painter's
		// convex fill mis-tessellates (ADR-0149 SD6 records the deferred
		// alternatives).
		n := min(len(s.xs), len(s.ys))
		fill := (colHex &^ 0xff) | 0x55
		yr := tr.pxY(s.yref)
		for i := 1; i < n; i++ {
			if math.IsNaN(s.xs[i-1]) || math.IsNaN(s.ys[i-1]) || math.IsNaN(s.xs[i]) || math.IsNaN(s.ys[i]) {
				continue
			}
			qx := []float32{tr.pxX(s.xs[i-1]), tr.pxX(s.xs[i]), tr.pxX(s.xs[i]), tr.pxX(s.xs[i-1])}
			qy := []float32{tr.pxY(s.ys[i-1]), tr.pxY(s.ys[i]), yr, yr}
			c.PaintPolygonFilled(qx, qy, color.Hex(fill)).Send()
		}
		p.emitLineWeighted(s, tr, colHex, weight)
	case kindStairs:
		// Step-after: 2n-1 vertices per NaN-free run, one polyline each.
		n := min(len(s.xs), len(s.ys))
		st.scratchX = st.scratchX[:0]
		st.scratchY = st.scratchY[:0]
		flush := func() {
			if len(st.scratchX) > 1 {
				c.PaintPolyline(st.scratchX, st.scratchY, color.Hex(colHex), weight).Send()
			}
			st.scratchX = st.scratchX[:0]
			st.scratchY = st.scratchY[:0]
		}
		for i := range n {
			if math.IsNaN(s.xs[i]) || math.IsNaN(s.ys[i]) {
				flush()
				continue
			}
			px, py := tr.pxX(s.xs[i]), tr.pxY(s.ys[i])
			if k := len(st.scratchY); k > 0 {
				st.scratchX = append(st.scratchX, px)
				st.scratchY = append(st.scratchY, st.scratchY[k-1])
			}
			st.scratchX = append(st.scratchX, px)
			st.scratchY = append(st.scratchY, py)
		}
		flush()
	case kindStems:
		n := min(len(s.xs), len(s.ys))
		yr := tr.pxY(s.yref)
		st.scratchX = st.scratchX[:0]
		st.scratchY = st.scratchY[:0]
		for i := range n {
			if math.IsNaN(s.xs[i]) || math.IsNaN(s.ys[i]) {
				continue
			}
			px, py := tr.pxX(s.xs[i]), tr.pxY(s.ys[i])
			c.PaintLine(px, yr, px, py, color.Hex(colHex), weight).Send()
			st.scratchX = append(st.scratchX, px)
			st.scratchY = append(st.scratchY, py)
		}
		c.PaintMarkers(st.scratchX, st.scratchY, uint8(MarkerCircle), 3.0, color.Hex(colHex), weight).Send()
	case kindInfV:
		for _, x := range s.xs {
			if math.IsNaN(x) {
				continue
			}
			px := tr.pxX(x)
			c.PaintLine(px, areaY, px, areaY+areaH, color.Hex(colHex), weight).Send()
		}
	case kindInfH:
		for _, y := range s.ys {
			if math.IsNaN(y) {
				continue
			}
			py := tr.pxY(y)
			c.PaintLine(areaX, py, areaX+areaW, py, color.Hex(colHex), weight).Send()
		}
	case kindErrV:
		// Whiskers default to the fixed error-bar foreground (upstream's
		// ErrorBar style color), not the series color, so they read on
		// top of the bars/lines they decorate; SetNextColor overrides.
		ec := uint32(colErrorBar)
		if s.colOk {
			ec = s.colHex
		}
		n := min(len(s.xs), len(s.ys), len(s.neg), len(s.pos))
		for i := range n {
			if math.IsNaN(s.xs[i]) || math.IsNaN(s.ys[i]) {
				continue
			}
			px := tr.pxX(s.xs[i])
			py0 := tr.pxY(s.ys[i] - s.neg[i])
			py1 := tr.pxY(s.ys[i] + s.pos[i])
			c.PaintLine(px, py0, px, py1, color.Hex(ec), weight).Send()
			c.PaintLine(px-errCapPx, py0, px+errCapPx, py0, color.Hex(ec), weight).Send()
			c.PaintLine(px-errCapPx, py1, px+errCapPx, py1, color.Hex(ec), weight).Send()
		}
	case kindErrH:
		ec := uint32(colErrorBar)
		if s.colOk {
			ec = s.colHex
		}
		n := min(len(s.xs), len(s.ys), len(s.neg), len(s.pos))
		for i := range n {
			if math.IsNaN(s.xs[i]) || math.IsNaN(s.ys[i]) {
				continue
			}
			py := tr.pxY(s.ys[i])
			px0 := tr.pxX(s.xs[i] - s.neg[i])
			px1 := tr.pxX(s.xs[i] + s.pos[i])
			c.PaintLine(px0, py, px1, py, color.Hex(ec), weight).Send()
			c.PaintLine(px0, py-errCapPx, px0, py+errCapPx, color.Hex(ec), weight).Send()
			c.PaintLine(px1, py-errCapPx, px1, py+errCapPx, color.Hex(ec), weight).Send()
		}
	case kindShadedBetween:
		// One quad per segment between the curves. A crossing segment
		// renders its bowtie quad as-is, like upstream's two-curve
		// shaded. An explicit SetNextColor is used verbatim (caller owns
		// the alpha); the palette default gets a soft fill alpha.
		fill := (colHex &^ 0xff) | 0x55
		if s.colOk {
			fill = s.colHex
		}
		n := min(len(s.xs), len(s.ys), len(s.ys2))
		for i := 1; i < n; i++ {
			if math.IsNaN(s.xs[i-1]) || math.IsNaN(s.ys[i-1]) || math.IsNaN(s.ys2[i-1]) ||
				math.IsNaN(s.xs[i]) || math.IsNaN(s.ys[i]) || math.IsNaN(s.ys2[i]) {
				continue
			}
			qx := []float32{tr.pxX(s.xs[i-1]), tr.pxX(s.xs[i]), tr.pxX(s.xs[i]), tr.pxX(s.xs[i-1])}
			qy := []float32{tr.pxY(s.ys[i-1]), tr.pxY(s.ys[i]), tr.pxY(s.ys2[i]), tr.pxY(s.ys2[i-1])}
			c.PaintPolygonFilled(qx, qy, color.Hex(fill)).Send()
		}
	case kindDigital:
		// Digital channels pin to the plot-area bottom in pixel space and
		// ignore the y axis entirely (upstream contract); visible channels
		// stack upward in declaration order via the per-frame offset.
		base := areaY + areaH - 1 - p.digitalOffset
		chanMax := float32(digitalBitH)
		digitalRuns(s.xs, s.ys, func(x0, x1, v float64) {
			h := float32(1.5)
			if v > 0 {
				h = digitalBitH * float32(v)
			}
			chanMax = max(chanMax, h)
			c.PaintRectFilled(tr.pxX(x0), base-h, tr.pxX(x1), base, 0, color.Hex(colHex)).Send()
		})
		p.digitalOffset += chanMax + digitalBitGap
	case kindPieSlice:
		p.emitPieSlice(s, tr, colHex)
	case kindImage:
		p.emitImage(s, tr)
	case kindBoxes:
		p.emitBoxes(s, tr)
	case kindText:
		p.emitText(s, tr)
	case kindCustom:
		// Caller-drawn item (custom.go): hand over this frame's transform
		// and geometry; declaration order already put us at the right z.
		if s.custom == nil {
			return
		}
		dc := DrawCtx{
			T:     Transform{tr},
			AreaX: areaX, AreaY: areaY, AreaW: areaW, AreaH: areaH,
			W: p.w, H: p.h,
			Color:       colHex,
			Weight:      weight,
			Highlighted: s.label != "" && s.label == st.legendHover,
		}
		if s.unclipped {
			// Lift the plot-area clip for the call, restore it after — the
			// M0 clip stack stays balanced either way.
			c.PaintClipPop().Send()
			s.custom(dc)
			c.PaintClipPush(areaX, areaY, areaX+areaW, areaY+areaH).Send()
		} else {
			s.custom(dc)
		}
	}
}

// Digital-channel geometry: bit height scaled by the sample value, and the
// stacking gap between channels (upstream's DigitalBitHeight/DigitalBitGap
// defaults; there is no style system to override them yet, see doc.go).
const (
	digitalBitH   = 8.0
	digitalBitGap = 4.0
	errCapPx      = 3.0
)

// emitLegend draws the entry list with color swatches and — when
// interactive — stamps one sense region per entry; clicks toggle series
// visibility, hover highlights. leg holds each distinct label's first
// series index (legendIndices).
func (p *Plot) emitLegend(leg []int, areaX, areaY float32, interactive bool) {
	st := p.st
	maxChars := 0
	for _, si := range leg {
		if n := len(p.series[si].label); n > maxChars {
			maxChars = n
		}
	}
	if len(leg) == 0 {
		return
	}
	const rowH, pad, swatch = 16.0, 6.0, 10.0
	lw := pad*3 + swatch + float32(maxChars)*charW
	lh := pad*2 + float32(len(leg))*rowH
	lx, ly := areaX+8, areaY+8
	c.PaintRectFilled(lx, ly, lx+lw, ly+lh, 3.0, color.Hex(0x14171dee)).Send()
	c.PaintRectStroke(lx, ly, lx+lw, ly+lh, 3.0, color.Hex(colBorder), 1.0).Send()
	for row, si := range leg {
		s := &p.series[si]
		ry := ly + pad + float32(row)*rowH
		colHex := paletteDeep[s.slot%len(paletteDeep)]
		if s.colOk {
			colHex = s.colHex
		}
		swCol, txtCol := colHex, uint32(colTickLabel)
		if st.hidden[s.label] {
			swCol = (colHex &^ 0xff) | 0x40
			txtCol = 0x667080ff
		}
		c.PaintRectFilled(lx+pad, ry+(rowH-swatch)/2, lx+pad+swatch, ry+(rowH+swatch)/2, 2.0, color.Hex(swCol)).Send()
		c.PaintText(lx+pad*2+swatch, ry+rowH/2, 0, 1, s.label, tickFontSize, color.Hex(txtCol)).Monospace().Send()
		if interactive {
			c.PaintSenseRegion(p.ids.PrepareStr("legend-"+s.label), lx, ry, lw, rowH).Send()
		}
	}
}

// emitLineWeighted projects one series through the transform and emits one
// polyline per NaN-free run at the given stroke weight.
func (p *Plot) emitLineWeighted(s *seriesFrame, tr transform, colHex uint32, weight float32) {
	st := p.st
	n := min(len(s.xs), len(s.ys))
	st.scratchX = st.scratchX[:0]
	st.scratchY = st.scratchY[:0]
	flush := func() {
		if len(st.scratchX) == 1 {
			// A single surviving point still deserves pixels: a dot.
			c.PaintCircleFilled(st.scratchX[0], st.scratchY[0], weight, color.Hex(colHex)).Send()
		} else if len(st.scratchX) > 1 {
			c.PaintPolyline(st.scratchX, st.scratchY, color.Hex(colHex), weight).Send()
		}
		st.scratchX = st.scratchX[:0]
		st.scratchY = st.scratchY[:0]
	}
	for i := range n {
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
