package widgets

import (
	"fmt"
	"slices"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/nav"
)

// The graphview demos (ADR-0224, ADR-0225) are five registered demos —
// ring, force-directed, hierarchical, exploration, styling — so the
// screenshot tour captures each one whole. They share the controls and
// the event log below.

// gvCommon is the per-demo state every graphview demo carries: the shared
// navigation toggles and the event log.
type gvCommon struct {
	labelsAlways bool
	fitAlways    bool
	showHover    bool
	eventLog     []graphview.Event
}

func newGvCommon() gvCommon { return gvCommon{labelsAlways: true} }

// gvInteract turns every interaction option on.
func gvInteract(o graphview.Options) graphview.Options {
	o.NodeClicking = true
	o.NodeSelection = true
	o.EdgeClicking = true
	o.EdgeSelection = true
	o.BackgroundClicking = true
	o.RectSelection = true
	return o
}

// demoGraphviewWidth is the canvas width: the pane's, as the layout probe
// reported it one frame ago, else a fallback that fits the stage.
func demoGraphviewWidth(ids *c.WidgetIdStack, key string) float32 {
	w := float32(960)
	if pw, _, ok := c.CapturePaneSize(ids.PrepareStr(key).Derive()); ok && pw > 100 {
		w = pw
	}
	return w
}

// controls draws the shared navigation row and pushes the toggles into
// the view's options.
func (cm *gvCommon) controls(ids *c.WidgetIdStack, v *graphview.View) {
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("gv-fit-now"), c.Atoms().Text("fit now").Keep()).SendResp().HasPrimaryClicked() {
			v.FitNow()
		}
		if c.Button(ids.PrepareStr("gv-fit-sel"), c.Atoms().Text("fit to selection").Keep()).SendResp().HasPrimaryClicked() {
			v.FitNodes(slices.Collect(v.SelectedNodes()))
		}
		c.Checkbox(ids.PrepareStr("gv-fit-always"), cm.fitAlways, "continuous fit").SendRespVal(&cm.fitAlways)
		c.Checkbox(ids.PrepareStr("gv-labels"), cm.labelsAlways, "labels always").SendRespVal(&cm.labelsAlways)
	}
	v.Opts.FitToScreen = cm.fitAlways
	v.Opts.LabelsAlways = cm.labelsAlways
}

// events lists the selection and the last events of the view.
func (cm *gvCommon) events(ids *c.WidgetIdStack, v *graphview.View) {
	c.Separator().Send()
	c.Checkbox(ids.PrepareStr("gv-show-hover"), cm.showHover, "show hover enter/leave events").SendRespVal(&cm.showHover)
	for id := range v.SelectedNodes() {
		c.Label(fmt.Sprintf("  selected node=%d", id)).Send()
	}
	for k := range v.SelectedEdges() {
		c.Label(fmt.Sprintf("  selected edge=%d→%d id=%d", k.From, k.To, k.Id)).Send()
	}
	for _, ev := range v.Events() {
		if !cm.showHover && (ev.Kind == graphview.EventKindNodeHoverEnter || ev.Kind == graphview.EventKindNodeHoverLeave ||
			ev.Kind == graphview.EventKindEdgeHoverEnter || ev.Kind == graphview.EventKindEdgeHoverLeave) {
			continue
		}
		cm.eventLog = append(cm.eventLog, ev)
	}
	const keep = 8
	if len(cm.eventLog) > keep {
		cm.eventLog = cm.eventLog[len(cm.eventLog)-keep:]
	}
	c.Label(fmt.Sprintf("recent events (last %d):", keep)).Send()
	if len(cm.eventLog) == 0 {
		c.Label("(click or drag a node, an edge or the background to see events)").Send()
		return
	}
	for _, ev := range cm.eventLog {
		switch {
		case ev.Kind == graphview.EventKindAuraToggle:
			c.Label(fmt.Sprintf("  %s  aura=%q", ev.Kind.String(), ev.Aura)).Send()
		case ev.Kind.IsEdge():
			c.Label(fmt.Sprintf("  %s  edge=%d→%d id=%d", ev.Kind.String(), ev.From, ev.To, ev.Edge)).Send()
		case ev.Kind.IsBackground():
			c.Label(fmt.Sprintf("  %s  world=(%.0f, %.0f)", ev.Kind.String(), ev.X, ev.Y)).Send()
		default:
			c.Label(fmt.Sprintf("  %s  node=%d", ev.Kind.String(), ev.Node)).Send()
		}
	}
}

// --- ring ---------------------------------------------------------------

// graphviewRingState is the random-layout ring: five nodes with donuts,
// parallel edges and a self-loop, and the interaction options to switch
// off one by one.
type graphviewRingState struct {
	gvCommon
	v       *graphview.View
	nodes   []graphview.NodeSpec
	edges   []graphview.EdgeSpec
	noDrag  bool
	noHover bool
	noZoom  bool
}

func newGraphviewRingState(ids *c.WidgetIdStack) (st *graphviewRingState) {
	st = &graphviewRingState{gvCommon: newGvCommon()}
	st.v = graphview.New(ids, "graphview-ring", gvInteract(graphview.Options{Layout: graphview.LayoutRandom}))
	// Five categorical nodes (Okabe-Ito cycle, ADR-0156), neutral edges, one
	// parallel edge and one self-loop to show the curved forms. Each node
	// carries a donut (ADR-0224 §SD9): the shares differ per node, n2 uses
	// explicit semantic colours, n4 is a partial ring with a Total so the
	// remainder shows as the track, n5 has none.
	const n = 5
	edgeCol := color.Hex(styletokens.NeutralBorderDefault.AsHex())
	semantic := color.ColorsFromU32([]uint32{
		styletokens.SuccessDefault.AsHex(), styletokens.WarningDefault.AsHex(), styletokens.ErrorDefault.AsHex(),
	})
	donuts := []graphview.Donut{
		{Values: []float32{3, 1}},
		{Values: []float32{5, 3, 2}, Colors: semantic},
		{Values: []float32{1, 1, 1, 1}},
		{Values: []float32{35}, Total: 100},
		{},
	}
	for i := range uint64(n) {
		fill := color.Hex(styletokens.NeutralBgSurface.AsHex())
		if donuts[i].IsEmpty() {
			fill = color.Hex(styletokens.QualitativeCycle(int(i)).AsHex())
		}
		st.nodes = append(st.nodes, graphview.NodeSpec{Id: i + 1, Label: fmt.Sprintf("n%d", i+1), Color: fill, Radius: 9, Donut: donuts[i]})
	}
	// Edge ids (ADR-0224 §SD13) tell the 1→2 pair's two edges apart in
	// hover, click and selection.
	for i := range uint64(n) {
		from, to := i+1, (i+1)%n+1
		st.edges = append(st.edges, graphview.EdgeSpec{From: from, To: to, Id: i + 1, Label: fmt.Sprintf("%d→%d", from, to), Color: edgeCol})
	}
	st.edges = append(st.edges,
		graphview.EdgeSpec{From: 1, To: 2, Id: 100, Label: "parallel", Color: edgeCol},
		graphview.EdgeSpec{From: 3, To: 3, Id: 101, Label: "loop", Color: edgeCol},
	)
	return
}

func demoGraphviewRing(ids *c.WidgetIdStack, st *graphviewRingState) {
	c.Label("Random layout. Drag a node, drag the background to pan, Shift-drag it for a rectangle selection, Ctrl+wheel to zoom, click to select, right-click for a secondary click; the 1→2 pair shows two parallel edges with their own ids and n3 a self-loop. Each node wears a donut: shares on n1–n3, a 35 % progress ring on n4, none on n5.").Send()
	st.controls(ids, st.v)
	for range c.Horizontal().KeepIter() {
		c.Checkbox(ids.PrepareStr("gv-ring-nodrag"), st.noDrag, "no dragging").SendRespVal(&st.noDrag)
		c.Checkbox(ids.PrepareStr("gv-ring-nohover"), st.noHover, "no hover").SendRespVal(&st.noHover)
		c.Checkbox(ids.PrepareStr("gv-ring-nozoom"), st.noZoom, "no zoom and pan").SendRespVal(&st.noZoom)
	}
	st.v.Opts.NoDragging, st.v.Opts.NoHover, st.v.Opts.NoZoomAndPan = st.noDrag, st.noHover, st.noZoom
	st.v.Render(st.nodes, st.edges, demoGraphviewWidth(ids, "gv-ring-pane"), 360)
	// Readout: the hovered node and where n1 sits in the canvas — the
	// hover path through R24 and the camera, legible to a headless scene.
	hover := "none"
	if id, ok := st.v.HoveredNode(); ok {
		hover = fmt.Sprintf("n%d", id)
	}
	x, y, _ := st.v.NodeCanvasPosition(1)
	ox, oy, _ := st.v.CanvasScreenOrigin()
	c.Label(fmt.Sprintf("hover: %s · n1 at (%.0f, %.0f) · canvas at (%.0f, %.0f)", hover, x, y, ox, oy)).Send()
	st.events(ids, st.v)
}

// --- force-directed -----------------------------------------------------

// graphviewForceState is the force-directed binary tree with every
// ForceParams knob, pins, donuts, auras, and a saved layout to restore.
type graphviewForceState struct {
	gvCommon
	v        *graphview.View
	nodes    int
	nodesF   float64 // slider binding for nodes
	nodeSet  []graphview.NodeSpec
	edgeSet  []graphview.EdgeSpec
	cg       bool
	dt       float64
	damping  float64
	eps      float64
	maxStep  float64
	kScale   float64
	paused   bool
	donuts   bool
	shares   []float32 // three shares per node, backing the donuts
	pinRoot  bool
	hold     bool // dropped nodes stay where they were dropped
	rest     bool // pause the simulation once it has settled
	auras    bool
	overlap  bool
	auraIds  [][]string // per node, the auras it belongs to when auras are on
	savedPos map[uint64][2]float32
	savedSel []uint64
}

func newGraphviewForceState(ids *c.WidgetIdStack) (st *graphviewForceState) {
	st = &graphviewForceState{
		gvCommon: newGvCommon(),
		nodes:    40, nodesF: 40, cg: true, dt: 0.05, damping: 0.3, eps: 0.1, maxStep: 20, kScale: 1,
	}
	st.v = graphview.New(ids, "graphview-force", gvInteract(graphview.Options{Layout: graphview.LayoutForceDirectedCG}))
	st.rebuild()
	// The first frame shows a settled tree rather than the spread-out start,
	// which is what a tour capture wants to see.
	st.v.FastForward(300)
	return
}

// rebuild regenerates the binary tree at the chosen size. Deterministic, so
// the tour captures the same picture every run.
func (st *graphviewForceState) rebuild() {
	st.nodeSet = st.nodeSet[:0]
	st.edgeSet = st.edgeSet[:0]
	col := color.Hex(styletokens.InfoDefault.AsHex())
	st.shares = st.shares[:0]
	st.auraIds = st.auraIds[:0]
	for i := uint64(1); i <= uint64(st.nodes); i++ {
		// Auras (ADR-0224 §SD11) group the tree by its depth-two subtrees:
		// nodes 4–7 head a team each, their descendants join it, nodes 2 and
		// 3 sit in both of their children's teams, the root in none.
		st.auraIds = append(st.auraIds, demoForceTeams(i))
		// Three deterministic shares per node, so the donut option costs no
		// allocation when toggled and the capture stays stable.
		h := i*0x9e3779b97f4a7c15 + 0x7f4a7c15
		st.shares = append(st.shares, float32(h>>60)+1, float32((h>>56)&15)+1, float32((h>>52)&15)+1)
		st.nodeSet = append(st.nodeSet, graphview.NodeSpec{Id: i, Label: fmt.Sprintf("#%d", i), Color: col, Radius: 6})
		if i >= 2 {
			st.edgeSet = append(st.edgeSet, graphview.EdgeSpec{From: (i-1)/2 + 1, To: i})
		}
	}
}

// demoForceTeams returns the aura ids of node i of the binary tree whose
// parent of node n is (n-1)/2+1: the team of its depth-two ancestor, or
// both teams under it for the depth-one nodes.
func demoForceTeams(i uint64) []string {
	team := func(head uint64) string { return fmt.Sprintf("team %d", head-3) }
	switch {
	case i == 1:
		return nil
	case i <= 3:
		return []string{team(2 * i), team(2*i + 1)}
	}
	for i > 7 {
		i = (i-1)/2 + 1
	}
	return []string{team(i)}
}

func demoGraphviewForce(ids *c.WidgetIdStack, st *graphviewForceState) {
	c.Label("Fruchterman–Reingold, with or without centre gravity, over a binary tree. Every parameter is live; the layout pauses by hand or once settled. A declared pin holds the root, dropped nodes may stay put, donuts and auras dress the nodes. Save the layout and restore it after a reset, selection included.").Send()
	st.controls(ids, st.v)
	// The slider binds a stable field (the databinding lands next frame), and
	// the graph is rebuilt when the rounded count moves.
	c.SliderF64(ids.PrepareStr("gv-force-n"), st.nodesF, 2, 20000).Logarithmic(true).Text("nodes (binary tree)").SendRespVal(&st.nodesF)
	if n := int(st.nodesF); n != st.nodes && n >= 2 {
		st.nodes = n
		st.rebuild()
	}
	c.Checkbox(ids.PrepareStr("gv-force-cg"), st.cg, "centre gravity (FR+CG)").SendRespVal(&st.cg)
	c.SliderF64(ids.PrepareStr("gv-force-dt"), st.dt, 0.01, 0.5).Text("dt").SendRespVal(&st.dt)
	c.SliderF64(ids.PrepareStr("gv-force-damping"), st.damping, 0.01, 1).Text("damping").SendRespVal(&st.damping)
	c.SliderF64(ids.PrepareStr("gv-force-eps"), st.eps, 0.001, 1).Text("epsilon").SendRespVal(&st.eps)
	c.SliderF64(ids.PrepareStr("gv-force-maxstep"), st.maxStep, 1, 100).Text("max step").SendRespVal(&st.maxStep)
	c.SliderF64(ids.PrepareStr("gv-force-kscale"), st.kScale, 0.1, 5).Text("k scale").SendRespVal(&st.kScale)
	for range c.Horizontal().KeepIter() {
		c.Checkbox(ids.PrepareStr("gv-force-paused"), st.paused, "paused").SendRespVal(&st.paused)
		c.Checkbox(ids.PrepareStr("gv-force-rest"), st.rest, "pause once settled").SendRespVal(&st.rest)
		c.Checkbox(ids.PrepareStr("gv-force-donuts"), st.donuts, "donut rings").SendRespVal(&st.donuts)
	}
	for range c.Horizontal().KeepIter() {
		c.Checkbox(ids.PrepareStr("gv-force-pinroot"), st.pinRoot, "pin the root at the canvas centre (declared pin)").SendRespVal(&st.pinRoot)
		c.Checkbox(ids.PrepareStr("gv-force-hold"), st.hold, "dropped nodes stay put (double-click releases)").SendRespVal(&st.hold)
	}
	for range c.Horizontal().KeepIter() {
		c.Checkbox(ids.PrepareStr("gv-force-auras"), st.auras, "auras by depth-two subtree, with a clickable legend").SendRespVal(&st.auras)
		c.Checkbox(ids.PrepareStr("gv-force-overlap"), st.overlap, "auras may overlap").SendRespVal(&st.overlap)
	}
	width := demoGraphviewWidth(ids, "gv-force-pane")
	for i := range st.nodeSet {
		st.nodeSet[i].Donut = graphview.Donut{}
		if st.donuts {
			st.nodeSet[i].Donut = graphview.Donut{Values: st.shares[3*i : 3*i+3]}
		}
		st.nodeSet[i].Pinned = false
		st.nodeSet[i].Auras = nil
		if st.auras {
			st.nodeSet[i].Auras = st.auraIds[i]
		}
	}
	if st.pinRoot && len(st.nodeSet) > 0 {
		// The declared pin is re-stated every frame in world units; here the
		// canvas centre, so it follows a pane resize.
		st.nodeSet[0].Pinned, st.nodeSet[0].PinX, st.nodeSet[0].PinY = true, width/2, 200
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("gv-force-reset"), c.Atoms().Text("reset layout").Keep()).SendResp().HasPrimaryClicked() {
			st.v.ResetLayout()
		}
		if c.Button(ids.PrepareStr("gv-force-ff"), c.Atoms().Text("fast-forward 200 steps").Keep()).SendResp().HasPrimaryClicked() {
			st.v.FastForward(200)
		}
		// Save and restore: Positions is the bulk read, SetNodePosition and
		// the programmatic selection the restore (ADR-0224 §SD12).
		if c.Button(ids.PrepareStr("gv-force-save"), c.Atoms().Text("save layout + selection").Keep()).SendResp().HasPrimaryClicked() {
			st.savedPos = make(map[uint64][2]float32, st.v.Metrics().NodeCount)
			for id, p := range st.v.Positions() {
				st.savedPos[id] = p
			}
			st.savedSel = slices.Collect(st.v.SelectedNodes())
		}
		if st.savedPos != nil && c.Button(ids.PrepareStr("gv-force-restore"), c.Atoms().Text(fmt.Sprintf("restore (%d nodes, %d selected)", len(st.savedPos), len(st.savedSel))).Keep()).SendResp().HasPrimaryClicked() {
			for id, p := range st.savedPos {
				st.v.SetNodePosition(id, p[0], p[1])
			}
			st.v.ClearSelection()
			for _, id := range st.savedSel {
				st.v.SelectNode(id)
			}
			st.v.FitNow()
		}
	}

	o := &st.v.Opts
	o.Layout = graphview.LayoutForceDirected
	if st.cg {
		o.Layout = graphview.LayoutForceDirectedCG
	}
	o.Force = graphview.ForceParams{
		Dt: float32(st.dt), Damping: float32(st.damping), Epsilon: float32(st.eps),
		MaxStep: float32(st.maxStep), KScale: float32(st.kScale), Paused: st.paused,
		PauseOnSettle: st.rest,
	}
	o.PinOnDrag = st.hold
	o.Auras = graphview.AuraParams{Enabled: st.auras, Overlap: st.overlap, Legend: graphview.AuraLegendInside}
	st.v.Render(st.nodeSet, st.edgeSet, width, 400)
	for _, ev := range st.v.Events() {
		if ev.Kind == graphview.EventKindNodeDoubleClick {
			st.v.UnpinNode(ev.Node)
		}
	}
	m := st.v.Metrics()
	c.Label(fmt.Sprintf("nodes=%d edges=%d pinned=%d steps=%d avg displacement=%.4f settled=%v paused=%v camera moved=%v",
		m.NodeCount, m.EdgeCount, m.PinnedCount, m.Steps, m.LastDisplacement, m.Settled, m.Paused, m.CameraMoved)).Send()
	st.events(ids, st.v)
}

// --- hierarchical -------------------------------------------------------

// graphviewHierState is the hierarchical layout over one tree or a forest.
type graphviewHierState struct {
	gvCommon
	v      *graphview.View
	nodes  []graphview.NodeSpec
	edges  []graphview.EdgeSpec
	row    float64
	col    float64
	cntr   bool
	lr     bool
	forest bool
}

func newGraphviewHierState(ids *c.WidgetIdStack) (st *graphviewHierState) {
	st = &graphviewHierState{gvCommon: newGvCommon(), row: 60, col: 50, cntr: true}
	st.v = graphview.New(ids, "graphview-hier", gvInteract(graphview.Options{Layout: graphview.LayoutHierarchical}))
	st.rebuild()
	return
}

// rebuild makes a ten-node binary tree, plus a five-node second tree and a
// cycle of two when the forest is on, so the forest packing shows.
func (st *graphviewHierState) rebuild() {
	st.nodes = st.nodes[:0]
	st.edges = st.edges[:0]
	col := color.Hex(styletokens.AccentDefault.AsHex())
	for i := uint64(1); i <= 10; i++ {
		st.nodes = append(st.nodes, graphview.NodeSpec{Id: i, Label: fmt.Sprintf("h%d", i), Color: col})
		if i >= 2 {
			st.edges = append(st.edges, graphview.EdgeSpec{From: (i-1)/2 + 1, To: i})
		}
	}
	if !st.forest {
		return
	}
	col2 := color.Hex(styletokens.QualitativeCycle(2).AsHex())
	for i := uint64(11); i <= 15; i++ {
		st.nodes = append(st.nodes, graphview.NodeSpec{Id: i, Label: fmt.Sprintf("h%d", i), Color: col2})
		if i >= 12 {
			st.edges = append(st.edges, graphview.EdgeSpec{From: 11, To: i})
		}
	}
	col3 := color.Hex(styletokens.QualitativeCycle(3).AsHex())
	st.nodes = append(st.nodes, graphview.NodeSpec{Id: 16, Label: "c1", Color: col3}, graphview.NodeSpec{Id: 17, Label: "c2", Color: col3})
	st.edges = append(st.edges, graphview.EdgeSpec{From: 16, To: 17}, graphview.EdgeSpec{From: 17, To: 16})
}

func demoGraphviewHier(ids *c.WidgetIdStack, st *graphviewHierState) {
	c.Label("The tree walk of the hierarchical layout: roots are the nodes without an incoming edge, subtrees pack left to right, a cycle with no root starts its own tree. Deterministic in the topology alone.").Send()
	st.controls(ids, st.v)
	c.SliderF64(ids.PrepareStr("gv-hier-row"), st.row, 10, 200).Text("row distance (levels)").SendRespVal(&st.row)
	c.SliderF64(ids.PrepareStr("gv-hier-col"), st.col, 10, 200).Text("column distance (siblings)").SendRespVal(&st.col)
	for range c.Horizontal().KeepIter() {
		c.Checkbox(ids.PrepareStr("gv-hier-centre"), st.cntr, "centre parent over children").SendRespVal(&st.cntr)
		c.Checkbox(ids.PrepareStr("gv-hier-lr"), st.lr, "orientation: left-right").SendRespVal(&st.lr)
		wasForest := st.forest
		c.Checkbox(ids.PrepareStr("gv-hier-forest"), st.forest, "forest: a second tree and a two-node cycle").SendRespVal(&st.forest)
		if wasForest != st.forest {
			st.rebuild()
			st.v.FitNow()
		}
	}
	o := &st.v.Opts
	o.Hier = graphview.HierParams{RowDist: float32(st.row), ColDist: float32(st.col), CenterParent: st.cntr}
	if st.lr {
		o.Hier.Orientation = graphview.OrientationLeftRight
	}
	st.v.Render(st.nodes, st.edges, demoGraphviewWidth(ids, "gv-hier-pane"), 320)
	st.events(ids, st.v)
}

// --- exploration ---------------------------------------------------------

// demoExploreNodes is the size of the exploration universe's first
// component; a second, smaller component follows it.
const (
	demoExploreNodes  = 150
	demoExploreSecond = 20
)

// graphviewExploreState is a navigator (ADR-0225) over a generated
// universe, drawn by a view in the radial or the force layout.
type graphviewExploreState struct {
	gvCommon
	nv     *nav.Navigator
	v      *graphview.View
	mode   nav.ModeE
	dir    nav.DirectionE
	radial bool
	radius float64
	tail   float64
	specs  map[uint64]graphview.NodeSpec   // every node's spec, for loading a stub
	held   map[uint64][]graphview.EdgeSpec // a stub's own edges, withheld until loaded
	centre []uint64
}

func newGraphviewExploreState(ids *c.WidgetIdStack) (st *graphviewExploreState) {
	st = &graphviewExploreState{gvCommon: newGvCommon(), mode: nav.ModeFocus, radial: true, radius: 2, tail: 1}
	st.v = graphview.New(ids, "graphview-explore", gvInteract(graphview.Options{Layout: graphview.LayoutRadial}))
	st.buildUniverse()
	return
}

// buildUniverse fills the navigator with a deterministic universe: a binary
// tree of demoExploreNodes nodes with a chord from every fifth node, a
// second tree of demoExploreSecond nodes no edge reaches, and every ninth
// node a stub whose own edges are held back until the navigator wants it
// (ADR-0225 §SD5). The universe is styled once here; relevance, focus and
// the badge are applied per Declare by the Style hook.
func (st *graphviewExploreState) buildUniverse() {
	total := uint64(demoExploreNodes + demoExploreSecond)
	st.specs = make(map[uint64]graphview.NodeSpec, total)
	st.held = make(map[uint64][]graphview.EdgeSpec, total/9+1)
	base := styletokens.InfoDefault.AsHex()
	second := styletokens.QualitativeCycle(2).AsHex()
	focused := styletokens.WarningDefault.AsHex()
	st.nv = nav.New(nav.Options{
		Style: func(id uint64, info nav.NodeInfo, spec *graphview.NodeSpec) {
			alpha := uint32(0x50 + 0xaf*info.Relevance)
			col := base
			if id > demoExploreNodes {
				col = second
			}
			if info.Focused {
				col = focused
			}
			spec.Color = color.Hex(col&^0xff | alpha)
			spec.Radius = 4 + 5*info.Relevance
			if info.Stub {
				spec.Label += " (stub)"
			}
			if info.HiddenNeighbours > 0 {
				spec.Label = fmt.Sprintf("%s +%d", spec.Label, info.HiddenNeighbours)
			}
		},
	})
	isStub := func(id uint64) bool { return id%9 == 0 }
	var nodes []nav.Node
	var edges []graphview.EdgeSpec
	parent := func(i uint64) (uint64, bool) {
		switch {
		case i >= 2 && i <= demoExploreNodes:
			return (i-1)/2 + 1, true
		case i > demoExploreNodes+1:
			return (i-demoExploreNodes-1)/2 + demoExploreNodes + 1, true
		}
		return 0, false
	}
	for i := uint64(1); i <= total; i++ {
		spec := graphview.NodeSpec{Id: i, Label: fmt.Sprintf("#%d", i)}
		st.specs[i] = spec
		nodes = append(nodes, nav.Node{Spec: spec, Stub: isStub(i)})
		var own []graphview.EdgeSpec
		if p, ok := parent(i); ok {
			own = append(own, graphview.EdgeSpec{From: p, To: i})
		}
		if i%5 == 0 && i <= demoExploreNodes {
			if to := (i*7)%demoExploreNodes + 1; to != i {
				own = append(own, graphview.EdgeSpec{From: i, To: to})
			}
		}
		for _, e := range own {
			// A stub's outgoing edges are its own neighbourhood: withheld.
			// The edge that reaches it from its parent is how it is known.
			if isStub(e.From) {
				st.held[e.From] = append(st.held[e.From], e)
				continue
			}
			edges = append(edges, e)
		}
	}
	st.nv.AddNodes(nodes)
	st.nv.AddEdges(edges)
	st.nv.SetInitial([]uint64{1})
	st.applyMode()
	st.nv.Reset()
}

// applyMode pushes the demo's toggles into the navigator's options.
func (st *graphviewExploreState) applyMode() {
	st.nv.Opts.Mode = st.mode
	st.nv.Opts.ExpandDir = st.dir
	st.nv.Opts.FocusRadius = int(st.radius)
	st.nv.Opts.FocusTailRadius = int(st.tail)
	st.nv.Opts.ExpandDepth = int(st.radius)
}

func demoGraphviewExplore(ids *c.WidgetIdStack, st *graphviewExploreState) {
	c.Label("Exploration over a 170-node universe in two components (ADR-0225): the picture is what the navigator derives from a few gestures. Double-click a node to focus it (focus mode) or to expand and collapse it (manual mode); right-click hides it. A \"+n\" on a label counts neighbours not shown. Every ninth node is a stub whose own edges arrive a frame after they are wanted, standing in for a load. Show-all over the radial layout packs the second component beside the first.").Send()
	st.controls(ids, st.v)
	wasMode := st.mode
	for range c.Horizontal().KeepIter() {
		c.Label("mode:").Send()
		for i, m := range []struct {
			mode nav.ModeE
			text string
		}{{nav.ModeFocus, "focus"}, {nav.ModeManual, "manual"}, {nav.ModeShowAll, "show all"}} {
			var clicked bool
			if c.RadioButton(ids.PrepareSeq(uint64(0x6a10+i)), c.Atoms().Text(m.text).Keep(), st.mode == m.mode).SendRespVal(&clicked).HasPrimaryClicked() {
				st.mode = m.mode
			}
		}
		c.Label("   expand direction:").Send()
		for i, d := range []struct {
			dir  nav.DirectionE
			text string
		}{{nav.DirectionBoth, "both"}, {nav.DirectionOut, "out"}, {nav.DirectionIn, "in"}} {
			var clicked bool
			if c.RadioButton(ids.PrepareSeq(uint64(0x6a20+i)), c.Atoms().Text(d.text).Keep(), st.dir == d.dir).SendRespVal(&clicked).HasPrimaryClicked() {
				st.dir = d.dir
			}
		}
	}
	c.SliderF64(ids.PrepareStr("gv-x-radius"), st.radius, 1, 4).Text("focus radius / expand depth").SendRespVal(&st.radius)
	c.SliderF64(ids.PrepareStr("gv-x-tail"), st.tail, 1, 4).Text("tail radius (older focus nodes)").SendRespVal(&st.tail)
	for range c.Horizontal().KeepIter() {
		c.Checkbox(ids.PrepareStr("gv-x-radial"), st.radial, "radial layout around the focus nodes (else force-directed)").SendRespVal(&st.radial)
		if c.Button(ids.PrepareStr("gv-x-reset"), c.Atoms().Text("reset exploration").Keep()).SendResp().HasPrimaryClicked() || wasMode != st.mode {
			st.applyMode()
			st.nv.Reset()
			st.v.ResetLayout()
		}
		if hidden := st.nv.HiddenNodes(); len(hidden) > 0 {
			if c.Button(ids.PrepareStr("gv-x-unhide"), c.Atoms().Text(fmt.Sprintf("show %d hidden", len(hidden))).Keep()).SendResp().HasPrimaryClicked() {
				for _, id := range slices.Clone(hidden) {
					st.nv.Show(id)
				}
			}
		}
		if c.Button(ids.PrepareStr("gv-x-second"), c.Atoms().Text("show #151 (second component)").Keep()).SendResp().HasPrimaryClicked() {
			st.nv.Show(demoExploreNodes + 1)
			if st.mode == nav.ModeFocus {
				st.nv.Focus(demoExploreNodes+1, 1)
			}
		}
	}
	st.applyMode()

	// The load: every stub wanted last frame becomes loaded, with its own
	// edges when it has any.
	for _, id := range slices.Clone(st.nv.Pending()) {
		st.nv.AddNodes([]nav.Node{{Spec: st.specs[id]}})
		if held, ok := st.held[id]; ok {
			st.nv.AddEdges(held)
			delete(st.held, id)
		}
	}

	ns, es := st.nv.Declare()
	o := &st.v.Opts
	o.Layout = graphview.LayoutForceDirectedCG
	if st.radial {
		o.Layout = graphview.LayoutRadial
	}
	st.centre = st.centre[:0]
	if st.mode == nav.ModeFocus {
		st.centre = append(st.centre, st.nv.FocusNodes()...)
	} else {
		st.centre = append(st.centre, 1)
	}
	o.Radial.Centers = st.centre
	st.v.Render(ns, es, demoGraphviewWidth(ids, "gv-explore-pane"), 420)
	for _, ev := range st.v.Events() {
		st.nv.Apply(ev)
		if ev.Kind == graphview.EventKindNodeSecondaryClick {
			st.nv.Hide(ev.Node)
		}
	}
	c.Label(fmt.Sprintf("visible=%d of %d · focus=%v · hidden=%d · wanted stubs=%d", len(ns), st.nv.NodeCount(), st.nv.FocusNodes(), len(st.nv.HiddenNodes()), len(st.nv.Pending()))).Send()
	st.events(ids, st.v)
}

// --- styling and weights ------------------------------------------------

// graphviewStyleState is a hub with spokes under the force layout, with
// weighted edges, zoom bounds, a node outline, monospace labels and auras
// with explicit styles.
type graphviewStyleState struct {
	gvCommon
	v        *graphview.View
	nodes    []graphview.NodeSpec
	edges    []graphview.EdgeSpec
	weighted bool
	bounded  bool
	stroke   float64
	mono     bool
	auras    bool
}

// demoStyleSpokes is the number of spokes around the hub.
const demoStyleSpokes = 8

func newGraphviewStyleState(ids *c.WidgetIdStack) (st *graphviewStyleState) {
	st = &graphviewStyleState{gvCommon: newGvCommon(), weighted: true, stroke: 1.5, auras: true}
	st.v = graphview.New(ids, "graphview-style", gvInteract(graphview.Options{Layout: graphview.LayoutForceDirectedCG}))
	st.v.FastForward(300)
	hub := color.Hex(styletokens.AccentDefault.AsHex())
	st.nodes = append(st.nodes, graphview.NodeSpec{Id: 1, Label: "hub", Color: hub, Radius: 9, Auras: []string{"near", "far"}})
	for i := uint64(1); i <= demoStyleSpokes; i++ {
		id := i + 1
		aura := "far"
		if i%2 == 0 {
			aura = "near"
		}
		st.nodes = append(st.nodes, graphview.NodeSpec{
			Id: id, Label: fmt.Sprintf("s%d", i), Radius: 6, Auras: []string{aura},
			Color: color.Hex(styletokens.QualitativeCycle(int(i % 2)).AsHex()),
		})
		st.edges = append(st.edges, graphview.EdgeSpec{From: 1, To: id, Id: id})
	}
	// A rim between the far spokes, weakly.
	for i := uint64(1); i <= demoStyleSpokes; i += 2 {
		next := i + 2
		if next > demoStyleSpokes {
			next = 1
		}
		st.edges = append(st.edges, graphview.EdgeSpec{From: i + 1, To: next + 1, Id: 100 + i})
	}
	return
}

func demoGraphviewStyle(ids *c.WidgetIdStack, st *graphviewStyleState) {
	c.Label("A hub and eight spokes. Odd spokes hang on long, weak edges and even ones on short, strong ones (EdgeSpec.Length and Strength, ADR-0224 §SD13): with the weights on, two rings form; off, one. Zoom bounds cap the wheel at half and double; the node outline and the monospace face are Style fields; the two auras carry explicit fills, the far one a dashed-looking hairline and no legend row.").Send()
	st.controls(ids, st.v)
	for range c.Horizontal().KeepIter() {
		c.Checkbox(ids.PrepareStr("gv-style-weighted"), st.weighted, "edge length and strength").SendRespVal(&st.weighted)
		c.Checkbox(ids.PrepareStr("gv-style-bounded"), st.bounded, "zoom bounds 0.5–2").SendRespVal(&st.bounded)
		c.Checkbox(ids.PrepareStr("gv-style-mono"), st.mono, "monospace labels").SendRespVal(&st.mono)
		c.Checkbox(ids.PrepareStr("gv-style-auras"), st.auras, "styled auras").SendRespVal(&st.auras)
	}
	c.SliderF64(ids.PrepareStr("gv-style-stroke"), st.stroke, 0, 4).Text("node outline width (0 keeps a node one batched marker)").SendRespVal(&st.stroke)
	for i := range st.edges {
		e := &st.edges[i]
		e.Length, e.Strength = 0, 0
		if !st.weighted {
			continue
		}
		switch {
		case e.Id > 100: // rim
			e.Length, e.Strength = 1, 0.3
		case e.To%2 == 0: // odd spoke ids are even numbers: far
			e.Length, e.Strength = 2.5, 0.6
		default:
			e.Length, e.Strength = 0.6, 2
		}
	}
	o := &st.v.Opts
	o.ZoomMin, o.ZoomMax = 0, 0
	if st.bounded {
		o.ZoomMin, o.ZoomMax = 0.5, 2
	}
	o.Style.NodeStrokeW = float32(st.stroke)
	o.Style.NodeStroke = color.Hex(styletokens.NeutralTextPrimary.AsHex())
	o.Style.Monospace = st.mono
	o.Auras = graphview.AuraParams{
		Enabled: st.auras, Legend: graphview.AuraLegendInside, Overlap: true, Intensity: 4,
		Styles: map[string]graphview.AuraStyle{
			"near": {Fill: color.Hex(styletokens.SuccessDefault.AsHex()&^0xff | 0x40), Label: "short, strong spokes", ZIndex: 1},
			"far":  {Fill: color.Hex(styletokens.InfoDefault.AsHex()&^0xff | 0x30), Line: color.Hex(styletokens.InfoDefault.AsHex()), LineWidth: 1, NoLegend: true},
		},
	}
	st.v.Render(st.nodes, st.edges, demoGraphviewWidth(ids, "gv-style-pane"), 380)
	zoom, _, _ := st.v.Camera()
	c.Label(fmt.Sprintf("zoom=%.2f · weighted=%v", zoom, st.weighted)).Send()
	st.events(ids, st.v)
}
