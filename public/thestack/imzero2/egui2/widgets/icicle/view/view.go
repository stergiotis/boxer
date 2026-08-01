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
	// ColorByLabel hashes the frame's label into the qualitative cycle
	// (ADR-0156). A given function keeps its colour everywhere it appears,
	// including across two captures, which is what makes two pictures
	// comparable. The classic flamegraph's warm random palette is decorative
	// by intent; this keeps its stability and drops the randomness.
	ColorByLabel ColorModeE = iota
	// ColorByDepth ramps a sequential palette over the depth, which reads the
	// structure rather than the identity.
	ColorByDepth
)

// Hit is a node index, or nothing. It is a struct rather than a bare int
// because the zero value has to mean "nothing" while leaving node 0
// addressable — an Opts{} must not silently select the root.
type Hit struct {
	// Node indexes Layout.Nodes. Meaningful only when Ok.
	Node int32
	// Ok distinguishes a real hit from the empty one.
	Ok bool
}

// NoHit is the empty result, and also the zero value.
var NoHit = Hit{}

// NodeHit builds a hit on a layout node index.
func NodeHit(i int) Hit { return Hit{Node: int32(i), Ok: true} }

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
	// Legend gives the two drawing layers (frames, labels) labelled custom
	// items, so implot renders a legend row with a visibility toggle for
	// each. Off by default: the legend costs plot area and overlays the
	// frames, which are self-labelling.
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
	// colour read as one, and a seven-colour cycle puts same-coloured
	// siblings next to each other often enough to matter; the gap is
	// background showing through, so it costs no extra paint call.
	gapPx = 1.0
	// minRectPx is the narrowest frame still worth emitting. Below this a
	// rectangle is a sliver that cannot be read or clicked, and at a deep
	// zoom there are a great many of them.
	minRectPx = 0.75
	// labelPadPx is the inset between a frame's edge and its label.
	labelPadPx = 3.0
	// glyphWidthRatio estimates a glyph's advance as a fraction of the font
	// size. Text measurement is still deferred (ADR-0149 SD6), so label
	// fitting is an estimate — it under-measures CJK, which will elide late.
	glyphWidthRatio = 0.6
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
// clicked distinguishes a click that landed on no frame (click == NoHit,
// clicked true) from no click at all (clicked false) — the difference a host
// needs to implement click-to-pin with click-away-to-clear.
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
		return NoHit, NoHit, false
	}
	for p := range implot.Scoped(ids, title, w, h) {
		hover, click, clicked = Probe(p, lay)
		Setup(p, lay, opts)
		if click.Ok {
			// CondAlways, but only on the frame the click arrived, so the
			// zoom sticks without pinning the axis against later panning.
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
	// The span is derived, not chosen: it is however many rows of RowPx fit
	// in the area. Capped at the tree's depth, so a shallow tree fills the
	// pane with taller rows instead of leaving most of it blank.
	span := rows
	if _, _, _, areaH, ok := p.PlotAreaPrev(); ok && opts.RowPx > 0 {
		if visible := float64(areaH / opts.RowPx); visible < rows {
			span = visible
		}
	}
	// Bound the scroll to the tree. The root sits at the y=0 edge either way,
	// so the extent runs away from zero in the growth direction.
	lo, hi := -rows, 0.0
	if lay.Orientation == icicle.OrientFlame {
		lo, hi = 0.0, rows
	}
	p.SetupAxisLimitsConstraints(implot.AxisY1, lo, hi)

	flame := lay.Orientation == icicle.OrientFlame
	cur0, cur1, known := p.AxisLimits(implot.AxisY1)
	switch {
	case opts.ResetView:
		// Back to the root edge, discarding any depth scroll.
		if flame {
			p.SetupAxisLimits(implot.AxisY1, 0, span, implot.CondAlways)
		} else {
			p.SetupAxisLimits(implot.AxisY1, -span, 0, implot.CondAlways)
		}
	case known && math.Abs((cur1-cur0)-span) > spanEps:
		// The pane resized, so the span no longer holds RowPx. Re-assert it
		// while keeping the root-side edge, which is what stops a resize from
		// also scrolling the view.
		if flame {
			p.SetupAxisLimits(implot.AxisY1, cur0, cur0+span, implot.CondAlways)
		} else {
			p.SetupAxisLimits(implot.AxisY1, cur1-span, cur1, implot.CondAlways)
		}
	case flame:
		p.SetupAxisLimits(implot.AxisY1, 0, span, implot.CondOnce)
	default:
		p.SetupAxisLimits(implot.AxisY1, -span, 0, implot.CondOnce)
	}

	if !opts.Legend {
		p.NoLegend()
	}
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
// click != NoHit: a click that landed on empty plot area returns NoHit with
// clicked true, and that is what clears a pinned selection.
func Probe(p *implot.Plot, lay *icicle.Layout) (hover Hit, click Hit, clicked bool) {
	if p == nil || lay == nil {
		return NoHit, NoHit, false
	}
	resolve := func(x float64, y float64) Hit {
		if i := lay.NodeAt(x, y); i >= 0 {
			return NodeHit(i)
		}
		return NoHit
	}
	if x, y, ok := p.HoverPlotPos(); ok {
		hover = resolve(x, y)
	}
	if x, y, ok := p.Clicked(); ok {
		click, clicked = resolve(x, y), true
	}
	return hover, click, clicked
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
	// Custom items do not contribute to auto-fit, so the extent has to be
	// declared or a double-click would fit to nothing. Only x: the depth axis
	// is NoZoom, so it never fits.
	p.IncludeX(0)
	p.IncludeX(lay.Report.Total)

	label := func(s string) string {
		if opts.Legend {
			return s
		}
		return ""
	}
	p.Custom(label("frames"), st.drawFrames)
	if !st.opts.HideLabels {
		p.Custom(label("labels"), st.drawLabels)
	}
}

// withDefaults resolves the zero values, and folds an out-of-range hit onto
// NoHit so a stale index from a previous layout cannot address a node.
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
			*h = NoHit
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
type state struct {
	lay  *icicle.Layout
	opts Opts
	seq  styletokens.SequentialE
	// batched rect scratch
	rMinX, rMinY, rMaxX, rMaxY []float32
	rCols                      []uint32
}

func (s *state) prepare(lay *icicle.Layout, opts Opts) {
	s.lay, s.opts = lay, opts
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
	return styletokens.QualitativeCycle(int(labelHash(n.Label))).AsHex()
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
	a := s.rowDist(f.plotY(f.areaY))
	b := s.rowDist(f.plotY(f.areaY + f.areaH))
	if a > b {
		a, b = b, a
	}
	lo, hi = int(math.Floor(a)), int(math.Floor(b))
	if lo < 0 {
		lo = 0
	}
	if last := len(s.lay.Rows) - 1; hi > last {
		hi = last
	}
	return lo, hi, lo <= hi
}

// rowDist is the distance from the root edge in row units, which is the depth
// before flooring — the view-side counterpart of the layout's sign rule.
func (s *state) rowDist(y float64) float64 {
	if s.lay.Orientation == icicle.OrientFlame {
		return y
	}
	return -y
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
	x0 = float32(math.Round(float64(f.pxX(n.X0))))
	x1 = float32(math.Round(float64(f.pxX(n.X1)))) - gapPx
	// Plot y grows up and pixel y grows down, so the node's upper edge Y1 is
	// the smaller pixel value.
	y0 = float32(math.Round(float64(f.pxY(n.Y1))))
	y1 = float32(math.Round(float64(f.pxY(n.Y0)))) - gapPx
	if x1-x0 < minRectPx || y1 <= y0 {
		return 0, 0, 0, 0, false
	}
	return x0, y0, x1, y1, true
}

// collectFrames fills the batch buffers with every visible frame's rectangle.
// It is kept apart from the emission so the geometry — the sign rules, the
// pixel snapping and the culling — can be exercised without an FFI runtime.
func (s *state) collectFrames(f frame) {
	s.rMinX, s.rMinY = s.rMinX[:0], s.rMinY[:0]
	s.rMaxX, s.rMaxY, s.rCols = s.rMaxX[:0], s.rMaxY[:0], s.rCols[:0]
	s.eachVisible(f, func(n *icicle.Node, _ int) {
		x0, y0, x1, y1, ok := rectOf(f, n)
		if !ok {
			return
		}
		s.rMinX = append(s.rMinX, x0)
		s.rMinY = append(s.rMinY, y0)
		s.rMaxX = append(s.rMaxX, x1)
		s.rMaxY = append(s.rMaxY, y1)
		s.rCols = append(s.rCols, s.fill(n))
	})
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
	size := s.opts.FontSize
	if y1-y0 < size {
		return "", 0, 0, false // the row is shorter than the glyphs
	}
	avail := x1 - x0 - 2*labelPadPx
	text = elide(n.Label, int(avail/(size*glyphWidthRatio)))
	if text == "" {
		return "", 0, 0, false
	}
	return text, x0 + labelPadPx, (y0 + y1) / 2, true
}

func (s *state) drawLabels(dc implot.DrawCtx) {
	f := frameOf(dc)
	size := s.opts.FontSize
	s.eachVisible(f, func(n *icicle.Node, _ int) {
		text, x, y, ok := s.labelFor(f, n)
		if !ok {
			return
		}
		c.PaintText(x, y, 0, 1, text, size,
			color.Hex(contrastText(s.fill(n)))).Send()
	})
}

// elide shortens a label to fit, or returns "" when not even an ellipsis and
// one character would fit — a one-glyph label is noise, not information.
func elide(s string, maxChars int) string {
	if maxChars < 2 {
		return ""
	}
	// Bytes are never fewer than runes, so a string that fits by length fits
	// by glyphs, and the common case avoids the conversion.
	if len(s) <= maxChars {
		return s
	}
	r := []rune(s)
	if len(r) <= maxChars {
		return s
	}
	return string(r[:maxChars-1]) + "…"
}

// contrastText picks the readable ink for a fill by WCAG relative luminance.
// Both ends are IDS neutrals rather than pure black and white.
func contrastText(fill uint32) uint32 {
	lin := func(shift uint) float64 {
		v := float64(fill>>shift&0xff) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	y := 0.2126*lin(24) + 0.7152*lin(16) + 0.0722*lin(8)
	// 0.18 is the luminance at which the two neutrals contrast equally
	// against a fill; above it the dark ink wins.
	if y > 0.18 {
		return styletokens.NeutralBgExtreme.AsHex()
	}
	return styletokens.NeutralTextExtreme.AsHex()
}
