package play

import (
	"fmt"
	"math"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
	"github.com/zeebo/xxh3"
)

// play_graphview_panel.go is the ADR-0227 Graphview dock tab: the LIVE reading
// of the graph contract the Network tab defines — a force-directed or
// hierarchical layout owned in Go, over widgets/graphview (ADR-0224). Same two
// CTEs, same columns, same lanes (play_network_source.go); what differs is the
// question. A layered drawing reads *what comes before what*; this one reads
// *what clumps with what*, which is why the two are separate tabs rather than
// one tab with a mode — a dock holds one tab per id, and the two readings are
// most useful side by side.
//
// Three of the contract's channels land differently here (§SD4, §SD5):
// `group` names an AURA as well as colouring the node, `weight` sizes the node
// rather than its label box, and `donut` — inert in the layered tab — draws a
// ring of proportional slices around it. `shape` is the one channel this panel
// cannot honour: graphview draws circles.

const (
	// graphviewMaxVertices / graphviewMaxEdges bound the model (§SD9). Unlike
	// the Network tab's, these are not a layout engine's ceiling but a frame
	// budget: the simulation steps inside the same 60 Hz frame that paints the
	// graph and the rest of the app. Measured on one laptop (not a trial), the
	// package's parallel Barnes–Hut repulsion is around half a millisecond at
	// 1 000 nodes and a few at 5 000; legibility gives out well before either.
	graphviewMaxVertices = 2000
	graphviewMaxEdges    = 6000

	// graphviewLabelBudget is the vertex count past which every-node labels
	// stop being a reading (§SD11). Above it labels follow hover, selection and
	// drag, and the status line says so — a rule read off the model rather than
	// a remembered toggle, so one result always draws the same way.
	//
	// Low, because these labels are SCREEN-sized (ADR-0224 §SD7): they do not
	// shrink with the graph, so past a few dozen they overlap into a grey mass
	// whatever the zoom, and a result's labels are database values rather than
	// the short ids a hand-built demo uses. Measured against the help corpus's
	// own graph queries, which run to tens of vertices with names like
	// `AIRCRAFT ARGENTINA CORP TRUSTEE`.
	graphviewLabelBudget = 48

	// graphviewMinRadius / graphviewMaxRadius bound the node radius a `weight`
	// asks for, in world units. The floor is near the widget's own default so
	// the lightest node still reads as a node; the ceiling is where a disc
	// starts swallowing its neighbours at the spacing the force layout gives.
	graphviewMinRadius = 4
	graphviewMaxRadius = 18

	// graphviewMinEdgeW / graphviewMaxEdgeW bound the edge width a `weight`
	// asks for, in screen pixels — the widget's default stroke at the bottom.
	graphviewMinEdgeW = 1.5
	graphviewMaxEdgeW = 6

	// graphviewKScale widens the force layout's ideal edge length (§SD12) over the
	// crate's `k = sqrt(area/n)`. The crate sized its graphs for labels drawn
	// at the node radius; these are screen-sized (ADR-0224 §SD7), so the room
	// one node needs does not shrink as the graph grows and the stock k packs
	// a result graph into a knot. A caller-visible knob is deferred until a
	// query wants one — this is the panel's reading of its own contract.
	graphviewKScale = 2

	// graphviewSettleSteps is the simulation work a freshly built declaration
	// is given before its first paint, in node-steps: the step count is this
	// over the node count, floored and capped. A budget rather than a step
	// count because the step is linear-ish in the nodes — a fixed 200 steps is
	// imperceptible at 50 nodes and a visible stall at 2 000 — and the reason
	// for spending it at all is that Fruchterman–Reingold from a fresh
	// placement takes hundreds of steps to stop looking like a knot. What the
	// reader would otherwise watch is the algorithm, not the graph.
	graphviewSettleSteps = 30000
	graphviewSettleMin   = 40
	graphviewSettleMax   = 400

	// graphviewFreezeSteps is where the panel stops a simulation that has not
	// converged. Fruchterman–Reingold does not always reach the widget's
	// epsilon — a graph with a heavy hub keeps trading small displacements
	// indefinitely — and a dock tab that steps forever is a query tool burning
	// a core on a picture nobody is watching change. The widget leaves the
	// timing to its caller (it offers Paused and IsSettled, not a timeout), so
	// this is the caller's answer: freeze, say so, and let `settle` or
	// `re-lay-out` start it again.
	graphviewFreezeSteps = 3000
)

// graphviewPaneFill is the canvas's box — the shared pane rule
// (play_pane_box.go). The floor is taller than the Network's: a force layout
// spreads into whatever room it is given, so a short box does not crop the
// drawing (the camera fits) but it does push every node into a band where the
// labels collide.
var graphviewPaneFill = paneFill{
	slack: 12, minW: 360, maxW: 1600, minH: 260,
	fallbackW: 760, fallbackH: 460,
}

// graphviewIDSalt namespaces the panel's canvas and pane probe — distinct from
// the Network's and the System graph's so the three drawings never collide;
// the per-instance idSeed (nextVizSeed) keeps two live PlayApps apart.
const graphviewIDSalt uint64 = 0x67726170766965DD

// graphviewLayoutE is the chrome's layout vocabulary: the three of ADR-0224's
// four a result graph has a use for. Random placement is a starting state, not
// a reading, so it is not offered.
type graphviewLayoutE uint8

const (
	graphviewLayoutForce   graphviewLayoutE = iota // Fruchterman–Reingold
	graphviewLayoutGravity                         // FR with centre gravity
	graphviewLayoutTree                            // the hierarchical walk
)

// widgetLayout maps the chrome's choice onto the widget's.
func (inst graphviewLayoutE) widgetLayout() graphview.LayoutE {
	switch inst {
	case graphviewLayoutGravity:
		return graphview.LayoutForceDirectedCG
	case graphviewLayoutTree:
		return graphview.LayoutHierarchical
	}
	return graphview.LayoutForceDirected
}

// netIds interns the contract's string vertex ids into the uint64 keys
// graphview declares with, and back again for the id a click publishes
// (§SD7). Built with the model, in vertex order, so it is a deterministic
// function of the model and the widget's retained positions keep their nodes
// frame to frame.
type netIds struct {
	to   map[string]uint64
	from map[uint64]string
}

func newNetIds(n int) (inst *netIds) {
	return &netIds{to: make(map[string]uint64, n), from: make(map[uint64]string, n)}
}

// intern is xxh3 of the id, probed upward while the slot belongs to a
// different string. A collision is vanishingly unlikely at these sizes and
// the probe is what makes it harmless rather than a merged pair of vertices;
// the reverse map, not the hash, is what the published id comes from.
func (inst *netIds) intern(s string) (id uint64) {
	if id, ok := inst.to[s]; ok {
		return id
	}
	id = xxh3.HashString(s)
	for {
		prev, taken := inst.from[id]
		if !taken || prev == s {
			break
		}
		id++
	}
	inst.to[s] = id
	inst.from[id] = s
	return
}

// name is the declared id an interned key stands for, or "" for a key this
// model does not carry.
func (inst *netIds) name(id uint64) string { return inst.from[id] }

// known reports whether the declaration carries this id at all.
func (inst *netIds) known(s string) bool {
	if inst == nil {
		return false
	}
	_, ok := inst.to[s]
	return ok
}

// graphviewModelKey is what the cached declaration was built for (§SD8): the
// two lanes' content fingerprints, whether each side served anything at all
// (a fingerprint of zero is also "nothing"), and the resolved claims, since a
// re-Run can change which columns the same bytes are read through.
type graphviewModelKey struct {
	edgesFP, verticesFP uint64
	haveEdges, haveVert bool
	ec                  networkEdgesClaim
	vc                  networkVerticesClaim
}

// GraphviewDriver owns the Graphview tab state: the widget (which owns the
// positions, the camera and the simulation), the chrome's choices, and the
// declaration cached against the lanes' fingerprints. Its inputs arrive
// through the shared source, which it reads the lane status back from.
type GraphviewDriver struct {
	ids    *c.WidgetIdStack
	idSeed uint64
	src    *networkSource
	view   *graphview.View

	layout  graphviewLayoutE
	orient  graphview.OrientationE
	paused  bool
	auras   bool
	overlap bool
	// frozen is the panel's own pause, set when the simulation has run past
	// the freeze budget without settling. Cleared by a rebuild and by the two
	// buttons that ask for more layout.
	frozen bool

	// paneW / paneH are the last box the pane probe reported, held across
	// frames for the Network panel's reason: the probe answers nothing on the
	// first frame and on the frame a hidden tab comes back, and resizing the
	// canvas to a fallback on those frames would flash.
	paneW, paneH float32

	// The cached declaration and what it was built for. Rebuilt only when the
	// key changes, so a running simulation formats no cells (§SD8).
	key     graphviewModelKey
	keyOk   bool
	nodes   []graphview.NodeSpec
	edges   []graphview.EdgeSpec
	names   *netIds
	groups  []string
	capped  bool
	labeled bool // the label budget's verdict for the cached model

	// selectedID mirrors the widget's selection as the DECLARED id, published
	// as `selection_key`. The row-index `selection` stays unpublished for
	// ADR-0129 §SD4's reason, which this panel inherits unchanged: the
	// vertices come from a private lane rather than an observable split node,
	// so a cursor emit would be clamped away and would jerk the other panels
	// to row 0.
	selectedID string
}

// NewGraphviewDriver builds the driver over the shared source. src may be nil
// (tests, an unwired host): the panel then shows its empty state.
func NewGraphviewDriver(ids *c.WidgetIdStack, src *networkSource) (inst *GraphviewDriver) {
	// Auras default OFF, overlapping when switched on (§SD4). A force layout
	// interleaves the members of a group that is not also a cluster — every
	// operator sits among its own aircraft types — and the blob over such a
	// group is a mass rather than a reading, so the switch belongs to the
	// reader who can see whether the grouping is spatial. Overlapping is the
	// default once it is on because without it a cell goes to the aura with
	// the most members reaching it, and the larger group simply swallows the
	// smaller one: two groups, one blob.
	inst = &GraphviewDriver{ids: ids, idSeed: nextVizSeed(), src: src,
		layout: graphviewLayoutGravity, overlap: true}
	inst.view = graphview.New(ids, "play-graphview", graphview.Options{
		Layout:        graphview.LayoutForceDirectedCG,
		NodeClicking:  true,
		NodeSelection: true,
	})
	return
}

// graphviewPanel is the PanelI face over the same two channels the Network
// panel declares — the same CTEs, read through the same claims (§SD1).
type graphviewPanel struct {
	driver *GraphviewDriver
}

func (inst graphviewPanel) ID() PanelID { return "graphview" }

func (inst graphviewPanel) Channels() []ChannelSpec {
	return []ChannelSpec{
		{ID: chEdges, Required: true, Label: "edges"},
		{ID: chVertices, Required: false, Label: "vertices"},
	}
}

func (inst graphviewPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	return acceptGraphChannel(ch, schema)
}

// Render draws the graph. A vertex click publishes `selection_key` (the
// clicked vertex id); the row-index `selection` stays unpublished — see
// GraphviewDriver.selectedID.
func (inst graphviewPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	edges, ok := filled[chEdges]
	if !ok {
		return
	}
	ec, ok := edges.Claim.(networkEdgesClaim)
	if !ok {
		return
	}
	vc := networkVerticesClaim{idCol: -1, labelCol: -1, groupCol: -1, shapeCol: -1, toneCol: -1, weightCol: -1,
		donutCol: -1, donutTotalCol: -1}
	var vertRec arrow.RecordBatch
	if v, has := filled[chVertices]; has {
		if got, isC := v.Claim.(networkVerticesClaim); isC {
			vc = got
			vertRec = v.Rec
		}
	}
	inst.driver.render(edges.Rec, ec, vertRec, vc, emit)
}

// render rebuilds the declaration when its inputs changed, draws it, and
// mirrors the widget's selection outward.
func (inst *GraphviewDriver) render(edgesRec arrow.RecordBatch, ec networkEdgesClaim, vertRec arrow.RecordBatch, vc networkVerticesClaim, emit SignalEmitterI) {
	key := graphviewModelKey{ec: ec, vc: vc}
	if inst.src != nil {
		key.edgesFP, key.verticesFP = inst.src.edgesFP, inst.src.verticesFP
	}
	key.haveEdges, key.haveVert = edgesRec != nil, vertRec != nil
	if !inst.keyOk || key != inst.key {
		m := buildNetModel(edgesRec, ec, vertRec, vc,
			netCaps{vertices: graphviewMaxVertices, edges: graphviewMaxEdges})
		inst.rebuild(&m)
		inst.key, inst.keyOk = key, true
		// A new result is a freshly laid-out graph, which is the case the fit
		// latch exists for (ADR-0224 §SD4): the widget arms it on its first
		// nodes and never again, so without this a re-Run against a different
		// graph would be framed by the camera the last one was left at.
		// Positions of surviving ids are kept — only the framing is re-armed.
		inst.view.FitNow()
		inst.view.FastForward(graphviewSettleBudget(len(inst.nodes)))
		inst.frozen = false
	}
	inst.pruneSelection(emit)

	inst.renderControls()
	c.Label(inst.statusLine()).Send()

	if len(inst.nodes) == 0 {
		for rt := range c.RichTextLabel("The `edges` CTE produced no drawable edges, and there are no `vertices` rows.") {
			rt.Small().Weak()
		}
		return
	}

	// The hint goes ABOVE the canvas, for the Network panel's reason: the pane
	// probe reports the room left for the NEXT widget, so the canvas has to be
	// the last thing in the body or it holds a scrollbar open, which narrows
	// the pane, which resizes the canvas.
	for rt := range c.RichTextLabel("drag pans and moves a node, ctrl+scroll zooms; click a node to select it") {
		rt.Small().Weak()
	}
	c.Separator().Horizontal().Send()
	if availW, availH, ok := c.CapturePaneSize(graphviewIDSalt ^ inst.idSeed ^ 0x1); ok {
		inst.paneW, inst.paneH = availW, availH
	}
	w, h := graphviewPaneFill.box(inst.paneW, inst.paneH)

	o := &inst.view.Opts
	o.Layout = inst.layout.widgetLayout()
	o.Force.KScale = graphviewKScale
	o.Force.Paused = inst.paused || inst.frozen
	o.Hier.Orientation = inst.orient
	o.LabelsAlways = inst.labeled
	// The aura colours are the WIDGET's cycle rather than this contract's group
	// palette, which the node bodies take: that palette is the *Subtle
	// background tones, chosen dark so one light ink reads on them, and a
	// translucent blob of one over the same dark panel would be a blob nobody
	// can see. The legend names the group, so the reader pairs them by label.
	//
	// The aura reach stays at the widget's default. Shortening it looks like a
	// way to tighten the blobs and is not: the ramps of one group stop
	// overlapping, so the group fragments into an island per node — and every
	// island is a concave filled polygon (ADR-0224 §SD11), which is the paint
	// the merged blob exists to avoid.
	o.Auras = graphview.AuraParams{
		Enabled: inst.auras && len(inst.groups) > 0,
		Overlap: inst.overlap,
		Legend:  true,
	}

	inst.view.Render(inst.nodes, inst.edges, w, h)

	// Freeze on the way out, so the verdict is read from the frame that was
	// just drawn and takes effect on the next one.
	if m := inst.view.Metrics(); m.Steps >= graphviewFreezeSteps && !inst.view.IsSettled() {
		inst.frozen = true
	}

	// Selection arrives as events, in order: selecting a second node reports
	// the first's deselect before the new select, so replaying them in order
	// leaves the right id. A background click deselects and reports it, which
	// is what clears the published value.
	changed := false
	for _, ev := range inst.view.Events() {
		switch ev.Kind {
		case graphview.EventKindNodeSelect:
			inst.selectedID, changed = inst.names.name(ev.Node), true
		case graphview.EventKindNodeDeselect:
			if inst.selectedID == inst.names.name(ev.Node) {
				inst.selectedID, changed = "", true
			}
		}
	}
	if changed && emit != nil {
		// The empty string is the honest "nothing focused" value — a query
		// reading `{selection_key:String}` sees the state it started in.
		emit.Emit(signalSelectionKey, inst.selectedID)
	}
}

// pruneSelection clears a published selection the current declaration no longer
// carries. The widget drops such a selection silently — it has no id left to
// report it against — so without this a query would keep reading a vertex that
// is not in the graph.
func (inst *GraphviewDriver) pruneSelection(emit SignalEmitterI) (cleared bool) {
	if inst.selectedID == "" || inst.names.known(inst.selectedID) {
		return false
	}
	inst.selectedID = ""
	if emit != nil {
		emit.Emit(signalSelectionKey, "")
	}
	return true
}

// rebuild resolves the neutral model into the widget's declaration: the ids
// are interned (§SD7), `group` becomes an aura id and a fill, `weight` becomes
// a radius and an edge width, and `donut` becomes the ring.
func (inst *GraphviewDriver) rebuild(m *netModel) {
	inst.capped = m.capped
	inst.groups = m.groups()
	inst.labeled = len(m.Vertices) <= graphviewLabelBudget
	inst.names = newNetIds(len(m.Vertices))
	inst.nodes = make([]graphview.NodeSpec, 0, len(m.Vertices))
	for i := range m.Vertices {
		v := &m.Vertices[i]
		n := graphview.NodeSpec{Id: inst.names.intern(v.ID), Label: v.Label}
		if col, ok := m.vertexFill(*v); ok {
			n.Color = col
		}
		n.Radius = graphviewRadius(v.Weight, m.maxNodeWeight)
		if v.Group != "" {
			// One slice per vertex, built with the model and reused until it
			// changes; the widget reads it during Render and keeps no copy.
			n.Auras = []string{v.Group}
		}
		if len(v.Donut) > 0 {
			n.Donut = graphview.Donut{Values: v.Donut, Total: v.DonutTotal}
		}
		inst.nodes = append(inst.nodes, n)
	}

	seqPalette := styletokens.SequentialDefault()
	bandLo := networkMagnitudeBandLo(seqPalette, styletokens.NeutralBgPanel, styletokens.NeutralBorderDefault)
	inst.edges = make([]graphview.EdgeSpec, 0, len(m.Edges))
	for i := range m.Edges {
		e := &m.Edges[i]
		spec := graphview.EdgeSpec{
			From:  inst.names.intern(e.From),
			To:    inst.names.intern(e.To),
			Label: e.Label,
		}
		// A `tone` wins the colour, being the more specific claim; a `weight`
		// still widens the edge under it, and where no tone was named it also
		// ramps the colour at the SAME normalised position as the width, so
		// the two channels cannot disagree (ADR-0167 §SD4).
		if col, ok := m.edgeStroke(*e); ok {
			spec.Color = col
		} else if e.Weight > 0 && m.maxWeight > 0 {
			spec.Color = color.Hex(networkMagnitudeRamp(seqPalette, bandLo, e.Weight, m.maxWeight).AsHex())
		}
		spec.Width = graphviewEdgeWidth(e.Weight, m.maxWeight)
		inst.edges = append(inst.edges, spec)
	}
}

// graphviewSettleBudget is how many steps a declaration of n nodes is given
// before its first paint — the node-step budget over n, within the floor and
// the cap.
func graphviewSettleBudget(n int) uint32 {
	if n <= 0 {
		return 0
	}
	return uint32(min(max(graphviewSettleSteps/n, graphviewSettleMin), graphviewSettleMax))
}

// graphviewRadius maps a vertex weight onto a node radius in world units: the
// square root of its share of the heaviest, so the DISC's area carries the
// value, and the same root every other magnitude channel uses (ADR-0167).
// Zero — no weight column, or nothing positive in it — takes the style
// default.
func graphviewRadius(w float64, maxW float64) float32 {
	if w <= 0 || maxW <= 0 {
		return 0
	}
	t := math.Sqrt(min(w, maxW) / maxW)
	return float32(graphviewMinRadius + (graphviewMaxRadius-graphviewMinRadius)*t)
}

// graphviewEdgeWidth is graphviewRadius for an edge, in screen pixels.
func graphviewEdgeWidth(w float64, maxW float64) float32 {
	if w <= 0 || maxW <= 0 {
		return 0
	}
	t := math.Sqrt(min(w, maxW) / maxW)
	return float32(graphviewMinEdgeW + (graphviewMaxEdgeW-graphviewMinEdgeW)*t)
}

// renderControls draws the layout choice and the three switches that change
// what the same layout SHOWS. Changing any of them is free: the declaration is
// cached against the lanes, not against the chrome.
func (inst *GraphviewDriver) renderControls() {
	ids := inst.ids
	for range c.Horizontal().KeepIter() {
		c.Label("layout").Send() // designlint:ignore=L1 (field caption; lowercase matches its control's own options)
		selector.Segmented(ids, "gv-layout", &inst.layout).
			Inline().
			Style(selector.StyleSelectable).
			Option(graphviewLayoutForce, "force").
			Option(graphviewLayoutGravity, "gravity").
			Option(graphviewLayoutTree, "tree").
			SendResp()
		if inst.layout == graphviewLayoutTree {
			selector.Segmented(ids, "gv-orient", &inst.orient).
				Inline().
				Style(selector.StyleSelectable).
				Option(graphview.OrientationTopDown, "top-down").
				Option(graphview.OrientationLeftRight, "left-right").
				SendResp()
		}
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("gv-fit"), c.Atoms().Text("fit").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.FitNow()
		}
		if c.Button(ids.PrepareStr("gv-reset"), c.Atoms().Text("re-lay-out").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.ResetLayout()
			inst.frozen = false
		}
		// Fast-forward is what a large graph wants instead of watching it
		// converge: the steps run before the frame paints.
		if c.Button(ids.PrepareStr("gv-ff"), c.Atoms().Text("settle").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.FastForward(graphviewSettleBudget(len(inst.nodes)))
			inst.frozen = false
		}
		c.Checkbox(ids.PrepareStr("gv-paused"), inst.paused, "paused").SendRespVal(&inst.paused)
		// Auras are offered only when the vertices named a `group` — the
		// column they are drawn from (§SD4).
		if len(inst.groups) > 0 {
			c.Checkbox(ids.PrepareStr("gv-auras"), inst.auras, "auras by group").SendRespVal(&inst.auras)
			if inst.auras {
				c.Checkbox(ids.PrepareStr("gv-overlap"), inst.overlap, "auras may overlap").SendRespVal(&inst.overlap)
			}
		}
	}
}

// statusLine reports the drawn shape, what the caps and the label budget did
// to it, and how far the simulation has got — the settle state being the one
// readout a live layout has and a static one does not.
func (inst *GraphviewDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d nodes · %d edges", len(inst.nodes), len(inst.edges))
	if inst.capped {
		fmt.Fprintf(&b, " · capped at %d nodes / %d edges (add a LIMIT or filter)",
			graphviewMaxVertices, graphviewMaxEdges)
	}
	if !inst.labeled {
		fmt.Fprintf(&b, " · labels on hover past %d nodes", graphviewLabelBudget)
	}
	if len(inst.groups) > 0 {
		fmt.Fprintf(&b, " · %d group(s)", len(inst.groups))
	}
	m := inst.view.Metrics()
	switch {
	case inst.paused:
		b.WriteString(" · paused")
	case inst.frozen:
		fmt.Fprintf(&b, " · frozen after %d steps, still moving (%.3f) — settle or re-lay-out",
			m.Steps, m.LastDisplacement)
	case inst.view.IsSettled():
		b.WriteString(" · settled")
	case m.Steps > 0:
		fmt.Fprintf(&b, " · settling (%.3f)", m.LastDisplacement)
	}
	b.WriteString(inst.src.statusSuffix())
	return b.String()
}

// renderGraphviewTab is the Graphview dock tab body: the same two CTEs the
// Network tab reads, demanded on the same lanes (ADR-0227 §SD2), then the
// PanelI dispatch. Like the Network tab it does not read the active result.
func (inst *PlayApp) renderGraphviewTab() {
	inputs, release := inst.graphChannelInputs()
	defer release()

	reject := dispatchPanel(graphviewPanel{driver: inst.graphviewDriver}, inputs, inst.sigEmit)
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
