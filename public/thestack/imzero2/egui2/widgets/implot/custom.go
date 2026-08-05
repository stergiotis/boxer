package implot

// Custom-item lane (ADR-0149 Update 2026-07-31): the re-expression of
// upstream ImPlot's custom-rendering idiom — GetPlotDrawList +
// PushPlotClipRect + PlotToPixels — under this port's deferred emission.
// Upstream locks axes at the first item, so PlotToPixels works immediately;
// here the frame's transform exists only inside End (auto-fit resolves
// against the whole frame's data first), so custom drawing is a closure the
// plot invokes during emission instead of an accessor the caller polls.
// Painting after End is not an option either: End emits the PaintCanvas
// drain, and later paint opcodes would land in the next widget's canvas.

// Transform maps plot space to canvas pixels through the axis scales — the
// axis-separated re-idiomization of upstream's PlotToPixels / PixelsToPlot
// pair. Obtain it from a DrawCtx; it is valid only for the frame (and the
// plot) it was handed on. The zero value is unusable.
type Transform struct{ t transform }

// PxX projects a plot-space x to a canvas-pixel x.
func (tr Transform) PxX(v float64) float32 { return tr.t.pxX(v) }

// PxY projects a plot-space y to a canvas-pixel y (plot-space up is
// pixel-space down).
func (tr Transform) PxY(v float64) float32 { return tr.t.pxY(v) }

// PlotX inverts a canvas-pixel x to plot space, through the axis scale's
// inverse (correct on log/symlog, not just linear).
func (tr Transform) PlotX(px float32) float64 { return tr.t.plotX(px) }

// PlotY inverts a canvas-pixel y to plot space.
func (tr Transform) PlotY(px float32) float64 { return tr.t.plotY(px) }

// DrawCtx is what a Custom closure receives: this frame's transform, the
// plot-area rect and full canvas size (canvas pixels — the space every
// Paint* opcode emitted by the closure uses), and the item's resolved
// style. Valid only during the call; do not retain it.
type DrawCtx struct {
	// T converts plot space ↔ canvas pixels for this frame.
	T Transform
	// AreaX/AreaY/AreaW/AreaH is the plot-area rect: the region inside the
	// axes, which clipped closures are confined to. Pixel-pinned geometry
	// (lane rows, bottom-pinned strips — the Digital-item pattern) anchors
	// on it.
	AreaX, AreaY, AreaW, AreaH float32
	// W, H is the full canvas size, gutters included — the bound for
	// CustomUnclipped drawing.
	W, H float32
	// Color is the item's resolved color: its palette slot by label, or the
	// SetNextColor override. Weight is the resolved stroke weight — the
	// SetNextWeight override or the series default, with the legend-hover
	// emphasis (×2) already applied, so drawing with it echoes the
	// built-in series highlight for free.
	Color  uint32
	Weight float32
	// Highlighted reports that the item's legend row is hovered, so custom
	// items can echo the built-in series hover emphasis.
	Highlighted bool
}

// Custom records a caller-drawn item: fn runs during End, after auto-fit
// has resolved and the frame transform exists, clipped to the plot area.
// Items emit in declaration order, so declaring a Custom before a series
// draws under it and after draws over it — upstream's call-order z-model,
// preserved across the deferred emission.
//
// A labeled Custom participates like any item: it takes a palette slot,
// gets a legend row with a visibility toggle (a hidden item's closure is
// not invoked), merges with same-label items, and reports legend hover via
// DrawCtx.Highlighted. label "" is anonymous: no legend row, own palette
// slot. SetNextColor / SetNextWeight apply and arrive as DrawCtx.Color /
// DrawCtx.Weight.
//
// The closure must only paint (and read caller state): declaring items,
// Setup* calls, or sense regions from inside it is unsupported — item
// declarations during emission are debug-logged no-ops, and sense-region
// emission order is the plot's hit-test priority contract. Custom items do
// not contribute to auto-fit; declare their extent with IncludeX /
// IncludeY. On a detached plot (NewDetached) the closure never runs.
//
// Validation: nil fn is a no-op.
func (p *Plot) Custom(label string, fn func(DrawCtx)) *Plot {
	if fn == nil {
		return p
	}
	p.addSeries(seriesFrame{kind: kindCustom, label: label, custom: fn}, false, false)
	return p
}

// CustomUnclipped is Custom without the plot-area clip: the closure may
// paint anywhere on the plot's canvas (0,0 .. W,H), e.g. callouts that
// spill past the area border, or decorations in the gutters. The gutters
// are laid out for the axes, not reserved for custom content — drawing
// into them shares space with tick labels and titles (a gutter-reservation
// knob is deferred until the timeline adoption proves the need; see the
// ADR-0149 update).
//
// Validation: nil fn is a no-op.
func (p *Plot) CustomUnclipped(label string, fn func(DrawCtx)) *Plot {
	if fn == nil {
		return p
	}
	p.addSeries(seriesFrame{kind: kindCustom, label: label, custom: fn, unclipped: true}, false, false)
	return p
}

// HoverPixelPos returns the pointer position in canvas pixels while it is
// over the plot area — the pixel-space complement of HoverPlotPos, for
// hit-testing pixel-pinned custom geometry (lane rows). One frame behind,
// like every register read; ok is false when the pointer is elsewhere or
// the plot has not rendered yet.
func (p *Plot) HoverPixelPos() (px float32, py float32, ok bool) {
	if p == nil || p.st == nil {
		return 0, 0, false // nil-safe for headless widget tests
	}
	st := p.st
	if !st.hoverOk || !st.prevOk {
		return 0, 0, false
	}
	return st.hoverPos[0], st.hoverPos[1], true
}

// ClickedPixelPos reports a primary click on the plot area in canvas
// pixels — the pixel-space complement of Clicked. One frame behind.
func (p *Plot) ClickedPixelPos() (px float32, py float32, ok bool) {
	if p == nil || p.st == nil || !p.clickOk || !p.st.prevOk {
		return 0, 0, false
	}
	return p.clickPos[0], p.clickPos[1], true
}

// PlotAreaPrev returns last frame's plot-area rect in canvas pixels. It is
// the declaration-time counterpart of DrawCtx's area fields: hit tests
// against pixel-pinned custom geometry run before End has laid this frame
// out, so they test against the previous frame's rect — the same one-frame
// lag as every readback, imperceptible at interactive rates. ok is false
// until the plot has rendered once.
func (p *Plot) PlotAreaPrev() (x float32, y float32, w float32, h float32, ok bool) {
	if p == nil || p.st == nil || !p.st.prevOk {
		return 0, 0, 0, 0, false
	}
	pr := p.st.prev
	return float32(pr.px0), float32(pr.py0), float32(pr.plotW), float32(pr.plotH), true
}

// AxisRangePrev returns the axis range the plot held entering this frame —
// what the last gesture (pan, wheel, box-zoom) left behind, before this
// frame's Setup calls and autofit revise it. It is the range counterpart of
// PlotAreaPrev, and carries the same one-frame lag for the same reason: a
// caller that must decide WHAT to declare needs the viewport before End has
// laid this frame out.
//
// The use it exists for is viewport-aware decimation — a caller holding more
// samples than the axis has pixels reduces to what the range can show, which
// it cannot do without knowing the range. ok is false until the plot has
// rendered once (declare the full series on that first frame) and for a
// degenerate range, so a caller never divides by a zero span.
func (p *Plot) AxisRangePrev(axis AxisE) (vmin float64, vmax float64, ok bool) {
	if p == nil || p.st == nil || !p.st.prevOk {
		return 0, 0, false
	}
	ax := &p.st.x
	if axis == AxisY1 {
		ax = &p.st.y
	}
	if !ax.hasRange || !(ax.rng.Max > ax.rng.Min) {
		return 0, 0, false
	}
	return ax.rng.Min, ax.rng.Max, true
}
