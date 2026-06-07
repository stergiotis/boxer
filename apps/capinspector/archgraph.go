package capinspector

// archgraph.go renders the cap-broker schematic as a static layered (Sugiyama)
// graph through the layeredgraph widget (ADR-0069): the App→capability→backend
// DAG is laid out once by Graphviz `dot` in-process (cgo-free WASM via
// goccyengine) and painted through the existing imzero2 binding. Only the
// per-frame colours vary — selected cap, effective backend, degraded cap —
// applied through view.RenderOpts hooks. The live audit activity that used to
// sit inside the cap boxes renders as a companion sparkline strip beneath the
// schematic (renderActivityStrip): the widget paints one text label per node,
// so it cannot host an in-node sparkline. This file is the layeredgraph
// adoption that superseded the hand-painted radial hexagon.

import (
	"context"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
)

// appNodeID is the layeredgraph node id of the prototypical App root. capNodeID
// and backendNodeID namespace the cap and backend nodes so the flat node-id
// space stays collision-free: CapTask ships a backend whose Id is "task",
// which would otherwise alias the "task" cap id.
const appNodeID = "app"

func capNodeID(capId CapId) string                       { return "cap/" + capId }
func backendNodeID(capId CapId, backendID string) string { return "be/" + capId + "/" + backendID }

// nodeKind tags each layeredgraph node so the colour and click hooks can branch
// without re-parsing the id string on the hot path.
type nodeKind uint8

const (
	nodeApp nodeKind = iota
	nodeCap
	nodeBackend
)

// nodeMeta is the reverse map entry for one node id: which kind it is, the cap
// it belongs to (empty for the app root), and — for a backend node — its
// backend id. Built once alongside the cached layout and read every frame.
type nodeMeta struct {
	kind      nodeKind
	cap       CapId
	backendID string
}

// capGraphModel builds the directed App→capability→backend graph from the
// (closed) Registry. One App root, one node per cap, one node per available
// backend; edges App→cap and cap→backend. Deterministic order so the cached
// layout is stable across runs.
func capGraphModel() layeredgraph.GraphModel {
	caps := allCapIdsOrdered()
	m := layeredgraph.GraphModel{
		Nodes: make([]layeredgraph.Node, 0, 1+len(caps)*3),
		Edges: make([]layeredgraph.Edge, 0, len(caps)*3),
	}
	m.Nodes = append(m.Nodes, layeredgraph.Node{ID: appNodeID, Label: "App"})
	for _, capId := range caps {
		cn := capNodeID(capId)
		m.Nodes = append(m.Nodes, layeredgraph.Node{ID: cn, Label: diagramCapLabel(capId)})
		m.Edges = append(m.Edges, layeredgraph.Edge{From: appNodeID, To: cn})
		for _, b := range Registry[capId].Backends {
			bn := backendNodeID(capId, b.Id)
			m.Nodes = append(m.Nodes, layeredgraph.Node{ID: bn, Label: b.Display})
			m.Edges = append(m.Edges, layeredgraph.Edge{From: cn, To: bn})
		}
	}
	return m
}

// buildNodeMeta builds the node-id→nodeMeta reverse map the per-frame hooks
// read. Mirrors capGraphModel's id construction exactly.
func buildNodeMeta() map[string]nodeMeta {
	caps := allCapIdsOrdered()
	meta := make(map[string]nodeMeta, 1+len(caps)*3)
	meta[appNodeID] = nodeMeta{kind: nodeApp}
	for _, capId := range caps {
		meta[capNodeID(capId)] = nodeMeta{kind: nodeCap, cap: capId}
		for _, b := range Registry[capId].Backends {
			meta[backendNodeID(capId, b.Id)] = nodeMeta{kind: nodeBackend, cap: capId, backendID: b.Id}
		}
	}
	return meta
}

// capArchLayout caches the static schematic layout, computed once via the
// Graphviz engine. The topology is closed — the Registry never grows at
// runtime — so every inspector window and every frame reuses one layout and
// only the colours change. These package-level vars are touched only from the
// single-threaded imzero2 render loop, so they need no mutex. ensureArchLayout
// retries while the layout is nil, so a transient WASM failure recovers on a
// later frame instead of sticking; capArchLayoutErr carries the last failure
// for the degraded message.
var (
	capArchLayout    *layeredgraph.Layout
	capArchLayoutErr error
	capArchNodeMeta  map[string]nodeMeta
)

// ensureArchLayout lazily lays out the schematic (no WASM runtime spins up
// until the inspector is first opened) and caches the result. Returns the
// cached layout, its node-meta map, and the last layout error.
func ensureArchLayout() (*layeredgraph.Layout, map[string]nodeMeta, error) {
	if capArchLayout == nil {
		eng, err := goccyengine.Shared()
		if err != nil {
			capArchLayoutErr = err
			return nil, nil, err
		}
		capArchLayout, capArchLayoutErr = eng.Layout(context.Background(), capGraphModel(),
			layeredgraph.LayoutOpts{RankDir: layeredgraph.RankDirTopBottom, FontSize: 14})
		if capArchLayout != nil {
			capArchNodeMeta = buildNodeMeta()
		}
	}
	return capArchLayout, capArchNodeMeta, capArchLayoutErr
}

// archIDSalt is a high-entropy constant; graphIDBase adds the per-instance seed
// so two open inspector windows don't collide their canvas / sense-region ids.
const archIDSalt uint64 = 0x9e3779b97f4a7c15

func (inst *App) graphIDBase() uint64 { return archIDSalt + inst.seed }

const (
	// chromeW reserves room for the ScrollArea's vertical scrollbar so the
	// canvas plus scrollbar fits the captured available width without
	// provoking a horizontal scroll.
	archChromeW    float32 = 24.0
	archMinCanvasW float32 = 460.0
	archMaxCanvasW float32 = 1400.0
	archFallbackW  float32 = 820.0
	// The schematic is wide (caps + backends in a row) and short (three
	// ranks); clamp the height derived from its own aspect so it neither
	// floats in a tall empty box nor squashes flat.
	archMinCanvasH float32 = 200.0
	archMaxCanvasH float32 = 680.0
)

// renderGraph draws the cap-broker architecture (App → capabilities → backends)
// as the cached layered graph and, beneath it, the live-activity strip. The
// layout is computed once; per frame only the colours change via the view's
// NodeFill/EdgeStroke hooks, and a click on a cap or backend node reselects
// that cap (the picker row above is the equivalent affordance). Static
// fit-to-view for v1 (no pan/zoom) — the graph is small and a fixed frame keeps
// captures deterministic.
func (inst *App) renderGraph(spec CapSpec) {
	lay, meta, err := ensureArchLayout()
	if err != nil {
		c.Label("architecture graph unavailable: " + err.Error()).Send()
		return
	}
	if lay == nil {
		return
	}

	// Responsive width: track the panel's available width (captured last
	// frame; NaN until the first capture lands). The surrounding ScrollArea
	// uses AutoShrink(false,false), so this reflects the panel width, not the
	// canvas's own previous width.
	sm := c.CurrentApplicationState.StateManager
	avail := sm.GetAvailableSize()
	c.CaptureAvailableSize()
	canvasW := archFallbackW
	if avail.W == avail.W && avail.W > archChromeW { // avail.W == avail.W rejects NaN
		canvasW = avail.W - archChromeW
	}
	canvasW = max(min(canvasW, archMaxCanvasW), archMinCanvasW)
	// Height from the layout's own aspect so the short three-rank graph fills
	// the canvas instead of sitting in a tall empty box.
	canvasH := archMinCanvasH
	if lay.Width > 0 {
		canvasH = canvasW * float32(lay.Height/lay.Width)
	}
	canvasH = max(min(canvasH, archMaxCanvasH), archMinCanvasH)

	res := view.Render(inst.graphIDBase(), lay, view.RenderOpts{
		CanvasW:    canvasW,
		CanvasH:    canvasH,
		NodeFill:   archNodeFill(meta, spec.Id),
		NodeText:   archNodeText(meta, spec.Id),
		EdgeStroke: archEdgeStroke(meta, spec.Id),
	})
	// Click a cap or backend node to select that cap (a backend selects its
	// parent cap); the App root is inert.
	if res.Clicked != "" {
		if m, ok := meta[res.Clicked]; ok && (m.kind == nodeCap || m.kind == nodeBackend) {
			inst.selectedCap = m.cap
		}
	}

	c.AddSpace(styletokens.GapItems(inst.density))
	inst.renderActivityStrip(canvasW, spec.Id)
}

// archNodeText pairs each node's label ink with the fill archNodeFill picked.
// The light highlight tones (selected cap, degraded cap, effective backend)
// take near-black ink; the dark App / idle / inactive tones return ok=false and
// fall through to the style's light default (NeutralTextPrimary). A single
// global NodeText can't satisfy both halves, so the view resolves ink per node.
func archNodeText(meta map[string]nodeMeta, selected CapId) func(id string) (color.Color, bool) {
	dark := color.Hex(styletokens.NeutralBgExtreme.AsHex())
	return func(id string) (color.Color, bool) {
		m, ok := meta[id]
		if !ok {
			return color.Color{}, false
		}
		switch m.kind {
		case nodeCap:
			if m.cap == selected || ActiveBackend(m.cap) == "" {
				return dark, true
			}
		case nodeBackend:
			if m.backendID == ActiveBackend(m.cap) {
				return dark, true
			}
		}
		return color.Color{}, false
	}
}

// archNodeFill colours each node by its role and live state, preserving the
// prior schematic's semantics with IDS tones: blue App root, green selected
// cap, red degraded cap (no effective backend — replaces the old "!" badge),
// amber effective backend, dim idle/inactive. App / idle / inactive keep the
// dark IDS *Subtle tones and the highlights the light *Default tones; archNodeText
// flips the label ink per node so both read.
func archNodeFill(meta map[string]nodeMeta, selected CapId) func(id string) (color.Color, bool) {
	return func(id string) (color.Color, bool) {
		m, ok := meta[id]
		if !ok {
			return color.Color{}, false
		}
		switch m.kind {
		case nodeApp:
			return color.Hex(styletokens.AccentSubtle.AsHex()), true
		case nodeCap:
			switch {
			case m.cap == selected:
				return color.Hex(styletokens.SuccessDefault.AsHex()), true
			case ActiveBackend(m.cap) == "":
				return color.Hex(styletokens.ErrorDefault.AsHex()), true
			}
			return color.Hex(styletokens.NeutralSubtle.AsHex()), true
		case nodeBackend:
			if m.backendID == ActiveBackend(m.cap) {
				return color.Hex(styletokens.WarningDefault.AsHex()), true
			}
			return color.Hex(styletokens.NeutralSubtle.AsHex()), true
		}
		return color.Color{}, false
	}
}

// archEdgeStroke dims edges into a non-effective backend and lights up the
// selected cap's edges to its backends (focus), mirroring how fsmview lights
// the next-possible transitions. Other edges keep the style default.
func archEdgeStroke(meta map[string]nodeMeta, selected CapId) func(from, to string) (color.Color, bool) {
	return func(from, to string) (color.Color, bool) {
		if m, ok := meta[to]; ok && m.kind == nodeBackend && m.backendID != ActiveBackend(m.cap) {
			return color.Hex(styletokens.NeutralBorderFaint.AsHex()), true
		}
		if m, ok := meta[from]; ok && m.kind == nodeCap && m.cap == selected {
			return color.Hex(styletokens.AccentDefault.AsHex()), true
		}
		return color.Color{}, false
	}
}

const (
	// archStripH is the height of the companion activity strip canvas.
	archStripH       float32 = 64.0
	archStripLabelPt float32 = 9.5
	archStripPadX    float32 = 6.0
)

// renderActivityStrip paints the live per-cap audit activity beneath the
// schematic: one cell per cap (allCapIdsOrdered), a compact cap-id label over a
// rolling-window bar sparkline. It is the companion the layered graph can't
// host in-node — the graph carries the wiring, the strip the live traffic the
// old in-box sparklines showed. The selected cap's bars take the green
// highlight; the rest sit in blue-grey. Read-only: selection happens on the
// graph nodes and the picker row above. Bars reuse Tally's rolling histogram
// (audit_sparkline.go); the normalisation matches the old paintCapSparkline.
func (inst *App) renderActivityStrip(canvasW float32, selected CapId) {
	const (
		bgFill       uint32  = 0x161616ff
		labelCol     uint32  = 0x9aa0a6ff
		labelSelCol  uint32  = 0x44cc88ff
		baselineCol  uint32  = 0x40404080
		barCol       uint32  = 0x8090a0d0
		barSelCol    uint32  = 0x44cc88e0
		minScaleMax  uint64  = 4   // floor so 1-2 audits don't render saturated
		minBarHeight float32 = 1.5 // keep 1-count buckets visible
		labelY       float32 = 11.0
		baselineY    float32 = 54.0
		barTop       float32 = 20.0
	)
	caps := allCapIdsOrdered()
	cellW := canvasW / float32(len(caps))
	barAreaH := baselineY - barTop
	for i, capId := range caps {
		cellX := float32(i) * cellW
		sel := capId == selected

		lc := labelCol
		if sel {
			lc = labelSelCol
		}
		c.PaintText(cellX+cellW*0.5, labelY, 1, 1, capId, archStripLabelPt, color.Hex(lc)).Send()

		stripX0 := cellX + archStripPadX
		stripX1 := cellX + cellW - archStripPadX
		// Baseline first so the bars sit on top of it.
		c.PaintLine(stripX0, baselineY, stripX1, baselineY, color.Hex(baselineCol), 1.0).Send()

		snap := Tally.Snapshot(capId)
		var maxV uint64
		for _, v := range snap {
			if v > maxV {
				maxV = v
			}
		}
		if maxV == 0 {
			continue // baseline-only; nothing else to paint
		}
		scaleMax := max(maxV, minScaleMax)
		bc := barCol
		if sel {
			bc = barSelCol
		}
		stripW := stripX1 - stripX0
		barW := stripW / float32(sparkBuckets)
		for k, v := range snap {
			if v == 0 {
				continue
			}
			bh := max(float32(v)/float32(scaleMax)*barAreaH, minBarHeight)
			bx0 := stripX0 + float32(k)*barW + 0.3
			bx1 := stripX0 + float32(k+1)*barW - 0.3
			c.PaintRectFilled(bx0, baselineY-bh, bx1, baselineY, 0.0, color.Hex(bc)).Send()
		}
	}
	c.PaintCanvas(ids.PrepareStr("activity-strip"), canvasW, archStripH).
		Background(color.Hex(bgFill)).
		Send()
}
