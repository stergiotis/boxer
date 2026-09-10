package widgets

import (
	"fmt"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
)

// graphviewDemoState carries the three graphview instances of the gallery
// demo (ADR-0224, proposed) — the ring, the force-directed tree and the
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
		return o
	}
	st.ring = graphview.New(ids, "graphview-ring", interact(graphview.Options{Layout: graphview.LayoutRandom}))
	st.force = graphview.New(ids, "graphview-force", interact(graphview.Options{Layout: graphview.LayoutForceDirectedCG}))
	st.hier = graphview.New(ids, "graphview-hier", interact(graphview.Options{Layout: graphview.LayoutHierarchical}))

	// Ring: five categorical nodes (Okabe-Ito cycle, ADR-0156), neutral
	// edges, one parallel edge and one self-loop to show the curved forms.
	const n = 5
	edgeCol := color.Hex(styletokens.NeutralBorderDefault.AsHex())
	for i := range uint64(n) {
		st.ringNodes = append(st.ringNodes, graphview.NodeSpec{
			Id: i + 1, Label: fmt.Sprintf("n%d", i+1),
			Color: color.Hex(styletokens.QualitativeCycle(int(i)).AsHex()),
		})
	}
	for i := range uint64(n) {
		from, to := i+1, (i+1)%n+1
		st.ringEdges = append(st.ringEdges, graphview.EdgeSpec{From: from, To: to, Label: fmt.Sprintf("%d→%d", from, to), Color: edgeCol})
	}
	st.ringEdges = append(st.ringEdges,
		graphview.EdgeSpec{From: 1, To: 2, Label: "parallel", Color: edgeCol},
		graphview.EdgeSpec{From: 3, To: 3, Label: "loop", Color: edgeCol},
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
	for i := uint64(1); i <= uint64(st.forceNodes); i++ {
		st.forceNodeSet = append(st.forceNodeSet, graphview.NodeSpec{Id: i, Label: fmt.Sprintf("#%d", i), Color: col})
		if i >= 2 {
			st.forceEdgeSet = append(st.forceEdgeSet, graphview.EdgeSpec{From: (i-1)/2 + 1, To: i})
		}
	}
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
	c.Checkbox(ids.PrepareStr("gv-fit-always"), st.fitAlways, "continuous fit (all graphs)").SendRespVal(&st.fitAlways)
	c.Checkbox(ids.PrepareStr("gv-labels"), st.labelsAlways, "labels always").SendRespVal(&st.labelsAlways)
	for _, v := range []*graphview.View{st.ring, st.force, st.hier} {
		v.Opts.FitToScreen = st.fitAlways
		v.Opts.LabelsAlways = st.labelsAlways
	}
}

func demoGraphviewRing(ids *c.WidgetIdStack, st *graphviewDemoState) {
	c.Label("Random layout. Drag a node, drag the background to pan, Ctrl+wheel to zoom, click to select; the 1→2 pair shows a parallel edge and n3 a self-loop.").Send()
	st.ring.Render(st.ringNodes, st.ringEdges, demoGraphviewWidth(ids, "gv-ring-pane"), 360)
	// Readout: the hovered node and where n1 sits in the canvas — the
	// hover path through R24 and the camera, legible to a headless scene.
	hover := "none"
	if id, ok := st.ring.Hovered(); ok {
		hover = fmt.Sprintf("n%d", id)
	}
	x, y, _ := st.ring.NodeScreenPosition(1)
	ox, oy, _ := st.ring.CanvasScreenOrigin()
	c.Label(fmt.Sprintf("hover: %s · n1 at (%.0f, %.0f) · canvas at (%.0f, %.0f)", hover, x, y, ox, oy)).Send()
}

func demoGraphviewForce(ids *c.WidgetIdStack, st *graphviewDemoState) {
	// The slider binds a stable field (the databinding lands next frame), and
	// the graph is rebuilt when the rounded count moves.
	c.SliderF64(ids.PrepareStr("gv-force-n"), st.forceNodesF, 2, 3000).Text("nodes (binary tree)").SendRespVal(&st.forceNodesF)
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
	}
	st.force.Render(st.forceNodeSet, st.forceEdgeSet, demoGraphviewWidth(ids, "gv-force-pane"), 400)
	m := st.force.Metrics()
	c.Label(fmt.Sprintf("nodes=%d edges=%d steps=%d avg displacement=%.4f settled=%v",
		m.NodeCount, m.EdgeCount, m.Steps, m.LastDisplacement, st.force.IsSettled())).Send()
}

func demoGraphviewHier(ids *c.WidgetIdStack, st *graphviewDemoState) {
	c.SliderF64(ids.PrepareStr("gv-hier-row"), st.hierRow, 10, 200).Text("row distance (levels)").SendRespVal(&st.hierRow)
	c.SliderF64(ids.PrepareStr("gv-hier-col"), st.hierCol, 10, 200).Text("column distance (siblings)").SendRespVal(&st.hierCol)
	c.Checkbox(ids.PrepareStr("gv-hier-centre"), st.hierCntr, "centre parent over children").SendRespVal(&st.hierCntr)
	c.Checkbox(ids.PrepareStr("gv-hier-lr"), st.hierLR, "orientation: left-right (else top-down)").SendRespVal(&st.hierLR)
	o := &st.hier.Opts
	prev := o.Hier
	o.Hier = graphview.HierParams{RowDist: float32(st.hierRow), ColDist: float32(st.hierCol), CenterParent: st.hierCntr}
	if st.hierLR {
		o.Hier.Orientation = graphview.OrientationLeftRight
	}
	if o.Hier != prev {
		// The static layout re-runs on topology change only; a parameter
		// change asks for it explicitly.
		st.hier.ResetLayout()
	}
	st.hier.Render(st.hierNodes, st.hierEdges, demoGraphviewWidth(ids, "gv-hier-pane"), 300)
}

func graphviewEventKindName(k graphview.EventKindE) string {
	switch k {
	case graphview.EventKindNodeClick:
		return "NodeClick"
	case graphview.EventKindNodeDoubleClick:
		return "NodeDoubleClick"
	case graphview.EventKindNodeSelect:
		return "NodeSelect"
	case graphview.EventKindNodeDeselect:
		return "NodeDeselect"
	case graphview.EventKindNodeDragStart:
		return "NodeDragStart"
	case graphview.EventKindNodeDragEnd:
		return "NodeDragEnd"
	case graphview.EventKindNodeHoverEnter:
		return "NodeHoverEnter"
	case graphview.EventKindNodeHoverLeave:
		return "NodeHoverLeave"
	case graphview.EventKindEdgeClick:
		return "EdgeClick"
	case graphview.EventKindEdgeSelect:
		return "EdgeSelect"
	case graphview.EventKindEdgeDeselect:
		return "EdgeDeselect"
	}
	return fmt.Sprintf("kind-%d", k)
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
			c.Label(fmt.Sprintf("  selected edge=%d→%d  graph=%s", k[0], k[1], e.name)).Send()
		}
		for _, ev := range e.v.Events() {
			if !st.showHover && (ev.Kind == graphview.EventKindNodeHoverEnter || ev.Kind == graphview.EventKindNodeHoverLeave) {
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
		c.Label("(click or drag a node or edge to see events)").Send()
		return
	}
	for _, ev := range st.eventLog {
		if ev.Kind.IsEdge() {
			c.Label(fmt.Sprintf("  %s  edge=%d→%d", graphviewEventKindName(ev.Kind), ev.From, ev.To)).Send()
		} else {
			c.Label(fmt.Sprintf("  %s  node=%d", graphviewEventKindName(ev.Kind), ev.Node)).Send()
		}
	}
}
