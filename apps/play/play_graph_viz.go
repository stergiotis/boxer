package play

import (
	"context"
	"fmt"
	"hash/fnv"
	"sort"
	"strings"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
)

// play_graph_viz.go is the Graph tab's system drawing: the whole reactive
// surface — prelude constants, live signals, query nodes (split CTEs, the
// sink, and the panel-authored bands/raster nodes), and the panel tabs — as
// one layered graph (the layeredgraph widget, ADR-0069), dataflow
// left-to-right with the panel→signal write-backs looping visibly back. It
// draws only what the runtime already knows: the split's read/depends edges,
// the tab resolutions (bindings, 6c), and the signal store's provenance
// (last writer, 5e). The MODEL is rebuilt per frame (cheap, pure); the
// LAYOUT — a Graphviz run in in-process WASM — is cached and recomputed only
// when the topology fingerprint changes (a Run, a bind, a first write to a
// new signal). Node labels deliberately carry no live values, so value churn
// (a selection click) never relayouts; values live in the Signals section.

// vizKindE tags a drawn node for the per-frame colour hooks.
type vizKindE uint8

const (
	vizConst     vizKindE = iota // a SET-pinned prelude constant (D1)
	vizSignal                    // a live signal (held, or referenced and filled)
	vizUnfilled                  // referenced by a query, neither bound nor held (D3)
	vizQuery                     // a split node (CTE)
	vizSink                      // the split's sink statement
	vizPanelNode                 // a panel-authored node (bands, map raster)
	vizTab                       // a result-consuming tab
)

// Node-id namespaces. The flat id space must stay collision-free, and the
// prefixes double as the click/colour dispatch.
func vizConstID(name string) string { return "const/" + name }
func vizSigID(name string) string   { return "sig/" + name }
func vizNodeID(id NodeID) string    { return "node/" + string(id) }
func vizPNodeID(name string) string { return "pnode/" + name }
func vizTabID(tabID string) string  { return "tab/" + tabID }

// buildSystemGraphModel assembles the drawing's model + kind map from this
// frame's state. Deterministic (sorted) so the fingerprint is stable.
func (inst *PlayApp) buildSystemGraphModel() (m layeredgraph.GraphModel, meta map[string]vizKindE) {
	meta = make(map[string]vizKindE, 32)
	nodes := make(map[string]layeredgraph.Node, 32)
	type edgeKey struct{ from, to string }
	edges := make(map[edgeKey]layeredgraph.Edge, 48)
	addNode := func(id, label string, shape layeredgraph.NodeShape, kind vizKindE) {
		if _, dup := nodes[id]; dup {
			return
		}
		nodes[id] = layeredgraph.Node{ID: id, Label: label, Shape: shape}
		meta[id] = kind
	}
	addEdge := func(from, to, label string) {
		k := edgeKey{from, to}
		if _, dup := edges[k]; dup {
			return
		}
		edges[k] = layeredgraph.Edge{From: from, To: to, Label: label}
	}

	// Query nodes: the split's, or a synthesized `main` before any split —
	// the degenerate single-node graph the panels observe either way.
	split := inst.currentSplit
	qnodes := split.Nodes
	if len(qnodes) == 0 {
		qnodes = []splitNode{{ID: mainNodeID, Kind: splitNodeStatement}}
	}
	bound := inst.paramSyncedValues
	included := make(map[NodeID]bool, len(qnodes))
	for i := range qnodes {
		n := &qnodes[i]
		included[n.ID] = true
		kind := vizQuery
		label := string(n.ID)
		if n.Kind == splitNodeStatement {
			kind = vizSink
			label = string(n.ID) + " · sink"
		}
		addNode(vizNodeID(n.ID), label, layeredgraph.NodeShapeBox, kind)
	}
	for i := range qnodes {
		n := &qnodes[i]
		for _, dep := range n.DependsOn {
			if included[dep] {
				addEdge(vizNodeID(dep), vizNodeID(n.ID), "")
			}
		}
		for _, r := range n.Reads {
			if _, isBound := bound[r]; isBound {
				addNode(vizConstID(r), "SET "+r, layeredgraph.NodeShapeBox, vizConst)
				addEdge(vizConstID(r), vizNodeID(n.ID), "")
				continue
			}
			addNode(vizSigID(r), r, layeredgraph.NodeShapeEllipse, vizSignal)
			addEdge(vizSigID(r), vizNodeID(n.ID), "")
		}
	}

	// Result-consuming tabs (the PanelI-bearing specs), fed by their
	// resolved node (a 6c binding, the observe override, or the sink).
	tabIDs := make(map[string]bool, 8)
	hasTimeline := false
	for _, spec := range inst.tabs.all() {
		if spec.Panel == nil {
			continue
		}
		tabIDs[spec.ID] = true
		if spec.ID == "timeline" {
			hasTimeline = true
		}
		addNode(vizTabID(spec.ID), spec.Title, layeredgraph.NodeShapeBox, vizTab)
		feed := inst.resolvedTabNode(spec.ID)
		if !included[feed] {
			feed = qnodes[len(qnodes)-1].ID // degraded: fall back to the last (sink) node
		}
		label := ""
		if spec.ID == "timeline" {
			label = "events"
		}
		addEdge(vizNodeID(feed), vizTabID(spec.ID), label)
	}

	// The Timeline's bands node — panel-authored, on its own lane (4b/5d).
	if hasTimeline && inst.timeline != nil && strings.TrimSpace(inst.timelineBandsSql) != "" {
		addNode(vizPNodeID("bands"), "bands", layeredgraph.NodeShapeBox, vizPanelNode)
		addEdge(vizPNodeID("bands"), vizTabID("timeline"), "bands")
		for _, name := range inst.timeline.bandsSlotNames {
			if inst.timeline.bandsBound[name] {
				continue // bands-prelude constant — internal to the bands editor
			}
			addNode(vizSigID(name), name, layeredgraph.NodeShapeEllipse, vizSignal)
			addEdge(vizSigID(name), vizPNodeID("bands"), "")
		}
	}

	// Held signals + provenance write-backs. The Map's raster node appears
	// once its viewport signals exist (the settled camera has written).
	heldVP := false
	rows := inst.graph.signalRows()
	for _, r := range rows {
		addNode(vizSigID(r.Name), r.Name, layeredgraph.NodeShapeEllipse, vizSignal)
		if strings.HasPrefix(r.Name, "vp_") {
			heldVP = true
		}
	}
	if heldVP {
		addNode(vizPNodeID("map"), "map raster", layeredgraph.NodeShapeBox, vizPanelNode)
		for _, s := range mapViewportSignals {
			addNode(vizSigID(string(s)), string(s), layeredgraph.NodeShapeEllipse, vizSignal)
			addEdge(vizSigID(string(s)), vizPNodeID("map"), "")
		}
		// The Map tab is chrome (no PanelI); include it as the raster's
		// consumer when registered.
		for _, spec := range inst.tabs.all() {
			if spec.ID == "map" {
				addNode(vizTabID("map"), spec.Title, layeredgraph.NodeShapeBox, vizTab)
				addEdge(vizPNodeID("map"), vizTabID("map"), "")
				break
			}
		}
	}
	for _, r := range rows {
		writer := r.Writer
		if writer == signalWriterMap {
			writer = "map"
		}
		if tabIDs[writer] || (writer == "map" && heldVP) {
			addEdge(vizTabID(writer), vizSigID(r.Name), "")
		}
	}

	// Unfilled marking (D3): referenced, neither bound nor held.
	held := make(map[string]bool, len(rows))
	for _, r := range rows {
		held[r.Name] = true
	}
	for id, kind := range meta {
		if kind != vizSignal {
			continue
		}
		name := strings.TrimPrefix(id, "sig/")
		if !held[name] {
			meta[id] = vizUnfilled
		}
	}

	m.Nodes = make([]layeredgraph.Node, 0, len(nodes))
	for _, n := range nodes {
		m.Nodes = append(m.Nodes, n)
	}
	sort.Slice(m.Nodes, func(i, j int) bool { return m.Nodes[i].ID < m.Nodes[j].ID })
	m.Edges = make([]layeredgraph.Edge, 0, len(edges))
	for _, e := range edges {
		m.Edges = append(m.Edges, e)
	}
	sort.Slice(m.Edges, func(i, j int) bool {
		if m.Edges[i].From != m.Edges[j].From {
			return m.Edges[i].From < m.Edges[j].From
		}
		return m.Edges[i].To < m.Edges[j].To
	})
	return
}

// systemGraphKey fingerprints the model's TOPOLOGY (ids, labels, shapes,
// edges) — the layout-cache key. Kind flips (e.g. unfilled → held) and value
// changes are colour-only and deliberately absent.
func systemGraphKey(m layeredgraph.GraphModel) string {
	h := fnv.New64a()
	for _, n := range m.Nodes {
		fmt.Fprintf(h, "n|%s|%s|%d;", n.ID, n.Label, n.Shape)
	}
	for _, e := range m.Edges {
		fmt.Fprintf(h, "e|%s|%s|%s;", e.From, e.To, e.Label)
	}
	return fmt.Sprintf("%x", h.Sum64())
}

// vizIDSalt namespaces the drawing's canvas + sense-region ids; vizSeed (per
// PlayApp) keeps two live instances (e.g. play and an embedder) collision-free.
const vizIDSalt uint64 = 0xa5b35705f00dfeed

// renderSystemGraph draws the system graph into the Graph tab. Layout is
// cached on the topology fingerprint; a layout failure degrades to one line
// and retries on the next topology change.
func (inst *PlayApp) renderSystemGraph() {
	model, meta := inst.buildSystemGraphModel()
	if len(model.Nodes) == 0 {
		return
	}
	if key := systemGraphKey(model); key != inst.vizKey || (inst.vizLayout == nil && inst.vizErr == nil) {
		inst.vizKey = key
		inst.vizLayout = nil
		eng, err := goccyengine.Shared()
		if err == nil {
			inst.vizLayout, err = eng.Layout(context.Background(), model,
				layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirLeftRight, FontSize: 13})
		}
		inst.vizErr = err
	}
	if inst.vizLayout == nil {
		msg := "system graph unavailable (layout engine)"
		if inst.vizErr != nil {
			msg += ": " + truncateRunes(firstLine(inst.vizErr.Error()), 80)
		}
		for rt := range c.RichTextLabel(msg) {
			rt.Small().Weak()
		}
		return
	}

	// Fit: width bounded, height from the layout's own aspect, both clamped
	// so a big graph stays pannable rather than exploding the pane.
	lw, lh := inst.vizLayout.Width, inst.vizLayout.Height
	if lw <= 0 || lh <= 0 {
		return
	}
	w := float32(min(max(lw, 480), 880))
	hRatio := float32(lh / lw)
	h := min(max(w*hRatio, 160), 440)

	fill := func(id string) (col color.Color, ok bool) {
		switch meta[id] {
		case vizConst:
			return color.Hex(styletokens.NeutralBgFaint.AsHex()), true
		case vizSignal:
			return color.Hex(styletokens.AccentSubtle.AsHex()), true
		case vizUnfilled:
			return color.Hex(styletokens.WarningSubtle.AsHex()), true
		case vizPanelNode:
			return color.Hex(styletokens.InfoSubtle.AsHex()), true
		case vizTab:
			return color.Hex(styletokens.SuccessSubtle.AsHex()), true
		case vizSink:
			return color.Hex(styletokens.NeutralBgExtreme.AsHex()), true
		}
		return
	}
	edgeStroke := func(from, to string) (col color.Color, ok bool) {
		if strings.HasPrefix(from, "tab/") && strings.HasPrefix(to, "sig/") {
			// The write-back half of the reactive loop.
			return color.Hex(styletokens.AccentDefault.AsHex()), true
		}
		return
	}
	res := view.Render(vizIDSalt+inst.vizSeed, inst.vizLayout, view.RenderOpts{
		Style:      view.DefaultStyle(),
		CanvasW:    w,
		CanvasH:    h,
		NodeFill:   fill,
		EdgeStroke: edgeStroke,
		State:      &inst.vizView,
	})
	// Clicking a query node observes it — the drawing doubles as the observe
	// gesture (the per-node buttons below remain for bindings).
	if strings.HasPrefix(res.Clicked, "node/") {
		id := NodeID(strings.TrimPrefix(res.Clicked, "node/"))
		if _, ok := findSplitNode(inst.currentSplit, id); ok {
			inst.observedNode = id
		}
	}
	for rt := range c.RichTextLabel("constants ▭ and signals ⬭ feed queries ▭ feed panels ▭; panel writes loop back (accent). drag pans, ctrl+scroll zooms; click a query node to observe it") {
		rt.Small().Weak()
	}
}

// nextVizSeed hands each PlayApp instance a distinct id-seed for the drawing
// (two instances in one process — e.g. play plus an embedder — must not
// collide their canvas/sense-region ids).
func nextVizSeed() uint64 {
	vizSeedCounter += 0x9e3779b97f4a7c15
	return vizSeedCounter
}

// vizSeedCounter is render-thread-only (NewPlayApp runs there).
var vizSeedCounter uint64
