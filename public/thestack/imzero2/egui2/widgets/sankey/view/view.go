// Package view draws a sankey.Layout as implot custom items (ADR-0159).
// It is the imzero2-facing half of the flow widget: the sankey package stays
// UI-free, and this package is the only one that imports the egui2 bindings.
//
// implot owns the frame. The layout lives in a unit box, both axes are pinned
// to [0,1], and every mark is projected through the frame transform inside a
// Custom closure — so pan, pointer-anchored wheel zoom, box-zoom and
// double-click fit all work without this package implementing any of them.
// Hit tests are in plot space for the same reason: they read HoverPlotPos and
// Clicked and probe the layout directly, so they stay correct at any zoom.
//
// There is no new IDL surface. Ribbons take one of two existing routes —
// a concave filled polygon per ribbon, or the whole diagram as one batched
// rect call (see FillMode) — and both sample the same geometry the hit test
// uses.
package view

import (
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sankey"
)

// FillMode selects how ribbons reach the painter. Both routes draw the same
// sampled geometry (ADR-0159 SD2).
type FillMode uint8

const (
	// FillPolygon emits one concave filled polygon per ribbon, outlined with
	// a hairline of its own colour. The hairline is not decoration: a mesh
	// fill bypasses epaint's feathering, so without it the ribbon edge is
	// unantialiased.
	//
	// A concave fill becomes an egui Mesh. The screenshot tour's SVG
	// exporter handles that (one polygon per triangle), but the headless
	// scene lane does not, so a ribbon drawn this way leaves no geometry
	// there — use FillColumns when the output is headed for that lane
	// (ADR-0159 SD5).
	FillPolygon FillMode = iota
	// FillColumns rasterizes every ribbon in the diagram as ~2 px vertical
	// strips in a single batched rect call — one paint opcode for the whole
	// diagram, a source-to-target gradient at no extra cost, and ordinary
	// rects and polylines, so it renders in every export lane. It stair-steps
	// slightly where a ribbon edge is steep, which the boundary polylines
	// cover.
	FillColumns
)

// Hit is what a pointer is over, in layout terms. Both fields are -1 when
// nothing matches; a pointer inside a node bar reports that node and no link.
type Hit struct {
	Node int
	Link int
}

// NoHit is the empty result.
var NoHit = Hit{Node: -1, Link: -1}

// Opts configures one draw. The zero value is usable: concave-polygon
// ribbons, no legend, labels shown, default palette, nothing highlighted.
type Opts struct {
	// Fill selects the ribbon route.
	Fill FillMode
	// Samples is how finely each ribbon edge is sampled. 0 means
	// sankey.DefaultSamples. FillColumns raises it to about one strip every
	// two pixels, so this is effectively a floor there.
	Samples int
	// Gradient tints each ribbon from its source colour to its target
	// colour. FillColumns only — a single polygon has one colour.
	Gradient bool
	// Hover is what Probe returned this frame. Hovering emphasises a ribbon
	// and dims the rest.
	//
	// Both Hover and Selected are Hit rather than a bare index so that the
	// zero value means "nothing", leaving index 0 addressable: selecting the
	// first link is Hit{Node: -1, Link: 0}, which is not the zero Hit. Draw
	// reads a zero Hit as NoHit.
	Hover Hit
	// Selected stays emphasised regardless of the pointer — hosts use it for
	// click-to-pin.
	Selected Hit
	// Layers gives the three drawing layers (flows, nodes, labels) labelled
	// custom items, so implot's legend renders a row per layer with a
	// visibility toggle. Off by default: the legend costs plot area, and a
	// flow diagram is largely self-labelling.
	Layers bool
	// HideLabels suppresses the node labels entirely.
	HideLabels bool
	// FontSize for node labels; 0 means defaultFontSize.
	FontSize float32
	// RibbonAlpha is the alpha applied to ribbon fills, 0 meaning
	// defaultRibbonAlpha. Ribbons overlap, so they are translucent by
	// default.
	RibbonAlpha uint8
	// NodeColor overrides a node's colour; ok=false falls back to
	// NodeLayout.Color, then to the qualitative palette (ADR-0156).
	NodeColor func(n *sankey.NodeLayout) (rgba uint32, ok bool)
	// LinkColor overrides a ribbon's colour; ok=false falls back to
	// LinkLayout.Color, then to the source node's colour.
	LinkColor func(l *sankey.LinkLayout) (rgba uint32, ok bool)
}

const (
	defaultFontSize    = 11.0
	defaultRibbonAlpha = 0x99
	// dimAlpha is what a ribbon fades to while another one is emphasised.
	dimAlpha = 0x33
	// glyphWidthRatio estimates a glyph's advance as a fraction of the font
	// size. Text measurement is still deferred (ADR-0149 SD6), so label
	// fitting is an estimate — it under-measures CJK, which will overlap.
	glyphWidthRatio = 0.6
	// labelGapPx is the space between a node bar and its label.
	labelGapPx = 5.0
	// minLabelBarPx is the shortest bar that still gets a label.
	minLabelBarPx = 7.0
)

// Show opens a plot sized w×h, pins it to the unit box, probes the pointer,
// draws lay, and returns what the pointer was over. It is the straight-line
// entry point; a host that wants to add its own items to the same plot should
// drive implot itself and call Probe and Draw.
//
// clicked distinguishes a click that landed on nothing (click == NoHit,
// clicked true) from no click at all (clicked false) — the difference a host
// needs to implement click-to-pin with click-away-to-clear.
//
// The returned hover and click are one frame behind, like every register read.
func Show(ids *c.WidgetIdStack, title string, w float32, h float32, lay *sankey.Layout, opts Opts) (hover Hit, click Hit, clicked bool) {
	var r Renderer
	return r.Show(ids, title, w, h, lay, opts)
}

// Show is the Renderer method behind the free function, reusing this
// Renderer's buffers across frames.
func (r *Renderer) Show(ids *c.WidgetIdStack, title string, w float32, h float32, lay *sankey.Layout, opts Opts) (hover Hit, click Hit, clicked bool) {
	hover, click = NoHit, NoHit
	if lay == nil {
		return
	}
	for p := range implot.Scoped(ids, title, w, h) {
		Setup(p, opts)
		hover, click, clicked = Probe(p, lay, opts.Samples)
		opts.Hover = hover
		r.Draw(p, lay, opts)
	}
	return hover, click, clicked
}

// Setup applies the axis configuration a flow diagram needs: both axes pinned
// to the unit box and their grid and labels suppressed. CondOnce, not
// CondAlways, so a pan or zoom sticks. Call it before Draw when driving
// implot yourself.
func Setup(p *implot.Plot, opts Opts) {
	hidden := implot.AxisFlagsNoGrid | implot.AxisFlagsNoTickLabels
	p.SetupAxes("", "", hidden, hidden)
	p.SetupAxisLimits(implot.AxisX1, 0, 1, implot.CondOnce)
	p.SetupAxisLimits(implot.AxisY1, 0, 1, implot.CondOnce)
	if !opts.Layers {
		p.NoLegend()
	}
}

// Probe resolves the pointer against the layout, in plot space. A node bar
// wins over a ribbon underneath it.
//
// clicked reports that a click happened at all, which is not the same as
// click != NoHit: a click that landed on empty plot area returns NoHit with
// clicked true, and that is what clears a pinned selection.
func Probe(p *implot.Plot, lay *sankey.Layout, samples int) (hover Hit, click Hit, clicked bool) {
	hover, click = NoHit, NoHit
	if p == nil || lay == nil {
		return
	}
	var scratch sankey.Ribbon
	resolve := func(x float64, y float64) Hit {
		if n := lay.NodeAt(x, y); n >= 0 {
			return Hit{Node: n, Link: -1}
		}
		return Hit{Node: -1, Link: lay.LinkAt(x, y, samples, &scratch)}
	}
	if x, y, ok := p.HoverPlotPos(); ok {
		hover = resolve(x, y)
	}
	if x, y, ok := p.Clicked(); ok {
		click, clicked = resolve(x, y), true
	}
	return hover, click, clicked
}

// Draw declares the diagram's custom items into p, in back-to-front order:
// ribbons, then node bars, then labels. Declaration order is z-order, so a
// host adding its own items can place them relative to these.
func Draw(p *implot.Plot, lay *sankey.Layout, opts Opts) {
	var r Renderer
	r.Draw(p, lay, opts)
}

// Draw is the Renderer method behind the free function, reusing this
// Renderer's buffers across frames.
func (r *Renderer) Draw(p *implot.Plot, lay *sankey.Layout, opts Opts) {
	if p == nil || lay == nil {
		return
	}
	st := &r.s
	st.prepare(lay, normalizeOpts(opts))
	label := func(s string) string {
		if opts.Layers {
			return s
		}
		return ""
	}
	p.Custom(label("flows"), st.drawFlows)
	p.Custom(label("nodes"), st.drawNodes)
	if !opts.HideLabels {
		p.Custom(label("labels"), st.drawLabels)
	}
}

// normalizeOpts folds the zero Hit onto NoHit. Without it an Opts{} would
// emphasise node 0 and dim everything else, which is the wrong reading of
// "the caller said nothing".
func normalizeOpts(o Opts) Opts {
	if o.Hover == (Hit{}) {
		o.Hover = NoHit
	}
	if o.Selected == (Hit{}) {
		o.Selected = NoHit
	}
	return o
}

// Renderer owns the scratch buffers a draw needs, so a host that keeps one
// across frames allocates only on the first — the free Draw and Show
// functions build a throwaway Renderer per call, which is fine for a small
// diagram and wasteful for a large one in a render loop.
//
// One Renderer per diagram pane. The buffers are filled at declaration time
// and read by the Custom closures during End, so two diagrams sharing a
// Renderer in one frame would see the second overwrite the first's geometry.
type Renderer struct {
	s state
}

// state carries the per-draw scratch buffers.
type state struct {
	lay      *sankey.Layout
	opts     Opts
	samples  int
	fontSize float32
	alpha    uint8
	nodeCol  []uint32
	ribbon   sankey.Ribbon
	xs, ys   []float32
	// batched rect scratch, FillColumns only
	rMinX, rMinY, rMaxX, rMaxY []float32
	rCols                      []uint32
}

// prepare re-points the state at this frame's layout and options, resolving
// the option defaults and the per-node colours. Buffers are kept.
func (s *state) prepare(lay *sankey.Layout, opts Opts) {
	s.lay, s.opts = lay, opts
	s.samples, s.fontSize, s.alpha = opts.Samples, opts.FontSize, opts.RibbonAlpha
	if s.samples < 1 {
		s.samples = sankey.DefaultSamples
	}
	if s.fontSize <= 0 {
		s.fontSize = defaultFontSize
	}
	if s.alpha == 0 {
		s.alpha = defaultRibbonAlpha
	}
	s.nodeCol = growU32(s.nodeCol, len(lay.Nodes))
	for i := range lay.Nodes {
		s.nodeCol[i] = s.resolveNodeColor(i)
	}
}

// newState is the test and one-shot entry point: a fresh state, prepared.
func newState(lay *sankey.Layout, opts Opts) *state {
	s := &state{}
	s.prepare(lay, opts)
	return s
}

func (s *state) resolveNodeColor(i int) uint32 {
	n := &s.lay.Nodes[i]
	if s.opts.NodeColor != nil {
		if col, ok := s.opts.NodeColor(n); ok {
			return col
		}
	}
	if n.Color != 0 {
		return n.Color
	}
	// Cycle by position in the node slice, not by the within-stage index:
	// the latter gives every single-node stage slot 0, which collapses a
	// whole spine to one hue. Callers who want a meaningful encoding — by
	// category, by sector — pass NodeColor.
	return styletokens.QualitativeCycle(i).AsHex()
}

func (s *state) linkColor(i int) uint32 {
	l := &s.lay.Links[i]
	if s.opts.LinkColor != nil {
		if col, ok := s.opts.LinkColor(l); ok {
			return col
		}
	}
	if l.Color != 0 {
		return l.Color
	}
	return s.nodeCol[l.Source]
}

// emphasis reports the alpha a ribbon should carry: full while it is the
// pointer's or the selection's, dimmed while some other ribbon is, and the
// default otherwise. Hovering a node emphasises everything joined to it.
func (s *state) emphasis(li int) uint8 {
	l := &s.lay.Links[li]
	touches := func(h Hit) bool {
		return li == h.Link || (h.Node >= 0 && (l.Source == h.Node || l.Target == h.Node))
	}
	if touches(s.opts.Hover) || touches(s.opts.Selected) {
		return 0xff
	}
	if s.focusActive() {
		return dimAlpha
	}
	return s.alpha
}

// focusActive reports whether anything is emphasised, which is what turns the
// dimming of everything else on.
func (s *state) focusActive() bool {
	for _, h := range [2]Hit{s.opts.Hover, s.opts.Selected} {
		if h.Node >= 0 || h.Link >= 0 {
			return true
		}
	}
	return false
}

func withAlpha(rgba uint32, a uint8) uint32 { return (rgba &^ 0xff) | uint32(a) }

func (s *state) drawFlows(dc implot.DrawCtx) {
	if s.opts.Fill == FillColumns {
		s.drawFlowsColumns(dc)
		return
	}
	n := s.samples + 1
	s.xs = grow(s.xs, 2*n)
	s.ys = grow(s.ys, 2*n)
	for li := range s.lay.Links {
		s.lay.Links[li].Sample(s.samples, &s.ribbon)
		col := withAlpha(s.linkColor(li), s.emphasis(li))
		// The outline runs along the top edge left to right, then back along
		// the bottom: one simple ring, which is what the ear clipper wants.
		for i := range n {
			s.xs[i] = dc.T.PxX(s.ribbon.Xs[i])
			s.ys[i] = dc.T.PxY(s.ribbon.Top[i])
			j := n + i
			k := n - 1 - i
			s.xs[j] = dc.T.PxX(s.ribbon.Xs[k])
			s.ys[j] = dc.T.PxY(s.ribbon.Bot[k])
		}
		c.PaintPolygonFilled(s.xs, s.ys, color.Hex(col)).
			Concave().
			Stroke(color.Hex(col), 1).
			Send()
	}
}

// drawFlowsColumns rasterizes every ribbon into one batched rect call. The
// sample count is raised so each strip is about two pixels wide, which is
// what keeps the stepped edge below notice.
func (s *state) drawFlowsColumns(dc implot.DrawCtx) {
	samples := s.samples
	if px := int(dc.AreaW / 2); px > samples {
		samples = min(px, 512)
	}
	s.rMinX, s.rMinY = s.rMinX[:0], s.rMinY[:0]
	s.rMaxX, s.rMaxY, s.rCols = s.rMaxX[:0], s.rMaxY[:0], s.rCols[:0]
	for li := range s.lay.Links {
		l := &s.lay.Links[li]
		l.Sample(samples, &s.ribbon)
		a := s.emphasis(li)
		from := s.linkColor(li)
		to := from
		if s.opts.Gradient {
			to = s.nodeCol[l.Target]
			if s.opts.LinkColor == nil && l.Color == 0 {
				from = s.nodeCol[l.Source]
			}
		}
		for i := 0; i < samples; i++ {
			// Snap the column boundaries to whole pixels. Abutting rects at
			// fractional x each get a feathered edge, and at ribbon alpha the
			// half-covered seams read as vertical banding across the whole
			// diagram. Rounding is safe for tiling because neighbours share a
			// boundary value and so round identically; a column that collapses
			// to zero width simply drops out without opening a gap.
			x0 := float32(math.Round(float64(dc.T.PxX(s.ribbon.Xs[i]))))
			x1 := float32(math.Round(float64(dc.T.PxX(s.ribbon.Xs[i+1]))))
			if x1 <= x0 {
				continue
			}
			// Mid-segment edges: the strip is a rect standing in for a
			// trapezoid, so take the average of its two ends.
			top := (dc.T.PxY(s.ribbon.Top[i]) + dc.T.PxY(s.ribbon.Top[i+1])) / 2
			bot := (dc.T.PxY(s.ribbon.Bot[i]) + dc.T.PxY(s.ribbon.Bot[i+1])) / 2
			s.rMinX = append(s.rMinX, x0)
			s.rMaxX = append(s.rMaxX, x1)
			s.rMinY = append(s.rMinY, top) // plot y up, pixel y down
			s.rMaxY = append(s.rMaxY, bot)
			t := 0.0
			if samples > 1 {
				t = float64(i) / float64(samples-1)
			}
			s.rCols = append(s.rCols, withAlpha(lerpRGB(from, to, t), a))
		}
	}
	if len(s.rMinX) == 0 {
		return
	}
	c.PaintRectsFilled(s.rMinX, s.rMinY, s.rMaxX, s.rMaxY, color.ColorsFromU32(s.rCols)).Send()
	// Stroke the boundaries so the stepped fill edge reads as a curve.
	n := samples + 1
	s.xs = grow(s.xs, n)
	s.ys = grow(s.ys, n)
	for li := range s.lay.Links {
		s.lay.Links[li].Sample(samples, &s.ribbon)
		col := withAlpha(s.linkColor(li), s.emphasis(li))
		for _, edge := range [2][]float64{s.ribbon.Top, s.ribbon.Bot} {
			for i := range n {
				s.xs[i] = dc.T.PxX(s.ribbon.Xs[i])
				s.ys[i] = dc.T.PxY(edge[i])
			}
			c.PaintPolyline(s.xs, s.ys, color.Hex(col), 1).Send()
		}
	}
}

func (s *state) drawNodes(dc implot.DrawCtx) {
	nn := len(s.lay.Nodes)
	s.rMinX, s.rMinY = grow(s.rMinX, nn), grow(s.rMinY, nn)
	s.rMaxX, s.rMaxY = grow(s.rMaxX, nn), grow(s.rMaxY, nn)
	s.rCols = growU32(s.rCols, nn)
	for i := range s.lay.Nodes {
		n := &s.lay.Nodes[i]
		s.rMinX[i] = dc.T.PxX(n.X0)
		s.rMaxX[i] = dc.T.PxX(n.X1)
		s.rMinY[i] = dc.T.PxY(n.Y1) // plot top is the smaller pixel y
		s.rMaxY[i] = dc.T.PxY(n.Y0)
		s.rCols[i] = withAlpha(s.nodeCol[i], 0xff)
	}
	c.PaintRectsFilled(s.rMinX, s.rMinY, s.rMaxX, s.rMaxY, color.ColorsFromU32(s.rCols)).Send()
	// Selection is the accent role, not a bare white.
	ring := color.Hex(styletokens.AccentStrong.AsHex())
	for _, h := range [2]int{s.opts.Hover.Node, s.opts.Selected.Node} {
		if h >= 0 && h < nn {
			c.PaintRectStroke(s.rMinX[h], s.rMinY[h], s.rMaxX[h], s.rMaxY[h], 0, ring, 1.5).Send()
		}
	}
}

func (s *state) drawLabels(dc implot.DrawCtx) {
	ink := color.Hex(styletokens.NeutralTextPrimary.AsHex())
	last := s.lay.Stages - 1
	for i := range s.lay.Nodes {
		n := &s.lay.Nodes[i]
		top, bot := dc.T.PxY(n.Y1), dc.T.PxY(n.Y0)
		if bot-top < minLabelBarPx {
			continue // too short to label without colliding with its neighbours
		}
		cy := (top + bot) / 2
		// Labels sit outside the bar, turning inward at the last stage so
		// they stay inside the plot area.
		var px float32
		var anchorH uint8
		if n.Stage == last {
			px, anchorH = dc.T.PxX(n.X0)-labelGapPx, 2
		} else {
			px, anchorH = dc.T.PxX(n.X1)+labelGapPx, 0
		}
		// The estimate idiom: no text measurement channel exists yet
		// (ADR-0149 SD6), so a label that would leave the area is dropped
		// rather than clipped mid-glyph.
		w := float32(len(n.Label)) * s.fontSize * glyphWidthRatio
		if anchorH == 0 && px+w > dc.AreaX+dc.AreaW {
			continue
		}
		if anchorH == 2 && px-w < dc.AreaX {
			continue
		}
		c.PaintText(px, cy, anchorH, 1, n.Label, s.fontSize, ink).Send()
	}
}

// lerpRGB blends two 0xRRGGBBAA colours by t, keeping a's alpha channel
// (callers stamp the alpha afterwards).
func lerpRGB(a uint32, b uint32, t float64) uint32 {
	if t <= 0 {
		return a
	}
	if t >= 1 {
		return b&^0xff | a&0xff
	}
	ch := func(shift uint) uint32 {
		av := float64((a >> shift) & 0xff)
		bv := float64((b >> shift) & 0xff)
		return uint32(av+(bv-av)*t) & 0xff
	}
	return ch(24)<<24 | ch(16)<<16 | ch(8)<<8 | a&0xff
}

func grow(s []float32, n int) []float32 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]float32, n)
}

func growU32(s []uint32, n int) []uint32 {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]uint32, n)
}
