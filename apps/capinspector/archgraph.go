package capinspector

// archgraph.go renders the cap-broker schematic through the layeredgraph
// widget (ADR-0069): the App root over one card per capability, each card a
// row node listing the capability's backends. Graphviz `dot`, in-process
// (cgo-free WASM via goccyengine), sizes the cards; packArchGrid places them
// in a grid whose column count follows the panel width, and routes the
// App→capability edges as a bus through the gaps. Only the per-frame colours
// vary — selected cap, effective backend, degraded cap — applied through
// view.RenderOpts hooks. The live audit activity renders as a companion
// sparkline strip beneath the schematic (renderActivityStrip): the widget
// paints text per node, so it cannot host an in-node sparkline.
//
// Laid out by dot, the schematic was one wide rank: thirteen caps and their
// eighteen backends top to bottom fit an 820 px panel at labels of about
// 5 px; left to right they kept 10 px but filled about a third of its width.
// Wrapping the caps into further ranks with minlen saved little width, since
// the long App edges take a slot in every rank they cross. A grid is the
// shape the cards want, and it needs no layout search.

import (
	"context"
	"math"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/icons"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
)

// appNodeID is the layeredgraph node id of the prototypical App root.
// capNodeID namespaces the cap cards so the flat node-id space stays
// collision-free whatever a cap is called. A backend is a row of its cap's
// card, keyed by the backend's Id within that card.
const appNodeID = "app"

func capNodeID(capId CapId) string { return "cap/" + capId }

// nodeKind tags each layeredgraph node so the colour and click hooks can branch
// without re-parsing the id string on the hot path.
type nodeKind uint8

const (
	nodeApp nodeKind = iota
	nodeCap
)

// nodeMeta is the reverse map entry for one node id: which kind it is and the
// cap it belongs to (empty for the app root). Built once alongside the cached
// layout and read every frame.
type nodeMeta struct {
	kind nodeKind
	cap  CapId
}

// capGraphModel builds the App→capability graph from the (closed) Registry:
// one App root and one card per cap, the card's rows its available backends.
// Deterministic order so the cached layout is stable across runs.
func capGraphModel() layeredgraph.GraphModel {
	caps := allCapIdsOrdered()
	m := layeredgraph.GraphModel{
		Nodes: make([]layeredgraph.Node, 0, 1+len(caps)),
		Edges: make([]layeredgraph.Edge, 0, len(caps)),
	}
	m.Nodes = append(m.Nodes, layeredgraph.Node{ID: appNodeID, Label: "App"})
	for _, capId := range caps {
		cn := capNodeID(capId)
		n := layeredgraph.Node{ID: cn, Label: diagramCapLabel(capId)}
		for _, b := range Registry[capId].Backends {
			n.Rows = append(n.Rows, layeredgraph.Row{ID: b.Id, Text: b.Display})
		}
		m.Nodes = append(m.Nodes, n)
		m.Edges = append(m.Edges, layeredgraph.Edge{ID: cn, From: appNodeID, To: cn})
	}
	return m
}

// buildNodeMeta builds the node-id→nodeMeta reverse map the per-frame hooks
// read. Mirrors capGraphModel's id construction exactly.
func buildNodeMeta() map[string]nodeMeta {
	caps := allCapIdsOrdered()
	meta := make(map[string]nodeMeta, 1+len(caps))
	meta[appNodeID] = nodeMeta{kind: nodeApp}
	for _, capId := range caps {
		meta[capNodeID(capId)] = nodeMeta{kind: nodeCap, cap: capId}
	}
	return meta
}

// Grid geometry, in layout units (points before the view's fit scale).
const (
	archGridMargin = 8.0
	// archGutterX separates the columns; the bus trunk runs down the middle
	// gutter, so it is wide enough to read as a lane.
	archGutterX = 24.0
	// archGutterY separates the rows; each row's bus runs archBusDrop above it.
	archGutterY = 30.0
	archBusDrop = 14.0
	archArrowL  = 7.0
	// archAppGap is the space between the App root and the first row.
	archAppGap = 26.0
)

// packArchGrid places the engine-sized nodes of sized into cols columns: the
// App root on top, the cards in reading order beneath it, each as wide as the
// widest so the columns align and top-aligned within its row so every arrow
// lands at the same height. The App→cap edges leave the App down the gutter
// nearest the middle (the trunk), turn along the bus above their card's row
// and drop onto the card. Edges sharing trunk and bus overlap, which is what
// draws them as one bus. cols < 2 is taken as 2: the trunk needs a gutter.
func packArchGrid(sized *layeredgraph.Layout, cols int) *layeredgraph.Layout {
	cols = max(cols, 2)
	app := sized.Nodes[0]
	cards := sized.Nodes[1:]
	cols = min(cols, max(len(cards), 2))
	var cardW float64
	for i := range cards {
		cardW = max(cardW, cards[i].W)
	}
	rows := (len(cards) + cols - 1) / cols
	rowTop := make([]float64, rows+1)
	rowTop[0] = archGridMargin + app.H + archAppGap
	for r := range rows {
		var h float64
		for i := r * cols; i < min((r+1)*cols, len(cards)); i++ {
			h = max(h, cards[i].H)
		}
		rowTop[r+1] = rowTop[r] + h + archGutterY
	}

	out := &layeredgraph.Layout{
		Nodes:        make([]layeredgraph.NodeLayout, 0, len(sized.Nodes)),
		Edges:        make([]layeredgraph.EdgeLayout, 0, len(cards)),
		Width:        2*archGridMargin + float64(cols)*cardW + float64(cols-1)*archGutterX,
		Height:       rowTop[rows] - archGutterY + archGridMargin,
		FontSize:     sized.FontSize,
		EdgeFontSize: sized.EdgeFontSize,
	}
	trunkX := archGridMargin + float64(cols/2)*(cardW+archGutterX) - archGutterX/2
	app.Center = layeredgraph.Point{X: trunkX, Y: archGridMargin + app.H/2}
	out.Nodes = append(out.Nodes, app)
	appBottom := archGridMargin + app.H

	for i := range cards {
		r, col := i/cols, i%cols
		n := placeCard(cards[i], archGridMargin+float64(col)*(cardW+archGutterX), rowTop[r], cardW)
		out.Nodes = append(out.Nodes, n)
		busY := rowTop[r] - archBusDrop
		cx := n.Center.X
		pts := make([]layeredgraph.Point, 0, 1+3*3)
		pts = append(pts, layeredgraph.Point{X: trunkX, Y: appBottom})
		pts = appendStraight(pts, layeredgraph.Point{X: trunkX, Y: busY})
		pts = appendStraight(pts, layeredgraph.Point{X: cx, Y: busY})
		pts = appendStraight(pts, layeredgraph.Point{X: cx, Y: rowTop[r] - archArrowL})
		head := layeredgraph.Point{X: cx, Y: rowTop[r]}
		out.Edges = append(out.Edges, layeredgraph.EdgeLayout{
			ID: n.ID, Index: i, From: app.ID, To: n.ID, Points: pts, ArrowHead: &head,
		})
	}
	return out
}

// placeCard moves an engine-sized card so its top-left corner is (x, y) and
// widens it to w, carrying its title and row rectangles along. The painter
// left-aligns row text from the box's edge, so a wider box only adds room.
func placeCard(n layeredgraph.NodeLayout, x float64, y float64, w float64) layeredgraph.NodeLayout {
	dy := y - (n.Center.Y - n.H/2)
	fit := func(r layeredgraph.Rect) layeredgraph.Rect {
		return layeredgraph.Rect{
			Min: layeredgraph.Point{X: x, Y: r.Min.Y + dy},
			Max: layeredgraph.Point{X: x + w, Y: r.Max.Y + dy},
		}
	}
	if n.IsRowNode() {
		n.Title = fit(n.Title)
		rows := make([]layeredgraph.RowLayout, len(n.Rows))
		for i, rl := range n.Rows {
			rl.Rect = fit(rl.Rect)
			rows[i] = rl
		}
		n.Rows = rows
	}
	n.Center = layeredgraph.Point{X: x + w/2, Y: y + n.H/2}
	n.W = w
	return n
}

// appendStraight appends a straight segment to the end of pts as the cubic
// the painter strokes: control points at a third and two thirds.
func appendStraight(pts []layeredgraph.Point, to layeredgraph.Point) []layeredgraph.Point {
	from := pts[len(pts)-1]
	dx, dy := to.X-from.X, to.Y-from.Y
	return append(pts,
		layeredgraph.Point{X: from.X + dx/3, Y: from.Y + dy/3},
		layeredgraph.Point{X: from.X + 2*dx/3, Y: from.Y + 2*dy/3},
		to)
}

// capArchSized caches the engine's sizing of the cards, computed once via the
// Graphviz engine, and capArchGrids the grid packed from it per column count.
// The topology is closed — the Registry never grows at runtime — so every
// inspector window and every frame reuses them and only the colours change.
// These package-level vars are touched only from the single-threaded imzero2
// render loop, so they need no mutex. ensureArchSized retries while the sizing
// is nil, so a transient WASM failure recovers on a later frame instead of
// sticking; capArchLayoutErr carries the last failure for the degraded message.
var (
	capArchSized     *layeredgraph.Layout
	capArchGrids     map[int]*layeredgraph.Layout
	capArchLayoutErr error
	capArchNodeMeta  map[string]nodeMeta
)

// ensureArchSized lazily sizes the cards (no WASM runtime spins up until the
// inspector is first opened) and caches the result. Returns the cached sizing,
// its node-meta map, and the last layout error. The engine is given the nodes
// alone: their placement is packArchGrid's, so dot's would be discarded.
func ensureArchSized() (*layeredgraph.Layout, map[string]nodeMeta, error) {
	if capArchSized == nil {
		eng, err := goccyengine.Shared()
		if err != nil {
			capArchLayoutErr = err
			return nil, nil, err
		}
		m := capGraphModel()
		m.Edges = nil
		capArchSized, capArchLayoutErr = eng.Layout(context.Background(), m,
			layeredgraph.LayoutOpts{FontSize: 14})
		if capArchSized != nil {
			capArchNodeMeta = buildNodeMeta()
			capArchGrids = make(map[int]*layeredgraph.Layout)
		}
	}
	return capArchSized, capArchNodeMeta, capArchLayoutErr
}

// archGrid returns the grid of cols columns from grids, packing it from sized
// and caching it there on first use.
func archGrid(grids map[int]*layeredgraph.Layout, sized *layeredgraph.Layout, cols int) *layeredgraph.Layout {
	g, ok := grids[cols]
	if !ok {
		g = packArchGrid(sized, cols)
		grids[cols] = g
	}
	return g
}

// pickArchGrid chooses, from grids packed of sized, the grid for a canvas canvasW wide and at most maxH
// tall: the one whose labels draw largest, counting no scale past natural
// size as larger, so that among the grids that fit at natural size the one
// with the fewest rows — the widest — wins. A row count takes the fewest
// columns that hold it: thirteen cards in two rows are seven and six, not
// twelve and one.
func pickArchGrid(grids map[int]*layeredgraph.Layout, sized *layeredgraph.Layout, canvasW float32, maxH float32) *layeredgraph.Layout {
	n := len(sized.Nodes) - 1
	var best *layeredgraph.Layout
	bestScale := -1.0
	for rows := 1; rows <= n; rows++ {
		cols := (n + rows - 1) / rows
		if cols < 2 && best != nil {
			break
		}
		g := archGrid(grids, sized, cols)
		s := math.Min(1, math.Min(float64(canvasW)/g.Width, float64(maxH)/g.Height))
		if s > bestScale {
			best, bestScale = g, s
		}
	}
	return best
}

// archIDSalt is a high-entropy constant that namespaces this widget's canvas /
// sense-region ids. view.Render lays its own fresh WidgetIdStack over the base
// graphIDBase returns, so the base must carry the per-window entropy itself:
// graphIDBase XORs archIDSalt with the window-unique host salt already on
// inst.ids (pushed by windowhost.renderWindowBody, ADR-0026 §SD9) so two open
// inspector windows derive disjoint canvas / sense-region ids.
const archIDSalt uint64 = 0x9e3779b97f4a7c15

// archPaneProbeSalt namespaces the pane probe's r21 slot, distinct from the
// canvas ids archIDSalt covers; both go through the instance's id stack, so
// each window probes its own panel.
const archPaneProbeSalt uint64 = 0xa4c89a9ee0be0001

func (inst *App) graphIDBase() uint64 { return inst.ids.PrepareHighEntropy(archIDSalt).Derive() }

const (
	// chromeW reserves room for the ScrollArea's vertical scrollbar so the
	// canvas plus scrollbar fits the captured available width without
	// provoking a horizontal scroll.
	archChromeW    float32 = 24.0
	archMinCanvasW float32 = 460.0
	archMaxCanvasW float32 = 1400.0
	archFallbackW  float32 = 820.0
	// The grid's aspect follows the panel (pickArchGrid); clamp the height
	// derived from it so it neither squashes flat nor outgrows the window.
	archMinCanvasH float32 = 120.0
	archMaxCanvasH float32 = 680.0
)

// renderGraph draws the cap-broker architecture (App → capabilities, each
// card listing its backends) as the cached grid and, beneath it, the
// live-activity strip. The grid is picked per frame from the panel width; per
// frame only the colours change via the view's NodeFill/RowFill/EdgeStroke
// hooks, and a click on a card reselects that cap (the picker row above is
// the equivalent affordance). Static fit-to-view (no pan/zoom) — the graph is
// small and a fixed frame keeps captures deterministic.
//
// deferred: the nodes have no accessibility-tree entries — layeredgraph
// hit-tests the canvas pointer rather than registering a widget per node, and
// the bindings have no way to name an interactive painted region. Until both
// exist a screen reader cannot reach a node and a scene clicks one by
// coordinates (scenes/capinspector.scene.md); the picker row is the
// accessible equivalent.
func (inst *App) renderGraph(spec CapSpec) {
	sized, meta, err := ensureArchSized()
	if err != nil {
		c.Label("architecture graph unavailable: " + err.Error()).Send()
		return
	}
	if sized == nil {
		return
	}

	// Responsive width: track the panel's available width, from this app's own
	// seq-keyed probe (one frame behind; absent until the first lands). The
	// surrounding ScrollArea uses AutoShrink(false,false), so this reflects the
	// panel width, not the canvas's own previous width. Seq-keyed and not
	// CaptureAvailableSize, whose one process-wide slot the frame's last
	// capture wins — this app has a single reader today, and that is exactly
	// the condition that quietly stops holding.
	availW, _, _ := c.CapturePaneSize(inst.ids.PrepareHighEntropy(archPaneProbeSalt).Derive())
	canvasW := archFallbackW
	if availW > archChromeW {
		canvasW = availW - archChromeW
	}
	canvasW = max(min(canvasW, archMaxCanvasW), archMinCanvasW)
	lay := pickArchGrid(capArchGrids, sized, canvasW, archMaxCanvasH)
	// Height from the layout's own aspect, so the graph fills the canvas
	// instead of sitting in an empty box.
	canvasH := archMinCanvasH
	if lay.Width > 0 {
		canvasH = canvasW * float32(lay.Height/lay.Width)
	}
	canvasH = max(min(canvasH, archMaxCanvasH), archMinCanvasH)

	res := view.Render(inst.graphIDBase(), inst.archSelectedLast(lay, spec.Id), view.RenderOpts{
		Style:      archStyle(),
		CanvasW:    canvasW,
		CanvasH:    canvasH,
		NodeFill:   archNodeFill(meta, spec.Id),
		NodeText:   archNodeText(meta, spec.Id),
		RowFill:    archRowFill(meta, spec.Id),
		EdgeStroke: archEdgeStroke(spec.Id),
	})
	// Click a card to select that cap; the App root is inert.
	if res.Clicked != "" {
		if m, ok := meta[res.Clicked]; ok && m.kind == nodeCap {
			inst.selectedCap = m.cap
		}
	}

	inst.renderArchLegend()
	c.AddSpace(styletokens.GapItems(inst.density))
	inst.renderActivityStrip(canvasW, spec.Id)
}

// archSelectedLast returns lay with the selected cap's edge moved to the end,
// in a copy reused across frames. The edges share the trunk and the buses, and
// the view strokes them in order, so the highlighted one is drawn last or a
// grey one is drawn over it.
func (inst *App) archSelectedLast(lay *layeredgraph.Layout, selected CapId) *layeredgraph.Layout {
	inst.archDrawn = *lay
	inst.archEdges = inst.archEdges[:0]
	sel := -1
	for i := range lay.Edges {
		if lay.Edges[i].To == capNodeID(selected) {
			sel = i
			continue
		}
		inst.archEdges = append(inst.archEdges, lay.Edges[i])
	}
	if sel >= 0 {
		inst.archEdges = append(inst.archEdges, lay.Edges[sel])
	}
	inst.archDrawn.Edges = inst.archEdges
	return &inst.archDrawn
}

// archLegendEntry is one key in the legend row: a filled swatch for a node
// tone, or a short stroke for an edge tone.
type archLegendEntry struct {
	tone  func() color.Color
	edge  bool
	label string
}

var archLegendEntries = []archLegendEntry{
	{tone: archToneApp, label: "App"},
	{tone: archToneSelected, label: "selected capability"},
	{tone: archToneDegraded, label: "no active backend"},
	{tone: archToneEffective, label: "active backend"},
	{tone: archToneIdle, label: "idle capability"},
	{tone: archToneFocusEdge, edge: true, label: "selected wiring"},
}

// renderArchLegend draws one wrapped row keying the schematic's colours,
// inline under the graph so it is read together with it. Each entry is one
// label — swatch glyph and text as atoms of a single widget — so a wrap
// never separates a swatch from its text, and the row wraps instead of
// widening the window. A node swatch is the square outline glyph in the
// graph's node-outline ink over the tone as background, which keeps the
// dark App / idle tones visible; an edge swatch is a dash in the tone.
func (inst *App) renderArchLegend() {
	outline := color.Hex(styletokens.NeutralBorderDefault.AsHex())
	gap := styletokens.GapItems(inst.density)
	for range c.HorizontalWrapped().KeepIter() {
		for i, e := range archLegendEntries {
			if i > 0 {
				c.AddSpace(gap)
			}
			atoms := c.Atoms()
			if e.edge {
				atoms = atoms.BeginRichTextColored(e.tone(), color.Transparent, icons.PhMinus).End()
			} else {
				atoms = atoms.BeginRichTextColored(outline, e.tone(), icons.PhSquare).End()
			}
			c.LabelAtoms(atoms.BeginRichText(" " + e.label).End().Keep()).Extend().Send()
		}
	}
}

// archNodeText pairs each card's ink with the fill archNodeFill picked. The
// light highlight tones (selected cap, degraded cap) take the extreme
// background as ink — near-black in the dark theme, near-white in the light
// one, either way the far side of a *Default tone; the App and idle cards
// return ok=false and keep the style's default ink. One ink serves a card's
// title and its rows, which is what archRowFill has to respect.
func archNodeText(meta map[string]nodeMeta, selected CapId) func(id string) (color.Color, bool) {
	return func(id string) (color.Color, bool) {
		if archCardLit(meta, id, selected) {
			return color.Hex(styletokens.NeutralBgExtreme.AsHex()), true
		}
		return color.Color{}, false
	}
}

// archCardLit reports whether the card id draws on a highlight tone, and so
// in the extreme-background ink.
func archCardLit(meta map[string]nodeMeta, id string, selected CapId) bool {
	m, ok := meta[id]
	return ok && m.kind == nodeCap && (m.cap == selected || ActiveBackend(m.cap) == "")
}

// The schematic's tones, shared by the fill / stroke hooks and the legend
// row beneath the graph so the two cannot drift apart. Resolved per call so
// they follow the active theme.
func archToneApp() color.Color       { return color.Hex(styletokens.AccentSubtle.AsHex()) }
func archToneSelected() color.Color  { return color.Hex(styletokens.SuccessDefault.AsHex()) }
func archToneDegraded() color.Color  { return color.Hex(styletokens.ErrorDefault.AsHex()) }
func archToneEffective() color.Color { return color.Hex(styletokens.WarningDefault.AsHex()) }
func archToneIdle() color.Color      { return color.Hex(styletokens.NeutralSubtle.AsHex()) }
func archToneFocusEdge() color.Color { return color.Hex(styletokens.AccentDefault.AsHex()) }

// archToneEffectiveFaint is the effective-backend tone blended a quarter of
// the way into the idle fill, for the band on an idle card: dark enough (dark
// theme) or light enough (light theme) for the default ink. Blended here and
// opaque, because the view paints a bottom row's rounded band as overlapping
// shapes and a translucent tone would show the overlap.
func archToneEffectiveFaint() color.Color {
	const t = 0.28
	a, b := styletokens.WarningDefault, styletokens.NeutralSubtle
	mix := func(x uint8, y uint8) uint8 { return uint8(float64(y) + t*(float64(x)-float64(y)) + 0.5) }
	return color.Hex(styletokens.RGBA8{R: mix(a.R, b.R), G: mix(a.G, b.G), B: mix(a.B, b.B), A: 255}.AsHex())
}

// archStyle is the view's default style with the title band removed, so a
// card's state colour fills its title line too, and the title takes the
// card's ink.
func archStyle() view.Style {
	st := view.DefaultStyle()
	st.TitleFill = color.Color{}
	st.TitleText = color.Color{}
	return st
}

// archNodeFill colours each node by its role and live state: the App root,
// the selected cap, a degraded cap (no effective backend), and idle caps.
func archNodeFill(meta map[string]nodeMeta, selected CapId) func(id string) (color.Color, bool) {
	return func(id string) (color.Color, bool) {
		m, ok := meta[id]
		if !ok {
			return color.Color{}, false
		}
		switch m.kind {
		case nodeApp:
			return archToneApp(), true
		case nodeCap:
			switch {
			case m.cap == selected:
				return archToneSelected(), true
			case ActiveBackend(m.cap) == "":
				return archToneDegraded(), true
			}
			return archToneIdle(), true
		}
		return color.Color{}, false
	}
}

// archRowFill bands the row of each card's effective backend, in the tone
// the card's ink can be read on: opaque on a lit card, faint on an idle one.
// The other backends stay on the card's fill.
func archRowFill(meta map[string]nodeMeta, selected CapId) func(nodeID string, rowID string) (color.Color, bool) {
	return func(nodeID string, rowID string) (color.Color, bool) {
		m, ok := meta[nodeID]
		if !ok || m.kind != nodeCap || rowID != ActiveBackend(m.cap) {
			return color.Color{}, false
		}
		if archCardLit(meta, nodeID, selected) {
			return archToneEffective(), true
		}
		return archToneEffectiveFaint(), true
	}
}

// archEdgeStroke lights up the App's edge into the selected cap; the rest
// keep the style default.
func archEdgeStroke(selected CapId) func(e *layeredgraph.EdgeLayout) (color.Color, bool) {
	return func(e *layeredgraph.EdgeLayout) (color.Color, bool) {
		if e.To == capNodeID(selected) {
			return archToneFocusEdge(), true
		}
		return color.Color{}, false
	}
}

const (
	// archStripH is the height of the companion activity strip canvas.
	archStripH       float32 = 64.0
	archStripLabelPt float32 = 9.5
	archStripPadX    float32 = 6.0
)

// renderActivityStrip paints the live per-cap audit activity beneath the
// schematic: one cell per cap (allCapIdsOrdered), a compact cap-id label over a
// rolling-window bar sparkline. It is the companion the layered graph can't
// host in-node — the graph carries the wiring, the strip the live traffic the
// old in-box sparklines showed. The selected cap's bars take the green
// highlight; the rest sit in blue-grey. Read-only: selection happens on the
// graph nodes and the picker row above. Bars reuse Tally's rolling histogram
// (audit_sparkline.go); the normalisation matches the old paintCapSparkline.
func (inst *App) renderActivityStrip(canvasW float32, selected CapId) {
	// Colours resolve from the palette so the strip follows the theme:
	// a step below the panel for the canvas, secondary text for labels,
	// the success tone for the selected cap, translucent neutrals for the
	// baseline and the idle bars.
	var (
		bgFill      = styletokens.NeutralBgExtreme.AsHex()
		labelCol    = styletokens.NeutralTextSecondary.AsHex()
		labelSelCol = styletokens.SuccessDefault.AsHex()
		baselineCol = styletokens.NeutralBorderFaint.AsHex()&^0xff | 0x80
		barCol      = styletokens.NeutralTextSecondary.AsHex()&^0xff | 0xd0
		barSelCol   = styletokens.SuccessDefault.AsHex()&^0xff | 0xe0
	)
	const (
		minScaleMax  uint64  = 4   // floor so 1-2 audits don't render saturated
		minBarHeight float32 = 1.5 // keep 1-count buckets visible
		labelY       float32 = 11.0
		baselineY    float32 = 54.0
		barTop       float32 = 20.0
	)
	caps := allCapIdsOrdered()
	cellW := canvasW / float32(len(caps))
	barAreaH := baselineY - barTop
	for i, capId := range caps {
		cellX := float32(i) * cellW
		sel := capId == selected

		lc := labelCol
		if sel {
			lc = labelSelCol
		}
		c.PaintText(cellX+cellW*0.5, labelY, 1, 1, capId, archStripLabelPt, color.Hex(lc)).Send()

		stripX0 := cellX + archStripPadX
		stripX1 := cellX + cellW - archStripPadX
		// Baseline first so the bars sit on top of it.
		c.PaintLine(stripX0, baselineY, stripX1, baselineY, color.Hex(baselineCol), 1.0).Send()

		snap := Tally.Snapshot(capId)
		var maxV uint64
		for _, v := range snap {
			if v > maxV {
				maxV = v
			}
		}
		if maxV == 0 {
			continue // baseline-only; nothing else to paint
		}
		scaleMax := max(maxV, minScaleMax)
		bc := barCol
		if sel {
			bc = barSelCol
		}
		stripW := stripX1 - stripX0
		barW := stripW / float32(sparkBuckets)
		for k, v := range snap {
			if v == 0 {
				continue
			}
			bh := max(float32(v)/float32(scaleMax)*barAreaH, minBarHeight)
			bx0 := stripX0 + float32(k)*barW + 0.3
			bx1 := stripX0 + float32(k+1)*barW - 0.3
			c.PaintRectFilled(bx0, baselineY-bh, bx1, baselineY, 0.0, color.Hex(bc)).Send()
		}
	}
	c.PaintCanvas(inst.ids.PrepareStr("activity-strip"), canvasW, archStripH).
		Background(color.Hex(bgFill)).
		Send()
}
