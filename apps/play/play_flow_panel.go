package play

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
)

// play_flow_panel.go is the ADR-0153 Flow dock tab: a clause-level dataflow
// graph of the ACTIVE node's SQL (play_flow_model.go), drawn with the
// layeredgraph widget. A tool pane like Docs — it reads the split, not the
// query result, so it registers with no PanelI and writes no signal. Selection
// is local (the ADR-0129 lesson: these nodes are clauses, not observable split
// nodes, and could not drive the node-scoped selection signal anyway).

// flowIDSalt namespaces the tab's canvas + sense-region + probe ids — distinct
// from vizIDSalt (System graph) and networkIDSalt so the three drawings never
// collide; per-instance idSeed (nextVizSeed) keeps two live PlayApps apart.
const flowIDSalt uint64 = 0xf10a11ce5c0ffee1

// flowDriver owns the Flow tab state: the memoised derivation (re-derived only
// when the active node's identity, SQL or sibling set changes), the cached
// layout (recomputed only on topology or rank-direction change), the pan/zoom
// view and the local selection. No client, no lanes — derivation is static —
// so there is nothing to Close.
type flowDriver struct {
	ids    *c.WidgetIdStack
	idSeed uint64

	rankDir layeredgraph.RankDir
	view    view.ViewState

	// selectedID highlights the last-clicked node and feeds the detail line.
	selectedID string

	memoKey  string
	graph    flowGraph
	graphErr error

	layout    *layeredgraph.Layout
	layoutKey string
	layoutErr error
}

func newFlowDriver(ids *c.WidgetIdStack) *flowDriver {
	// Left-right by default: a clause spine reads like a pipeline, and the
	// System graph made the same choice (ADR-0153 §SD3).
	return &flowDriver{ids: ids, idSeed: nextVizSeed(), rankDir: layeredgraph.RankDirLeftRight}
}

// flowMemoKey is the derivation memo identity: the node id (it names the
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

// ensure re-derives the graph when the memo key moves; a selection whose node
// vanished is dropped.
func (inst *flowDriver) ensure(node splitNode) {
	key := flowMemoKey(node)
	if key == inst.memoKey {
		return
	}
	inst.memoKey = key
	sibs := make(map[string]struct{}, len(node.DependsOn))
	for _, d := range node.DependsOn {
		sibs[string(d)] = struct{}{}
	}
	inst.graph, inst.graphErr = buildFlowGraph(node.SQL, sibs, string(node.ID))
	if inst.graphErr != nil {
		inst.graph = flowGraph{}
	}
	if inst.selectedID != "" {
		found := false
		for _, n := range inst.graph.Nodes {
			if n.ID == inst.selectedID {
				found = true
				break
			}
		}
		if !found {
			inst.selectedID = ""
		}
	}
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
	inst.flow.render(inst.currentSplit, active, inst.splitErr)
}

func (inst *flowDriver) render(split splitResult, active NodeID, splitErr error) {
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

	inst.renderControls(active)
	inst.ensure(node)
	if inst.graphErr != nil {
		for rt := range c.RichTextLabel("no flow graph: " + truncateRunes(firstLine(inst.graphErr.Error()), 140)) {
			rt.Small().Weak()
		}
		return
	}

	model := flowToModel(inst.graph)
	c.Label(inst.statusLine()).Send()
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
		NodeFill: inst.nodeFill,
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

	inst.renderDetailLine()
	for rt := range c.RichTextLabel("drag pans, ctrl+scroll zooms; click a node for its clause text") {
		rt.Small().Weak()
	}
}

// nodeFill paints the selected node with the accent, sources with a neutral
// tint (the inputs read as one band) and the result like the System graph's
// sink; stages keep the default body.
func (inst *flowDriver) nodeFill(id string) (col color.Color, ok bool) {
	if inst.selectedID != "" && id == inst.selectedID {
		return color.Hex(styletokens.AccentDefault.AsHex()), true
	}
	for _, n := range inst.graph.Nodes {
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

// renderControls draws the layout-direction toggle plus the active node badge.
func (inst *flowDriver) renderControls(active NodeID) {
	for range c.Horizontal().KeepIter() {
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

// renderDetailLine shows the selected node's clause text (ADR-0153 §SD4 — the
// snippet stands in for the deferred editor-range highlight).
func (inst *flowDriver) renderDetailLine() {
	if inst.selectedID == "" {
		return
	}
	for _, n := range inst.graph.Nodes {
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

func (inst *flowDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d nodes · %d edges", len(inst.graph.Nodes), len(inst.graph.Edges))
	if inst.graph.Capped {
		fmt.Fprintf(&b, " · capped (%d-node / depth-%d bound)", flowMaxNodes, flowMaxDepth)
	}
	return b.String()
}
