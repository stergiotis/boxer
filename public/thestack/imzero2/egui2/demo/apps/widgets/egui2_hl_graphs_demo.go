package widgets

import (
	"fmt"
	"time"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// graphsDemoState carries per-window state for the "graphs" umbrella,
// split into four sub-state structs that mirror the four sub-demos:
// the 5-node ring, the dynamic force-directed tree, the hierarchical
// tree, and the global navigation tunables + event log readout.
type graphsDemoState struct {
	basic   graphsBasicState
	dynamic graphsDynamicState
	hier    graphsHierState
	global  graphsGlobalState
}

type graphsBasicState struct {
	multiNode     bool
	multiEdge     bool
	fixedWidth    bool
	fixedWidthVal float64
	labelsAlways  bool
}

// graphsDynamicState — FR/FR+CG tunables plus one-shot flags consumed
// on the next frame so .ResetLayout() / .FastForwardSteps() fire once
// per click rather than every frame.
type graphsDynamicState struct {
	start              time.Time
	nodes              int
	parent             []uint64
	resetPending       bool
	fastForwardPending uint32
	useCenterGravity   bool
	dt                 float64
	damping            float64
	epsilon            float64
	maxStep            float64
	kScale             float64
	cAttract           float64
	cRepulse           float64
	running            bool
}

type graphsHierState struct {
	rowDist      float64
	colDist      float64
	centerParent bool
	leftRight    bool
}

// graphsGlobalState — controls shared by all three graphs (fit /
// padding / zoom speed) plus the cross-demo event log.
type graphsGlobalState struct {
	eventLog        []c.GraphEvent
	showHoverEvents bool
	fitToScreen     bool // continuous-fit override (off by default; the one-shot latch handles framing)
	fitNowPending   bool // set by the "fit now" button, consumed by every graph this frame
	fitPadding      float64
	zoomSpeed       float64
}

func newGraphsDemoState() (st *graphsDemoState) {
	st = &graphsDemoState{
		basic: graphsBasicState{
			fixedWidthVal: 480.0,
			labelsAlways:  true,
		},
		dynamic: graphsDynamicState{
			start:            time.Now(),
			useCenterGravity: true,
			dt:               0.05,
			damping:          0.3,
			epsilon:          0.1,
			maxStep:          20.0,
			kScale:           1.0,
			cAttract:         1.0,
			cRepulse:         1.0,
			running:          true,
		},
		hier: graphsHierState{
			rowDist:      60.0,
			colDist:      50.0,
			centerParent: true,
		},
		global: graphsGlobalState{
			fitToScreen: false, // one-shot fit by default; toggle on for legacy continuous fit
			fitPadding:  0.1,
			zoomSpeed:   0.1,
		},
	}
	return
}

// =============================================================================
// DEMO: Basic 5-node ring — LayoutRandom, coloured nodes AND edges, optional
// fixed-width to showcase the non-fill sizing path + multi-select toggles.
// =============================================================================

func demoGraphBasic(ids *c.WidgetIdStack, st *graphsDemoState) {
	bs := &st.basic
	gs := &st.global

	c.Checkbox(ids.PrepareStr("graph-basic-multinode"), bs.multiNode, "multi-select nodes").
		SendRespVal(&bs.multiNode)
	c.Checkbox(ids.PrepareStr("graph-basic-multiedge"), bs.multiEdge, "multi-select edges").
		SendRespVal(&bs.multiEdge)
	c.Checkbox(ids.PrepareStr("graph-basic-labels"), bs.labelsAlways, "labels always").
		SendRespVal(&bs.labelsAlways)
	c.Checkbox(ids.PrepareStr("graph-basic-fixwidth"), bs.fixedWidth, "fixed width (demo .Width())").
		SendRespVal(&bs.fixedWidth)
	if bs.fixedWidth {
		c.SliderF64(ids.PrepareStr("graph-basic-fixwidth-val"), bs.fixedWidthVal, 200.0, 1200.0).
			Text("fixed width px").SendRespVal(&bs.fixedWidthVal)
	}

	const n = 5
	// Migrated to IDS qualitative cycle (batlowS, 10 perceptually-uniform
	// colours per Crameri 2018). Edges offset by 5 (half-cycle) so they
	// land on the opposite side of the palette from nodes — keeps the
	// edge-vs-node distinction the old hardcoded palettes provided.
	for i := uint64(0); i < n; i++ {
		c.GraphNode(i+1, fmt.Sprintf("n%d", i+1)).
			Color(color.Hex(styletokens.QualitativeCycle(int(i)).AsHex())).Send()
	}
	for i := uint64(0); i < n; i++ {
		from := i + 1
		to := (i+1)%n + 1
		c.GraphEdge(from, to).
			Label(fmt.Sprintf("%d→%d", from, to)).
			Color(color.Hex(styletokens.QualitativeCycle(int(i) + 5).AsHex())).
			Send()
	}

	g := c.Graph(ids.PrepareStr("graph-basic")).
		Height(400).
		FitToScreen(gs.fitToScreen).
		FitPadding(float32(gs.fitPadding)).
		ZoomSpeed(float32(gs.zoomSpeed)).
		ZoomAndPan(true).
		DraggingEnabled(true).
		HoverEnabled(true).
		NodeClickingEnabled(true).
		NodeSelectionEnabled(true).
		NodeSelectionMultiEnabled(bs.multiNode).
		EdgeClickingEnabled(true).
		EdgeSelectionEnabled(true).
		EdgeSelectionMultiEnabled(bs.multiEdge).
		LabelsAlways(bs.labelsAlways)
	if bs.fixedWidth {
		g = g.Width(float32(bs.fixedWidthVal))
	}
	if gs.fitNowPending {
		g = g.FitNow()
	}
	g.Send()
}

// =============================================================================
// DEMO: Dynamic force-directed tree — user picks FR or FR+CG variant and
// tunes all seven FR simulation parameters.
// =============================================================================

func demoGraphDynamic(ids *c.WidgetIdStack, st *graphsDemoState) {
	ds := &st.dynamic
	gs := &st.global

	c.Checkbox(ids.PrepareStr("graph-dynamic-cg"), ds.useCenterGravity, "use center gravity (FR+CG)").
		SendRespVal(&ds.useCenterGravity)
	layout := uint8(c.GraphLayoutForceDirected)
	if ds.useCenterGravity {
		layout = uint8(c.GraphLayoutForceDirectedCG)
	}

	c.SliderF64(ids.PrepareStr("graph-dynamic-dt"), ds.dt, 0.01, 0.5).
		Text("FR dt").SendRespVal(&ds.dt)
	c.SliderF64(ids.PrepareStr("graph-dynamic-damping"), ds.damping, 0.01, 1.0).
		Text("FR damping").SendRespVal(&ds.damping)
	c.SliderF64(ids.PrepareStr("graph-dynamic-epsilon"), ds.epsilon, 0.001, 1.0).
		Text("FR epsilon").SendRespVal(&ds.epsilon)
	c.SliderF64(ids.PrepareStr("graph-dynamic-maxstep"), ds.maxStep, 1.0, 100.0).
		Text("FR max_step").SendRespVal(&ds.maxStep)
	c.SliderF64(ids.PrepareStr("graph-dynamic-kscale"), ds.kScale, 0.1, 5.0).
		Text("FR k_scale").SendRespVal(&ds.kScale)
	c.SliderF64(ids.PrepareStr("graph-dynamic-cattract"), ds.cAttract, 0.1, 5.0).
		Text("FR c_attract").SendRespVal(&ds.cAttract)
	c.SliderF64(ids.PrepareStr("graph-dynamic-crepulse"), ds.cRepulse, 0.1, 5.0).
		Text("FR c_repulse").SendRespVal(&ds.cRepulse)
	c.Checkbox(ids.PrepareStr("graph-dynamic-running"), ds.running, "FR is_running").
		SendRespVal(&ds.running)

	resetAtoms := c.Atoms().Text("reset layout").Keep()
	ffAtoms := c.Atoms().Text("fast-forward 200 steps").Keep()
	if c.Button(ids.PrepareStr("graph-dynamic-reset"), resetAtoms).SendResp().HasPrimaryClicked() {
		ds.resetPending = true
	}
	if c.Button(ids.PrepareStr("graph-dynamic-fastforward"), ffAtoms).SendResp().HasPrimaryClicked() {
		ds.fastForwardPending = 200
	}

	// Grow the chain by one node every ~1.5s, up to 12 nodes, then pause.
	target := int(time.Since(ds.start).Seconds()/1.5) + 1
	if target > 12 {
		target = 12
	}
	if target > ds.nodes {
		ds.nodes = target
		ds.parent = ds.parent[:0]
		for i := uint64(1); i < uint64(ds.nodes); i++ {
			parent := (i - 1) / 2
			ds.parent = append(ds.parent, parent+1)
		}
	}

	for i := uint64(0); i < uint64(ds.nodes); i++ {
		c.GraphNode(i+1, fmt.Sprintf("#%d", i+1)).Color(color.Hex(styletokens.InfoDefault.AsHex())).Send()
	}
	for i, parent := range ds.parent {
		c.GraphEdge(parent, uint64(i+2)).Send()
	}

	g := c.Graph(ids.PrepareStr("graph-dynamic")).
		Height(360).
		FitToScreen(gs.fitToScreen).
		FitPadding(float32(gs.fitPadding)).
		ZoomSpeed(float32(gs.zoomSpeed)).
		Layout(layout).
		LayoutDt(float32(ds.dt)).
		LayoutDamping(float32(ds.damping)).
		LayoutEpsilon(float32(ds.epsilon)).
		LayoutMaxStep(float32(ds.maxStep)).
		LayoutKScale(float32(ds.kScale)).
		LayoutCAttract(float32(ds.cAttract)).
		LayoutCRepulse(float32(ds.cRepulse)).
		LayoutRunning(ds.running).
		ZoomAndPan(true).
		DraggingEnabled(true).
		HoverEnabled(true).
		NodeClickingEnabled(true).
		NodeSelectionEnabled(true).
		EdgeClickingEnabled(true).
		LabelsAlways(true)
	if ds.resetPending {
		g = g.ResetLayout()
		ds.resetPending = false
	}
	if ds.fastForwardPending > 0 {
		g = g.FastForwardSteps(ds.fastForwardPending)
		ds.fastForwardPending = 0
	}
	if gs.fitNowPending {
		g = g.FitNow()
	}
	g.Send()
}

// =============================================================================
// DEMO: Hierarchical layout — same binary tree data, different geometry.
// Covers LayoutRowDist, LayoutColDist, LayoutCenterParent, LayoutOrientation.
// =============================================================================

func demoGraphHierarchical(ids *c.WidgetIdStack, st *graphsDemoState) {
	hs := &st.hier
	gs := &st.global

	c.SliderF64(ids.PrepareStr("graph-hier-row"), hs.rowDist, 10.0, 200.0).
		Text("row_dist (level spacing)").SendRespVal(&hs.rowDist)
	c.SliderF64(ids.PrepareStr("graph-hier-col"), hs.colDist, 10.0, 200.0).
		Text("col_dist (sibling spacing)").SendRespVal(&hs.colDist)
	c.Checkbox(ids.PrepareStr("graph-hier-center"), hs.centerParent, "center parent above children").
		SendRespVal(&hs.centerParent)
	c.Checkbox(ids.PrepareStr("graph-hier-lr"), hs.leftRight, "orientation: LeftRight (else TopDown)").
		SendRespVal(&hs.leftRight)
	orientation := uint8(c.GraphHierarchicalOrientationTopDown)
	if hs.leftRight {
		orientation = uint8(c.GraphHierarchicalOrientationLeftRight)
	}

	// A small 10-node binary tree — enough to see the hierarchy.
	const n = 10
	for i := uint64(1); i <= n; i++ {
		c.GraphNode(i, fmt.Sprintf("h%d", i)).Color(color.Hex(styletokens.AccentDefault.AsHex())).Send()
	}
	for i := uint64(2); i <= n; i++ {
		parent := (i-1)/2 + 1
		c.GraphEdge(parent, i).Send()
	}

	g := c.Graph(ids.PrepareStr("graph-hierarchical")).
		Height(300).
		FitToScreen(gs.fitToScreen).
		FitPadding(float32(gs.fitPadding)).
		ZoomSpeed(float32(gs.zoomSpeed)).
		Layout(uint8(c.GraphLayoutHierarchical)).
		LayoutRowDist(float32(hs.rowDist)).
		LayoutColDist(float32(hs.colDist)).
		LayoutCenterParent(hs.centerParent).
		LayoutOrientation(orientation).
		ZoomAndPan(true).
		DraggingEnabled(true).
		HoverEnabled(true).
		NodeClickingEnabled(true).
		NodeSelectionEnabled(true).
		EdgeClickingEnabled(true).
		LabelsAlways(true)
	if gs.fitNowPending {
		g = g.FitNow()
	}
	g.Send()
}

// =============================================================================
// DEMO: Event log — drains interaction events, shows selection + metrics,
// global navigation tunables, and an optional hover-events view.
// =============================================================================

func graphEventKindName(k c.GraphEventKindE) string {
	switch k {
	case c.GraphEventKindNodeClick:
		return "NodeClick"
	case c.GraphEventKindNodeDoubleClick:
		return "NodeDoubleClick"
	case c.GraphEventKindNodeSelect:
		return "NodeSelect"
	case c.GraphEventKindNodeDeselect:
		return "NodeDeselect"
	case c.GraphEventKindNodeDragStart:
		return "NodeDragStart"
	case c.GraphEventKindNodeDragEnd:
		return "NodeDragEnd"
	case c.GraphEventKindNodeHoverEnter:
		return "NodeHoverEnter"
	case c.GraphEventKindNodeHoverLeave:
		return "NodeHoverLeave"
	case c.GraphEventKindEdgeClick:
		return "EdgeClick"
	case c.GraphEventKindEdgeSelect:
		return "EdgeSelect"
	case c.GraphEventKindEdgeDeselect:
		return "EdgeDeselect"
	}
	return fmt.Sprintf("kind-%d", k)
}

// demoGraphGlobalNavControls is rendered BEFORE each graph section so the
// user can see that FitToScreen / FitPadding / ZoomSpeed apply to every
// graph that uses them.
func demoGraphGlobalNavControls(ids *c.WidgetIdStack, st *graphsDemoState) {
	gs := &st.global
	// One-shot fit: re-frames every graph once, then lets the layout
	// settle and leaves manual pan/zoom alone. Consumed below by each
	// graph's .FitNow() and cleared in demoGraphEventLog (rendered last).
	fitAtoms := c.Atoms().Text("fit now (all graphs)").Keep()
	if c.Button(ids.PrepareStr("graph-fit-now"), fitAtoms).SendResp().HasPrimaryClicked() {
		gs.fitNowPending = true
	}
	c.Checkbox(ids.PrepareStr("graph-fit-to-screen"), gs.fitToScreen, "continuous fit (override, all graphs)").
		SendRespVal(&gs.fitToScreen)
	c.SliderF64(ids.PrepareStr("graph-fit-padding"), gs.fitPadding, 0.0, 0.5).
		Text("fit_padding (all graphs)").SendRespVal(&gs.fitPadding)
	c.SliderF64(ids.PrepareStr("graph-zoom-speed"), gs.zoomSpeed, 0.01, 1.0).
		Text("zoom_speed (all graphs)").SendRespVal(&gs.zoomSpeed)
}

func demoGraphEventLog(ids *c.WidgetIdStack, st *graphsDemoState) {
	gs := &st.global

	// This section renders after every graph, so a one-shot "fit now"
	// request set this frame has been consumed by all of them by now.
	gs.fitNowPending = false

	c.Separator().Send()

	c.Checkbox(ids.PrepareStr("graph-show-hover"), gs.showHoverEvents, "show hover enter/leave events").
		SendRespVal(&gs.showHoverEvents)

	// Per-graph metrics — snapshot from the previous frame's render.
	c.Separator().Send()
	metrics := c.FetchGraphMetrics()
	c.Label(fmt.Sprintf("metrics: %d graph(s) rendered", len(metrics))).Send()
	for _, m := range metrics {
		line := fmt.Sprintf("  graph=%d  nodes=%d  edges=%d  frSteps=%d  frDisp=%.4f",
			m.GraphId, m.NodeCount, m.EdgeCount, m.FrSteps, m.FrLastDisplacement)
		c.Label(line).Send()
	}

	// Current selection snapshot.
	sel := c.FetchGraphSelection()
	c.Label(fmt.Sprintf("selection: %d item(s)", len(sel))).Send()
	for _, it := range sel {
		if it.IsNode {
			c.Label(fmt.Sprintf("  node=%d  graph=%d", it.KeyA, it.GraphId)).Send()
		} else {
			c.Label(fmt.Sprintf("  edge=%d→%d  graph=%d", it.KeyA, it.KeyB, it.GraphId)).Send()
		}
	}

	events := c.FetchGraphEvents()
	for _, e := range events {
		if !gs.showHoverEvents &&
			(e.Kind == c.GraphEventKindNodeHoverEnter || e.Kind == c.GraphEventKindNodeHoverLeave) {
			continue
		}
		gs.eventLog = append(gs.eventLog, e)
	}
	const keep = 10
	if len(gs.eventLog) > keep {
		gs.eventLog = gs.eventLog[len(gs.eventLog)-keep:]
	}

	c.Separator().Send()
	if gs.showHoverEvents {
		c.Label(fmt.Sprintf("recent events (last %d, hover included):", keep)).Send()
	} else {
		c.Label(fmt.Sprintf("recent events (last %d, hover filtered):", keep)).Send()
	}
	if len(gs.eventLog) == 0 {
		c.Label("(click or drag a node/edge to see events)").Send()
		return
	}
	for _, e := range gs.eventLog {
		var line string
		if e.IsEdge() {
			line = fmt.Sprintf("  %s  edge=%d→%d  graph=%d",
				graphEventKindName(e.Kind), e.KeyA, e.KeyB, e.GraphId)
		} else {
			line = fmt.Sprintf("  %s  node=%d  graph=%d",
				graphEventKindName(e.Kind), e.KeyA, e.GraphId)
		}
		c.Label(line).Send()
	}
}
