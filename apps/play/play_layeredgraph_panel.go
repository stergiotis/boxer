package play

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
)

// play_layeredgraph_panel.go is the ADR-0129 Network dock tab: a result set
// rendered as a directed node-link graph over the layeredgraph widget
// (ADR-0069). The graph is read from two convention-named CTEs of the user's
// own query — `edges` (required) and `vertices` (optional) — each pulled off
// the split graph on its own lane, the kanban `lanes`-CTE mechanism (ADR-0122
// §SD6) applied twice. When no `vertices` CTE is present the vertex set is
// inferred from the edge endpoints.
//
// The contract is named columns rather than detection (§SD2), like kanban:
// edges carry `source`/`target` (+ optional `label`); vertices carry `id`
// (+ optional `label`, `group`, `shape`). Nothing but intent separates a
// source column from a target column, so the panel asks for the names.

const (
	// Edge columns (chEdges). source/target are the graph-data standard and
	// avoid the `from` SQL keyword (§SD2 kill-reason).
	networkSourceCol = "source"
	networkTargetCol = "target"
	// Vertex columns (chVertices). label is shared with the edge contract —
	// the two live in different CTEs, so one name serves both.
	networkIDCol    = "id"
	networkGroupCol = "group"
	networkShapeCol = "shape"
	networkLabelCol = "label"

	// networkEdgesNodeID / networkVerticesNodeID are the CTEs the two channels
	// bind to (§SD1). Nodes of the user's own split graph, demanded on their
	// own lanes — not panel-authored queries.
	networkEdgesNodeID    NodeID = "edges"
	networkVerticesNodeID NodeID = "vertices"

	// networkMaxVertices / networkMaxEdges bound the model (§SD5). Layered
	// layout of a Graphviz-WASM run is a tens-to-low-hundreds instrument; a
	// large result is both slow to lay out and unreadable, so the excess is
	// dropped and counted in the status line rather than silently truncated.
	networkMaxVertices = 400
	networkMaxEdges    = 1000
)

// networkIDSalt namespaces the panel's canvas + per-node sense-region ids —
// distinct from the System graph's vizIDSalt so the two drawings never collide;
// per-instance idSeed (from nextVizSeed) keeps two live PlayApps apart.
const networkIDSalt uint64 = 0x6e37c0de9a11f00d

// networkGroupPalette colours the optional `group` column by distinct value.
// These are the *Subtle background tones — the INVERSE of a kanban dot (§SD2):
// a node body is a background, so the palette is background fills (dark, L≈0.2)
// and the default light NodeText reads on them, where the kanban dot vocabulary
// deliberately excludes the *Subtle tones because a dot is a foreground mark.
var networkGroupPalette = []styletokens.RGBA8{
	styletokens.AccentSubtle,
	styletokens.InfoSubtle,
	styletokens.SuccessSubtle,
	styletokens.WarningSubtle,
	styletokens.ErrorSubtle,
	styletokens.NeutralSubtle,
}

func networkGroupColor(idx int) color.Color {
	return color.Hex(networkGroupPalette[idx%len(networkGroupPalette)].AsHex())
}

// parseNetworkShape maps a `shape` cell to a node boundary; the box is the
// default for an absent or unrecognised value.
func parseNetworkShape(s string) layeredgraph.NodeShape {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ellipse", "oval":
		return layeredgraph.NodeShapeEllipse
	case "circle":
		return layeredgraph.NodeShapeCircle
	default:
		return layeredgraph.NodeShapeBox
	}
}

// networkEdgesClaim / networkVerticesClaim are the resolved column indices a
// channel's schema yields in AcceptForChannel and Render consumes. -1 marks an
// absent optional column.
type networkEdgesClaim struct {
	srcCol, tgtCol, labelCol int
}

type networkVerticesClaim struct {
	idCol, labelCol, groupCol, shapeCol int
}

// NetworkDriver owns the Network tab state: the two input lanes, the cached
// layout (recomputed only on a topology or rank-direction change, so a
// selection click never re-lays-out), and the pan/zoom view.
type NetworkDriver struct {
	ids    *c.WidgetIdStack
	idSeed uint64

	// edgesLane / verticesLane run the `edges` / `vertices` CTEs of the user's
	// split on their own lanes (nil for an unwired host — tests). The status
	// mirrors let a failed lane say so rather than reading as "no graph".
	edgesLane       *nodeLane
	verticesLane    *nodeLane
	edgesLoading    bool
	verticesLoading bool
	edgesErr        error
	verticesErr     error

	rankDir layeredgraph.RankDir
	view    view.ViewState

	// selectedID highlights the last-clicked node. Selection is LOCAL to the
	// panel in v1: the graph's vertices come from a private lane, not an
	// observable split node, so publishing to the shared `selection` signal
	// would be clamped away (syncSelectionClamp sends a cursor on an unbound
	// node home) and would jerk the other panels to row 0. Cross-panel
	// selection waits for the graph's CTEs to become observable nodes (ADR-0129
	// §SD7 — the observe/bind direction).
	selectedID string

	layout    *layeredgraph.Layout
	layoutKey string
	layoutErr error

	// Last-build stats for the status line.
	nodeCount int
	edgeCount int
	capped    bool
}

// NewNetworkDriver builds the driver. client may be nil (tests, an unwired
// host): the lanes are then absent and the panel shows its empty-state.
func NewNetworkDriver(ids *c.WidgetIdStack, client *Client) (inst *NetworkDriver) {
	inst = &NetworkDriver{ids: ids, idSeed: nextVizSeed(), rankDir: layeredgraph.RankDirTopBottom}
	if client != nil {
		inst.edgesLane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("network-edges")},
			memory.NewGoAllocator(), 0)
		inst.verticesLane = newNodeLane(clientExecutor{client: client, opts: newExecOptions("network-vertices")},
			memory.NewGoAllocator(), 0)
	}
	return
}

// forgetLanes clears both lane memos so the next demand re-executes, even for
// an unchanged (SQL, params) pair — the Run hook (executeRun), matching the
// intermediate and bound lanes. Without it a re-Run after a transient failure
// (a wrong endpoint, a server that was down) memo-hits the stored error — its
// key is the SQL, and the endpoint is not part of it — so the graph never
// recovers though the main result does.
func (inst *NetworkDriver) forgetLanes() {
	if inst == nil {
		return
	}
	if inst.edgesLane != nil {
		inst.edgesLane.forget()
	}
	if inst.verticesLane != nil {
		inst.verticesLane.forget()
	}
}

// layeredGraphPanel is the PanelI face. Acceptance is schema-only and cheap —
// it runs every frame — because the contract is a question about column names,
// which the schema answers on its own.
type layeredGraphPanel struct {
	driver *NetworkDriver
}

func (inst layeredGraphPanel) ID() PanelID { return "network" }

// Channels declares the required edges plus the optional decorating vertices.
// The panel renders as soon as chEdges is filled; chVertices, when present,
// supplies labels/groups/shapes and the isolated (edge-free) nodes.
func (inst layeredGraphPanel) Channels() []ChannelSpec {
	return []ChannelSpec{
		{ID: chEdges, Required: true, Label: "edges"},
		{ID: chVertices, Required: false, Label: "vertices"},
	}
}

func (inst layeredGraphPanel) AcceptForChannel(ch ChannelID, schema *arrow.Schema, sig SignalEnvI) (claim ChannelClaim, reason string) {
	switch ch {
	case chEdges:
		if schema == nil {
			reason = "Run a query with an `edges` CTE (columns `source` and `target`) to see a graph."
			return
		}
		ec, r := resolveNetworkEdges(schema)
		if r != "" {
			reason = r
			return
		}
		claim = ec
		return
	case chVertices:
		if schema == nil {
			reason = "no vertices result" // optional channel: reason is swallowed by the dispatcher
			return
		}
		vc, r := resolveNetworkVertices(schema)
		if r != "" {
			reason = r
			return
		}
		claim = vc
		return
	}
	reason = "unknown channel"
	return
}

// Render draws the graph. emit is unused — selection is local in v1 (see
// NetworkDriver.selectedID), so the panel publishes no signal.
func (inst layeredGraphPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	edges, ok := filled[chEdges]
	if !ok {
		return
	}
	ec, ok := edges.Claim.(networkEdgesClaim)
	if !ok {
		return
	}
	vc := networkVerticesClaim{idCol: -1, labelCol: -1, groupCol: -1, shapeCol: -1}
	var vertRec arrow.RecordBatch
	if v, has := filled[chVertices]; has {
		if got, isC := v.Claim.(networkVerticesClaim); isC {
			vc = got
			vertRec = v.Rec
		}
	}
	inst.driver.render(edges.Rec, ec, vertRec, vc)
}

// resolveNetworkEdges applies the §SD2 edge contract to a schema. Pure and
// schema-only; source/target are read through formatCell (total over Arrow
// types), so they carry no type requirement — a numeric id is a fine key.
func resolveNetworkEdges(schema *arrow.Schema) (ec networkEdgesClaim, reason string) {
	ec = networkEdgesClaim{srcCol: -1, tgtCol: -1, labelCol: -1}
	for ci, f := range schema.Fields() {
		switch f.Name {
		case networkSourceCol:
			ec.srcCol = ci
		case networkTargetCol:
			ec.tgtCol = ci
		case networkLabelCol:
			ec.labelCol = ci
		}
	}
	if ec.srcCol < 0 || ec.tgtCol < 0 {
		var missing []string
		if ec.srcCol < 0 {
			missing = append(missing, "`source`")
		}
		if ec.tgtCol < 0 {
			missing = append(missing, "`target`")
		}
		reason = fmt.Sprintf("The graph's `edges` CTE needs a %s column. Name them in the query — e.g. "+
			"WITH edges AS (SELECT a AS source, b AS target FROM t) SELECT * FROM edges — and optionally add a "+
			"`vertices` CTE (`id`, `label`, `group`, `shape`) to decorate the nodes.",
			strings.Join(missing, " and a "))
	}
	return
}

// resolveNetworkVertices applies the §SD2 vertex contract. Only `id` is
// required; a vertices CTE missing it is rejected, and because the channel is
// optional the panel simply draws from the edges alone (endpoint inference).
func resolveNetworkVertices(schema *arrow.Schema) (vc networkVerticesClaim, reason string) {
	vc = networkVerticesClaim{idCol: -1, labelCol: -1, groupCol: -1, shapeCol: -1}
	for ci, f := range schema.Fields() {
		switch f.Name {
		case networkIDCol:
			vc.idCol = ci
		case networkLabelCol:
			vc.labelCol = ci
		case networkGroupCol:
			vc.groupCol = ci
		case networkShapeCol:
			vc.shapeCol = ci
		}
	}
	if vc.idCol < 0 {
		reason = "the `vertices` CTE needs an `id` column"
	}
	return
}

// networkBuild is the outcome of mapping the two result sets to a GraphModel:
// the model plus the per-vertex group fill Render's NodeFill hook reads.
type networkBuild struct {
	model  layeredgraph.GraphModel
	fillOf map[string]color.Color // vertex id → group fill (absent → default)
	capped bool
}

// buildNetworkModel maps the edges/vertices records to a directed GraphModel
// (§SD2): vertices are de-duplicated by id, an edge endpoint with no vertices
// row synthesises a node (so a partial or absent `vertices` CTE still draws
// every edge), parallel (source,target) pairs collapse, and both inputs are
// capped. Node ids must be unique (the widget's invariant) — the dedup enforces
// it. Deterministic given the records, so the layout key is stable frame to
// frame.
func buildNetworkModel(edgesRec arrow.RecordBatch, ec networkEdgesClaim, vertRec arrow.RecordBatch, vc networkVerticesClaim) (b networkBuild) {
	b.fillOf = make(map[string]color.Color)
	nodes := make([]layeredgraph.Node, 0, 64)
	seen := make(map[string]struct{}, 64)
	groupIdx := make(map[string]int, 8)

	// addSynth adds an edge endpoint with no vertices row; false means the
	// vertex cap is reached, so the caller must drop the edge rather than leave
	// it referencing a node the model does not contain.
	addSynth := func(id string) bool {
		if _, ok := seen[id]; ok {
			return true
		}
		if len(nodes) >= networkMaxVertices {
			return false
		}
		seen[id] = struct{}{}
		nodes = append(nodes, layeredgraph.Node{ID: id, Label: id})
		return true
	}

	haveVerts := vertRec != nil && vc.idCol >= 0
	if haveVerts {
		rows := vertRec.NumRows()
		for row := range rows {
			if len(nodes) >= networkMaxVertices {
				b.capped = true
				break
			}
			id := formatCell(vertRec, vc.idCol, row)
			if id == "" {
				continue
			}
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			node := layeredgraph.Node{ID: id, Label: id}
			if vc.labelCol >= 0 {
				if l := formatCell(vertRec, vc.labelCol, row); l != "" {
					node.Label = l
				}
			}
			if vc.shapeCol >= 0 {
				node.Shape = parseNetworkShape(formatCell(vertRec, vc.shapeCol, row))
			}
			nodes = append(nodes, node)
			if vc.groupCol >= 0 {
				if g := formatCell(vertRec, vc.groupCol, row); g != "" {
					idx, ok := groupIdx[g]
					if !ok {
						idx = len(groupIdx)
						groupIdx[g] = idx
					}
					b.fillOf[id] = networkGroupColor(idx)
				}
			}
		}
	}

	edges := make([]layeredgraph.Edge, 0, 64)
	edgeSeen := make(map[[2]string]struct{}, 64)
	if edgesRec != nil {
		rows := edgesRec.NumRows()
		for row := range rows {
			if len(edges) >= networkMaxEdges {
				b.capped = true
				break
			}
			src := formatCell(edgesRec, ec.srcCol, row)
			tgt := formatCell(edgesRec, ec.tgtCol, row)
			if src == "" || tgt == "" {
				continue
			}
			key := [2]string{src, tgt}
			if _, dup := edgeSeen[key]; dup {
				continue
			}
			if !addSynth(src) || !addSynth(tgt) {
				b.capped = true
				continue // a dangling endpoint (vertex cap reached) drops the edge
			}
			edgeSeen[key] = struct{}{}
			e := layeredgraph.Edge{From: src, To: tgt}
			if ec.labelCol >= 0 {
				e.Label = formatCell(edgesRec, ec.labelCol, row)
			}
			edges = append(edges, e)
		}
	}
	b.model = layeredgraph.GraphModel{Nodes: nodes, Edges: edges}
	return
}

// render maps the two results into a graph, lays it out (cached), draws it, and
// tracks the locally-selected node.
func (inst *NetworkDriver) render(edgesRec arrow.RecordBatch, ec networkEdgesClaim, vertRec arrow.RecordBatch, vc networkVerticesClaim) {
	inst.renderControls()

	b := buildNetworkModel(edgesRec, ec, vertRec, vc)
	inst.nodeCount = len(b.model.Nodes)
	inst.edgeCount = len(b.model.Edges)
	inst.capped = b.capped
	c.Label(inst.statusLine()).Send()

	if len(b.model.Nodes) == 0 {
		for rt := range c.RichTextLabel("The `edges` CTE produced no drawable edges, and there are no `vertices` rows.") {
			rt.Small().Weak()
		}
		return
	}

	// Layout is cached on the topology fingerprint (+ rank direction). A
	// selection click changes only the highlight, not the topology, so it never
	// re-runs the Graphviz-WASM layout — the play_graph_viz.go idiom.
	key := networkModelKey(b.model, inst.rankDir)
	if key != inst.layoutKey || (inst.layout == nil && inst.layoutErr == nil) {
		inst.layoutKey = key
		inst.layout = nil
		eng, err := goccyengine.Shared()
		if err == nil {
			inst.layout, err = eng.Layout(context.Background(), b.model,
				layeredgraph.LayoutOpts{RankDir: inst.rankDir, FontSize: 13})
		}
		inst.layoutErr = err
	}
	if inst.layout == nil {
		msg := "graph layout unavailable (layout engine)"
		if inst.layoutErr != nil {
			msg += ": " + truncateRunes(firstLineOf(inst.layoutErr.Error()), 80)
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
	// Fill the pane width: a full-width separator, then a Seq-keyed UiRect probe
	// reads its span next frame (the passes-tab idiom — a per-seq R21 slot, so
	// it does not contend with the editor's single CaptureAvailableSize
	// register). Height follows the layout's aspect (clamped); the tab scrolls
	// if the graph is taller than the leaf. Filling the width is what maximises
	// the drawing — view.Render fits uniformly, so a wide graph is
	// width-constrained and a taller canvas would only add margin. One-frame
	// lag; the first frame falls back to a conservative width.
	sm := c.CurrentApplicationState.StateManager
	c.Separator().Horizontal().Send()
	probeSeq := networkIDSalt ^ inst.idSeed ^ 0x1
	c.CaptureUiRect(probeSeq)
	paneW := float32(760)
	if r, ok := sm.GetUiRect(probeSeq); ok && r.MaxX > r.MinX {
		paneW = r.MaxX - r.MinX
	}
	w := min(max(paneW-12, 360), 1600)
	h := min(max(w*float32(lh/lw), 200), 720)

	fill := func(id string) (col color.Color, ok bool) {
		if inst.selectedID != "" && id == inst.selectedID {
			return color.Hex(styletokens.AccentDefault.AsHex()), true
		}
		col, ok = b.fillOf[id]
		return
	}
	res := view.Render(networkIDSalt+inst.idSeed, inst.layout, view.RenderOpts{
		Style:    view.DefaultStyle(),
		CanvasW:  w,
		CanvasH:  h,
		NodeFill: fill,
		State:    &inst.view,
	})
	// A vertex click highlights it (local selection — v1 publishes no shared
	// signal, see selectedID); clicking the highlighted node again clears it.
	if res.Clicked != "" {
		if inst.selectedID == res.Clicked {
			inst.selectedID = ""
		} else {
			inst.selectedID = res.Clicked
		}
	}
	for rt := range c.RichTextLabel("drag pans, ctrl+scroll zooms; click a node to highlight it") {
		rt.Small().Weak()
	}
}

// renderControls draws the layout-direction toggle (§SD4). Changing it re-keys
// the layout cache, so the next frame re-lays-out.
func (inst *NetworkDriver) renderControls() {
	for range c.Horizontal().KeepIter() {
		c.Label("layout").Send()
		if c.Button(inst.ids.PrepareStr("net-tb"), c.Atoms().Text("top-down").Keep()).
			Frame(false).Selected(inst.rankDir == layeredgraph.RankDirTopBottom).
			SendResp().HasPrimaryClicked() {
			inst.rankDir = layeredgraph.RankDirTopBottom
		}
		if c.Button(inst.ids.PrepareStr("net-lr"), c.Atoms().Text("left-right").Keep()).
			Frame(false).Selected(inst.rankDir == layeredgraph.RankDirLeftRight).
			SendResp().HasPrimaryClicked() {
			inst.rankDir = layeredgraph.RankDirLeftRight
		}
	}
}

func (inst *NetworkDriver) statusLine() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d nodes · %d edges", inst.nodeCount, inst.edgeCount)
	if inst.capped {
		fmt.Fprintf(&b, " · capped at %d nodes / %d edges (add a LIMIT or filter)", networkMaxVertices, networkMaxEdges)
	}
	switch {
	case inst.edgesErr != nil:
		fmt.Fprintf(&b, " · edges query failed: %v", inst.edgesErr)
	case inst.verticesErr != nil:
		fmt.Fprintf(&b, " · vertices query failed: %v", inst.verticesErr)
	case inst.edgesLoading || inst.verticesLoading:
		b.WriteString(" · …")
	}
	return b.String()
}

// networkModelKey fingerprints the model's TOPOLOGY (ids, labels, shapes,
// edges) plus the rank direction — the layout-cache key. Group/selection are
// colour-only and deliberately absent, so value churn never re-lays-out.
func networkModelKey(m layeredgraph.GraphModel, rd layeredgraph.RankDir) string {
	h := fnv.New64a()
	fmt.Fprintf(h, "rd|%d;", rd)
	for _, n := range m.Nodes {
		fmt.Fprintf(h, "n|%s|%s|%d;", n.ID, n.Label, n.Shape)
	}
	for _, e := range m.Edges {
		fmt.Fprintf(h, "e|%s|%s|%s;", e.From, e.To, e.Label)
	}
	return fmt.Sprintf("%x", h.Sum64())
}

// renderNetworkTab is the Network dock tab body (ADR-0129): the two named CTEs
// demanded on their lanes, then the PanelI dispatch. Unlike the other result
// panels it does not read the active result — its inputs are the `edges` and
// `vertices` CTEs by name, each on its own lane (like the Kanban lanes node).
func (inst *PlayApp) renderNetworkTab() {
	edgesRec, edgesSchema := inst.demandNetworkEdges()
	if edgesRec != nil {
		defer edgesRec.Release()
	}
	vertRec, vertSchema := inst.demandNetworkVertices()
	if vertRec != nil {
		defer vertRec.Release()
	}

	inputs := map[ChannelID]channelInput{
		chEdges: {node: networkEdgesNodeID, rec: edgesRec, schema: edgesSchema, sig: inst.frameSig},
	}
	// Offer the vertices channel only when the CTE exists (a schema-only view
	// still fills it, so an inventory that legitimately returned nothing reads
	// as "no vertices" rather than as pending).
	if vertRec != nil || vertSchema != nil {
		inputs[chVertices] = channelInput{node: networkVerticesNodeID, rec: vertRec, schema: vertSchema, sig: inst.frameSig}
	}
	reject := dispatchPanel(layeredGraphPanel{driver: inst.networkDriver}, inputs, inst.sigEmit)
	if reject != "" {
		if inst.networkDriver != nil && inst.networkDriver.edgesLoading {
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

// demandNetworkEdges compiles the query's `edges` CTE — if it has one — and
// demands it on the driver's edges lane, returning the retained result for the
// chEdges channel (the caller MUST Release rec). Mirrors demandKanbanLanes: the
// node comes from the last Run's split, so its signal reads resolve like any
// other node's and a SET-bound name travels inside the fused SQL.
func (inst *PlayApp) demandNetworkEdges() (rec arrow.RecordBatch, schema *arrow.Schema) {
	d := inst.networkDriver
	if d == nil || d.edgesLane == nil {
		return
	}
	node, ok := findSplitNode(inst.currentSplit, networkEdgesNodeID)
	if !ok {
		d.edgesLoading = false
		d.edgesErr = nil
		return
	}
	v := d.edgesLane.demand(compiledNode{
		SQL:    fuseNode(inst.currentSplit, networkEdgesNodeID),
		Params: resolveSignalNames(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	d.edgesLoading = v.loading
	d.edgesErr = v.err // mirrored every demand — nil clears (no latch)
	return v.rec, v.schema
}

// demandNetworkVertices is demandNetworkEdges for the optional `vertices` CTE.
func (inst *PlayApp) demandNetworkVertices() (rec arrow.RecordBatch, schema *arrow.Schema) {
	d := inst.networkDriver
	if d == nil || d.verticesLane == nil {
		return
	}
	node, ok := findSplitNode(inst.currentSplit, networkVerticesNodeID)
	if !ok {
		d.verticesLoading = false
		d.verticesErr = nil
		return
	}
	v := d.verticesLane.demand(compiledNode{
		SQL:    fuseNode(inst.currentSplit, networkVerticesNodeID),
		Params: resolveSignalNames(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	d.verticesLoading = v.loading
	d.verticesErr = v.err
	return v.rec, v.schema
}
