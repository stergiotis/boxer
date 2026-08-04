// Package view draws an icicle.Layout as implot custom items (ADR-0160).
// It is the imzero2-facing half of the widget: the icicle package stays
// UI-free, and this package is the only one that imports the egui2 bindings.
//
// implot owns the frame. The value axis carries the tree's own units, so its
// ticks read as samples, bytes or seconds and its stock gestures are exactly
// the ones the form wants: the pointer-anchored wheel zoom is a flamegraph's
// zoom, and a double-click fit is "back to the whole profile". Clicking a
// frame is an assignment to that axis rather than a re-root, which keeps the
// clicked frame's ancestors visible above it.
//
// The depth axis is different in kind, and is configured to behave that way:
// it is declared AxisFlagsNoZoom so a gesture may scroll it but not scale it,
// and its span is derived from the plot-area height so a row keeps a stable
// pixel height however deep the tree goes (ADR-0160 SD3).
//
// Hit tests are in plot space — Probe reads HoverPlotPos and Clicked and asks
// the layout directly — so they stay correct at any zoom, and there is no new
// IDL surface: frames are one batched rect call, labels are ordinary text.
package view

import (
	"math"
	"sort"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// ColorModeE selects how a frame's fill is chosen. Both modes read the IDS
// data-encoding palettes; neither invents a colour (ADR-0160 SD6).
type ColorModeE uint8

const (
	// ColorByLabel hashes the frame's label into the warm band of the
	// Lajolla sequential scale — the flamegraph's conventional red-through-
	// yellow gamut, realised from the IDS tokens rather than the classic
	// random warm RGB. The hash keeps what the randomness never had: a given
	// function keeps its colour everywhere it appears, including across two
	// captures, which is what makes two pictures comparable. The scale is
	// pinned; Opts.Sequential configures only ColorByDepth.
	ColorByLabel ColorModeE = iota
	// ColorByDepth ramps a sequential palette over the depth, which reads the
	// structure rather than the identity.
	ColorByDepth
)

// Hit is a node index, or nothing. It is a struct rather than a bare int
// because the zero value has to mean "nothing" while leaving node 0
// addressable — an Opts{} must not silently select the root.
//
// It reads the same way as sankey/view.Hit, the other widget on this lane:
// Hit{} is the empty one and None reports it.
type Hit struct {
	// Node indexes Layout.Nodes. Meaningful only when Ok.
	Node int32
	// Ok distinguishes a real hit from the empty one.
	Ok bool
}

// NodeHit builds a hit on a layout node index.
func NodeHit(i int) Hit { return Hit{Node: int32(i), Ok: true} }

// None reports the empty hit — the zero value, and what a probe over empty
// plot area returns.
func (h Hit) None() bool { return !h.Ok }

// Opts configures one draw. The zero value is usable: colour by label,
// labels shown, no legend, nothing highlighted.
type Opts struct {
	// RowPx is the row height in canvas pixels. It is a *minimum*: when the
	// whole tree fits in the plot area the rows share the height out and are
	// taller, and only once it does not do rows settle at exactly this and
	// the depth axis start scrolling. 0 means DefaultRowPx.
	RowPx float32
	// Color selects the fill scheme.
	Color ColorModeE
	// Sequential is the palette ColorByDepth ramps. The zero value defers to
	// the user's configured default (styletokens.SequentialDefault).
	//
	// That zero is also SequentialBatlow, so Batlow cannot be asked for by
	// name — it arrives only by being the configured default. Deliberate: a
	// widget deferring to the user's IDS setting is the behaviour worth
	// having at the zero value, and the alternative costs a sentinel in a
	// shared enum.
	Sequential styletokens.SequentialE
	// NodeColor overrides a frame's fill; ok=false falls back to the scheme.
	NodeColor func(n *icicle.Node) (rgba uint32, ok bool)
	// Hover and Selected are emphasised with an outline. Hosts set Hover from
	// what Probe returned and Selected from their own pinning.
	Hover, Selected Hit
	// HideLabels suppresses the frame labels entirely.
	HideLabels bool
	// FontSize for frame labels; 0 means DefaultFontSize.
	FontSize float32
	// XLabel names the value axis; empty falls back to the layout's unit.
	XLabel string
	// Legend labels the drawing so implot renders a legend row, whose
	// visibility toggle hides the icicle. One row, not two: the frames and
	// their labels are one picture, and a label without its frame under it is
	// unreadable. Use HideLabels for a labels-only switch.
	//
	// Off by default: the legend costs plot area and overlays the frames,
	// which are self-labelling.
	Legend bool
	// ResetView re-applies the initial ranges this frame, discarding whatever
	// the user has zoomed or scrolled to.
	//
	// Set it on the frame the layout is replaced. A plot's ranges are
	// retained per plot id and the initial limits are applied CondOnce, so a
	// host that swaps in a tree with a different total would otherwise keep
	// looking at the previous tree's value window — showing a slice of the
	// new tree, or nothing at all. The widget cannot detect the swap for
	// itself: a Layout carries no identity to compare against.
	//
	// It outranks a click that arrived the same frame: that click was
	// resolved against the outgoing layout, so honouring its zoom would land
	// on whatever node happens to occupy those coordinates in the new one.
	ResetView bool
}

// Drawing defaults. Exported because a host reproducing the geometry needs
// the same numbers.
const (
	// DefaultRowPx is the minimum row height. Comfortably above the label
	// font size, so a row that gets a label can show it.
	DefaultRowPx = 18.0
	// DefaultFontSize is the frame-label size.
	DefaultFontSize = styletokens.CaptionPt
)

const (
	// gapPx separates neighbouring frames. Two abutting rectangles of similar
	// colour read as one, and a hash into one warm band puts similar-coloured
	// siblings next to each other constantly; the gap is background showing
	// through, so it costs no extra paint call.
	gapPx = 1.0
	// minRectPx is the narrowest frame still worth emitting, measured after
	// the gap has been taken out of it. Below this a rectangle is a sliver
	// that says nothing, and at a deep zoom there are a great many of them.
	//
	// Because the edges are snapped first, the surviving width is a whole
	// number of pixels, so the effective cut is at minRectPx+gapPx on the
	// un-inset span — 2 px, not the fraction this constant reads as. snapX
	// is where that is decided, and the hit test goes through it too, so a
	// frame that is not drawn is not hittable either.
	minRectPx = 0.75
	// labelPadPx is the inset between a frame's edge and its label.
	labelPadPx = 3.0
	// spanEps is the slack on the derived depth-axis span before it is
	// re-asserted; below it the difference is sub-pixel.
	spanEps = 1e-3
)

// Show opens a plot sized w×h, configures its axes, probes the pointer, draws
// lay, and returns what the pointer was over. It is the straight-line entry
// point; a host that wants different click behaviour, or its own items in the
// same plot, should drive implot itself and call Probe, Setup and Draw.
//
// A click on a frame zooms the value axis to that frame — the conventional
// flamegraph gesture. A double-click anywhere fits back to the whole tree,
// which is implot's own reset and needs nothing here.
//
// clicked distinguishes a click that landed on no frame (click.None(),
// clicked true) from no click at all (clicked false) — the difference a host
// needs to implement click-to-pin with click-away-to-clear.
//
// title is the label above the plot and also its retained identity: implot
// keys a plot's ranges, legend state and previous transform by it, under the
// "visible##hidden" convention Begin documents. Two icicles in one frame
// sharing a title share one set of ranges — give them "Alloc##left" and
// "Alloc##right" rather than the same string.
//
// The returned hover and click are one frame behind, like every register read.
func Show(ids *c.WidgetIdStack, title string, w float32, h float32, lay *icicle.Layout, opts Opts) (hover Hit, click Hit, clicked bool) {
	var r Renderer
	return r.Show(ids, title, w, h, lay, opts)
}

// Show is the Renderer method behind the free function, reusing this
// Renderer's buffers across frames.
func (r *Renderer) Show(ids *c.WidgetIdStack, title string, w float32, h float32, lay *icicle.Layout, opts Opts) (hover Hit, click Hit, clicked bool) {
	if lay == nil {
		return Hit{}, Hit{}, false
	}
	for p := range implot.Scoped(ids, title, w, h) {
		hover, click, clicked = Probe(p, lay)
		Setup(p, lay, opts)
		if click.Ok && !opts.ResetView {
			// CondAlways, but only on the frame the click arrived, so the
			// zoom sticks without pinning the axis against later panning.
			//
			// Both this and Setup's reset are CondAlways and this one runs
			// second, so on the frame a layout is replaced it would otherwise
			// win — zooming to a node resolved against the outgoing layout's
			// transform, and undoing the reset that was the point of the swap.
			ZoomTo(p, lay, int(click.Node))
		}
		opts.Hover = hover
		r.Draw(p, lay, opts)
	}
	return hover, click, clicked
}

// Setup applies the axis configuration an icicle needs: a value axis in the
// tree's own units, and a depth axis that scrolls but does not scale. Call it
// before Draw when driving implot yourself.
func Setup(p *implot.Plot, lay *icicle.Layout, opts Opts) {
	if p == nil || lay == nil {
		return
	}
	opts = opts.withDefaults(lay)

	xlabel := opts.XLabel
	if xlabel == "" {
		xlabel = lay.Report.Unit
	}
	p.SetupAxes(xlabel, "",
		implot.AxisFlagsNone,
		implot.AxisFlagsNoGrid|implot.AxisFlagsNoTickLabels|implot.AxisFlagsNoZoom)
	initial := implot.CondOnce
	if opts.ResetView {
		initial = implot.CondAlways
	}
	p.SetupAxisLimits(implot.AxisX1, 0, lay.Report.Total, initial)
	p.SetupAxisLimitsConstraints(implot.AxisX1, 0, lay.Report.Total)

	rows := float64(lay.Report.Rows)
	flame := lay.Orientation == icicle.OrientFlame
	_, _, _, areaH, areaOk := p.PlotAreaPrev()
	span := depthSpan(rows, areaH, opts.RowPx, areaOk)

	// Bound the scroll to the tree.
	clampLo, clampHi := rootWindow(rows, flame)
	p.SetupAxisLimitsConstraints(implot.AxisY1, clampLo, clampHi)

	cur0, cur1, known := p.AxisLimits(implot.AxisY1)
	lo, hi, cond := depthWindow(cur0, cur1, span, flame, known, opts.ResetView)
	p.SetupAxisLimits(implot.AxisY1, lo, hi, cond)

	if !opts.Legend {
		p.NoLegend()
	}
}

// depthSpan is the depth axis's span in rows: however many rows of rowPx fit
// in the plot area, capped at the tree's own depth. The span is derived, not
// chosen — that is what makes RowPx a minimum rather than a height. A shallow
// tree hits the cap and fills the pane with taller rows instead of leaving
// most of it blank; a deep one sits at exactly rowPx and scrolls.
//
// areaOk is false before the plot has rendered once: there is no area to
// divide then, so the whole tree is the span.
func depthSpan(rows float64, areaH float32, rowPx float32, areaOk bool) float64 {
	if !areaOk || !(areaH > 0) || !(rowPx > 0) {
		return rows
	}
	if visible := float64(areaH / rowPx); visible < rows {
		return visible
	}
	return rows
}

// depthWindow resolves the depth axis's limits and the condition to apply
// them under.
//
// The span is derived from the pane, so it has to be re-asserted when the
// pane changes — but asserting it every frame would pin the axis and a scroll
// could never stick. Hence the condition is part of the answer: CondAlways
// only when the window is genuinely wrong, CondOnce otherwise.
func depthWindow(cur0 float64, cur1 float64, span float64, flame bool, known bool, reset bool) (lo float64, hi float64, cond implot.Cond) {
	switch {
	case reset:
		// Back to the root edge, discarding any depth scroll.
		lo, hi = rootWindow(span, flame)
		return lo, hi, implot.CondAlways
	case known && math.Abs((cur1-cur0)-span) > spanEps:
		// The pane resized, so the window no longer holds rows at rowPx.
		// Re-assert the span while keeping the root-side edge, which is what
		// stops a resize from scrolling the view as well as re-scaling it.
		if flame {
			return cur0, cur0 + span, implot.CondAlways
		}
		return cur1 - span, cur1, implot.CondAlways
	default:
		// The span still holds. CondOnce seeds the window on the first frame
		// and leaves it alone on every one after, so a pan sticks.
		lo, hi = rootWindow(span, flame)
		return lo, hi, implot.CondOnce
	}
}

// rootWindow places a span against the root edge. The root sits at y=0 in
// both orientations, so the window runs away from zero in the growth
// direction: with the tree's full depth this is the scroll bound, and with
// the derived span it is the initial view.
func rootWindow(span float64, flame bool) (lo float64, hi float64) {
	if flame {
		return 0, span
	}
	return -span, 0
}

// ZoomTo pins the value axis to one node's span — the click-to-zoom of a
// flamegraph. It is an axis assignment rather than a re-root, so the node's
// ancestors stay on screen above it, spanning the full width as they should.
//
// CondAlways, so call it only on the frame the gesture arrived; leaving it on
// every frame would pin the axis against panning.
func ZoomTo(p *implot.Plot, lay *icicle.Layout, node int) {
	if p == nil || lay == nil || node < 0 || node >= len(lay.Nodes) {
		return
	}
	n := &lay.Nodes[node]
	if !(n.X1 > n.X0) {
		return
	}
	p.SetupAxisLimits(implot.AxisX1, n.X0, n.X1, implot.CondAlways)
}

// Probe resolves the pointer against the layout, in plot space.
//
// clicked reports that a click happened at all, which is not the same as
// !click.None(): a click that landed on empty plot area returns the empty hit
// with clicked true, and that is what clears a pinned selection.
func Probe(p *implot.Plot, lay *icicle.Layout) (hover Hit, click Hit, clicked bool) {
	if p == nil || lay == nil {
		return Hit{}, Hit{}, false
	}
	proj, projOk := plotXProj(p)
	resolve := func(x float64, y float64) Hit {
		return resolveHit(lay, proj, projOk, x, y)
	}
	if x, y, ok := p.HoverPlotPos(); ok {
		hover = resolve(x, y)
	}
	if x, y, ok := p.Clicked(); ok {
		click, clicked = resolve(x, y), true
	}
	return hover, click, clicked
}

// resolveHit is Probe's decision for one point: the layout's answer, less the
// frames that were too narrow to have been drawn.
//
// The layout hit test is pure geometry and has no minimum, so on its own it
// reports slivers the renderer culled — a hover that names a function with
// nothing under the pointer, and an outline with no rectangle to trace. The
// cull is a pixel judgement, so it is applied here rather than pushed down
// into the layout, which never touches pixels (ADR-0160 SD5).
//
// With projOk false the plot has not rendered yet, so nothing has been culled
// and every hit stands.
func resolveHit(lay *icicle.Layout, proj xProj, projOk bool, x float64, y float64) Hit {
	i := lay.NodeAt(x, y)
	if i < 0 {
		return Hit{}
	}
	if projOk {
		n := &lay.Nodes[i]
		if _, _, wide := snapX(proj.px(n.X0), proj.px(n.X1)); !wide {
			return Hit{}
		}
	}
	return NodeHit(i)
}

// xProj is the value-to-pixel mapping of the value axis, as it stood when the
// plot last rendered. It is the half of a frame's transform the hit test
// needs, recovered from the plot's own readbacks rather than captured during
// a draw — Probe runs before Setup and outside any Custom closure, so there
// is no DrawCtx to take it from.
type xProj struct {
	originPx float64 // pixel x of vMin
	vMin     float64
	perValue float64 // pixels per value unit
}

func (x xProj) px(v float64) float64 { return x.originPx + (v-x.vMin)*x.perValue }

// plotXProj recovers last frame's value-axis mapping. ok is false before the
// plot has rendered once, and for a degenerate area or range.
//
// The axis is linear, which is what makes two readbacks enough. The widget
// always sets it that way; a host driving implot itself and choosing a log
// axis would get a mapping that disagrees with the drawn one.
func plotXProj(p *implot.Plot) (proj xProj, ok bool) {
	areaX, _, areaW, _, areaOk := p.PlotAreaPrev()
	vMin, vMax, limOk := p.AxisLimits(implot.AxisX1)
	if !areaOk || !limOk || !(areaW > 0) || !(vMax > vMin) {
		return xProj{}, false
	}
	return xProj{
		originPx: float64(areaX),
		vMin:     vMin,
		perValue: float64(areaW) / (vMax - vMin),
	}, true
}

// Draw declares the layout's custom items into p: frames, then labels.
// Declaration order is z-order, so a host adding its own items can place them
// relative to these.
func Draw(p *implot.Plot, lay *icicle.Layout, opts Opts) {
	var r Renderer
	r.Draw(p, lay, opts)
}

// Draw is the Renderer method behind the free function, reusing this
// Renderer's buffers across frames.
func (r *Renderer) Draw(p *implot.Plot, lay *icicle.Layout, opts Opts) {
	if p == nil || lay == nil {
		return
	}
	st := &r.s
	st.prepare(lay, opts.withDefaults(lay))
	// Custom items do not contribute to auto-fit, so both extents have to be
	// declared or a fit would fit to nothing. The depth axis is NoZoom, which
	// keeps the gestures off it but not Plot.FitNext — and an undeclared axis
	// fits to the seeded +Inf/-Inf, which sanitises to a single visible row.
	p.IncludeX(0)
	p.IncludeX(lay.Report.Total)
	rowLo, rowHi := rootWindow(float64(lay.Report.Rows), lay.Orientation == icicle.OrientFlame)
	p.IncludeY(rowLo)
	p.IncludeY(rowHi)

	// Both layers take the same legend label, so they get one legend row and
	// one toggle: implot keys its hidden set by label and dedups the rows by
	// it. Two rows would let a viewer hide the frames and keep the labels,
	// and the label ink is picked to contrast with a frame's fill — for most
	// of the palette that is near-black, invisible on the plot background.
	// Opts.HideLabels is the labels-only switch.
	label := ""
	if st.opts.Legend {
		label = "frames"
	}
	p.Custom(label, st.drawFrames)
	if !st.opts.HideLabels {
		p.Custom(label, st.drawLabels)
	}
}

// withDefaults resolves the zero values, and folds an out-of-range hit onto
// the empty one so a stale index from a previous layout cannot address a node.
func (o Opts) withDefaults(lay *icicle.Layout) Opts {
	if o.RowPx <= 0 {
		o.RowPx = DefaultRowPx
	}
	if o.FontSize <= 0 {
		o.FontSize = DefaultFontSize
	}
	n := int32(len(lay.Nodes))
	for _, h := range []*Hit{&o.Hover, &o.Selected} {
		if h.Ok && (h.Node < 0 || h.Node >= n) {
			*h = Hit{}
		}
	}
	return o
}

// Renderer owns the scratch buffers a draw needs, so a host that keeps one
// across frames allocates only on the first — the free Draw and Show
// functions build a throwaway Renderer per call, which is fine for a small
// tree and wasteful for a large one in a render loop.
//
// One Renderer per pane. The buffers are filled at declaration time and read
// by the Custom closures during End, so two icicles sharing a Renderer in one
// frame would see the second overwrite the first's geometry.
type Renderer struct {
	s state
}

// state carries the per-draw scratch buffers.
//
// The rect buffers are parallel arrays over the visible frames, and they
// carry the node index and the fill as well as the geometry so the label pass
// can read them instead of projecting and hashing every node a second time.
// That matters because the widget's cost argument (ADR-0160 C4) is that a
// frame's work tracks the nodes on screen — twice that is still linear, but
// it is twice.
type state struct {
	lay  *icicle.Layout
	opts Opts
	seq  styletokens.SequentialE
	// batched rect scratch, one entry per visible frame
	rMinX, rMinY, rMaxX, rMaxY []float32
	rCols                      []uint32
	rNode                      []int32 // index into lay.Nodes
	// collected says the buffers hold this draw's frames. Cleared by prepare
	// and set by collectFrames, so whichever custom item runs first fills
	// them and the other reads them.
	collected bool
}

func (s *state) prepare(lay *icicle.Layout, opts Opts) {
	s.lay, s.opts = lay, opts
	s.collected = false
	s.seq = opts.Sequential
	if s.seq == 0 {
		s.seq = styletokens.SequentialDefault()
	}
}

// fill resolves a frame's colour.
func (s *state) fill(n *icicle.Node) uint32 {
	if s.opts.NodeColor != nil {
		if col, ok := s.opts.NodeColor(n); ok {
			return col
		}
	}
	if s.opts.Color == ColorByDepth {
		t := float32(0)
		if d := s.lay.Report.MaxDepth; d > 0 {
			t = float32(n.Depth) / float32(d)
		}
		return styletokens.Sequential(s.seq, t).AsHex()
	}
	return styletokens.Sequential(styletokens.SequentialLajolla, flameT(labelHash(n.Label))).AsHex()
}

// The flame band is the slice of the Lajolla scale a label hash lands in.
// The full scale runs near-black to cream; the band keeps the middle that
// reads as fire — deep red through orange to golden yellow — and cuts the
// ends that read as background: below it the browns sink toward the plot
// surface, above it the creams wash into it. Lajolla's lightness varies
// monotonically, so across the band two names differ in lightness as well
// as hue — which is also what keeps the scheme legible under CVD, where the
// classic near-equal-lightness warm noise is not.
const (
	flameBandLo = 0.30
	flameBandHi = 0.90
)

// flameT maps a label hash onto the flame band. labelHash keeps 31 bits;
// dividing by 2³¹ lands in [0, 1] rather than [0, 1), float32 rounding the
// topmost hashes onto 1 exactly — fine, because the band ceiling is itself a
// flame colour and Sequential clamps.
func flameT(h uint32) float32 {
	return flameBandLo + (flameBandHi-flameBandLo)*(float32(h)/(1<<31))
}

// labelHash is FNV-1a over the label. Any stable hash would do; what matters
// is that it depends only on the name, so a frame keeps its colour across
// captures and across positions in the tree.
func labelHash(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h & 0x7fffffff
}

// frame is one draw's projection pair plus the plot-area rect: everything the
// geometry below needs from a DrawCtx. It exists so that the visible-window
// and rectangle arithmetic — the part with the sign rules and the rounding —
// can be exercised without a live plot, since implot.Transform cannot be
// constructed outside its own package.
type frame struct {
	pxX, pxY     func(float64) float32
	plotY        func(float32) float64
	plotX        func(float32) float64
	areaX, areaY float32
	areaW, areaH float32
}

func frameOf(dc implot.DrawCtx) frame {
	return frame{
		pxX: dc.T.PxX, pxY: dc.T.PxY,
		plotX: dc.T.PlotX, plotY: dc.T.PlotY,
		areaX: dc.AreaX, areaY: dc.AreaY, areaW: dc.AreaW, areaH: dc.AreaH,
	}
}

// visibleRows is the inclusive row window the plot area covers, and reports
// false when the layout is entirely off screen.
func (s *state) visibleRows(f frame) (lo int, hi int, ok bool) {
	a := s.lay.RowDist(f.plotY(f.areaY))
	b := s.lay.RowDist(f.plotY(f.areaY + f.areaH))
	if a > b {
		a, b = b, a
	}
	// Bound the window before converting, for the reason Layout.DepthAt
	// spells out: a degenerate transform hands back a non-finite coordinate,
	// and int() of one is INT_MIN here rather than anything a clamp on the
	// far side would recognise. `!(a <= b)` is the NaN branch.
	last := float64(len(s.lay.Rows) - 1)
	if !(a <= b) || a > last || b < 0 {
		return 0, 0, false
	}
	if a < 0 {
		a = 0
	}
	if b > last {
		b = last
	}
	return int(math.Floor(a)), int(math.Floor(b)), true
}

// visibleValues is the value window the plot area covers.
func (s *state) visibleValues(f frame) (lo float64, hi float64) {
	lo = f.plotX(f.areaX)
	hi = f.plotX(f.areaX + f.areaW)
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi
}

// eachVisible calls fn for every node inside the plot area, row by row. The
// row is binary-searched rather than scanned, so the cost tracks what is on
// screen and not the size of the tree (ADR-0160 SD7).
func (s *state) eachVisible(f frame, fn func(n *icicle.Node, idx int)) {
	rowLo, rowHi, ok := s.visibleRows(f)
	if !ok {
		return
	}
	xLo, xHi := s.visibleValues(f)
	for d := rowLo; d <= rowHi; d++ {
		row := s.lay.Rows[d]
		i := sort.Search(len(row), func(k int) bool { return s.lay.Nodes[row[k]].X1 > xLo })
		for ; i < len(row); i++ {
			n := &s.lay.Nodes[row[i]]
			if n.X0 >= xHi {
				break
			}
			fn(n, int(row[i]))
		}
	}
}

// rectOf projects a node to its pixel rectangle, already inset by the
// separating gap. ok is false for a frame too narrow or too short to draw.
//
// The edges are snapped to whole pixels: neighbours share a boundary value
// and so round identically, which keeps the gap uniform instead of letting
// each edge feather by a different fraction of a pixel.
func rectOf(f frame, n *icicle.Node) (x0, y0, x1, y1 float32, ok bool) {
	sx0, sx1, wide := snapX(float64(f.pxX(n.X0)), float64(f.pxX(n.X1)))
	// Plot y grows up and pixel y grows down, so the node's upper edge Y1 is
	// the smaller pixel value.
	y0 = float32(math.Round(float64(f.pxY(n.Y1))))
	y1 = float32(math.Round(float64(f.pxY(n.Y0)))) - gapPx
	if !wide || y1 <= y0 {
		return 0, 0, 0, 0, false
	}
	return float32(sx0), y0, float32(sx1), y1, true
}

// snapX turns a frame's unsnapped pixel edges into the drawn ones and reports
// whether what is left is wide enough to draw.
//
// It is one function rather than an inlined pair of expressions because the
// hit test has to reach the same verdict: a frame the renderer culls must not
// be hoverable, or the pointer names a node the eye cannot see and the hover
// ring outlines nothing. Two copies of this arithmetic would drift.
//
// The edges snap to whole pixels: neighbours share a boundary value and so
// round identically, which keeps the gap uniform instead of letting each edge
// feather by a different fraction of a pixel.
func snapX(px0 float64, px1 float64) (x0 float64, x1 float64, ok bool) {
	x0 = math.Round(px0)
	x1 = math.Round(px1) - gapPx
	return x0, x1, x1-x0 >= minRectPx
}

// collectFrames fills the batch buffers with every visible frame's rectangle.
// It is kept apart from the emission so the geometry — the sign rules, the
// pixel snapping and the culling — can be exercised without an FFI runtime.
func (s *state) collectFrames(f frame) {
	s.rMinX, s.rMinY = s.rMinX[:0], s.rMinY[:0]
	s.rMaxX, s.rMaxY, s.rCols = s.rMaxX[:0], s.rMaxY[:0], s.rCols[:0]
	s.rNode = s.rNode[:0]
	s.eachVisible(f, func(n *icicle.Node, idx int) {
		x0, y0, x1, y1, ok := rectOf(f, n)
		if !ok {
			return
		}
		s.rMinX = append(s.rMinX, x0)
		s.rMinY = append(s.rMinY, y0)
		s.rMaxX = append(s.rMaxX, x1)
		s.rMaxY = append(s.rMaxY, y1)
		s.rCols = append(s.rCols, s.fill(n))
		s.rNode = append(s.rNode, int32(idx))
	})
	s.collected = true
}

func (s *state) drawFrames(dc implot.DrawCtx) {
	f := frameOf(dc)
	s.collectFrames(f)
	if len(s.rMinX) == 0 {
		return
	}
	c.PaintRectsFilled(s.rMinX, s.rMinY, s.rMaxX, s.rMaxY, color.ColorsFromU32(s.rCols)).Send()

	// Emphasis is an outline in the accent role rather than a colour change,
	// so it does not collide with the fill scheme.
	ring := color.Hex(styletokens.AccentStrong.AsHex())
	for _, h := range [2]Hit{s.opts.Hover, s.opts.Selected} {
		if !h.Ok {
			continue
		}
		n := &s.lay.Nodes[h.Node]
		if x0, y0, x1, y1, ok := rectOf(f, n); ok {
			c.PaintRectStroke(x0, y0, x1, y1, styletokens.RoundingNone, ring, styletokens.StrokeRegular).Send()
		}
	}
}

// labelFor decides what text a frame gets and where it goes, or reports
// false when the rectangle cannot hold a readable label. Split from the
// emission for the same reason collectFrames is.
func (s *state) labelFor(f frame, n *icicle.Node) (text string, x float32, y float32, ok bool) {
	x0, y0, x1, y1, vis := rectOf(f, n)
	if !vis {
		return "", 0, 0, false
	}
	return s.labelIn(x0, y0, x1, y1, n.Label)
}

// labelIn is labelFor once the rectangle is known: the draw path already has
// one in the batch buffers and has no reason to project the node again.
func (s *state) labelIn(x0 float32, y0 float32, x1 float32, y1 float32, label string) (text string, x float32, y float32, ok bool) {
	size := s.opts.FontSize
	if y1-y0 < size {
		return "", 0, 0, false // the row is shorter than the glyphs
	}
	avail := x1 - x0 - 2*labelPadPx
	text = implot.Elide(label, avail, size)
	if text == "" {
		return "", 0, 0, false
	}
	return text, x0 + labelPadPx, (y0 + y1) / 2, true
}

func (s *state) drawLabels(dc implot.DrawCtx) {
	// Normally drawFrames has already filled the buffers — it is declared
	// first, and the two items share a legend label so they are hidden
	// together. Collecting here as well keeps that an optimisation rather
	// than a requirement.
	if !s.collected {
		s.collectFrames(frameOf(dc))
	}
	size := s.opts.FontSize
	for i := range s.rNode {
		text, x, y, ok := s.labelIn(s.rMinX[i], s.rMinY[i], s.rMaxX[i], s.rMaxY[i],
			s.lay.Nodes[s.rNode[i]].Label)
		if !ok {
			continue
		}
		c.PaintText(x, y, 0, 1, text, size, color.Hex(contrastText(s.rCols[i]))).Send()
	}
}

// contrastText picks the readable ink for a fill by WCAG relative luminance.
// Both ends are IDS neutrals rather than pure black and white.
func contrastText(fill uint32) uint32 {
	if implot.RelativeLuminance(fill) > inkSwitchL {
		return styletokens.NeutralBgExtreme.AsHex()
	}
	return styletokens.NeutralTextExtreme.AsHex()
}

// inkSwitchL is the fill luminance at which the two neutrals contrast equally
// against it; above the switch the dark ink is the better of the two. It works
// out at 0.1734 for the palette as it stands, where both inks give 4.45:1.
//
// Derived rather than written down, so that regenerating the IDS palette
// moves it instead of leaving a stale constant behind. It is not the line
// between readable and not, only between the better and the worse of two
// readable choices. The flame band crosses the switch — a dark-to-light ramp
// must — so a fill sitting exactly on it reads at the 4.45:1 above, a hair
// under WCAG AA's 4.5 and the band's single worst point; the classic
// flamegraph draws black on its deepest red at nearer 3:1.
var inkSwitchL = func() float64 {
	d := implot.RelativeLuminance(styletokens.NeutralBgExtreme.AsHex())
	l := implot.RelativeLuminance(styletokens.NeutralTextExtreme.AsHex())
	return math.Sqrt((d+0.05)*(l+0.05)) - 0.05
}()
