package play

import (
	"fmt"
	"math"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/keelson/designsystem/colors/contrast"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// play_network_model.go owns the graph contract — the `edges` and `vertices`
// CTEs of ADR-0129 §SD2 — and maps a pair of results to a renderer-neutral
// model both graph panels draw from: the Network tab's layered drawing
// (play_layeredgraph_panel.go) and the Graphview tab's live one
// (play_graphview_panel.go). ADR-0225 §SD1 hoisted it here for the reason
// ADR-0166 §SD1 hoisted the hierarchy contract: two resolvers over one
// contract drift on the first column added to either.
//
// The model carries the contract's cells AS DECLARED — `group`, `shape` and
// `tone` as the strings the query wrote — rather than as resolved colours,
// because the two renderers spend them differently: a `group` is a node fill
// in both panels and an aura in the live one, and `shape` is a layered
// vocabulary the live panel has no circle-free way to honour. What IS resolved
// here is what the contract means: tone-over-group precedence, the palette
// order, and the magnitude ramp of ADR-0167, so the same query reads the same
// way in both tabs.

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

	// donut / donutTotal are the live panel's per-node breakdown (ADR-0225
	// §SD5): a list of numbers drawn as a ring of proportional slices around
	// the vertex, and an optional total that leaves the remainder as a muted
	// track, which turns the ring into a share or progress display. Read by
	// the Graphview tab alone — a layered node is a labelled box with no
	// room for one — and claimed only when the column really is a list of
	// numbers.
	networkDonutCol      = "donut"
	networkDonutTotalCol = "donut_total"

	// networkEdgesNodeID / networkVerticesNodeID are the CTEs the two channels
	// bind to (§SD1). Nodes of the user's own split graph, demanded on their
	// own lanes — not panel-authored queries.
	networkEdgesNodeID    NodeID = "edges"
	networkVerticesNodeID NodeID = "vertices"
)

// netCaps bounds one build. The ceiling is the CALLER'S because the two panels
// have different reasons for having one (ADR-0225 §SD9): a Graphviz-WASM
// layered run is a tens-to-low-hundreds instrument, while a force simulation is
// bounded by the frame it steps in. Excess is dropped and counted rather than
// silently truncated — both panels report it in their status line.
type netCaps struct {
	vertices int
	edges    int
}

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

// networkMagnitudeRamp samples the magnitude ramp for one weight — a node's or
// an edge's. Shared by every channel that spends one, so a fill, an ink and a
// stroke cannot drift onto different colours, and it carries the same square
// root the edge and node widths use (ADR-0167 §SD4).
func networkMagnitudeRamp(palette styletokens.SequentialE, bandLo float32, w float64, maxW float64) styletokens.RGBA8 {
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

// networkEdgesClaim / networkVerticesClaim are the resolved column indices a
// channel's schema yields in AcceptForChannel and Render consumes. -1 marks an
// absent optional column.
type networkEdgesClaim struct {
	srcCol, tgtCol, labelCol, toneCol, weightCol int
}

type networkVerticesClaim struct {
	idCol, labelCol, groupCol, shapeCol, toneCol, weightCol int
	// donutCol / donutTotalCol are spent by the live panel alone (§SD5); the
	// layered panel resolves them and ignores them, so one claim serves both.
	donutCol, donutTotalCol int
}

// acceptGraphChannel is the contract's acceptance, shared by both graph panels
// (ADR-0225 §SD1): schema-only and cheap enough to run every frame, because the
// question is about column names and the schema answers it on its own. The
// reject messages name the contract rather than either drawing, so the two tabs
// say the same thing about the same query.
func acceptGraphChannel(ch ChannelID, schema *arrow.Schema) (claim ChannelClaim, reason string) {
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
	vc = networkVerticesClaim{idCol: -1, labelCol: -1, groupCol: -1, shapeCol: -1, toneCol: -1, weightCol: -1,
		donutCol: -1, donutTotalCol: -1}
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
		case networkDonutCol:
			// A list of numbers or nothing (§SD5): a scalar column that
			// happens to be called `donut` is a name collision, not a ring.
			if netIsNumericList(f.Type) {
				vc.donutCol = ci
			}
		case networkDonutTotalCol:
			if isNumericType(f.Type) {
				vc.donutTotalCol = ci
			}
		}
	}
	if vc.idCol < 0 {
		reason = "the `vertices` CTE needs an `id` column"
	}
	return
}

// netVertex is one vertex of the contract as the query declared it. Group,
// Shape and Tone are the raw cells: what they LOOK like is the drawing panel's
// question, what they MEAN is this contract's.
type netVertex struct {
	ID     string
	Label  string
	Group  string
	Shape  string
	Tone   string
	Weight float64 // 0 is *unknown*, not zero
	// Donut / DonutTotal carry the optional ring (§SD5). float32 because that
	// is what the widget that draws it takes; nil draws none.
	Donut      []float32
	DonutTotal float32
}

// netEdge is one directed edge of the contract as declared.
type netEdge struct {
	From, To string
	Label    string
	Tone     string
	Weight   float64 // 0 is *unknown*, not zero
}

// netModel is a pair of results as one renderer-neutral graph (ADR-0225 §SD1).
type netModel struct {
	Vertices []netVertex
	Edges    []netEdge
	// groupIdx is the palette position of each distinct `group`, in the order
	// the vertices first named them — shared so one query colours the same in
	// both panels, and the aura order of the live one (§SD4).
	//
	// Assigned for EVERY named group, whether or not the vertex that named it
	// also carried a tone that wins the fill: a group's identity is not a
	// property of whether one of its members meant something else as well.
	groupIdx map[string]int
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

// vertexFill resolves a vertex body colour. An explicit tone wins over the
// group palette: `group` says "these belong together", `tone` says "this one
// means something", and a query that bothers to name a meaning meant it. An
// UNRECOGNISED tone falls through to the group rather than blanking the node.
// ok=false leaves the renderer's own default in charge.
func (inst *netModel) vertexFill(v netVertex) (col color.Color, ok bool) {
	if v.Tone != "" {
		if col, ok = networkTone(v.Tone, false); ok {
			return
		}
	}
	if v.Group == "" {
		return
	}
	idx, has := inst.groupIdx[v.Group]
	if !has {
		return
	}
	return networkGroupColor(idx), true
}

// edgeStroke resolves an edge's tone colour — a foreground stroke, so the
// Default variant rather than the Subtle one a node body takes. ok=false keeps
// the style default.
func (inst *netModel) edgeStroke(e netEdge) (col color.Color, ok bool) {
	if e.Tone == "" {
		return
	}
	return networkTone(e.Tone, true)
}

// groups lists the distinct `group` values in the order the vertices first
// named them: the palette order of both panels and the aura order of the live
// one, so a legend reads the same way twice.
func (inst *netModel) groups() (out []string) {
	if len(inst.groupIdx) == 0 {
		return
	}
	out = make([]string, len(inst.groupIdx))
	for g, idx := range inst.groupIdx {
		out[idx] = g
	}
	return
}

// buildNetModel maps the edges/vertices records to the neutral model (§SD2):
// vertices are de-duplicated by id, an edge endpoint with no vertices row
// synthesises one (so a partial or absent `vertices` CTE still draws every
// edge), parallel (source,target) pairs collapse, and both inputs are capped at
// the CALLER'S caps (ADR-0225 §SD9). Vertex ids must be unique — both widgets'
// invariant — and the dedup enforces it. Deterministic given the records, so
// the layout key of one panel and the interned ids of the other are stable
// frame to frame.
func buildNetModel(edgesRec arrow.RecordBatch, ec networkEdgesClaim, vertRec arrow.RecordBatch, vc networkVerticesClaim, caps netCaps) (m netModel) {
	m.groupIdx = make(map[string]int, 8)
	verts := make([]netVertex, 0, 64)
	seen := make(map[string]struct{}, 64)

	noteGroup := func(g string) {
		if g == "" {
			return
		}
		if _, ok := m.groupIdx[g]; !ok {
			m.groupIdx[g] = len(m.groupIdx)
		}
	}

	// addSynth adds an edge endpoint with no vertices row; false means the
	// vertex cap is reached, so the caller must drop the edge rather than leave
	// it referencing a vertex the model does not contain.
	addSynth := func(id string) bool {
		if _, ok := seen[id]; ok {
			return true
		}
		if len(verts) >= caps.vertices {
			return false
		}
		seen[id] = struct{}{}
		verts = append(verts, netVertex{ID: id, Label: id})
		return true
	}

	if vertRec != nil && vc.idCol >= 0 {
		var donut []float32
		rows := vertRec.NumRows()
		for row := range rows {
			if len(verts) >= caps.vertices {
				m.capped = true
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
			v := netVertex{ID: id, Label: id}
			if vc.labelCol >= 0 {
				if l := formatCell(vertRec, vc.labelCol, row); l != "" {
					v.Label = l
				}
			}
			if vc.shapeCol >= 0 {
				v.Shape = formatCell(vertRec, vc.shapeCol, row)
			}
			if vc.toneCol >= 0 {
				v.Tone = formatCell(vertRec, vc.toneCol, row)
			}
			if vc.groupCol >= 0 {
				v.Group = formatCell(vertRec, vc.groupCol, row)
				noteGroup(v.Group)
			}
			if vc.weightCol >= 0 {
				if w, ok := quantityCellValue(vertRec, vc.weightCol, row); ok && w > 0 {
					v.Weight = w
					m.maxNodeWeight = max(m.maxNodeWeight, w)
				}
			}
			if vc.donutCol >= 0 {
				// The slices are copied out rather than aliased: the scratch
				// buffer is reused for the next row.
				if got, ok := netDonutAt(vertRec.Column(vc.donutCol), int(row), donut[:0]); ok && len(got) > 0 {
					donut = got
					v.Donut = append([]float32(nil), got...)
				}
			}
			if vc.donutTotalCol >= 0 {
				if t, ok := quantityCellValue(vertRec, vc.donutTotalCol, row); ok && t > 0 {
					v.DonutTotal = float32(t)
				}
			}
			verts = append(verts, v)
		}
	}

	edges := make([]netEdge, 0, 64)
	edgeSeen := make(map[[2]string]struct{}, 64)
	if edgesRec != nil {
		rows := edgesRec.NumRows()
		for row := range rows {
			if len(edges) >= caps.edges {
				m.capped = true
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
				m.capped = true
				continue // a dangling endpoint (vertex cap reached) drops the edge
			}
			edgeSeen[key] = struct{}{}
			e := netEdge{From: src, To: tgt}
			if ec.labelCol >= 0 {
				e.Label = formatCell(edgesRec, ec.labelCol, row)
			}
			if ec.toneCol >= 0 {
				e.Tone = formatCell(edgesRec, ec.toneCol, row)
			}
			if ec.weightCol >= 0 {
				// A non-positive or unreadable cell leaves Weight at 0, which
				// both widgets read as *unknown* and draw as an ordinary edge
				// (ADR-0167 §SD2).
				if w, ok := quantityCellValue(edgesRec, ec.weightCol, row); ok && w > 0 {
					e.Weight = w
					m.maxWeight = max(m.maxWeight, w)
				}
			}
			edges = append(edges, e)
		}
	}
	m.Vertices, m.Edges = verts, edges
	return
}

// netIsNumericList reports whether a column can carry a donut: a list of a
// numeric type. A `donut` column that is not one is left unclaimed and stays an
// ordinary result column, for the reason the `weight` contract is numeric-only
// — a name collision is likelier than a ring drawn off parsed text.
func netIsNumericList(dt arrow.DataType) bool {
	var elem arrow.DataType
	switch t := dt.(type) {
	case *arrow.ListType:
		elem = t.Elem()
	case *arrow.LargeListType:
		elem = t.Elem()
	case *arrow.FixedSizeListType:
		elem = t.Elem()
	default:
		return false
	}
	return isNumericType(elem)
}

// netDonutAt appends the non-null elements of the list cell at row to dst.
// Non-positive elements are kept as zeros rather than skipped: a slice's
// position in the list is what pairs it with a colour, and dropping an empty
// share would rotate every share after it onto the wrong one.
//
// ok is false for a null cell or a column that is not list-typed.
func netDonutAt(arr arrow.Array, row int, dst []float32) (out []float32, ok bool) {
	out = dst
	if arr == nil || row < 0 || row >= arr.Len() || arr.IsNull(row) {
		return out, false
	}
	var inner arrow.Array
	var beg, end int64
	switch a := arr.(type) {
	case *array.List:
		beg, end = a.ValueOffsets(row)
		inner = a.ListValues()
	case *array.LargeList:
		beg, end = a.ValueOffsets(row)
		inner = a.ListValues()
	case *array.FixedSizeList:
		beg, end = a.ValueOffsets(row)
		inner = a.ListValues()
	default:
		return out, false
	}
	for i := beg; i < end; i++ {
		if inner.IsNull(int(i)) {
			out = append(out, 0)
			continue
		}
		v, got := numericCellValue(inner, i)
		if !got || v < 0 {
			v = 0
		}
		out = append(out, float32(v))
	}
	return out, true
}
