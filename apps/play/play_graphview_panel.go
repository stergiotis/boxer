package play

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/analytics/graph/algo"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/landoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
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
	// graphviewLayoutAuto is the chrome's *auto* position: whatever the
	// query's `graph_opts.layout` said, and the panel's own default when it
	// said nothing (ADR-0231 §SD1's precedence rule).
	graphviewLayoutAuto    graphviewLayoutE = iota
	graphviewLayoutForce                    // Fruchterman–Reingold
	graphviewLayoutGravity                  // FR with centre gravity
	graphviewLayoutTree                     // the hierarchical walk
	graphviewLayoutRadial                   // rings by hop distance (ADR-0225 §SD6)
	graphviewLayoutRandom                   // a starting state, reachable only from SQL
)

// graphviewLayoutDefault is what the panel draws when neither the query nor
// the reader named a layout — ADR-0227 §SD4's choice, unchanged by the query
// gaining a say.
const graphviewLayoutDefault = graphviewLayoutGravity

// widgetLayout maps the chrome's choice onto the widget's.
func (inst graphviewLayoutE) widgetLayout() graphview.LayoutE {
	switch inst {
	case graphviewLayoutGravity:
		return graphview.LayoutForceDirectedCG
	case graphviewLayoutTree:
		return graphview.LayoutHierarchical
	case graphviewLayoutRadial:
		return graphview.LayoutRadial
	case graphviewLayoutRandom:
		return graphview.LayoutRandom
	}
	return graphview.LayoutForceDirected
}

// String is the `graph_opts.layout` spelling, and the chrome's label.
func (inst graphviewLayoutE) String() string {
	switch inst {
	case graphviewLayoutForce:
		return "force"
	case graphviewLayoutGravity:
		return "force_gravity"
	case graphviewLayoutTree:
		return "hierarchical"
	case graphviewLayoutRadial:
		return "radial"
	case graphviewLayoutRandom:
		return "random"
	}
	return "auto"
}

// The Graphview tab's gesture seam (ADR-0231 §SD8): one family per gesture the
// widget reports. Their types and seeds are declared once in
// play_signal_decl.go; these are the names the panel writes and a query reads.
//
// The split that matters is ADR-0232 §SD9's: the STATE-shaped signals — hover,
// the selection set, the camera, the hidden auras — are read off the widget's
// own readers every frame, because the widget already holds that state and
// replaying events to rebuild it was this panel's own complexity. The
// EVENT-shaped ones — a focus, a context click, an edge click, a drop, a
// background click — are moments rather than state, so they come from the
// event queue and stay at their last value until the gesture happens again.
const (
	signalGvHover      SignalID = "gv_hover"
	signalGvSelection  SignalID = "gv_selection"
	signalGvFocus      SignalID = "gv_focus"
	signalGvContext    SignalID = "gv_context"
	signalGvEdgeSource SignalID = "gv_edge_source"
	signalGvEdgeTarget SignalID = "gv_edge_target"
	signalGvEdgeID     SignalID = "gv_edge_id"
	signalGvPinID      SignalID = "gv_pin_id"
	signalGvPinX       SignalID = "gv_pin_x"
	signalGvPinY       SignalID = "gv_pin_y"
	signalGvBgX        SignalID = "gv_bg_x"
	signalGvBgY        SignalID = "gv_bg_y"
	signalGvMinX       SignalID = "gv_min_x"
	signalGvMaxX       SignalID = "gv_max_x"
	signalGvMinY       SignalID = "gv_min_y"
	signalGvMaxY       SignalID = "gv_max_y"
	signalGvZoom       SignalID = "gv_zoom"
	signalGvAuraHidden SignalID = "gv_aura_hidden"
	// A located graph reads back in its own units (§SD3): while any vertex
	// carried lat/lon, the drop and the camera are published in degrees
	// beside the world-unit forms, so the query that wrote `lat`/`lon` gets
	// `lat`/`lon` back.
	signalGvPinLat SignalID = "gv_pin_lat"
	signalGvPinLon SignalID = "gv_pin_lon"
	signalGvMinLat SignalID = "gv_min_lat"
	signalGvMaxLat SignalID = "gv_max_lat"
	signalGvMinLon SignalID = "gv_min_lon"
	signalGvMaxLon SignalID = "gv_max_lon"
)

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
	// decl is the part of the settings row that is built INTO the declaration
	// — the encoding selectors and the undirected reading. The rest of
	// `graph_opts` (the layout, the spacing, `hide_edges`) is applied to the
	// widget's options per frame and re-keys nothing, so a settings row that
	// reads `{gv_zoom:Float64}` re-executes on every settle without
	// rebuilding the picture or re-framing the camera.
	decl                graphviewDeclKey
	haveEdges, haveVert bool
	ec                  networkEdgesClaim
	vc                  networkVerticesClaim
	gc                  networkGraphOptsClaim
}

// graphviewDeclKey is what the declaration depends on beyond the two records.
type graphviewDeclKey struct {
	sizeBy, toneBy, opacityBy, auraBy string
	undirected                        bool
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
	nodes   graphview.NodeColumns
	edges   graphview.EdgeColumns
	names   *netIds
	groups  []string
	capped  bool
	labeled bool // the label budget's verdict for the cached model

	// opts is the query's settings row (ADR-0231 §SD5), re-read every frame
	// and cheap: it is one row of already-materialised cells.
	opts graphOpts

	// metrics is the analytics engine's side (ADR-0229 §SD6): the CSR and the
	// computed columns, rebuilt with the declaration and cached on the CSR's
	// fingerprint, so a running simulation computes nothing per frame.
	metrics *graphviewMetrics
	// sizeBy is the size channel's encoding selector (ADR-0231 §SD6) as the
	// READER set it; the query's `graph_opts.size_by` is the default it
	// overrides, under §SD1's precedence rule. sizeBySet is what tells the
	// two apart, since "follow the query" and "size by weight" are both
	// empty strings.
	sizeBy     string
	sizeBySet  bool
	sizeReason string
	// chanReasons are the tone, opacity and aura selectors' refusals, for the
	// status line, rebuilt with the channels.
	chanReasons []string
	// orientSet / pinOnDragSet record that the reader moved the control, which
	// is what makes it an override rather than the zero value agreeing with a
	// default (§SD1).
	orientSet    bool
	aurasSet     bool
	pinOnDrag    bool
	pinOnDragSet bool
	// The checkboxes with an auto position bind a persistent mirror rather
	// than a local: the client writes the bound value at frame END, so a
	// local would be written after the compare that reads it and the control
	// would flip back next frame. *Sent is what went out last frame; a
	// mirror that differs from it at the top of a frame is the reader's click.
	aurasCtl, aurasSent     bool
	holdCtl, holdSent       bool
	basemap, basemapSet     bool
	basemapCtl, basemapSent bool
	seedRowsBuf             []int32
	// lastSeedKey is the seed set the seeded channels were last derived for;
	// a change re-derives the channels without rebuilding the model.
	lastSeedKey uint64
	// declaredSel is the selection the query declared on its last rebuild
	// (§SD2 `selected`), so a re-run can withdraw it and the seam can leave it
	// unpublished (§SD8: the round trip must not loop).
	declaredSel map[uint64]struct{}
	// prevKeys are the previous declaration's ids: a start position is spent
	// on a node that was not there before, not on one the reader may have
	// moved since.
	prevKeys map[uint64]struct{}
	// served is what the three lanes had served at the last rebuild, for the
	// own-signal rule (§SD8): a rebuild whose inputs diverged only on signals
	// this panel wrote keeps the camera.
	served netServed

	// The located mode (§SD3 as revised in ADR-0231's 2026-09-14 update):
	// while the declaration carries lat/lon and the reader has not switched
	// the basemap off, a portolan map owns the canvas and the graph paints
	// and picks inside it (ADR-0228). The offline atlas draws country
	// outlines where no tile server is configured.
	pm             *portolan.Map
	land           *landoverlay.Layer
	atlas          *worldmap.Atlas
	hostViewHash   uint64
	hostStableAt   time.Time
	hostFitPending bool
	hostFitIds     []uint64
	hostNodes      graphview.NodeColumns
	hostRadius     []float32
	// centers is the `center` column resolved to keys — the radial layout's
	// middle, which is data rather than a control (§SD2).
	centers []uint64
	fitBuf  []uint64
	// pendingState marks a rebuild whose declared selection, frame and
	// initial positions have not been spent yet; they need the reconcile to
	// have given the ids slots, so they run after the next render.
	pendingState bool
	// lastModel is the declaration's model, kept for that one pass.
	lastModel *netModel

	// The gesture seam's own state (ADR-0231 §SD8): the hover dwell, the
	// camera settle latch, and two scratch buffers the per-frame reader
	// passes reuse.
	hoverPending   string
	hoverPublished string
	hoverSince     time.Time
	cameraDirty    bool
	selBuf         []string
	auraBuf        []string

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
		layout: graphviewLayoutAuto, overlap: true, metrics: newGraphviewMetrics()}
	inst.view = graphview.New(ids, "play-graphview", graphview.Options{
		Layout:        graphview.LayoutForceDirectedCG,
		NodeClicking:  true,
		NodeSelection: true,
		// Edge and background clicking are on because the seam publishes
		// them (ADR-0231 §SD8); without them the contract's `edges.label`
		// and `edges.tone` are drawn but unselectable.
		EdgeClicking:       true,
		EdgeSelection:      true,
		BackgroundClicking: true,
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
		{ID: chGraphOpts, Required: false, Label: "graph_opts"},
	}
}

func (inst graphviewPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	if ch == chGraphOpts {
		// Every column is optional, so the settings CTE is never rejected for
		// its shape (ADR-0231 §SD5): one that names nothing the panel knows is
		// a table of ordinary result columns and the drawing keeps its
		// defaults.
		if schema == nil {
			return nil, "no graph_opts result"
		}
		return resolveGraphOpts(schema), ""
	}
	return acceptGraphChannel(ch, schema)
}

// Render draws the graph. A vertex click publishes `selection_key` (the
// clicked vertex id); the row-index `selection` stays unpublished — see
// GraphviewDriver.selectedID.
func (inst graphviewPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	if in, ok := graphChannelsToClaims(filled); ok {
		inst.driver.render(in.edges, in.ec, in.vertices, in.vc, in.opts, in.gc, emit)
	}
}

// render rebuilds the declaration when its inputs changed, draws it, and
// mirrors the widget's selection outward.
func (inst *GraphviewDriver) render(edgesRec arrow.RecordBatch, ec networkEdgesClaim, vertRec arrow.RecordBatch, vc networkVerticesClaim, optsRec arrow.RecordBatch, gc networkGraphOptsClaim, emit SignalEmitterI) {
	// The settings row is read before the model, because what it says about
	// the encoding channels and the undirected reading is part of what the
	// declaration is built from.
	inst.opts = buildGraphOpts(optsRec, gc)

	key := graphviewModelKey{ec: ec, vc: vc, gc: gc, decl: inst.declKey()}
	if inst.src != nil {
		key.edgesFP, key.verticesFP = inst.src.edgesFP, inst.src.verticesFP
	}
	key.haveEdges, key.haveVert = edgesRec != nil, vertRec != nil
	if !inst.keyOk || key != inst.key {
		// The own-signal rule (§SD8): when the lanes re-ran because a signal
		// THIS panel wrote moved — an expansion on `gv_focus`, a drop read
		// back through `gv_pin_*` — the picture is the reader's and the
		// camera stays; a Run, an edit or another panel's signal re-frames.
		keep := false
		if inst.keyOk && inst.src != nil {
			keep = inst.src.served().divergedOnlyOn(inst.served, graphviewOwnSignal)
		}
		inst.prevKeys = keySet(inst.nodes.Ids, inst.prevKeys)
		m := buildNetModelWith(edgesRec, ec, vertRec, vc,
			netCaps{vertices: graphviewMaxVertices, edges: graphviewMaxEdges},
			inst.selectorColumns())
		inst.rebuild(&m)
		inst.lastModel = &m
		inst.key, inst.keyOk = key, true
		if inst.src != nil {
			inst.served = inst.src.served()
		}
		if !keep {
			// A new result is a freshly laid-out graph, which is the case the
			// fit latch exists for (ADR-0224 §SD4): the widget arms it on its
			// first nodes and never again, so without this a re-Run against a
			// different graph would be framed by the camera the last one was
			// left at. Positions of surviving ids are kept — only the framing
			// is re-armed.
			inst.view.FitNow()
			inst.hostFitPending = true
		}
		inst.view.FastForward(graphviewSettleBudget(inst.nodes.Len()))
		inst.frozen = false
	}
	inst.pruneSelection(emit)
	// The seeded metrics — the distance family and `relevance` — measure from
	// the seed set `distance_from` names: the selection, or the hovered
	// vertex. A change of seed drops the columns that depend on it and
	// re-derives the channels that spend one, without rebuilding the model.
	if inst.metrics != nil && inst.names != nil {
		inst.syncSeeds()
	}

	inst.renderControls()
	c.Label(inst.statusLine()).Send()

	if inst.nodes.Len() == 0 {
		for rt := range c.RichTextLabel("The `edges` CTE produced no drawable edges, and there are no `vertices` rows.") {
			rt.Small().Weak()
		}
		return
	}

	// The hint goes ABOVE the canvas, for the Network panel's reason: the pane
	// probe reports the room left for the NEXT widget, so the canvas has to be
	// the last thing in the body or it holds a scrollbar open, which narrows
	// the pane, which resizes the canvas.
	hint := "drag pans and moves a node, ctrl+scroll zooms; click a node to select it"
	if inst.hosted() {
		hint = "drag pans the map and moves a node, wheel zooms; click a node to select it"
	}
	for rt := range c.RichTextLabel(hint) {
		rt.Small().Weak()
	}
	c.Separator().Horizontal().Send()
	if availW, availH, ok := c.CapturePaneSize(graphviewIDSalt ^ inst.idSeed ^ 0x1); ok {
		inst.paneW, inst.paneH = availW, availH
	}
	w, h := graphviewPaneFill.box(inst.paneW, inst.paneH)

	o := &inst.view.Opts
	g := &inst.opts
	o.Layout = inst.effectiveLayout().widgetLayout()
	// The query's spacing knobs reach the widget's own zero-as-default, so an
	// unset column simply leaves the default (ADR-0231 §SD5). `k_scale` is the
	// exception: this panel has a non-default of its own (§SD12), which the
	// query overrides rather than adds to.
	o.Force.KScale = graphviewKScale
	if g.KScale > 0 {
		o.Force.KScale = g.KScale
	}
	o.Force.CenterGravity = g.Gravity
	o.Force.Model, o.Force.Exaggeration = g.ForceModel, g.Exaggeration
	o.Radial.RingDist = g.RingDist
	o.Radial.Centers = inst.centers
	o.Hier.RowDist, o.Hier.ColDist = g.RowDist, g.ColDist
	o.HideEdges = g.HideEdges
	o.Undirected = g.Undirected
	o.PinOnDrag = inst.effectivePinOnDrag()
	o.Force.Paused = inst.paused || inst.frozen
	o.Hier.Orientation = inst.effectiveOrientation()
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
		Enabled: inst.effectiveAuras() && len(inst.groups) > 0,
		Overlap: inst.overlap,
		Legend:  graphview.AuraLegendInside,
	}

	if inst.hosted() {
		inst.renderHosted(w, h)
	} else {
		o.Style.NodeRadius = 0 // the style default, in world units
		if err := inst.view.RenderColumns(&inst.nodes, &inst.edges, w, h); err != nil {
			// Validate ran at rebuild, so reaching here is a panel bug rather
			// than a malformed query; the widget rendered nothing and kept its
			// state, and the next rebuild is the recovery.
			log.Error().Err(err).Msg("graphview render refused the declaration")
		}
	}

	if inst.pendingState {
		// The reconcile has now given every declared id a slot, so the
		// columns that are TOLD to the widget rather than declared to it can
		// be spent (§SD2).
		inst.applyDeclaredState(inst.lastModel)
		inst.pendingState = false
	}

	// Freeze on the way out, so the verdict is read from the frame that was
	// just drawn and takes effect on the next one.
	if graphviewFrozen(inst.view, graphviewFreezeSteps) {
		inst.frozen = true
	}

	inst.publishGestures(emit)
}

// publishGestures writes the gesture seam (ADR-0231 §SD8) under ADR-0232
// §SD9's split.
//
// STATE is read off the widget's readers: the widget already holds the
// selection, the hover, the camera and the hidden auras, so reading them is
// one pass and cannot drift from what is drawn. Replaying events to
// reconstruct them — which is what this panel used to do for the selection
// alone — got the answer right only as long as every kind that changes the
// state was handled. The store dedups, so a still frame writes nothing.
//
// MOMENTS come from the event queue, because they are not state: a
// double-click is not a property of the graph afterwards. They keep their last
// value until the gesture happens again, which is what lets a query filter on
// the last focus.
func (inst *GraphviewDriver) publishGestures(emit SignalEmitterI) {
	if emit == nil {
		return
	}
	v := inst.view

	// --- state -----------------------------------------------------------
	sel := inst.selBuf[:0]
	for id := range v.SelectedNodes() {
		// A selection the query DECLARED is the query's own statement read
		// back: publishing it would feed a query that declares `selected`
		// from `selection_key` its own output (§SD8).
		if _, declared := inst.declaredSel[id]; declared {
			continue
		}
		if name := inst.names.name(id); name != "" {
			sel = append(sel, name)
		}
	}
	inst.selBuf = sel
	emit.Emit(signalGvSelection, append([]string(nil), sel...))
	// selection_key keeps its meaning: the last of the selected set, and the
	// empty string for none. It is the cross-panel value other panes also
	// write, so it stays a scalar.
	last := ""
	if len(sel) > 0 {
		last = sel[len(sel)-1]
	}
	if last != inst.selectedID {
		inst.selectedID = last
		emit.Emit(signalSelectionKey, last)
	}

	// Hover publishes on a DWELL rather than on every crossing: a pointer
	// crossing a dense graph would otherwise re-run a Live query per node.
	hovered := ""
	if id, ok := v.HoveredNode(); ok {
		hovered = inst.names.name(id)
	}
	now := time.Now()
	if hovered != inst.hoverPending {
		inst.hoverPending, inst.hoverSince = hovered, now
	}
	if inst.hoverPending != inst.hoverPublished && now.Sub(inst.hoverSince) >= graphviewHoverDwell {
		inst.hoverPublished = inst.hoverPending
		emit.Emit(signalGvHover, inst.hoverPublished)
	}

	hidden := inst.auraBuf[:0]
	for id := range v.AuraIds() {
		if v.AuraHidden(id) {
			hidden = append(hidden, id)
		}
	}
	inst.auraBuf = hidden
	emit.Emit(signalGvAuraHidden, append([]string(nil), hidden...))

	// The camera publishes on SETTLE, as the Map's viewport does: a pan that
	// wrote per frame would re-run a Live query on every frame of the gesture
	// and trip the runaway breaker, which is the right behaviour for a loop
	// and the wrong one for a pan.
	switch {
	case inst.hosted():
		// The host owns the camera, so its view is what settles: the Map
		// tab's own debounce over the view hash.
		if vh := inst.pm.ViewHash(); vh != inst.hostViewHash {
			inst.hostViewHash, inst.hostStableAt, inst.cameraDirty = vh, now, true
		} else if inst.cameraDirty && now.Sub(inst.hostStableAt) >= mapDebounce {
			inst.cameraDirty = false
			inst.publishHostCamera(emit)
		}
	case v.Metrics().CameraMoved:
		inst.cameraDirty = true
	case inst.cameraDirty:
		inst.cameraDirty = false
		if minX, minY, maxX, maxY, ok := v.Bounds(); ok {
			emit.Emit(signalGvMinX, float64(minX))
			emit.Emit(signalGvMaxX, float64(maxX))
			emit.Emit(signalGvMinY, float64(minY))
			emit.Emit(signalGvMaxY, float64(maxY))
			if inst.located() {
				// y grows downward in the projection, so the northern edge is
				// the smaller y: the max latitude comes from minY.
				maxLat, minLon := inst.toLatLon(minX, minY)
				minLat, maxLon := inst.toLatLon(maxX, maxY)
				emit.Emit(signalGvMinLat, minLat)
				emit.Emit(signalGvMaxLat, maxLat)
				emit.Emit(signalGvMinLon, minLon)
				emit.Emit(signalGvMaxLon, maxLon)
			}
		}
		zoom, _, _ := v.Camera()
		emit.Emit(signalGvZoom, float64(zoom))
	}

	// --- moments ---------------------------------------------------------
	for _, ev := range v.Events() {
		switch ev.Kind {
		case graphview.EventKindNodeDoubleClick:
			// The expansion gesture: with Live on, this is what turns the
			// navigation layer's walk into a query (ADR-0231 §SD8).
			emit.Emit(signalGvFocus, inst.names.name(ev.Node))
		case graphview.EventKindNodeSecondaryClick:
			emit.Emit(signalGvContext, inst.names.name(ev.Node))
		case graphview.EventKindEdgeClick:
			emit.Emit(signalGvEdgeSource, inst.names.name(ev.From))
			emit.Emit(signalGvEdgeTarget, inst.names.name(ev.To))
			emit.Emit(signalGvEdgeID, strconv.FormatUint(ev.Edge, 10))
		case graphview.EventKindNodeDragEnd:
			// Where the user left it, which is what a `pin_x`/`pin_y` column
			// reads back to make the drop stick.
			emit.Emit(signalGvPinID, inst.names.name(ev.Node))
			emit.Emit(signalGvPinX, float64(ev.X))
			emit.Emit(signalGvPinY, float64(ev.Y))
			if inst.located() {
				lat, lon := inst.toLatLon(ev.X, ev.Y)
				emit.Emit(signalGvPinLat, lat)
				emit.Emit(signalGvPinLon, lon)
			}
		case graphview.EventKindBackgroundClick:
			emit.Emit(signalGvBgX, float64(ev.X))
			emit.Emit(signalGvBgY, float64(ev.Y))
		}
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
	n := m.NumVertices()
	inst.capped = m.capped
	inst.groups = m.groups()
	inst.labeled = n <= graphviewLabelBudget
	// The model already interned the ids and sorted its rows by them
	// (ADR-0232 §SD9), so the declaration is its columns and the widget's
	// slots come out in the same order as the engine's CSR slots.
	inst.names = m.names

	// The CSR is rebuilt with the declaration, which is also what drops every
	// metric computed for the old topology (ADR-0232 §SD8).
	if err := inst.metrics.buildGraph(m, inst.opts.Undirected); err != nil {
		log.Error().Err(err).Msg("graphview: the metric graph could not be built")
	}
	inst.lastSeedKey = inst.metrics.seedKey

	// Seam A's columns travel as they stand wherever the model and the widget
	// already agree on the shape (ADR-0231 §SD2): the model spells "not
	// declared for this row" as NaN and false, which is the widget's own
	// unset, so there is no per-row conversion between them.
	nc := graphview.NodeColumns{
		Ids:           m.Key,
		Label:         m.Label,
		Color:         make([]color.Color, n),
		Radius:        make([]float32, n),
		Opacity:       m.Opacity,
		NoPick:        m.NoPick,
		LabelAlways:   m.LabelAlways,
		PinX:          m.PinX,
		PinY:          m.PinY,
		PullX:         m.PullX,
		PullY:         m.PullY,
		PullStrengthX: m.PullSX,
		PullStrengthY: m.PullSY,
		AuraOffsets:   m.AuraStart,
		AuraIds:       m.AuraValues,
	}
	// The donut columns are already the list layout the widget takes, so they
	// are handed over as they stand rather than re-nested per row.
	nc.DonutOffsets, nc.DonutValues, nc.DonutTotal = m.DonutStart, m.DonutValues, m.DonutTotal
	nc.DonutColors = graphviewDonutColors(m)
	inst.nodes = nc
	inst.deriveChannels(m)

	seqPalette := styletokens.SequentialDefault()
	bandLo := networkMagnitudeBandLo(seqPalette, styletokens.NeutralBgSurface, styletokens.NeutralBorderDefault)
	ne := m.NumEdges()
	ec := graphview.EdgeColumns{
		From:     m.From,
		To:       m.To,
		Id:       m.EdgeID,
		Label:    m.EdgeLabel,
		Color:    make([]color.Color, ne),
		Width:    make([]float32, ne),
		Opacity:  m.EdgeOpacity,
		NoPick:   m.EdgeNoPick,
		Length:   m.EdgeLength,
		Strength: m.EdgeStrength,
	}
	for i := range ne {
		// A `tone` wins the colour, being the more specific claim; a `weight`
		// still widens the edge under it, and where no tone was named it also
		// ramps the colour at the SAME normalised position as the width, so
		// the two channels cannot disagree (ADR-0167 §SD4).
		w := m.EdgeWeight[i]
		if col, ok := m.edgeStroke(i); ok {
			ec.Color[i] = col
		} else if w > 0 && m.maxWeight > 0 {
			ec.Color[i] = color.Hex(networkMagnitudeRamp(seqPalette, bandLo, w, m.maxWeight).AsHex())
		}
		ec.Width[i] = graphviewEdgeWidth(w, m.maxWeight)
	}
	inst.edges = ec

	// The radial centres are a property of the data, so they come from a
	// column rather than a control (§SD2).
	inst.centers = inst.centers[:0]
	for i := range n {
		if m.Center[i] {
			inst.centers = append(inst.centers, m.Key[i])
		}
	}
	// `selected`, `fit` and `start_x`/`start_y` are applied to the WIDGET
	// rather than declared, and only once the reconcile has given the ids
	// slots — so they are queued here and spent after the next render.
	inst.pendingState = true

	// Validating once per rebuild rather than per frame is what the widget's
	// Validate is exported for; a disagreement here is this panel's bug, not
	// the query's, so it is logged and the declaration emptied rather than
	// shown to the reader as a rejected query.
	if err := inst.nodes.Validate(); err != nil {
		log.Error().Err(err).Msg("graphview node declaration is malformed")
		inst.nodes, inst.edges = graphview.NodeColumns{}, graphview.EdgeColumns{}
		return
	}
	if err := inst.edges.Validate(); err != nil {
		log.Error().Err(err).Msg("graphview edge declaration is malformed")
		inst.nodes, inst.edges = graphview.NodeColumns{}, graphview.EdgeColumns{}
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
	if math.IsNaN(w) || maxW <= 0 {
		// NaN is "not declared for this row" in a columnar declaration
		// (ADR-0232 §SD4), which is what takes the style default. A zero
		// would be an explicit zero radius — a node that is only its label —
		// which is not what an absent value means. The size channel spells
		// an unknown `weight` as NaN before it gets here, so a metric's
		// legitimate zero — a degree of none, a distance of nothing — is the
		// smallest disc rather than the default one.
		return graphviewUnsetF32
	}
	t := math.Sqrt(min(max(w, 0), maxW) / maxW)
	return float32(graphviewMinRadius + (graphviewMaxRadius-graphviewMinRadius)*t)
}

// graphviewEdgeWidth is graphviewRadius for an edge, in screen pixels.
func graphviewEdgeWidth(w float64, maxW float64) float32 {
	if math.IsNaN(w) || w <= 0 || maxW <= 0 {
		return graphviewUnsetF32 // the style default, not a hairline
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
			Option(graphviewLayoutAuto, "auto").
			Option(graphviewLayoutForce, "force").
			Option(graphviewLayoutGravity, "gravity").
			Option(graphviewLayoutTree, "tree").
			Option(graphviewLayoutRadial, "radial").
			SendResp()
		if inst.effectiveLayout() == graphviewLayoutTree {
			before := inst.orient
			selector.Segmented(ids, "gv-orient", &inst.orient).
				Inline().
				Style(selector.StyleSelectable).
				Option(graphview.OrientationTopDown, "top-down").
				Option(graphview.OrientationLeftRight, "left-right").
				SendResp()
			if inst.orient != before {
				inst.orientSet = true // the reader moved it: an override
			}
		}
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("gv-fit"), c.Atoms().Text("fit").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.FitNow()
			inst.hostFitPending = true
		}
		if c.Button(ids.PrepareStr("gv-reset"), c.Atoms().Text("re-lay-out").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.ResetLayout()
			inst.frozen = false
		}
		// Fast-forward is what a large graph wants instead of watching it
		// converge: the steps run before the frame paints.
		if c.Button(ids.PrepareStr("gv-ff"), c.Atoms().Text("settle").Keep()).SendResp().HasPrimaryClicked() {
			inst.view.FastForward(graphviewSettleBudget(inst.nodes.Len()))
			inst.frozen = false
		}
		c.Checkbox(ids.PrepareStr("gv-paused"), inst.paused, "paused").SendRespVal(&inst.paused)
		autoCheckbox(ids, "gv-hold", "hold dropped nodes", &inst.holdCtl, &inst.holdSent,
			&inst.pinOnDrag, &inst.pinOnDragSet, inst.opts.PinOnDrag)
		if inst.located() {
			autoCheckbox(ids, "gv-basemap", "basemap", &inst.basemapCtl, &inst.basemapSent,
				&inst.basemap, &inst.basemapSet, true)
		}
		// The size channel's selector. `graph_opts` will supply the default
		// this overrides (ADR-0231 §SD1); until then the chrome is the only
		// writer, and the vocabulary is the engine's own.
		cur := "auto"
		if inst.sizeBySet {
			cur = inst.sizeBy
			if cur == "" {
				cur = networkWeightCol
			}
		}
		for range c.ComboBox(ids.PrepareStr("gv-size-by"),
			c.WidgetText().Text("size by").Keep(),
			c.WidgetText().Text(cur).Keep()).
			KeepIter() {
			// "auto" is the *auto* position §SD1 gives the control: it hands
			// the channel back to whatever the query said.
			for i, name := range append([]string{"auto"}, graphviewSizeOptions()...) {
				if c.Button(ids.PrepareSeq(uint64(0x6000+i)),
					c.Atoms().Text(name).Keep()).
					Frame(false).
					Selected(cur == name).
					SendResp().HasPrimaryClicked() {
					switch name {
					case "auto":
						inst.sizeBy, inst.sizeBySet = "", false
					case networkWeightCol:
						inst.sizeBy, inst.sizeBySet = "", true
					default:
						inst.sizeBy, inst.sizeBySet = name, true
					}
					// The declaration carries the radius, so a change of
					// channel is a rebuild rather than a repaint.
					inst.keyOk = false
				}
			}
		}
		// Auras are offered only when the vertices named a `group` — the
		// column they are drawn from (§SD4).
		if len(inst.groups) > 0 {
			autoCheckbox(ids, "gv-auras", "auras by group", &inst.aurasCtl, &inst.aurasSent,
				&inst.auras, &inst.aurasSet, inst.aurasDefault())
			if inst.effectiveAuras() {
				c.Checkbox(ids.PrepareStr("gv-overlap"), inst.overlap, "auras may overlap").SendRespVal(&inst.overlap)
			}
		}
	}
	// Hosted, the legend cannot live in the canvas — a row stamped under the
	// map's own drag region could never be clicked (ADR-0224 §SD15) — so its
	// rows are buttons out here, toggling through the silent setters.
	if inst.hosted() && inst.effectiveAuras() {
		for range c.Horizontal().KeepIter() {
			for i, it := range inst.view.AuraLegendItems() {
				mark := "■ "
				if it.Hidden {
					mark = "□ "
				}
				if c.Button(ids.PrepareSeq(uint64(0x6100+i)), c.Atoms().Text(mark+it.Label).Keep()).
					Frame(false).SendResp().HasPrimaryClicked() {
					if it.Hidden {
						inst.view.ShowAura(it.Key)
					} else {
						inst.view.HideAura(it.Key)
					}
				}
			}
		}
	}
}

// autoCheckbox draws a checkbox that follows def while the reader has not
// touched it, and the reader's choice once they have (ADR-0231 §SD1's auto
// position). ctl is the persistent mirror the client writes back at frame end
// and sent what went out last frame: a mirror that differs from it now is the
// reader's click, which sets val and marks it set. The mirror has to be a
// field rather than a local — the client's write lands after the frame, into
// whatever the pointer named, and a local is gone by then.
func autoCheckbox(ids *c.WidgetIdStack, key, label string, ctl, sent, val, set *bool, def bool) {
	if *ctl != *sent {
		*val, *set = *ctl, true
	}
	if *set {
		*ctl = *val
	} else {
		*ctl = def
	}
	c.Checkbox(ids.PrepareStr(key), *ctl, label).SendRespVal(ctl)
	*sent = *ctl
}

// statusLine reports the drawn shape, what the caps and the label budget did
// to it, and how far the simulation has got — the settle state being the one
// readout a live layout has and a static one does not.
func (inst *GraphviewDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d nodes · %d edges", inst.nodes.Len(), inst.edges.Len())
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
	b.WriteString(graphviewSettleStatus(inst.view, inst.paused, inst.frozen))
	b.WriteString(inst.opts.statusNote())
	if inst.sizeReason != "" {
		fmt.Fprintf(&b, " · %s", inst.sizeReason)
	}
	for _, r := range inst.chanReasons {
		fmt.Fprintf(&b, " · %s", r)
	}
	if inst.hosted() {
		if basemap.Configured() {
			b.WriteString(" · located: over a basemap")
		} else {
			b.WriteString(" · located: over country outlines (no tile server configured)")
		}
	}
	if inst.metrics != nil {
		// A truncated metric is a valid result with a flag (ADR-0229 §SD4),
		// and a reader sizing nodes by a lower bound should know it is one.
		if names := inst.metrics.truncatedMetrics(); len(names) > 0 {
			fmt.Fprintf(&b, " · %s truncated — the value is a lower bound", strings.Join(names, ", "))
		}
	}
	b.WriteString(inst.src.statusSuffix())
	return b.String()
}

// renderGraphviewTab is the Graphview dock tab body: the same two CTEs the
// Network tab reads, demanded on the same lanes (ADR-0227 §SD2), then the
// PanelI dispatch. Like the Network tab it does not read the active result.
func (inst *PlayApp) renderGraphviewTab() {
	inst.renderGraphContractTab(graphviewPanel{driver: inst.graphviewDriver})
}

// graphviewHoverDwell is how long the pointer must rest on a node before
// `gv_hover` publishes it. A pointer crossing a dense graph passes over many
// nodes on the way to one; publishing each would re-run a Live query per
// crossing, and the reader meant only the one they stopped on.
const graphviewHoverDwell = 200 * time.Millisecond

// sizeChannel is the column the node radius ramps over and the maximum it
// normalises against (ADR-0231 §SD6): the metric a `size_by` selector names,
// or the contract's `weight` when it names none.
//
// A NaN — an unreached vertex under `distance`, a vertex with no neighbour
// pair under `clustering` — reads as *no value* and takes the style default,
// the same reading an absent `weight` has. It must not ramp to the bottom,
// which a zero would do.
func (inst *GraphviewDriver) sizeChannel(m *netModel) (vals []float64, maxV float64) {
	inst.sizeReason = ""
	sel, reason := parseGraphviewSelector(inst.effectiveSizeBy(), graphviewChannelSize, m.hasColumn)
	if reason != "" {
		inst.sizeReason = reason
	}
	if sel.IsZero() {
		return m.weightChannel(), m.maxNodeWeight
	}
	num, _, _, reason := inst.channelColumn(m, sel, graphviewChannelSize)
	if reason != "" || num == nil {
		if reason != "" {
			inst.sizeReason = reason
		}
		return m.weightChannel(), m.maxNodeWeight
	}
	return num, maxOf(num)
}

// weightChannel is the contract's `weight` as a size channel: NaN where a
// vertex has none, since the contract's 0 means *unknown* (ADR-0167 §SD2) and
// a channel's 0 is a value.
func (inst *netModel) weightChannel() []float64 {
	out := make([]float64, len(inst.Weight))
	for i, w := range inst.Weight {
		out[i] = math.NaN()
		if w > 0 {
			out[i] = w
		}
	}
	return out
}

// graphviewSizeOptions is the chrome's vocabulary for the size channel: the
// contract's own column, then every metric the engine computes. The list is
// the engine's (ADR-0232 §SD6) rather than a copy, so a metric added there
// appears here.
func graphviewSizeOptions() (out []string) {
	out = append(out, networkWeightCol)
	for _, m := range algo.Metrics() {
		if m.Kind() == algo.KindCategorical {
			continue // a label cannot size a node
		}
		out = append(out, m.String())
	}
	return
}

// rowOfKey is the model row an interned key sits at. The declaration is sorted
// by key (ADR-0232 §SD9), so this is a binary search rather than a map — and
// the row it finds is the metric column's slot as well.
func (inst *GraphviewDriver) rowOfKey(key uint64) (row int32, ok bool) {
	i, found := slices.BinarySearch(inst.nodes.Ids, key)
	if !found {
		return 0, false
	}
	return int32(i), true
}

// graphviewUnsetF32 is the columnar declaration's "not declared for this row"
// (ADR-0232 §SD4). It is what an absent magnitude spells, since in a column a
// zero is an explicit zero rather than a request for the default.
var graphviewUnsetF32 = float32(math.NaN())

// The precedence rule of ADR-0231 §SD1: the query sets the default and an
// explicit chrome setting overrides it, with the control's *auto* position
// meaning "whatever the query said". Each control that has an auto position
// resolves through one of these, so the rule is stated once per channel rather
// than inline at the point of use.

// effectiveLayout is the chrome's choice, else the query's, else the panel's
// own default (ADR-0227 §SD4).
func (inst *GraphviewDriver) effectiveLayout() graphviewLayoutE {
	if inst.layout != graphviewLayoutAuto {
		return inst.layout
	}
	if inst.opts.LayoutSet {
		return inst.opts.Layout
	}
	return graphviewLayoutDefault
}

// effectiveOrientation is the hierarchical growth direction. The chrome's
// control appears only under that layout, and its zero is top-down, which is
// also the widget's — so the query wins only while the reader has not touched
// it, which orientSet records.
func (inst *GraphviewDriver) effectiveOrientation() graphview.OrientationE {
	if inst.orientSet {
		return inst.orient
	}
	if inst.opts.OrientationSet {
		return inst.opts.Orientation
	}
	return graphview.OrientationTopDown
}

// effectivePinOnDrag decides whether a dropped node is held. Off by default
// (ADR-0231 §SD11): with `pin_x`/`pin_y` and `gv_pin_*` the query is the better
// place to decide whether a drop sticks, and a reader who wants nodes to hold
// has the checkbox.
func (inst *GraphviewDriver) effectivePinOnDrag() bool {
	if inst.pinOnDragSet {
		return inst.pinOnDrag
	}
	return inst.opts.PinOnDrag
}

// effectiveSizeBy is the size channel's selector: the chrome's when the reader
// picked one, else the query's `size_by`.
func (inst *GraphviewDriver) effectiveSizeBy() string {
	if inst.sizeBySet {
		return inst.sizeBy
	}
	return inst.opts.SizeBy
}

// effectiveSeeds is where the seeded metrics measure from (§SD5's
// `distance_from`): the selection by default, or the hovered vertex.
func (inst *GraphviewDriver) effectiveSeeds() (rows []int32) {
	rows = inst.seedRowsBuf[:0]
	if inst.opts.DistanceFrom == graphviewSeedHover {
		if id, ok := inst.view.HoveredNode(); ok {
			if r, found := inst.rowOfKey(id); found {
				rows = append(rows, r)
			}
		}
		return
	}
	for id := range inst.view.SelectedNodes() {
		if r, ok := inst.rowOfKey(id); ok {
			rows = append(rows, r)
		}
	}
	return
}

// graphviewDonutColors resolves the `donut_tones` column to the colours the
// ring slices take (§SD2). The column is tone FAMILIES rather than colours,
// like `tone` itself, so the design system stays the one place a family's
// appearance is decided; an unrecognised family leaves the slice on the
// qualitative cycle, which a zero colour is the widget's spelling for.
func graphviewDonutColors(m *netModel) color.Colors {
	if len(m.DonutToneValues) == 0 || m.DonutStart == nil {
		return nil
	}
	// Paired with DonutValues by index, so the slice has to be as long as the
	// values are — a query that tones only the first few slices leaves the
	// rest on the cycle.
	out := make(color.Colors, len(m.DonutValues))
	for i := range m.NumVertices() {
		vals := m.DonutStart[i+1] - m.DonutStart[i]
		tones := m.DonutToneValues[m.DonutToneStart[i]:m.DonutToneStart[i+1]]
		for j := range int(vals) {
			if j >= len(tones) {
				break
			}
			if col, ok := networkTone(tones[j], true); ok {
				// color.Colors is raw RGBA literals, and 0 is the widget's
				// "take the cycle" (ADR-0232 Update 2026-09-13).
				out[int(m.DonutStart[i])+j] = col.Literal()
			}
		}
	}
	return out
}

// applyDeclaredState spends the columns that are told to the widget rather
// than declared to it (§SD2): a declared selection, a declared frame, and an
// initial position. They run after the render that reconciled the declaration,
// because until then the ids have no slots.
//
// The declared selection is withdrawn before it is re-applied, so a re-run
// that moves `selected` from one node to another does not accumulate; what
// the reader selected by hand stays, since the widget's setters are additive
// and only the ids this panel declared are deselected. A start position is
// spent on a node that was not in the previous declaration: it is an initial
// position and then the node is free, and re-applying it to a survivor on
// every Live re-run would undo whatever the reader dragged.
func (inst *GraphviewDriver) applyDeclaredState(m *netModel) {
	if m == nil {
		return
	}
	for id := range inst.declaredSel {
		inst.view.DeselectNode(id)
	}
	if inst.declaredSel == nil {
		inst.declaredSel = make(map[uint64]struct{}, 8)
	}
	clear(inst.declaredSel)
	fit := inst.fitBuf[:0]
	for i := range m.NumVertices() {
		key := m.Key[i]
		if m.Selected[i] && inst.view.SelectNode(key) {
			inst.declaredSel[key] = struct{}{}
		}
		if m.Fit[i] {
			fit = append(fit, key)
		}
		if _, survived := inst.prevKeys[key]; survived {
			continue
		}
		x, y := m.StartX[i], m.StartY[i]
		if !math.IsNaN(float64(x)) && !math.IsNaN(float64(y)) {
			inst.view.SetNodePosition(key, x, y)
		}
	}
	inst.fitBuf = fit
	inst.hostFitIds = append(inst.hostFitIds[:0], fit...)
	if len(fit) > 0 {
		// A declared frame replaces the whole-graph fit the rebuild armed:
		// the query said what it wanted framed. Hosted, the map frames it.
		inst.view.FitNodes(fit)
		inst.hostFitPending = true
	}
}

// located reports that the declaration placed its vertices geographically, so
// a gesture reads back in degrees as well as world units (§SD3).
func (inst *GraphviewDriver) located() bool {
	return inst.lastModel != nil && inst.lastModel.Located
}

// toLatLon inverts the projection a located declaration was pinned with. The
// origin is the located set's centroid, so it has to come from the model
// rather than being recomputed.
func (inst *GraphviewDriver) toLatLon(x, y float32) (lat, lon float64) {
	m := inst.lastModel
	return netUnprojectWebMercator(float64(x)+m.GeoOriginX, float64(y)+m.GeoOriginY)
}

// effectiveAuras decides whether the blobs are drawn. Declaring `groups` or an
// `aura_by` selector is the query asking for auras, so it switches them on as
// the default (§SD4); ADR-0227 §SD4's off-by-default stays the rule for a
// query that declares only `group`, where the reader is the one who can see
// whether the grouping is also spatial.
func (inst *GraphviewDriver) effectiveAuras() bool {
	if inst.aurasSet {
		return inst.auras
	}
	if inst.opts.AuraBy != "" {
		return true
	}
	return inst.lastModel != nil && inst.lastModel.GroupsDeclared
}

// keySet rebuilds a set over ids in dst's storage.
func keySet(ids []uint64, dst map[uint64]struct{}) map[uint64]struct{} {
	if dst == nil {
		dst = make(map[uint64]struct{}, len(ids))
	}
	clear(dst)
	for _, id := range ids {
		dst[id] = struct{}{}
	}
	return dst
}

// graphviewOwnSignal reports whether a signal name is one this panel writes:
// the reserved names the declaration gives the Graphview tab (§SD8).
func graphviewOwnSignal(name string) bool {
	r, ok := reservedSignalIndex[SignalID(name)]
	return ok && r.Owner == "graphview"
}

// declKey is the part of the settings row and the chrome the declaration is
// built from.
func (inst *GraphviewDriver) declKey() graphviewDeclKey {
	return graphviewDeclKey{
		sizeBy: inst.effectiveSizeBy(), toneBy: inst.opts.ToneBy,
		opacityBy: inst.opts.OpacityBy, auraBy: inst.opts.AuraBy,
		undirected: inst.opts.Undirected,
	}
}

// selectorColumns names the vertices columns the four selectors spend that are
// neither a metric nor a contract column, so the build reads them (§SD6).
func (inst *GraphviewDriver) selectorColumns() (names []string) {
	for _, raw := range []string{inst.effectiveSizeBy(), inst.opts.ToneBy, inst.opts.OpacityBy, inst.opts.AuraBy} {
		s := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(raw), "-"))
		if s == "" {
			continue
		}
		if _, isMetric := algo.ParseMetric(s); isMetric {
			continue
		}
		switch s {
		case networkWeightCol, networkGroupCol, networkToneCol:
			continue
		}
		if !slices.Contains(names, s) {
			names = append(names, s)
		}
	}
	return
}

// hasColumn reports whether a selector may name this column: a contract
// column the model carries, or one the build read for the selectors.
func (inst *netModel) hasColumn(name string) bool {
	switch name {
	case networkWeightCol, networkGroupCol, networkToneCol:
		return true
	}
	_, ok := inst.Extra[name]
	return ok
}

// channelColumn resolves a selector against the model: quantities in num (NaN
// where a row has none), labels in lbl ("" where none), or a set per row in
// sets — exactly one of the three. reason is a status-line sentence when the
// channel cannot spend what the selector names.
func (inst *GraphviewDriver) channelColumn(m *netModel, sel graphviewSelector, ch graphviewChannelE) (num []float64, lbl []string, sets *netExtraColumn, reason string) {
	n := m.NumVertices()
	if sel.Metric != algo.MetricNone {
		col, ok := inst.metrics.column(sel.Metric)
		if !ok || len(col.Values) != n {
			// A seeded metric with nothing to measure from, or an engine
			// refusal: say so and leave the channel where it was.
			if sel.Metric.IsSeeded() {
				return nil, nil, nil, "`" + ch.String() + " = " + sel.Metric.String() + "`: select or hover a node to measure from"
			}
			return nil, nil, nil, "`" + ch.String() + " = " + sel.Metric.String() + "`: the engine did not compute it"
		}
		if sel.Metric.Kind() == algo.KindCategorical {
			lbl = make([]string, n)
			for i, v := range col.Values {
				if !math.IsNaN(v) {
					lbl[i] = strconv.FormatInt(int64(v), 10)
				}
			}
			return
		}
		return invertIf(col.Values, sel.Invert), nil, nil, ""
	}
	switch sel.Column {
	case networkWeightCol:
		num = make([]float64, n)
		for i, w := range m.Weight {
			num[i] = math.NaN()
			if w > 0 {
				num[i] = w
			}
		}
		return invertIf(num, sel.Invert), nil, nil, ""
	case networkGroupCol:
		return nil, m.Group, nil, quantityRefusal(ch, sel.Column)
	case networkToneCol:
		return nil, m.Tone, nil, quantityRefusal(ch, sel.Column)
	}
	x, ok := m.Extra[sel.Column]
	switch {
	case !ok:
		return nil, nil, nil, "`" + ch.String() + " = " + sel.Column + "`: no column or metric of that name"
	case x.IsNumeric():
		return invertIf(x.Num, sel.Invert), nil, nil, ""
	case x.IsList():
		if ch.wantsQuantity() {
			return nil, nil, nil, quantityRefusal(ch, sel.Column)
		}
		return nil, nil, &x, ""
	}
	return nil, x.Str, nil, quantityRefusal(ch, sel.Column)
}

// quantityRefusal is the sentence for a label column named where a quantity
// is needed, and the empty string where a label is fine.
func quantityRefusal(ch graphviewChannelE, col string) string {
	if !ch.wantsQuantity() {
		return ""
	}
	return "`" + ch.String() + " = " + col + "`: " + col + " is a label rather than a quantity, so it can colour or group but not size"
}

// invertIf flips a quantity column about its maximum when asked — the
// nearest the largest — leaving NaN in place, since absent is not the far end.
// The returned slice is the input when nothing is to invert.
func invertIf(vals []float64, invert bool) []float64 {
	if !invert {
		return vals
	}
	maxV := maxOf(vals)
	out := make([]float64, len(vals))
	for i, v := range vals {
		if math.IsNaN(v) {
			out[i] = v
			continue
		}
		out[i] = maxV - v
	}
	return out
}

// maxOf is the largest non-NaN value, 0 for none.
func maxOf(vals []float64) (maxV float64) {
	for _, v := range vals {
		if !math.IsNaN(v) && v > maxV {
			maxV = v
		}
	}
	return
}

// graphviewOpacityFloor is the least opacity the `opacity_by` ramp gives a
// node with a value, so the smallest is dim rather than gone: the widget has
// no spelling for fully transparent that a caller would want here.
const graphviewOpacityFloor = 0.15

// usesSeededChannel reports whether any channel spends a seeded metric, which
// is what makes a seed change worth a re-derivation.
func (inst *GraphviewDriver) usesSeededChannel() bool {
	for _, raw := range []string{inst.effectiveSizeBy(), inst.opts.ToneBy, inst.opts.OpacityBy, inst.opts.AuraBy} {
		s := strings.TrimPrefix(strings.TrimSpace(raw), "-")
		if m, ok := algo.ParseMetric(strings.TrimSpace(s)); ok && m.IsSeeded() {
			return true
		}
	}
	return false
}

// syncSeeds declares the seed set and, when it moved and a channel spends a
// seeded metric, re-derives the channels over the cached model: the radius,
// the fill, the opacity and the auras, without touching the topology or the
// positions.
func (inst *GraphviewDriver) syncSeeds() {
	rows := inst.effectiveSeeds()
	inst.seedRowsBuf = rows
	inst.metrics.setSeeds(rows)
	if inst.metrics.seedKey == inst.lastSeedKey || inst.lastModel == nil {
		return
	}
	inst.lastSeedKey = inst.metrics.seedKey
	if inst.usesSeededChannel() {
		inst.deriveChannels(inst.lastModel)
	}
}

// deriveChannels spends the four encoding selectors on the declaration
// (ADR-0231 §SD6) under §SD12's precedence: an absolute `radius` beats the
// size channel, a `tone_by` value beats `tone` and `group`, an `opacity_by`
// value beats `opacity`, and an `aura_by` set replaces `groups` and `group`.
// It writes into the cached declaration's own columns, so a re-derivation on
// a seed change touches nothing else.
func (inst *GraphviewDriver) deriveChannels(m *netModel) {
	n := m.NumVertices()
	if n == 0 || inst.nodes.Len() != n {
		return
	}
	inst.chanReasons = inst.chanReasons[:0]
	nc := &inst.nodes

	// Size.
	sizeVals, sizeMax := inst.sizeChannel(m)
	for i := range n {
		// An absolute `radius` wins over the size channel's share (§SD2): a
		// query that names a size in world units meant that size, where
		// `weight` and a metric are both shares of a maximum.
		if r := m.Radius[i]; !math.IsNaN(float64(r)) {
			nc.Radius[i] = r
			continue
		}
		nc.Radius[i] = graphviewRadius(sizeVals[i], sizeMax)
	}

	// Fill: `tone_by`, else the contract's tone-over-group.
	for i := range n {
		nc.Color[i] = color.Color{}
		if col, ok := m.vertexFill(i); ok {
			nc.Color[i] = col
		}
	}
	if sel, reason := parseGraphviewSelector(inst.opts.ToneBy, graphviewChannelTone, m.hasColumn); reason != "" {
		inst.chanReasons = append(inst.chanReasons, reason)
	} else if !sel.IsZero() {
		num, lbl, sets, reason := inst.channelColumn(m, sel, graphviewChannelTone)
		switch {
		case reason != "":
			inst.chanReasons = append(inst.chanReasons, reason)
		case num != nil:
			// A quantity ramps, at the same normalised position the edge
			// channel uses (ADR-0167 §SD4).
			seq := styletokens.SequentialDefault()
			bandLo := networkMagnitudeBandLo(seq, styletokens.NeutralBgSurface, styletokens.NeutralBorderDefault)
			maxV := maxOf(num)
			for i, v := range num {
				if !math.IsNaN(v) && maxV > 0 {
					nc.Color[i] = color.Hex(networkMagnitudeRamp(seq, bandLo, max(v, 0), maxV).AsHex())
				}
			}
		case lbl != nil:
			// A label takes the group palette by distinct value, in the order
			// the rows first name them.
			idx := make(map[string]int, 8)
			for i, l := range lbl {
				if l == "" {
					continue
				}
				k, seen := idx[l]
				if !seen {
					k = len(idx)
					idx[l] = k
				}
				nc.Color[i] = networkGroupColor(k)
			}
		case sets != nil:
			inst.chanReasons = append(inst.chanReasons, "`tone_by = "+sel.Column+"`: a set cannot colour one node")
		}
	}

	// Opacity: `opacity_by`, else the declared `opacity`.
	nc.Opacity = m.Opacity
	if sel, reason := parseGraphviewSelector(inst.opts.OpacityBy, graphviewChannelOpacity, m.hasColumn); reason != "" {
		inst.chanReasons = append(inst.chanReasons, reason)
	} else if !sel.IsZero() {
		num, _, _, reason := inst.channelColumn(m, sel, graphviewChannelOpacity)
		if reason != "" {
			inst.chanReasons = append(inst.chanReasons, reason)
		} else if num != nil {
			out := make([]float32, n)
			maxV := maxOf(num)
			for i, v := range num {
				out[i] = m.Opacity[i]
				if math.IsNaN(v) || maxV <= 0 {
					continue
				}
				share := min(max(v, 0), maxV) / maxV
				out[i] = float32(graphviewOpacityFloor + (1-graphviewOpacityFloor)*share)
			}
			nc.Opacity = out
		}
	}

	// Auras: `aura_by`, else the `groups` set or `group` (§SD4).
	nc.AuraOffsets, nc.AuraIds = m.AuraStart, m.AuraValues
	inst.groups = m.groups()
	if sel, reason := parseGraphviewSelector(inst.opts.AuraBy, graphviewChannelAura, m.hasColumn); reason != "" {
		inst.chanReasons = append(inst.chanReasons, reason)
	} else if !sel.IsZero() {
		num, lbl, sets, reason := inst.channelColumn(m, sel, graphviewChannelAura)
		if reason != "" {
			inst.chanReasons = append(inst.chanReasons, reason)
		} else {
			offsets := make([]int32, 1, n+1)
			var ids []string
			seen := make(map[string]struct{}, 8)
			var groups []string
			add := func(id string) {
				if id == "" {
					return
				}
				ids = append(ids, id)
				if _, dup := seen[id]; !dup {
					seen[id] = struct{}{}
					groups = append(groups, id)
				}
			}
			for i := range n {
				switch {
				case num != nil:
					if v := num[i]; !math.IsNaN(v) {
						add(strconv.FormatFloat(v, 'g', -1, 64))
					}
				case lbl != nil:
					add(lbl[i])
				case sets != nil:
					for _, id := range sets.ListValues[sets.ListStart[i]:sets.ListStart[i+1]] {
						add(id)
					}
				}
				offsets = append(offsets, int32(len(ids)))
			}
			if len(ids) > 0 {
				nc.AuraOffsets, nc.AuraIds = offsets, ids
			} else {
				nc.AuraOffsets, nc.AuraIds = nil, nil
			}
			slices.Sort(groups)
			inst.groups = groups
		}
	}
}

// aurasDefault is what the auras control follows on auto (§SD4): on when the
// query declared a `groups` set or an `aura_by` selector, off for `group`
// alone, where the reader judges whether the grouping is also spatial.
func (inst *GraphviewDriver) aurasDefault() bool {
	if inst.opts.AuraBy != "" {
		return true
	}
	return inst.lastModel != nil && inst.lastModel.GroupsDeclared
}

// effectiveBasemap decides whether a located graph is hosted in a map: the
// reader's switch when they moved it, else on.
func (inst *GraphviewDriver) effectiveBasemap() bool {
	if inst.basemapSet {
		return inst.basemap
	}
	return true
}

// hosted reports that this frame draws inside a map: a located declaration
// with the basemap on.
func (inst *GraphviewDriver) hosted() bool {
	return inst.located() && inst.effectiveBasemap() && inst.nodes.Len() > 0
}

// hostOrigin is the located set's centroid in the host's projected pixels at
// the reference zoom — the point the pins were measured from (§SD3).
func (inst *GraphviewDriver) hostOrigin() portolan.Point {
	return portolan.Point{X: inst.lastModel.GeoOriginX, Y: inst.lastModel.GeoOriginY}
}

// hostFitPadding is the room a framing leaves at the map's edges, in pixels.
const hostFitPadding = 48

// hostFitMaxZoom caps the framing of a single located vertex, which has no
// extent to fit.
const hostFitMaxZoom = 12

// renderHosted draws the located graph inside a portolan map (ADR-0228): the
// map owns the canvas, the pan, the wheel and the camera, and the graph
// paints and picks inside it at a world fixed at the reference zoom the pins
// were projected at, so the layout of the unlocated nodes has one equilibrium
// whatever the map shows. Without a configured tile server the offline atlas
// draws country outlines under the graph.
func (inst *GraphviewDriver) renderHosted(w, h float32) {
	m := inst.lastModel
	if inst.pm == nil {
		lat0, lon0 := netUnprojectWebMercator(m.GeoOriginX, m.GeoOriginY)
		inst.pm = portolan.New(inst.ids, portolan.Options{
			Source:  basemap.PortolanSource(),
			Loader:  basemap.PortolanLoader(),
			Center:  portolan.LL(lat0, lon0),
			Zoom:    netWebMercatorZoom,
			NoTiles: !basemap.Configured(),
		})
		inst.land = &landoverlay.Layer{}
		if a, err := worldmap.LoadAtlas(); err == nil {
			inst.atlas = a
		}
		inst.hostFitPending = true
	}
	origin := inst.hostOrigin()
	v := inst.pm.View()
	if inst.hostFitPending && v.Loaded() && v.Size().X > 0 {
		inst.hostFitPending = false
		inst.fitHost(m)
	}

	// The guest reads the pointer first and says what it took, so the map
	// stands down before it handles the same frame's input (ADR-0228 §SD2).
	canvas, area := inst.pm.Handles()
	claim := inst.view.HostedInput(graphview.HostCanvas{
		Canvas: canvas, Area: area, W: w, H: h,
		Camera: v.CameraAt(netWebMercatorZoom, origin),
	})
	inst.pm.SetPointerVeto(claim.Pointer)

	inst.pm.Render(w, h, func(p portolan.Projector) {
		if !basemap.Configured() && inst.atlas != nil {
			inst.land.Draw(p, inst.atlas, landoverlay.DefaultStyle())
		}
		// The map's handlers ran at the top of this Render, so the paint
		// takes the view as it is now rather than the one the pick used
		// (ADR-0228 §SD3b).
		cam := p.CameraAt(netWebMercatorZoom, origin)
		inst.view.SetHostCamera(cam)
		inst.scaleForHost(cam.Zoom)
		if err := inst.view.HostedPaintColumns(&inst.hostNodes, &inst.edges); err != nil {
			log.Error().Err(err).Msg("graphview hosted paint refused the declaration")
		}
	})
}

// scaleForHost prepares the frame's declaration for the host's camera: world
// units are reference-zoom pixels, so a radius meant as a screen size is
// divided by the camera's zoom to stay that size on screen (ADR-0228 §SD3a).
// Everything else in the declaration is shared with the plain path.
func (inst *GraphviewDriver) scaleForHost(zoom float32) {
	z := max(zoom, 1e-6)
	inst.hostNodes = inst.nodes
	inst.hostRadius = growToF32(inst.hostRadius, len(inst.nodes.Radius))
	for i, r := range inst.nodes.Radius {
		inst.hostRadius[i] = r / z
	}
	inst.hostNodes.Radius = inst.hostRadius
	inst.view.Opts.Style.NodeRadius = graphviewDefaultNodeRadius / z
	// The legend cannot live in the host's canvas (ADR-0224 §SD15); the
	// controls draw its rows.
	inst.view.Opts.Auras.Legend = graphview.AuraLegendExternal
}

// graphviewDefaultNodeRadius is the widget's own default, restated here so the
// hosted scale has a number to divide.
const graphviewDefaultNodeRadius = 5

func growToF32(s []float32, n int) []float32 {
	if cap(s) < n {
		return make([]float32, n)
	}
	return s[:n]
}

// fitHost frames the located vertices — or the `fit` rows among them when the
// query named some — in the host's view. A single point has no extent, so
// its zoom is capped.
func (inst *GraphviewDriver) fitHost(m *netModel) {
	minLat, minLon := math.Inf(1), math.Inf(1)
	maxLat, maxLon := math.Inf(-1), math.Inf(-1)
	count := 0
	want := func(i int) bool {
		if len(inst.hostFitIds) == 0 {
			return true
		}
		return slices.Contains(inst.hostFitIds, m.Key[i])
	}
	for i := range m.NumVertices() {
		la, lo := m.Lat[i], m.Lon[i]
		if math.IsNaN(la) || math.IsNaN(lo) || !want(i) {
			continue
		}
		minLat, maxLat = min(minLat, la), max(maxLat, la)
		minLon, maxLon = min(minLon, lo), max(maxLon, lo)
		count++
	}
	if count == 0 {
		return
	}
	_ = inst.pm.View().FitBounds(
		portolan.LatLngBoundsOf(portolan.LL(minLat, minLon), portolan.LL(maxLat, maxLon)),
		portolan.FitOptions{Padding: portolan.Point{X: hostFitPadding, Y: hostFitPadding}, MaxZoom: hostFitMaxZoom, HasMaxZoom: true})
}

// publishHostCamera writes the camera signals from the host's settled view:
// the bounds in degrees, the same bounds in the graph's world units, and the
// zoom as the guest camera's scale, so a query reads the located graph's
// camera in whichever units it wrote.
func (inst *GraphviewDriver) publishHostCamera(emit SignalEmitterI) {
	v := inst.pm.View()
	b := v.Bounds()
	emit.Emit(signalGvMinLat, b.GetSouth())
	emit.Emit(signalGvMaxLat, b.GetNorth())
	emit.Emit(signalGvMinLon, b.GetWest())
	emit.Emit(signalGvMaxLon, b.GetEast())
	m := inst.lastModel
	// y grows downward in the projection: the northern edge is the smaller y.
	x0, y0 := netProjectWebMercator(b.GetNorth(), b.GetWest())
	x1, y1 := netProjectWebMercator(b.GetSouth(), b.GetEast())
	emit.Emit(signalGvMinX, x0-m.GeoOriginX)
	emit.Emit(signalGvMaxX, x1-m.GeoOriginX)
	emit.Emit(signalGvMinY, y0-m.GeoOriginY)
	emit.Emit(signalGvMaxY, y1-m.GeoOriginY)
	emit.Emit(signalGvZoom, float64(v.CameraAt(netWebMercatorZoom, inst.hostOrigin()).Zoom))
}
