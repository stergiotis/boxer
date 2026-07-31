package play

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// play_flow_panel.go is the ADR-0153 Flow dock tab: a dataflow graph of the
// ACTIVE node's SQL, drawn with the layeredgraph widget. The graph comes from
// the selected LENS — the static clause-level derivation (play_flow_model.go,
// the default), or one of the ClickHouse EXPLAIN lenses (play_flow_lens.go),
// each on its own lane. A tool pane like Docs: no PanelI, no signal writes.
// Selection is local (the ADR-0129 lesson); on the statement lens it also
// tints the clicked clause's bytes in the editor (flowSelectionSection).

// flowIDSalt namespaces the tab's canvas + sense-region + probe ids — distinct
// from vizIDSalt (System graph) and networkIDSalt so the three drawings never
// collide; per-instance idSeed (nextVizSeed) keeps two live PlayApps apart.
const flowIDSalt uint64 = 0xf10a11ce5c0ffee1

// flowDriver owns the Flow tab state: per-lens derivation (the statement
// lens memoised on the node's identity/SQL/deps, the EXPLAIN lenses parsed
// once per served lane result), the cached layout (recomputed only on
// topology or rank-direction change), the pan/zoom view and the local
// selection.
type flowDriver struct {
	ids    *c.WidgetIdStack
	idSeed uint64

	lens    flowLens
	rankDir layeredgraph.RankDir
	view    view.ViewState

	// selectedID highlights the last-clicked node, feeds the detail line,
	// and — on the statement lens — the editor tint.
	selectedID string

	// Statement lens: the derivation memo and the node it derived from
	// (activeNode.SrcOff anchors the editor highlight).
	memoKey    string
	activeNode splitNode
	graph      flowGraph
	graphErr   error

	// EXPLAIN lenses: one lane per lens (nil map for an unwired host —
	// tests), the parse memoised on the lane's served key. lensShown tracks
	// which lens the parsed graph belongs to, so switching lenses clears the
	// display instead of showing the previous lens's graph while the new one
	// loads.
	lanes        map[flowLens]*nodeLane
	lensShown    flowLens
	lensKey      string
	lensGraph    flowGraph
	lensParseErr error

	layout    *layeredgraph.Layout
	layoutKey string
	layoutErr error
}

// newFlowDriver builds the driver. client may be nil (tests, an unwired
// host): the EXPLAIN lanes are then absent and the remote lenses show their
// empty-state.
func newFlowDriver(ids *c.WidgetIdStack, client *Client) (inst *flowDriver) {
	// Left-right by default: a clause spine reads like a pipeline, and the
	// System graph made the same choice (ADR-0153 §SD3).
	inst = &flowDriver{ids: ids, idSeed: nextVizSeed(), rankDir: layeredgraph.RankDirLeftRight}
	if client != nil {
		inst.lanes = map[flowLens]*nodeLane{
			lensAST:      newNodeLane(clientExecutor{client: client, opts: newExecOptions("flow-ast")}, memory.NewGoAllocator(), 0),
			lensPlan:     newNodeLane(clientExecutor{client: client, opts: newExecOptions("flow-plan")}, memory.NewGoAllocator(), 0),
			lensPipeline: newNodeLane(clientExecutor{client: client, opts: newExecOptions("flow-pipeline")}, memory.NewGoAllocator(), 0),
		}
	}
	return
}

// lensLane returns the current lens's lane (nil for the statement lens or an
// unwired host).
func (inst *flowDriver) lensLane() *nodeLane {
	if inst == nil || inst.lanes == nil {
		return nil
	}
	return inst.lanes[inst.lens]
}

// forgetLanes clears the EXPLAIN lane memos so the next demand re-executes —
// the Run hook, for the same reason as the Network's (a re-Run after a
// transient failure memo-hits the stored error otherwise).
func (inst *flowDriver) forgetLanes() {
	if inst == nil {
		return
	}
	for _, l := range inst.lanes {
		l.forget()
	}
}

// closeLanes closes the EXPLAIN lanes (the app Close hook).
func (inst *flowDriver) closeLanes() {
	if inst == nil {
		return
	}
	for _, l := range inst.lanes {
		l.close()
	}
}

// flowMemoKey is the statement-lens memo identity: the node id (it names the
// result), its SQL, and the sorted sibling set — deleting a sibling CTE
// re-classifies a source without changing the node's own SQL, so the deps are
// part of the key.
func flowMemoKey(node splitNode) string {
	deps := make([]string, len(node.DependsOn))
	for i, d := range node.DependsOn {
		deps[i] = string(d)
	}
	sort.Strings(deps)
	return string(node.ID) + "\x00" + node.SQL + "\x00" + strings.Join(deps, "\x01")
}

// ensure re-derives the statement-lens graph when the memo key moves; a
// selection whose node vanished is dropped.
func (inst *flowDriver) ensure(node splitNode) {
	key := flowMemoKey(node)
	if key == inst.memoKey {
		return
	}
	inst.memoKey = key
	inst.activeNode = node
	sibs := make(map[string]struct{}, len(node.DependsOn))
	for _, d := range node.DependsOn {
		sibs[string(d)] = struct{}{}
	}
	inst.graph, inst.graphErr = buildFlowGraph(node.SQL, sibs, string(node.ID))
	if inst.graphErr != nil {
		inst.graph = flowGraph{}
	}
	inst.pruneSelection(inst.graph)
}

// syncLens clears the shown lens graph — and the selection — when the
// selected lens changed: the parse memo is per served key, but a key from
// another lens's lane must not keep rendering under the new selection, and a
// node id from another lens's graph must not linger invisibly (ids do not
// carry across lenses).
func (inst *flowDriver) syncLens() {
	if inst.lensShown == inst.lens {
		return
	}
	inst.lensShown = inst.lens
	inst.lensKey = ""
	inst.lensGraph = flowGraph{}
	inst.lensParseErr = nil
	inst.selectedID = ""
}

// ensureLensGraph parses a lens lane's served lines once per served key; a
// selection whose node vanished is dropped.
func (inst *flowDriver) ensureLensGraph(key string, lines []string) {
	if key == "" || key == inst.lensKey {
		return
	}
	inst.lensKey = key
	inst.lensGraph, inst.lensParseErr = parseLensRecord(inst.lens, lines)
	if inst.lensParseErr != nil {
		inst.lensGraph = flowGraph{}
	}
	inst.pruneSelection(inst.lensGraph)
}

// pruneSelection drops a selection that does not exist in the graph now shown.
func (inst *flowDriver) pruneSelection(g flowGraph) {
	if inst.selectedID == "" {
		return
	}
	for _, n := range g.Nodes {
		if n.ID == inst.selectedID {
			return
		}
	}
	inst.selectedID = ""
}

// statementSelection returns the clicked node and the split node the
// statement-lens graph derived from — the editor-highlight source. Absent for
// remote lenses (their nodes carry no source ranges).
func (inst *flowDriver) statementSelection() (n flowNode, node splitNode, ok bool) {
	if inst == nil || inst.lens != lensStatement || inst.selectedID == "" || inst.graphErr != nil {
		return
	}
	for _, fn := range inst.graph.Nodes {
		if fn.ID == inst.selectedID {
			return fn, inst.activeNode, true
		}
	}
	return
}

// flowToModel projects the IR onto the widget model: boxes for sources and
// stages, ellipses for the union and result nodes (ADR-0153 §SD3).
func flowToModel(g flowGraph) layeredgraph.GraphModel {
	nodes := make([]layeredgraph.Node, len(g.Nodes))
	for i, n := range g.Nodes {
		shape := layeredgraph.NodeShapeBox
		if n.Kind == flowUnion || n.Kind == flowResult {
			shape = layeredgraph.NodeShapeEllipse
		}
		nodes[i] = layeredgraph.Node{ID: n.ID, Label: n.Label, Shape: shape}
	}
	edges := make([]layeredgraph.Edge, len(g.Edges))
	for i, e := range g.Edges {
		edges[i] = layeredgraph.Edge{From: e.From, To: e.To, Label: e.Label}
	}
	return layeredgraph.GraphModel{Nodes: nodes, Edges: edges}
}

// renderFlowTab is the Flow dock tab body (ADR-0153). The active node follows
// the observe gesture; when nothing is observed the plain mainNodeID may miss
// a disambiguated sink id ("main (sink)"), so the miss resolves to the split's
// sink before giving up.
func (inst *PlayApp) renderFlowTab() {
	active := inst.activeNodeID()
	if _, ok := findSplitNode(inst.currentSplit, active); !ok && active == mainNodeID {
		active = inst.currentSplit.Sink
	}
	d := inst.flow
	d.renderControls(active)
	d.syncLens()
	if d.lens.remote() {
		lines, feed := inst.demandFlowLens(active)
		d.renderLens(lines, feed)
		return
	}
	d.renderStatement(inst.currentSplit, active, inst.splitErr)
}

// flowLensFeed is what the app-side demand hands the driver about a remote
// lens's lane state this frame.
type flowLensFeed struct {
	reason  string // non-empty: why the lens cannot run (no endpoint, no node)
	loading bool
	err     error
	key     string
}

// demandFlowLens compiles `SELECT * FROM (EXPLAIN … <fused node>)` and demands
// it on the current lens's lane. The node's signal reads resolve like any
// other node's, so a `{p:Type}` slot inside the explained statement rides the
// URL — verified server-side substitution (play_flow_lens.go). Returns the
// result lines (the `explain` column's rows) plus the lane state.
func (inst *PlayApp) demandFlowLens(active NodeID) (lines []string, feed flowLensFeed) {
	d := inst.flow
	lane := d.lensLane()
	if lane == nil {
		feed.reason = "EXPLAIN lenses need a connected endpoint."
		return
	}
	node, ok := findSplitNode(inst.currentSplit, active)
	if !ok {
		feed.reason = "Run a query first — the lens explains the active node's SQL."
		return
	}
	v := lane.demand(compiledNode{
		SQL:    wrapExplain(d.lens, fuseNode(inst.currentSplit, active)),
		Params: resolveSignalNames(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	feed.loading = v.loading
	feed.err = v.err
	feed.key = v.key
	if v.rec != nil {
		defer v.rec.Release()
		rows := v.rec.NumRows()
		lines = make([]string, 0, rows)
		for row := range rows {
			lines = append(lines, formatCell(v.rec, 0, row))
		}
	}
	return
}

// renderStatement is the static lens's body: the split-derived clause graph.
func (inst *flowDriver) renderStatement(split splitResult, active NodeID, splitErr error) {
	node, ok := findSplitNode(split, active)
	if !ok {
		msg := "Run a query to see its dataflow."
		if splitErr != nil {
			msg = "The buffer did not split: " + truncateRunes(firstLine(splitErr.Error()), 120)
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}
	inst.ensure(node)
	if inst.graphErr != nil {
		for rt := range c.RichTextLabel("no flow graph: " + truncateRunes(firstLine(inst.graphErr.Error()), 140)) {
			rt.Small().Weak()
		}
		return
	}
	inst.renderGraph(inst.graph)
}

// renderLens is a remote lens's body: the parsed EXPLAIN graph, with the
// lane's loading/error state. The last-parsed graph stays visible through a
// reload or an error — the lane holds last-good the same way.
func (inst *flowDriver) renderLens(lines []string, feed flowLensFeed) {
	if feed.reason != "" {
		for rt := range c.RichTextLabel(feed.reason) {
			rt.Small().Weak()
		}
		return
	}
	if feed.err != nil {
		for rt := range c.RichTextLabel("EXPLAIN failed: " + truncateRunes(firstLine(feed.err.Error()), 140)) {
			rt.Small().Weak()
		}
	}
	if lines != nil {
		inst.ensureLensGraph(feed.key, lines)
	}
	if inst.lensParseErr != nil {
		for rt := range c.RichTextLabel("EXPLAIN output did not parse: " + truncateRunes(firstLine(inst.lensParseErr.Error()), 140)) {
			rt.Small().Weak()
		}
		return
	}
	if len(inst.lensGraph.Nodes) == 0 {
		if feed.loading {
			for rt := range c.RichTextLabel("asking the server…") {
				rt.Small().Weak()
			}
		}
		return
	}
	inst.renderGraph(inst.lensGraph)
}

// renderGraph lays out (cached) and draws one graph, tracks the local
// selection and shows the detail line — shared by every lens.
func (inst *flowDriver) renderGraph(g flowGraph) {
	model := flowToModel(g)
	c.Label(inst.statusLine(g)).Send()
	if len(model.Nodes) == 0 {
		return
	}

	// Layout cached on the topology fingerprint (+ rank direction) — a click
	// changes only the highlight, never the layout (the play_graph_viz.go /
	// Network idiom; networkModelKey reused, same package).
	key := networkModelKey(model, inst.rankDir)
	if key != inst.layoutKey || (inst.layout == nil && inst.layoutErr == nil) {
		inst.layoutKey = key
		inst.layout = nil
		eng, err := goccyengine.Shared()
		if err == nil {
			inst.layout, err = eng.Layout(context.Background(), model,
				layeredgraph.LayoutOpts{RankDir: inst.rankDir, FontSize: 13})
		}
		inst.layoutErr = err
	}
	if inst.layout == nil {
		msg := "flow layout unavailable (layout engine)"
		if inst.layoutErr != nil {
			msg += ": " + truncateRunes(firstLine(inst.layoutErr.Error()), 80)
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}

	lw, lh := inst.layout.Width, inst.layout.Height
	if lw <= 0 || lh <= 0 {
		return
	}
	// Fill the pane width via the separator + Seq-keyed UiRect probe (the
	// passes/Network idiom — per-seq R21 slot, never CaptureAvailableSize:
	// that single register is the Editor tab's). One-frame lag; the first
	// frame falls back to a conservative width.
	sm := c.CurrentApplicationState.StateManager
	c.Separator().Horizontal().Send()
	probeSeq := flowIDSalt ^ inst.idSeed ^ 0x1
	c.CaptureUiRect(probeSeq)
	paneW := float32(760)
	if r, ok := sm.GetUiRect(probeSeq); ok && r.MaxX > r.MinX {
		paneW = r.MaxX - r.MinX
	}
	w := min(max(paneW-12, 360), 1600)
	h := min(max(w*float32(lh/lw), 200), 720)

	res := view.Render(flowIDSalt+inst.idSeed, inst.layout, view.RenderOpts{
		Style:    view.DefaultStyle(),
		CanvasW:  w,
		CanvasH:  h,
		NodeFill: func(id string) (color.Color, bool) { return inst.nodeFill(g, id) },
		State:    &inst.view,
	})
	// A click toggles the LOCAL highlight (no shared-signal emit — ADR-0153
	// §SD4); clicking the highlighted node again clears it.
	if res.Clicked != "" {
		if inst.selectedID == res.Clicked {
			inst.selectedID = ""
		} else {
			inst.selectedID = res.Clicked
		}
	}

	inst.renderDetailLine(g)
	hint := "drag pans, ctrl+scroll zooms; click a node for its clause text"
	if inst.lens.remote() {
		hint = "drag pans, ctrl+scroll zooms; click a node for its step text"
	}
	for rt := range c.RichTextLabel(hint) {
		rt.Small().Weak()
	}
}

// nodeFill paints the selected node with the accent, sources with a neutral
// tint (the inputs read as one band) and the result like the System graph's
// sink; stages keep the default body.
func (inst *flowDriver) nodeFill(g flowGraph, id string) (col color.Color, ok bool) {
	if inst.selectedID != "" && id == inst.selectedID {
		return color.Hex(styletokens.AccentDefault.AsHex()), true
	}
	for _, n := range g.Nodes {
		if n.ID != id {
			continue
		}
		switch {
		case n.Kind.isSource():
			return color.Hex(styletokens.NeutralSubtle.AsHex()), true
		case n.Kind == flowResult:
			return color.Hex(styletokens.NeutralBgExtreme.AsHex()), true
		}
		break
	}
	return
}

// renderControls draws the lens selector, the layout-direction toggle and the
// active node badge.
func (inst *flowDriver) renderControls(active NodeID) {
	for range c.Horizontal().KeepIter() {
		c.Label("lens").Send()
		selector.Segmented(inst.ids, "flow-lens", &inst.lens).
			Inline().
			Frameless().
			Option(lensStatement, "statement").
			Option(lensAST, "ast").
			Option(lensPlan, "plan").
			Option(lensPipeline, "pipeline").
			SendResp()
		c.Label("layout").Send()
		selector.Segmented(inst.ids, "flow-rank-dir", &inst.rankDir).
			Inline().
			Frameless().
			Option(layeredgraph.RankDirLeftRight, "left-right").
			Option(layeredgraph.RankDirTopBottom, "top-down").
			SendResp()
		for rt := range c.RichTextLabel("node: " + string(active)) {
			rt.Small().Weak()
		}
	}
}

// renderDetailLine shows the selected node's clause/step text (ADR-0153 §SD4
// — on the statement lens the same selection also tints the editor bytes, see
// flowSelectionSection).
func (inst *flowDriver) renderDetailLine(g flowGraph) {
	if inst.selectedID == "" {
		return
	}
	for _, n := range g.Nodes {
		if n.ID != inst.selectedID {
			continue
		}
		text := n.Label
		if n.Detail != "" {
			text += " — " + n.Detail
		}
		for rt := range c.RichTextLabel(text) {
			rt.Small()
		}
		return
	}
}

func (inst *flowDriver) statusLine(g flowGraph) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d nodes · %d edges", len(g.Nodes), len(g.Edges))
	if g.Capped {
		fmt.Fprintf(&b, " · capped (%d-node / depth-%d bound)", flowMaxNodes, flowMaxDepth)
	}
	return b.String()
}
