package play

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/contrast"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
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
// edges carry `source`/`target` (+ optional `label`, `tone`); vertices carry
// `id` (+ optional `label`, `group`, `shape`, `tone`). Nothing but intent
// separates a source column from a target column, so the panel asks for the
// names.

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
	// tone names a design-system semantic family for one vertex or edge, for
	// when the drawing carries a *meaning* the auto-palette cannot: a
	// forbidden dependency is not "category 4", it is an error. Shared by
	// both contracts, like label.
	networkToneCol = "tone"
	// weight is the ORDINAL magnitude channel (ADR-0167): how *much* flowed
	// along an edge, as opposed to what it means. It is the opposite kind of
	// claim from `tone` and they compose — a weighted edge still takes its
	// tone, because a semantic claim is the more specific one (§SD5).
	// Numeric; non-positive is *unknown* and renders as an ordinary edge.
	networkWeightCol = "weight"

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

// magnitudeBandSteps is how finely networkMagnitudeBandLo searches the ramp.
// The band floor only has to be found to within a few percent — it is a
// legibility threshold, not a value — and a coarse walk keeps this cheap
// enough to run per render rather than being cached against a theme change.
const magnitudeBandSteps = 40

// networkMagnitudeBandLo is the palette position the weight ramp starts at:
// the first one whose contrast against the drawing's background reaches the
// ordinary edge stroke's.
//
// The rule it enforces is that **no weighted edge is less visible than an
// unweighted one**. A sequential palette runs from one end of the lightness
// range to the other, so on a dark surface its low end sinks into the
// background — and an edge that carries a small but *known* weight would then
// be harder to see than one carrying no weight at all, which is backwards.
// (Measured against the dark theme's panel, the default stroke sits at 4.55:1
// and Batlow only reaches that around t=0.5, so half the ramp is unusable.)
//
// Derived rather than pinned as a constant because both ends of the comparison
// are theme tokens: under a light theme the palette's dark end is the visible
// one and the floor lands elsewhere. The icicle's flame band (ADR-0160) solves
// the same problem with fixed bounds, which it can because it owns its plot
// surface; this ramp is drawn on whatever surface the style carries.
//
// No ceiling: the top of the ramp is the most visible colour available, which
// is exactly what the heaviest edge should be.
func networkMagnitudeBandLo(palette styletokens.SequentialE, bg styletokens.RGBA8, base styletokens.RGBA8) float32 {
	want := contrast.Ratio(base.R, base.G, base.B, bg.R, bg.G, bg.B)
	for i := range magnitudeBandSteps {
		t := float32(i) / float32(magnitudeBandSteps)
		s := styletokens.Sequential(palette, t)
		if contrast.Ratio(s.R, s.G, s.B, bg.R, bg.G, bg.B) >= want {
			return t
		}
	}
	// Nothing in the ramp reaches it. Fall back to the whole range rather than
	// collapsing to a single colour: a less legible ordering still orders.
	return 0
}

// networkNodeRamp samples the magnitude ramp for one node weight. Shared by
// the fill and the ink so the two cannot drift onto different colours, and it
// carries the same square root the edge width and the edge ramp use.
func networkNodeRamp(palette styletokens.SequentialE, bandLo float32, w float64, maxW float64) styletokens.RGBA8 {
	t := float32(math.Sqrt(min(w, maxW) / maxW))
	return styletokens.Sequential(palette, bandLo+(1-bandLo)*t)
}

// networkInkOn picks the label colour for a ramped node body: whichever of the
// style's own ink and the dark extreme contrasts better with the fill.
//
// The group and tone palettes are all *Subtle background tones, chosen dark so
// the one light ink reads on every one of them — a fixed pairing that works
// because the palette is fixed. A magnitude ramp is not: it sweeps the whole
// lightness range by construction, so its bright end would carry light ink on
// a light fill. The view offers NodeText beside NodeFill for exactly this, and
// choosing by measured contrast is what keeps the pairing honest as either the
// palette or the theme moves.
func networkInkOn(fill styletokens.RGBA8) styletokens.RGBA8 {
	light, dark := styletokens.NeutralTextPrimary, styletokens.NeutralBgExtreme
	lr := contrast.Ratio(light.R, light.G, light.B, fill.R, fill.G, fill.B)
	dr := contrast.Ratio(dark.R, dark.G, dark.B, fill.R, fill.G, fill.B)
	if dr > lr {
		return dark
	}
	return light
}

// networkTone maps a `tone` cell to a design-system colour. The vocabulary is
// the six semantic families — accent, info, success, warning, error, neutral —
// and the *role* picks the variant: a vertex body is a background, so it takes
// the Subtle tone the group palette also uses; an edge is a foreground stroke,
// where a subtle background tone would be invisible, so it takes Default.
// Anything else (including an empty cell) returns ok=false, leaving the group
// palette or the style default in charge — an unknown tone must not blank a
// node.
//
// Naming a family rather than a colour is what keeps ADR-0156's palette
// decision in one place: the query says what a vertex *means*, the design
// system says what that looks like.
func networkTone(s string, foreground bool) (col color.Color, ok bool) {
	var subtle, def styletokens.RGBA8
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "accent":
		subtle, def = styletokens.AccentSubtle, styletokens.AccentDefault
	case "info":
		subtle, def = styletokens.InfoSubtle, styletokens.InfoDefault
	case "success":
		subtle, def = styletokens.SuccessSubtle, styletokens.SuccessDefault
	case "warning":
		subtle, def = styletokens.WarningSubtle, styletokens.WarningDefault
	case "error":
		subtle, def = styletokens.ErrorSubtle, styletokens.ErrorDefault
	case "neutral":
		subtle, def = styletokens.NeutralSubtle, styletokens.NeutralDefault
	default:
		return
	}
	if foreground {
		return color.Hex(def.AsHex()), true
	}
	return color.Hex(subtle.AsHex()), true
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
	srcCol, tgtCol, labelCol, toneCol, weightCol int
}

type networkVerticesClaim struct {
	idCol, labelCol, groupCol, shapeCol, toneCol, weightCol int
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

	// selectedID highlights the last-clicked node, and is published as the
	// `selection_key` signal so a query can follow the click.
	//
	// The row-index `selection` signal stays unpublished, for the reason
	// ADR-0129 §SD4 records: the graph's vertices come from a private lane,
	// not an observable split node, so a cursor emit is clamped away
	// (syncSelectionClamp sends a cursor on an unbound node home) and would
	// jerk the other panels to row 0. A vertex id is a *value*, not a cursor
	// — nothing in play reads `selection_key`, so publishing it moves no
	// other panel — which is why it can cross the same boundary the cursor
	// cannot. The observe/bind direction (§SD7) remains the route to a real
	// shared cursor.
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

// Render draws the graph. A vertex click publishes `selection_key` (the
// clicked vertex id); the row-index `selection` stays unpublished — see
// NetworkDriver.selectedID for why the two differ.
func (inst layeredGraphPanel) Render(filled map[ChannelID]ChannelResult, emit SignalEmitterI) {
	edges, ok := filled[chEdges]
	if !ok {
		return
	}
	ec, ok := edges.Claim.(networkEdgesClaim)
	if !ok {
		return
	}
	vc := networkVerticesClaim{idCol: -1, labelCol: -1, groupCol: -1, shapeCol: -1, toneCol: -1, weightCol: -1}
	var vertRec arrow.RecordBatch
	if v, has := filled[chVertices]; has {
		if got, isC := v.Claim.(networkVerticesClaim); isC {
			vc = got
			vertRec = v.Rec
		}
	}
	inst.driver.render(edges.Rec, ec, vertRec, vc, emit)
}

// resolveNetworkEdges applies the §SD2 edge contract to a schema. Pure and
// schema-only; source/target are read through formatCell (total over Arrow
// types), so they carry no type requirement — a numeric id is a fine key.
func resolveNetworkEdges(schema *arrow.Schema) (ec networkEdgesClaim, reason string) {
	ec = networkEdgesClaim{srcCol: -1, tgtCol: -1, labelCol: -1, toneCol: -1, weightCol: -1}
	for ci, f := range schema.Fields() {
		switch f.Name {
		case networkSourceCol:
			ec.srcCol = ci
		case networkTargetCol:
			ec.tgtCol = ci
		case networkLabelCol:
			ec.labelCol = ci
		case networkToneCol:
			ec.toneCol = ci
		case networkWeightCol:
			// Claimed only when it can carry a quantity. A `weight` that is
			// not numeric is far more likely to be a column that happens to
			// share the name than a magnitude the author meant, and silently
			// widening every edge off a parsed string would be the worse
			// failure. Left unclaimed, it stays an ordinary result column.
			if isNumericType(f.Type) {
				ec.weightCol = ci
			}
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
			"`vertices` CTE (`id`, `label`, `group`, `shape`, `tone`) to decorate the nodes.",
			strings.Join(missing, " and a "))
	}
	return
}

// resolveNetworkVertices applies the §SD2 vertex contract. Only `id` is
// required; a vertices CTE missing it is rejected, and because the channel is
// optional the panel simply draws from the edges alone (endpoint inference).
func resolveNetworkVertices(schema *arrow.Schema) (vc networkVerticesClaim, reason string) {
	vc = networkVerticesClaim{idCol: -1, labelCol: -1, groupCol: -1, shapeCol: -1, toneCol: -1, weightCol: -1}
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
		case networkToneCol:
			vc.toneCol = ci
		case networkWeightCol:
			// Numeric-only, for the same reason the edge contract is.
			if isNumericType(f.Type) {
				vc.weightCol = ci
			}
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
	fillOf map[string]color.Color // vertex id → tone or group fill (absent → default)
	// strokeOf colours an edge by its endpoints, the key view.RenderOpts'
	// EdgeStroke hook is given. Only edges naming a tone appear.
	strokeOf map[[2]string]color.Color
	// maxWeight / maxNodeWeight are the heaviest edge and vertex weights
	// seen, or 0 when that side carries no `weight` column or nothing
	// positive in it. They are what the magnitude channels normalise against
	// (ADR-0167 §SD5) — the panel sees the whole result, where a book would
	// have to compute this in SQL and restate it per query. Kept apart
	// because the two are different quantities: an edge's cost and a node's
	// need not even share a unit.
	maxWeight     float64
	maxNodeWeight float64
	capped        bool
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
			if vc.weightCol >= 0 {
				if v, ok := quantityCellValue(vertRec, vc.weightCol, row); ok && v > 0 {
					node.Weight = v
					b.maxNodeWeight = max(b.maxNodeWeight, v)
				}
			}
			nodes = append(nodes, node)
			// An explicit tone wins over the group palette: `group` says
			// "these belong together", `tone` says "this one means something",
			// and a query that bothers to name a meaning meant it.
			toned := false
			if vc.toneCol >= 0 {
				if col, ok := networkTone(formatCell(vertRec, vc.toneCol, row), false); ok {
					b.fillOf[id] = col
					toned = true
				}
			}
			if !toned && vc.groupCol >= 0 {
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
			if ec.toneCol >= 0 {
				if col, ok := networkTone(formatCell(edgesRec, ec.toneCol, row), true); ok {
					if b.strokeOf == nil {
						b.strokeOf = make(map[[2]string]color.Color, 8)
					}
					b.strokeOf[key] = col
				}
			}
			if ec.weightCol >= 0 {
				// A non-positive or unreadable cell leaves Weight at 0, which
				// the widget reads as *unknown* and draws as an ordinary edge
				// (ADR-0167 §SD2).
				if v, ok := quantityCellValue(edgesRec, ec.weightCol, row); ok && v > 0 {
					e.Weight = v
					b.maxWeight = max(b.maxWeight, v)
				}
			}
			edges = append(edges, e)
		}
	}
	b.model = layeredgraph.GraphModel{Nodes: nodes, Edges: edges}
	return
}

// render maps the two results into a graph, lays it out (cached), draws it, and
// tracks the locally-selected node.
func (inst *NetworkDriver) render(edgesRec arrow.RecordBatch, ec networkEdgesClaim, vertRec arrow.RecordBatch, vc networkVerticesClaim, emit SignalEmitterI) {
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
			opts := layeredgraph.LayoutOpts{RankDir: inst.rankDir, FontSize: 13}
			if b.maxNodeWeight > 0 {
				// A vertex weight scales the font its label is laid out at, so
				// the box follows (ADR-0167 §SD3). The floor is this panel's
				// own FontSize rather than the package default, so an
				// unweighted-looking node stays the size it always was.
				opts.NodeFontSize = layeredgraph.WeightFontSize(b.model, 13, 0)
			}
			inst.layout, err = eng.Layout(context.Background(), b.model, opts)
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
	// it contends with nobody, unlike the single CaptureAvailableSize register
	// that the frame's last capture wins; captureUiAvailableRect is the same
	// slot without the separator, height included).
	// Height follows the layout's aspect (clamped); the tab scrolls
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

	seqPalette := styletokens.SequentialDefault()
	style := view.DefaultStyle()
	bandLo := networkMagnitudeBandLo(seqPalette, styletokens.NeutralBgPanel, styletokens.NeutralBorderDefault)

	// A vertex `weight` ramps the node body the same way it ramps an edge, and
	// loses to the same two more-specific claims: the selection highlight, and
	// an explicit tone or group.
	nodeWeights := make(map[string]float64, len(b.model.Nodes))
	if b.maxNodeWeight > 0 {
		for _, n := range b.model.Nodes {
			nodeWeights[n.ID] = n.Weight
		}
	}
	fill := func(id string) (col color.Color, ok bool) {
		if inst.selectedID != "" && id == inst.selectedID {
			return color.Hex(styletokens.AccentDefault.AsHex()), true
		}
		if col, ok = b.fillOf[id]; ok {
			return
		}
		if b.maxNodeWeight <= 0 {
			return
		}
		w, found := nodeWeights[id]
		if !found || w <= 0 {
			return
		}
		return color.Hex(networkNodeRamp(seqPalette, bandLo, w, b.maxNodeWeight).AsHex()), true
	}
	// Ink follows the fill, and only for the nodes the ramp actually painted:
	// everything else keeps the style default, which the tone and group
	// palettes were chosen against.
	nodeInk := func(id string) (col color.Color, ok bool) {
		if b.maxNodeWeight <= 0 || inst.selectedID == id {
			return
		}
		if _, toned := b.fillOf[id]; toned {
			return
		}
		w, found := nodeWeights[id]
		if !found || w <= 0 {
			return
		}
		return color.Hex(networkInkOn(networkNodeRamp(seqPalette, bandLo, w, b.maxNodeWeight)).AsHex()), true
	}
	// Edges carrying a `tone` are stroked with it; the rest keep the style
	// default. Nothing overrides the selection highlight, which is a fill.
	//
	// A `weight` adds the magnitude channels over the top (ADR-0167 §SD4):
	// width from the shared mapping, and a sequential ramp sampled at the SAME
	// normalised position so the two never disagree — a reader seeing a thick
	// pale edge would have to decide which channel to believe. An explicit
	// tone still wins the colour, being the more specific claim; it does not
	// touch the width, so a toned edge still carries its magnitude.
	weighted := b.maxWeight > 0
	var edgeWidth func(from, to string, weight float64) (float32, bool)
	var edgeWeights map[[2]string]float64
	if weighted {
		edgeWidth = view.WeightWidth(inst.layout, 0, 0)
		// The colour hook is keyed by endpoints only, so it needs the weight
		// looked up — off the layout, which carries it through for exactly
		// this reason, rather than off a second copy in the build.
		edgeWeights = make(map[[2]string]float64, len(inst.layout.Edges))
		for _, e := range inst.layout.Edges {
			edgeWeights[[2]string{e.From, e.To}] = e.Weight
		}
	}
	stroke := func(from, to string) (col color.Color, ok bool) {
		key := [2]string{from, to}
		if b.strokeOf != nil {
			if col, ok = b.strokeOf[key]; ok {
				return
			}
		}
		if !weighted {
			return
		}
		w, found := edgeWeights[key]
		if !found || w <= 0 {
			return
		}
		// Same square root as the width, so the channels stay in step, then
		// mapped onto the legible part of the ramp.
		t := float32(math.Sqrt(min(w, b.maxWeight) / b.maxWeight))
		return color.Hex(styletokens.Sequential(seqPalette, bandLo+(1-bandLo)*t).AsHex()), true
	}
	res := view.Render(networkIDSalt+inst.idSeed, inst.layout, view.RenderOpts{
		Style:      style,
		CanvasW:    w,
		CanvasH:    h,
		NodeFill:   fill,
		NodeText:   nodeInk,
		EdgeStroke: stroke,
		EdgeWidth:  edgeWidth,
		State:      &inst.view,
	})
	// A vertex click highlights it and publishes the id as `selection_key`;
	// clicking the highlighted node again clears both. The empty string is
	// the honest "nothing focused" value — a query reading
	// `{selection_key:String}` sees the same state it started in.
	if res.Clicked != "" {
		if inst.selectedID == res.Clicked {
			inst.selectedID = ""
		} else {
			inst.selectedID = res.Clicked
		}
		if emit != nil {
			emit.Emit(signalSelectionKey, inst.selectedID)
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
		selector.Segmented(inst.ids, "rank-dir", &inst.rankDir).
			Inline().
			Style(selector.StyleSelectable).
			Option(layeredgraph.RankDirTopBottom, "top-down").
			Option(layeredgraph.RankDirLeftRight, "left-right").
			SendResp()
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
	// A node's weight scales its font and therefore its box, so it is part of
	// the layout's identity (ADR-0167 §SD3). An EDGE's weight deliberately is
	// not: it never reaches the engine, so including it would only buy
	// needless re-layouts.
	for _, n := range m.Nodes {
		fmt.Fprintf(h, "n|%s|%s|%d|%v;", n.ID, n.Label, n.Shape, n.Weight)
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
		Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
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
		Params: resolveSignalNamesWithDefaults(node.Reads, inst.lastRunBound, inst.frameSig),
	})
	d.verticesLoading = v.loading
	d.verticesErr = v.err
	return v.rec, v.schema
}
