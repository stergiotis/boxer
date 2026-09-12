package play

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// play_layeredgraph_panel.go is the ADR-0129 Network dock tab: a result set
// rendered as a directed node-link graph over the layeredgraph widget
// (ADR-0069). The graph is read from two convention-named CTEs of the user's
// own query — `edges` (required) and `vertices` (optional) — each pulled off
// the split graph on its own lane, the kanban `lanes`-CTE mechanism (ADR-0122
// §SD6) applied twice. When no `vertices` CTE is present the vertex set is
// inferred from the edge endpoints.
//
// The contract is named columns rather than detection (§SD2), like kanban:
// edges carry `source`/`target` (+ optional `label`, `tone`); vertices carry
// `id` (+ optional `label`, `group`, `shape`, `tone`). Nothing but intent
// separates a source column from a target column, so the panel asks for the
// names.
//
// The contract itself — its columns, its resolvers and the record-to-model
// build — lives in play_network_model.go, which the Graphview tab reads the
// same rows through (ADR-0227 §SD1). What is here is the LAYERED reading of
// that model: a Graphviz-WASM layout cached on a topology fingerprint, painted
// through layeredgraph/view. The lanes that feed it are shared too
// (play_network_source.go).

const (

	// networkMaxVertices / networkMaxEdges bound the model (§SD5). Layered
	// layout of a Graphviz-WASM run is a tens-to-low-hundreds instrument; a
	// large result is both slow to lay out and unreadable, so the excess is
	// dropped and counted in the status line rather than silently truncated.
	networkMaxVertices = 400
	networkMaxEdges    = 1000

	// networkCanvasMaxH bounds the box a TALL layout asks for (see canvasBox).
	// The pane's own height needs no ceiling, since filling the leaf is the
	// point.
	networkCanvasMaxH = 720
	// networkAspectMargin is how far past the pane a layout's own height must
	// reach before it takes the box. It exists to break a feedback loop, not
	// for looks: a canvas past the pane opens the leaf's scrollbar, which
	// narrows the pane, which shortens the height the aspect asks for — so
	// without a dead band a layout landing within a scrollbar's width of the
	// pane would flip between the two answers every other frame. Comfortably
	// over the worst case, which is the ~12pt bar times the steepest aspect
	// the ceiling still leaves unclamped (maxH/minW = 2).
	networkAspectMargin = 40
)

// networkPaneFill is the canvas's box before the layout gets a say — the shared
// pane rule (play_pane_box.go).
var networkPaneFill = paneFill{
	slack: 12, minW: 360, maxW: 1600, minH: 200,
	fallbackW: 760, fallbackH: 460,
}

// networkIDSalt namespaces the panel's canvas + per-node sense-region ids —
// distinct from the System graph's vizIDSalt so the two drawings never collide;
// per-instance idSeed (from nextVizSeed) keeps two live PlayApps apart.
const networkIDSalt uint64 = 0x6e37c0de9a11f00d

// parseNetworkShape maps a `shape` cell to a node boundary; the box is the
// default for an absent or unrecognised value.
func parseNetworkShape(s string) layeredgraph.NodeShape {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ellipse", "oval":
		return layeredgraph.NodeShapeEllipse
	case "circle":
		return layeredgraph.NodeShapeCircle
	default:
		return layeredgraph.NodeShapeBox
	}
}

// NetworkDriver owns the Network tab state: the cached layout (recomputed only
// on a topology or rank-direction change, so a selection click never
// re-lays-out) and the pan/zoom view. Its inputs arrive through the shared
// source (play_network_source.go), which it reads the lane status back from.
type NetworkDriver struct {
	ids    *c.WidgetIdStack
	idSeed uint64

	// src is the shared pair of lanes; nil for an unwired host (tests), which
	// leaves the status line silent about them.
	src *networkSource

	rankDir layeredgraph.RankDir
	view    view.ViewState

	// paneW / paneH are the last box the pane probe reported. Held across
	// frames rather than read fresh: the probe answers nothing on the first
	// frame and again on the frame a hidden tab comes back (a seq that did not
	// capture is absent from the drain), and resizing the canvas to a fallback
	// on those frames would flash.
	paneW, paneH float32

	// selectedID highlights the last-clicked node, and is published as the
	// `selection_key` signal so a query can follow the click.
	//
	// The row-index `selection` signal stays unpublished, for the reason
	// ADR-0129 §SD4 records: the graph's vertices come from a private lane,
	// not an observable split node, so a cursor emit is clamped away
	// (syncSelectionClamp sends a cursor on an unbound node home) and would
	// jerk the other panels to row 0. A vertex id is a *value*, not a cursor
	// — nothing in play reads `selection_key`, so publishing it moves no
	// other panel — which is why it can cross the same boundary the cursor
	// cannot. The observe/bind direction (§SD7) remains the route to a real
	// shared cursor.
	selectedID string

	layout    *layeredgraph.Layout
	layoutKey string
	layoutErr error

	// Last-build stats for the status line.
	nodeCount int
	edgeCount int
	capped    bool
}

// NewNetworkDriver builds the driver over the shared source. src may be nil
// (tests, an unwired host): the panel then shows its empty state.
func NewNetworkDriver(ids *c.WidgetIdStack, src *networkSource) (inst *NetworkDriver) {
	inst = &NetworkDriver{ids: ids, idSeed: nextVizSeed(), src: src, rankDir: layeredgraph.RankDirTopBottom}
	return
}

// layeredGraphPanel is the PanelI face. Acceptance is schema-only and cheap —
// it runs every frame — because the contract is a question about column names,
// which the schema answers on its own.
type layeredGraphPanel struct {
	driver *NetworkDriver
}

func (inst layeredGraphPanel) ID() PanelID { return "network" }

// Channels declares the required edges plus the optional decorating vertices.
// The panel renders as soon as chEdges is filled; chVertices, when present,
// supplies labels/groups/shapes and the isolated (edge-free) nodes.
func (inst layeredGraphPanel) Channels() []ChannelSpec {
	return []ChannelSpec{
		{ID: chEdges, Required: true, Label: "edges"},
		{ID: chVertices, Required: false, Label: "vertices"},
	}
}

func (inst layeredGraphPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	return acceptGraphChannel(ch, schema)
}

// Render draws the graph. A vertex click publishes `selection_key` (the
// clicked vertex id); the row-index `selection` stays unpublished — see
// NetworkDriver.selectedID for why the two differ.
func (inst layeredGraphPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	edges, ok := filled[chEdges]
	if !ok {
		return
	}
	ec, ok := edges.Claim.(networkEdgesClaim)
	if !ok {
		return
	}
	vc := networkVerticesClaim{idCol: -1, labelCol: -1, groupCol: -1, shapeCol: -1, toneCol: -1, weightCol: -1}
	var vertRec arrow.RecordBatch
	if v, has := filled[chVertices]; has {
		if got, isC := v.Claim.(networkVerticesClaim); isC {
			vc = got
			vertRec = v.Rec
		}
	}
	inst.driver.render(edges.Rec, ec, vertRec, vc, emit)
}

// networkBuild is the shared model resolved FOR THE LAYERED WIDGET: the
// GraphModel the engine lays out, the per-vertex fill and per-edge stroke the
// view's NodeFill / EdgeStroke hooks serve, and the magnitude maxima carried
// through from the build.
type networkBuild struct {
	model  layeredgraph.GraphModel
	fillOf map[string]color.Color // vertex id → tone or group fill (absent → default)
	// strokeOf colours an edge by its endpoints, the key view.RenderOpts'
	// EdgeStroke hook is given. Only edges naming a tone appear.
	strokeOf map[[2]string]color.Color
	// maxWeight / maxNodeWeight are the heaviest edge and vertex weights seen
	// (netModel's, carried through): what the magnitude channels normalise
	// against (ADR-0167 §SD5).
	maxWeight     float64
	maxNodeWeight float64
	capped        bool
}

// buildNetworkLayered is this tab's whole mapping: the shared contract build at
// this panel's caps (§SD9), resolved for the layered widget.
func buildNetworkLayered(edgesRec arrow.RecordBatch, ec networkEdgesClaim, vertRec arrow.RecordBatch, vc networkVerticesClaim) networkBuild {
	m := buildNetModel(edgesRec, ec, vertRec, vc, netCaps{vertices: networkMaxVertices, edges: networkMaxEdges})
	return layeredBuild(&m)
}

// layeredBuild resolves the neutral model for this widget (ADR-0227 §SD1):
// `shape` becomes a node boundary, `tone` and `group` become the colours the
// hooks serve. `donut` is dropped — a laid-out label box has nowhere to put a
// ring, which is the asymmetry the second tab exists for (§SD5).
func layeredBuild(m *netModel) (b networkBuild) {
	b.maxWeight, b.maxNodeWeight, b.capped = m.maxWeight, m.maxNodeWeight, m.capped
	b.fillOf = make(map[string]color.Color, len(m.Vertices))
	nodes := make([]layeredgraph.Node, 0, len(m.Vertices))
	for i := range m.Vertices {
		v := &m.Vertices[i]
		nodes = append(nodes, layeredgraph.Node{
			ID: v.ID, Label: v.Label, Shape: parseNetworkShape(v.Shape), Weight: v.Weight,
		})
		if col, ok := m.vertexFill(*v); ok {
			b.fillOf[v.ID] = col
		}
	}
	edges := make([]layeredgraph.Edge, 0, len(m.Edges))
	for i := range m.Edges {
		e := &m.Edges[i]
		edges = append(edges, layeredgraph.Edge{From: e.From, To: e.To, Label: e.Label, Weight: e.Weight})
		if col, ok := m.edgeStroke(*e); ok {
			if b.strokeOf == nil {
				b.strokeOf = make(map[[2]string]color.Color, 8)
			}
			b.strokeOf[[2]string{e.From, e.To}] = col
		}
	}
	b.model = layeredgraph.GraphModel{Nodes: nodes, Edges: edges}
	return
}

// render maps the two results into a graph, lays it out (cached), draws it, and
// tracks the locally-selected node.
func (inst *NetworkDriver) render(edgesRec arrow.RecordBatch, ec networkEdgesClaim, vertRec arrow.RecordBatch, vc networkVerticesClaim, emit SignalEmitterI) {
	inst.renderControls()

	b := buildNetworkLayered(edgesRec, ec, vertRec, vc)
	inst.nodeCount = len(b.model.Nodes)
	inst.edgeCount = len(b.model.Edges)
	inst.capped = b.capped
	c.Label(inst.statusLine()).Send()

	if len(b.model.Nodes) == 0 {
		for rt := range c.RichTextLabel("The `edges` CTE produced no drawable edges, and there are no `vertices` rows.") {
			rt.Small().Weak()
		}
		return
	}

	// Layout is cached on the topology fingerprint (+ rank direction). A
	// selection click changes only the highlight, not the topology, so it never
	// re-runs the Graphviz-WASM layout — the play_graph_viz.go idiom.
	key := networkModelKey(b.model, inst.rankDir)
	if key != inst.layoutKey || (inst.layout == nil && inst.layoutErr == nil) {
		inst.layoutKey = key
		inst.layout = nil
		eng, err := goccyengine.Shared()
		if err == nil {
			opts := layeredgraph.LayoutOpts{RankDir: inst.rankDir, FontSize: 13}
			if b.maxNodeWeight > 0 {
				// A vertex weight scales the font its label is laid out at, so
				// the box follows (ADR-0167 §SD3). The floor is this panel's
				// own FontSize rather than the package default, so an
				// unweighted-looking node stays the size it always was.
				opts.NodeFontSize = layeredgraph.WeightFontSize(b.model, 13, 0)
			}
			inst.layout, err = eng.Layout(context.Background(), b.model, opts)
		}
		inst.layoutErr = err
	}
	if inst.layout == nil {
		msg := "graph layout unavailable (layout engine)"
		if inst.layoutErr != nil {
			msg += ": " + truncateRunes(firstLineOf(inst.layoutErr.Error()), 80)
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}

	if inst.layout.Width <= 0 || inst.layout.Height <= 0 {
		return
	}
	// The hint goes ABOVE the canvas, as the Icicle's and the Treemap's
	// readouts do, and here it is what lets the canvas be the LAST widget in
	// the body: the probe below reports the room left for the next widget, so
	// with the hint underneath, taking that room would push it past the fold
	// and hold a scrollbar open — which narrows the pane, which resizes the
	// canvas.
	for rt := range c.RichTextLabel("drag pans, ctrl+scroll zooms; click a node to highlight it") {
		rt.Small().Weak()
	}

	// Fill the pane, both ways: a full-width separator, then a seq-keyed probe
	// of the free rect that reads back next frame (a per-seq r21 slot, so it
	// contends with nobody, unlike the single CaptureAvailableSize register
	// that the frame's last capture wins). Emitted after the chrome and BEFORE
	// the canvas, since the rect is the room left for the NEXT widget.
	//
	// The height used to be the layout's aspect alone, on the reasoning that
	// view.Render fits uniformly so a taller canvas only adds margin. That is
	// true of the drawing and false of the pane: the canvas paints its own
	// background, so a wide graph — the aspect at its shortest — left the
	// bottom of the leaf empty. The pane's height is now a FLOOR on the box,
	// which is what captureUiAvailableRect makes readable and the ui-rect probe
	// this replaced did not.
	c.Separator().Horizontal().Send()
	if availW, availH, ok := c.CapturePaneSize(networkIDSalt ^ inst.idSeed ^ 0x1); ok {
		inst.paneW, inst.paneH = availW, availH
	}
	w, h := inst.canvasBox(inst.layout)

	seqPalette := styletokens.SequentialDefault()
	style := view.DefaultStyle()
	bandLo := networkMagnitudeBandLo(seqPalette, styletokens.NeutralBgPanel, styletokens.NeutralBorderDefault)

	// A vertex `weight` ramps the node body the same way it ramps an edge, and
	// loses to the same two more-specific claims: the selection highlight, and
	// an explicit tone or group.
	nodeWeights := make(map[string]float64, len(b.model.Nodes))
	if b.maxNodeWeight > 0 {
		for _, n := range b.model.Nodes {
			nodeWeights[n.ID] = n.Weight
		}
	}
	fill := func(id string) (col color.Color, ok bool) {
		if inst.selectedID != "" && id == inst.selectedID {
			return color.Hex(styletokens.AccentDefault.AsHex()), true
		}
		if col, ok = b.fillOf[id]; ok {
			return
		}
		if b.maxNodeWeight <= 0 {
			return
		}
		w, found := nodeWeights[id]
		if !found || w <= 0 {
			return
		}
		return color.Hex(networkMagnitudeRamp(seqPalette, bandLo, w, b.maxNodeWeight).AsHex()), true
	}
	// Ink follows the fill, and only for the nodes the ramp actually painted:
	// everything else keeps the style default, which the tone and group
	// palettes were chosen against.
	nodeInk := func(id string) (col color.Color, ok bool) {
		if b.maxNodeWeight <= 0 || inst.selectedID == id {
			return
		}
		if _, toned := b.fillOf[id]; toned {
			return
		}
		w, found := nodeWeights[id]
		if !found || w <= 0 {
			return
		}
		return color.Hex(networkInkOn(networkMagnitudeRamp(seqPalette, bandLo, w, b.maxNodeWeight)).AsHex()), true
	}
	// Edges carrying a `tone` are stroked with it; the rest keep the style
	// default. Nothing overrides the selection highlight, which is a fill.
	//
	// A `weight` adds the magnitude channels over the top (ADR-0167 §SD4):
	// width from the shared mapping, and a sequential ramp sampled at the SAME
	// normalised position so the two never disagree — a reader seeing a thick
	// pale edge would have to decide which channel to believe. An explicit
	// tone still wins the colour, being the more specific claim; it does not
	// touch the width, so a toned edge still carries its magnitude.
	weighted := b.maxWeight > 0
	var edgeWidth func(from, to string, weight float64) (float32, bool)
	var edgeWeights map[[2]string]float64
	if weighted {
		edgeWidth = view.WeightWidth(inst.layout, 0, 0)
		// The colour hook is keyed by endpoints only, so it needs the weight
		// looked up — off the layout, which carries it through for exactly
		// this reason, rather than off a second copy in the build.
		edgeWeights = make(map[[2]string]float64, len(inst.layout.Edges))
		for _, e := range inst.layout.Edges {
			edgeWeights[[2]string{e.From, e.To}] = e.Weight
		}
	}
	stroke := func(from, to string) (col color.Color, ok bool) {
		key := [2]string{from, to}
		if b.strokeOf != nil {
			if col, ok = b.strokeOf[key]; ok {
				return
			}
		}
		if !weighted {
			return
		}
		w, found := edgeWeights[key]
		if !found || w <= 0 {
			return
		}
		// Same square root as the width, so the channels stay in step, then
		// mapped onto the legible part of the ramp.
		t := float32(math.Sqrt(min(w, b.maxWeight) / b.maxWeight))
		return color.Hex(styletokens.Sequential(seqPalette, bandLo+(1-bandLo)*t).AsHex()), true
	}
	res := view.Render(networkIDSalt+inst.idSeed, inst.layout, view.RenderOpts{
		Style:      style,
		CanvasW:    w,
		CanvasH:    h,
		NodeFill:   fill,
		NodeText:   nodeInk,
		EdgeStroke: stroke,
		EdgeWidth:  edgeWidth,
		State:      &inst.view,
	})
	// A vertex click highlights it and publishes the id as `selection_key`;
	// clicking the highlighted node again clears both. The empty string is
	// the honest "nothing focused" value — a query reading
	// `{selection_key:String}` sees the same state it started in.
	if res.Clicked != "" {
		if inst.selectedID == res.Clicked {
			inst.selectedID = ""
		} else {
			inst.selectedID = res.Clicked
		}
		if emit != nil {
			emit.Emit(signalSelectionKey, inst.selectedID)
		}
	}
}

// canvasBox is the drawing's box: the pane, less a margin, with the width
// capped so an ultrawide leaf does not stretch the drawing across the screen
// and the height raised to what a TALL layout asks for.
//
// The pane is a floor on the height, not a ceiling, because the fit is uniform:
// a layout taller than it is wide, given a box the pane's height, would have
// its scale set by that height and come out smaller — and less legible — than
// the width already allows. Such a graph keeps the taller box and the tab
// scrolls to it, which is what the panel has always done; what changes is that
// a wide one no longer leaves the bottom of the leaf empty.
func (inst *NetworkDriver) canvasBox(lay *layeredgraph.Layout) (w, h float32) {
	w, h = networkPaneFill.box(inst.paneW, inst.paneH)
	if lay == nil || lay.Width <= 0 || lay.Height <= 0 {
		return
	}
	// The height at which the fit stops being width-limited — the tallest box
	// the drawing can use — clamped to the box a layered graph is legible in.
	full := min(max(w*float32(lay.Height/lay.Width), networkPaneFill.minH), networkCanvasMaxH)
	if full > h+networkAspectMargin {
		h = full
	}
	return
}

// renderControls draws the layout-direction toggle (§SD4). Changing it re-keys
// the layout cache, so the next frame re-lays-out.
func (inst *NetworkDriver) renderControls() {
	for range c.Horizontal().KeepIter() {
		c.Label("layout").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		selector.Segmented(inst.ids, "rank-dir", &inst.rankDir).
			Inline().
			Style(selector.StyleSelectable).
			Option(layeredgraph.RankDirTopBottom, "top-down").
			Option(layeredgraph.RankDirLeftRight, "left-right").
			SendResp()
	}
}

func (inst *NetworkDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d nodes · %d edges", inst.nodeCount, inst.edgeCount)
	if inst.capped {
		fmt.Fprintf(&b, " · capped at %d nodes / %d edges (add a LIMIT or filter)", networkMaxVertices, networkMaxEdges)
	}
	b.WriteString(inst.src.statusSuffix())
	return b.String()
}

// networkModelKey fingerprints the model's TOPOLOGY (ids, labels, shapes,
// edges) plus the rank direction — the layout-cache key. Group/selection are
// colour-only and deliberately absent, so value churn never re-lays-out.
func networkModelKey(m layeredgraph.GraphModel, rd layeredgraph.RankDir) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "rd|%d;", rd)
	// A node's weight scales its font and therefore its box, so it is part of
	// the layout's identity (ADR-0167 §SD3). An EDGE's weight deliberately is
	// not: it never reaches the engine, so including it would only buy
	// needless re-layouts.
	for _, n := range m.Nodes {
		fmt.Fprintf(h, "n|%s|%s|%d|%v;", n.ID, n.Label, n.Shape, n.Weight)
	}
	for _, e := range m.Edges {
		fmt.Fprintf(h, "e|%s|%s|%s;", e.From, e.To, e.Label)
	}
	return fmt.Sprintf("%x", h.Sum64())
}

// renderNetworkTab is the Network dock tab body (ADR-0129): the two named CTEs
// demanded on their lanes, then the PanelI dispatch. Unlike the other result
// panels it does not read the active result — its inputs are the `edges` and
// `vertices` CTEs by name, each on its own lane (like the Kanban lanes node).
func (inst *PlayApp) renderNetworkTab() {
	inputs, release := inst.graphChannelInputs()
	defer release()

	reject := dispatchPanel(layeredGraphPanel{driver: inst.networkDriver}, inputs, inst.sigEmit)
	if reject != "" {
		if inst.netSource.edgesPending() {
			for rt := range c.RichTextLabel("building the graph…") {
				rt.Small().Weak()
			}
			return
		}
		for rt := range c.RichTextLabel(reject) {
			rt.Small().Weak()
		}
	}
}

// demandNetworkEdges compiles the query's `edges` CTE — if it has one — and
// demands it on the driver's edges lane, returning the retained result for the
// chEdges channel (the caller MUST Release rec). Mirrors demandKanbanLanes: the
// node comes from the last Run's split, so its signal reads resolve like any
// other node's and a SET-bound name travels inside the fused SQL.
