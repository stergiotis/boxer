package widgets

import (
	"fmt"
	"slices"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
)

// graphviewDemoState carries the three graphview instances of the gallery
// demo (ADR-0224) — the ring, the force-directed tree and the
// hierarchical tree — plus the shared navigation toggles and event log. It
// mirrors the egui_graphs demo so the two can be compared side by side
// while the binding is still in the tree.
type graphviewDemoState struct {
	ring  *graphview.View
	force *graphview.View
	hier  *graphview.View

	ringNodes []graphview.NodeSpec
	ringEdges []graphview.EdgeSpec

	forceNodes   int
	forceNodesF  float64 // slider binding for forceNodes
	forceNodeSet []graphview.NodeSpec
	forceEdgeSet []graphview.EdgeSpec
	forceCG      bool
	forceDt      float64
	forceDamping float64
	forceEps     float64
	forceMaxStep float64
	forceKScale  float64
	forcePaused  bool
	forceDonuts  bool
	forceShares  []float32 // three shares per node, backing the donuts
	forcePinRoot bool
	forceHold    bool // dropped nodes stay where they were dropped
	forceRest    bool // pause the simulation once it has settled
	forceAuras   bool
	forceOverlap bool
	forceAuraIds [][]string // per node, the auras it belongs to when auras are on

	hierNodes []graphview.NodeSpec
	hierEdges []graphview.EdgeSpec
	hierRow   float64
	hierCol   float64
	hierCntr  bool
	hierLR    bool

	labelsAlways bool
	fitAlways    bool
	showHover    bool
	eventLog     []graphview.Event
}

func newGraphviewDemoState(ids *c.WidgetIdStack) (st *graphviewDemoState) {
	st = &graphviewDemoState{
		forceNodes:   40,
		forceNodesF:  40,
		forceCG:      true,
		forceDt:      0.05,
		forceDamping: 0.3,
		forceEps:     0.1,
		forceMaxStep: 20,
		forceKScale:  1,
		hierRow:      60,
		hierCol:      50,
		hierCntr:     true,
		labelsAlways: true,
	}
	interact := func(o graphview.Options) graphview.Options {
		o.NodeClicking = true
		o.NodeSelection = true
		o.EdgeClicking = true
		o.EdgeSelection = true
		o.BackgroundClicking = true
		o.RectSelection = true
		return o
	}
	st.ring = graphview.New(ids, "graphview-ring", interact(graphview.Options{Layout: graphview.LayoutRandom}))
	st.force = graphview.New(ids, "graphview-force", interact(graphview.Options{Layout: graphview.LayoutForceDirectedCG}))
	st.hier = graphview.New(ids, "graphview-hier", interact(graphview.Options{Layout: graphview.LayoutHierarchical}))

	// Ring: five categorical nodes (Okabe-Ito cycle, ADR-0156), neutral
	// edges, one parallel edge and one self-loop to show the curved forms.
	const n = 5
	edgeCol := color.Hex(styletokens.NeutralBorderDefault.AsHex())
	// Each node carries a donut (ADR-0224 §SD9): the shares differ per node,
	// n2 uses explicit semantic colours, n4 is a partial ring with a Total so
	// the remainder shows as the track, n5 has none.
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
	// Ringed nodes take a neutral disc so the slices carry the colour; the
	// plain n5 keeps a categorical fill.
	for i := range uint64(n) {
		fill := color.Hex(styletokens.NeutralBgSurface.AsHex())
		if donuts[i].IsEmpty() {
			fill = color.Hex(styletokens.QualitativeCycle(int(i)).AsHex())
		}
		st.ringNodes = append(st.ringNodes, graphview.NodeSpec{
			Id: i + 1, Label: fmt.Sprintf("n%d", i+1),
			Color:  fill,
			Radius: 9,
			Donut:  donuts[i],
		})
	}
	// Edge ids (ADR-0224 §SD13) tell the 1→2 pair's two edges apart in
	// hover, click and selection.
	for i := range uint64(n) {
		from, to := i+1, (i+1)%n+1
		st.ringEdges = append(st.ringEdges, graphview.EdgeSpec{From: from, To: to, Id: i + 1, Label: fmt.Sprintf("%d→%d", from, to), Color: edgeCol})
	}
	st.ringEdges = append(st.ringEdges,
		graphview.EdgeSpec{From: 1, To: 2, Id: 100, Label: "parallel", Color: edgeCol},
		graphview.EdgeSpec{From: 3, To: 3, Id: 101, Label: "loop", Color: edgeCol},
	)

	// Hierarchical: a ten-node binary tree.
	for i := uint64(1); i <= 10; i++ {
		st.hierNodes = append(st.hierNodes, graphview.NodeSpec{Id: i, Label: fmt.Sprintf("h%d", i), Color: color.Hex(styletokens.AccentDefault.AsHex())})
		if i >= 2 {
			st.hierEdges = append(st.hierEdges, graphview.EdgeSpec{From: (i-1)/2 + 1, To: i})
		}
	}
	st.rebuildForceGraph()
	return
}

// rebuildForceGraph regenerates the force demo's binary tree at the chosen
// size. Deterministic, so the tour captures the same picture every run.
func (st *graphviewDemoState) rebuildForceGraph() {
	st.forceNodeSet = st.forceNodeSet[:0]
	st.forceEdgeSet = st.forceEdgeSet[:0]
	col := color.Hex(styletokens.InfoDefault.AsHex())
	st.forceShares = st.forceShares[:0]
	st.forceAuraIds = st.forceAuraIds[:0]
	for i := uint64(1); i <= uint64(st.forceNodes); i++ {
		// Auras (ADR-0224 §SD11) group the tree by its depth-two subtrees:
		// nodes 4–7 head a team each, their descendants join it, nodes 2 and
		// 3 sit in both of their children's teams, the root in none.
		st.forceAuraIds = append(st.forceAuraIds, demoForceTeams(i))
		// Three deterministic shares per node, so the donut option costs no
		// allocation when toggled and the capture stays stable.
		h := i*0x9e3779b97f4a7c15 + 0x7f4a7c15
		st.forceShares = append(st.forceShares, float32(h>>60)+1, float32((h>>56)&15)+1, float32((h>>52)&15)+1)
		st.forceNodeSet = append(st.forceNodeSet, graphview.NodeSpec{Id: i, Label: fmt.Sprintf("#%d", i), Color: col, Radius: 6})
		if i >= 2 {
			st.forceEdgeSet = append(st.forceEdgeSet, graphview.EdgeSpec{From: (i-1)/2 + 1, To: i})
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

// demoGraphviewWidth is the canvas width: the pane's, as the layout probe
// reported it one frame ago, else a fallback that fits the stage.
func demoGraphviewWidth(ids *c.WidgetIdStack, key string) float32 {
	w := float32(960)
	if pw, _, ok := c.CapturePaneSize(ids.PrepareStr(key).Derive()); ok && pw > 100 {
		w = pw
	}
	return w
}

func demoGraphviewNav(ids *c.WidgetIdStack, st *graphviewDemoState) {
	if c.Button(ids.PrepareStr("gv-fit-now"), c.Atoms().Text("fit now (all graphs)").Keep()).SendResp().HasPrimaryClicked() {
		st.ring.FitNow()
		st.force.FitNow()
		st.hier.FitNow()
	}
	if c.Button(ids.PrepareStr("gv-fit-sel"), c.Atoms().Text("fit to selection (all graphs)").Keep()).SendResp().HasPrimaryClicked() {
		for _, v := range []*graphview.View{st.ring, st.force, st.hier} {
			v.FitNodes(slices.Collect(v.SelectedNodes()))
		}
	}
	c.Checkbox(ids.PrepareStr("gv-fit-always"), st.fitAlways, "continuous fit (all graphs)").SendRespVal(&st.fitAlways)
	c.Checkbox(ids.PrepareStr("gv-labels"), st.labelsAlways, "labels always").SendRespVal(&st.labelsAlways)
	for _, v := range []*graphview.View{st.ring, st.force, st.hier} {
		v.Opts.FitToScreen = st.fitAlways
		v.Opts.LabelsAlways = st.labelsAlways
	}
}

func demoGraphviewRing(ids *c.WidgetIdStack, st *graphviewDemoState) {
	c.Label("Random layout. Drag a node, drag the background to pan, Shift-drag it for a rectangle selection, Ctrl+wheel to zoom, click to select, right-click for a secondary click; the 1→2 pair shows two parallel edges with their own ids and n3 a self-loop. Each node wears a donut: shares on n1–n3, a 35 % progress ring on n4, none on n5.").Send()
	st.ring.Render(st.ringNodes, st.ringEdges, demoGraphviewWidth(ids, "gv-ring-pane"), 360)
	// Readout: the hovered node and where n1 sits in the canvas — the
	// hover path through R24 and the camera, legible to a headless scene.
	hover := "none"
	if id, ok := st.ring.HoveredNode(); ok {
		hover = fmt.Sprintf("n%d", id)
	}
	x, y, _ := st.ring.NodeCanvasPosition(1)
	ox, oy, _ := st.ring.CanvasScreenOrigin()
	c.Label(fmt.Sprintf("hover: %s · n1 at (%.0f, %.0f) · canvas at (%.0f, %.0f)", hover, x, y, ox, oy)).Send()
}

func demoGraphviewForce(ids *c.WidgetIdStack, st *graphviewDemoState) {
	// The slider binds a stable field (the databinding lands next frame), and
	// the graph is rebuilt when the rounded count moves.
	c.SliderF64(ids.PrepareStr("gv-force-n"), st.forceNodesF, 2, 20000).Logarithmic(true).Text("nodes (binary tree)").SendRespVal(&st.forceNodesF)
	if n := int(st.forceNodesF); n != st.forceNodes && n >= 2 {
		st.forceNodes = n
		st.rebuildForceGraph()
	}
	c.Checkbox(ids.PrepareStr("gv-force-cg"), st.forceCG, "centre gravity (FR+CG)").SendRespVal(&st.forceCG)
	c.SliderF64(ids.PrepareStr("gv-force-dt"), st.forceDt, 0.01, 0.5).Text("dt").SendRespVal(&st.forceDt)
	c.SliderF64(ids.PrepareStr("gv-force-damping"), st.forceDamping, 0.01, 1).Text("damping").SendRespVal(&st.forceDamping)
	c.SliderF64(ids.PrepareStr("gv-force-eps"), st.forceEps, 0.001, 1).Text("epsilon").SendRespVal(&st.forceEps)
	c.SliderF64(ids.PrepareStr("gv-force-maxstep"), st.forceMaxStep, 1, 100).Text("max step").SendRespVal(&st.forceMaxStep)
	c.SliderF64(ids.PrepareStr("gv-force-kscale"), st.forceKScale, 0.1, 5).Text("k scale").SendRespVal(&st.forceKScale)
	c.Checkbox(ids.PrepareStr("gv-force-paused"), st.forcePaused, "paused").SendRespVal(&st.forcePaused)
	c.Checkbox(ids.PrepareStr("gv-force-rest"), st.forceRest, "pause once settled (wakes on a drag or a change)").SendRespVal(&st.forceRest)
	c.Checkbox(ids.PrepareStr("gv-force-donuts"), st.forceDonuts, "donut rings on every node").SendRespVal(&st.forceDonuts)
	c.Checkbox(ids.PrepareStr("gv-force-pinroot"), st.forcePinRoot, "pin the root at the canvas centre (declared pin)").SendRespVal(&st.forcePinRoot)
	c.Checkbox(ids.PrepareStr("gv-force-hold"), st.forceHold, "dropped nodes stay put (double-click releases)").SendRespVal(&st.forceHold)
	c.Checkbox(ids.PrepareStr("gv-force-auras"), st.forceAuras, "auras by depth-two subtree, with a clickable legend").SendRespVal(&st.forceAuras)
	c.Checkbox(ids.PrepareStr("gv-force-overlap"), st.forceOverlap, "auras may overlap (else each cell goes to its strongest aura)").SendRespVal(&st.forceOverlap)
	width := demoGraphviewWidth(ids, "gv-force-pane")
	for i := range st.forceNodeSet {
		st.forceNodeSet[i].Donut = graphview.Donut{}
		if st.forceDonuts {
			st.forceNodeSet[i].Donut = graphview.Donut{Values: st.forceShares[3*i : 3*i+3]}
		}
		st.forceNodeSet[i].Pinned = false
		st.forceNodeSet[i].Auras = nil
		if st.forceAuras {
			st.forceNodeSet[i].Auras = st.forceAuraIds[i]
		}
	}
	if st.forcePinRoot && len(st.forceNodeSet) > 0 {
		// The declared pin is re-stated every frame in world units; here the
		// canvas centre, so it follows a pane resize.
		st.forceNodeSet[0].Pinned, st.forceNodeSet[0].PinX, st.forceNodeSet[0].PinY = true, width/2, 200
	}
	for range c.Horizontal().KeepIter() {
		if c.Button(ids.PrepareStr("gv-force-reset"), c.Atoms().Text("reset layout").Keep()).SendResp().HasPrimaryClicked() {
			st.force.ResetLayout()
		}
		if c.Button(ids.PrepareStr("gv-force-ff"), c.Atoms().Text("fast-forward 200 steps").Keep()).SendResp().HasPrimaryClicked() {
			st.force.FastForward(200)
		}
	}

	o := &st.force.Opts
	o.Layout = graphview.LayoutForceDirected
	if st.forceCG {
		o.Layout = graphview.LayoutForceDirectedCG
	}
	o.Force = graphview.ForceParams{
		Dt: float32(st.forceDt), Damping: float32(st.forceDamping), Epsilon: float32(st.forceEps),
		MaxStep: float32(st.forceMaxStep), KScale: float32(st.forceKScale), Paused: st.forcePaused,
		PauseOnSettle: st.forceRest,
	}
	o.PinOnDrag = st.forceHold
	o.Auras = graphview.AuraParams{Enabled: st.forceAuras, Overlap: st.forceOverlap, Legend: true}
	st.force.Render(st.forceNodeSet, st.forceEdgeSet, width, 400)
	for _, ev := range st.force.Events() {
		if ev.Kind == graphview.EventKindNodeDoubleClick {
			st.force.UnpinNode(ev.Node)
		}
	}
	m := st.force.Metrics()
	c.Label(fmt.Sprintf("nodes=%d edges=%d pinned=%d steps=%d avg displacement=%.4f settled=%v paused=%v",
		m.NodeCount, m.EdgeCount, m.PinnedCount, m.Steps, m.LastDisplacement, m.Settled, m.Paused)).Send()
}

func demoGraphviewHier(ids *c.WidgetIdStack, st *graphviewDemoState) {
	c.SliderF64(ids.PrepareStr("gv-hier-row"), st.hierRow, 10, 200).Text("row distance (levels)").SendRespVal(&st.hierRow)
	c.SliderF64(ids.PrepareStr("gv-hier-col"), st.hierCol, 10, 200).Text("column distance (siblings)").SendRespVal(&st.hierCol)
	c.Checkbox(ids.PrepareStr("gv-hier-centre"), st.hierCntr, "centre parent over children").SendRespVal(&st.hierCntr)
	c.Checkbox(ids.PrepareStr("gv-hier-lr"), st.hierLR, "orientation: left-right (else top-down)").SendRespVal(&st.hierLR)
	o := &st.hier.Opts
	o.Hier = graphview.HierParams{RowDist: float32(st.hierRow), ColDist: float32(st.hierCol), CenterParent: st.hierCntr}
	if st.hierLR {
		o.Hier.Orientation = graphview.OrientationLeftRight
	}
	st.hier.Render(st.hierNodes, st.hierEdges, demoGraphviewWidth(ids, "gv-hier-pane"), 300)
}

func demoGraphviewEventLog(ids *c.WidgetIdStack, st *graphviewDemoState) {
	c.Separator().Send()
	c.Checkbox(ids.PrepareStr("gv-show-hover"), st.showHover, "show hover enter/leave events").SendRespVal(&st.showHover)
	views := []struct {
		name string
		v    *graphview.View
	}{{"ring", st.ring}, {"force", st.force}, {"hier", st.hier}}
	for _, e := range views {
		for id := range e.v.SelectedNodes() {
			c.Label(fmt.Sprintf("  selected node=%d  graph=%s", id, e.name)).Send()
		}
		for k := range e.v.SelectedEdges() {
			c.Label(fmt.Sprintf("  selected edge=%d→%d id=%d  graph=%s", k.From, k.To, k.Id, e.name)).Send()
		}
		for _, ev := range e.v.Events() {
			if !st.showHover && (ev.Kind == graphview.EventKindNodeHoverEnter || ev.Kind == graphview.EventKindNodeHoverLeave ||
				ev.Kind == graphview.EventKindEdgeHoverEnter || ev.Kind == graphview.EventKindEdgeHoverLeave) {
				continue
			}
			st.eventLog = append(st.eventLog, ev)
		}
	}
	const keep = 10
	if len(st.eventLog) > keep {
		st.eventLog = st.eventLog[len(st.eventLog)-keep:]
	}
	c.Label(fmt.Sprintf("recent events (last %d):", keep)).Send()
	if len(st.eventLog) == 0 {
		c.Label("(click or drag a node, an edge or the background to see events)").Send()
		return
	}
	for _, ev := range st.eventLog {
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
