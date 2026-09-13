package play

import (
	"cmp"
	"fmt"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
	"math"
	"slices"
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
// (play_graphview_panel.go). ADR-0227 §SD1 hoisted it here for the reason
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

	// donut / donutTotal are the live panel's per-node breakdown (ADR-0227
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
// have different reasons for having one (ADR-0227 §SD9): a Graphviz-WASM
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
// noVerticesClaim is the claim of a query without a vertices CTE: every
// column index -1, so the build infers the vertices from the edge endpoints.
func noVerticesClaim() networkVerticesClaim {
	return networkVerticesClaim{idCol: -1, labelCol: -1, groupCol: -1, shapeCol: -1, toneCol: -1, weightCol: -1,
		donutCol: -1, donutTotalCol: -1}
}

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
// (ADR-0227 §SD1): schema-only and cheap enough to run every frame, because the
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

// netModel is a pair of results as one renderer-neutral graph (ADR-0227 §SD1),
// **in columns** (ADR-0232 §SD9).
//
// Every vertex column is parallel to Key, whose **ascending order is the
// model's row order**. That is also `csr.Graph`'s slot order, and — because
// the panel declares in this order — the widget's slot order too, so a metric
// the engine returns indexes the model's rows and the widget's arrays with no
// join written anywhere (ADR-0232 §SD3).
//
// Group, Shape and Tone are the raw cells: what they LOOK like is the drawing
// panel's question, what they MEAN is this contract's.
type netModel struct {
	// Vertex columns, parallel, in ascending Key order.
	Key    []uint64 // the interned id: the widget's node id and the sort key
	ID     []string // the id as the query declared it
	Label  []string
	Group  []string
	Shape  []string
	Tone   []string
	Weight []float64 // 0 is *unknown*, not zero
	// Donut is ragged, in the list layout the widget's columnar declaration
	// takes (ADR-0232 §SD2): vertex i's slices are
	// DonutValues[DonutStart[i]:DonutStart[i+1]]. DonutStart has one entry
	// more than the vertex count, or is nil when no vertex carries a ring.
	DonutStart  []int32
	DonutValues []float32
	DonutTotal  []float32

	// Edge columns, parallel, in declaration order. From and To are interned
	// keys; FromID and ToID keep the declared spelling for the renderers that
	// address a node by string.
	From       []uint64
	To         []uint64
	FromID     []string
	ToID       []string
	EdgeLabel  []string
	EdgeTone   []string
	EdgeWeight []float64 // 0 is *unknown*, not zero

	// names resolves a key back to its declared id, for the id a click
	// publishes (ADR-0227 §SD7).
	names *netIds
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
func (inst *netModel) vertexFill(i int) (col color.Color, ok bool) {
	if tone := inst.Tone[i]; tone != "" {
		if col, ok = networkTone(tone, false); ok {
			return
		}
	}
	g := inst.Group[i]
	if g == "" {
		return
	}
	idx, has := inst.groupIdx[g]
	if !has {
		return
	}
	return networkGroupColor(idx), true
}

// NumVertices and NumEdges size the two column groups.
func (inst *netModel) NumVertices() int { return len(inst.Key) }
func (inst *netModel) NumEdges() int    { return len(inst.From) }

// donutAt is vertex i's ring slices, or nil where it declared none.
func (inst *netModel) donutAt(i int) []float32 {
	if inst.DonutStart == nil {
		return nil
	}
	return inst.DonutValues[inst.DonutStart[i]:inst.DonutStart[i+1]]
}

// edgeStroke resolves an edge's tone colour — a foreground stroke, so the
// Default variant rather than the Subtle one a node body takes. ok=false keeps
// the style default.
func (inst *netModel) edgeStroke(i int) (col color.Color, ok bool) {
	tone := inst.EdgeTone[i]
	if tone == "" {
		return
	}
	return networkTone(tone, true)
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
// the CALLER'S caps (ADR-0227 §SD9). Vertex ids must be unique — both widgets'
// invariant — and the dedup enforces it. Deterministic given the records, so
// the layout key of one panel and the interned ids of the other are stable
// frame to frame.
func buildNetModel(edgesRec arrow.RecordBatch, ec networkEdgesClaim, vertRec arrow.RecordBatch, vc networkVerticesClaim, caps netCaps) (m netModel) {
	m.groupIdx = make(map[string]int, 8)
	m.names = newNetIds(64)
	seen := make(map[string]int, 64) // declared id -> row while building

	noteGroup := func(g string) {
		if g == "" {
			return
		}
		if _, ok := m.groupIdx[g]; !ok {
			m.groupIdx[g] = len(m.groupIdx)
		}
	}

	// appendVertex appends one row to every vertex column, so the columns stay
	// parallel by construction rather than by each caller remembering to.
	appendVertex := func(id, label, group, shape, tone string, weight float64,
		donut []float32, donutTotal float32) {
		seen[id] = len(m.Key)
		m.Key = append(m.Key, m.names.intern(id))
		m.ID = append(m.ID, id)
		m.Label = append(m.Label, label)
		m.Group = append(m.Group, group)
		m.Shape = append(m.Shape, shape)
		m.Tone = append(m.Tone, tone)
		m.Weight = append(m.Weight, weight)
		m.DonutTotal = append(m.DonutTotal, donutTotal)
		m.DonutValues = append(m.DonutValues, donut...)
		m.DonutStart = append(m.DonutStart, int32(len(m.DonutValues)))
	}

	// addSynth adds an edge endpoint with no vertices row; false means the
	// vertex cap is reached, so the caller must drop the edge rather than leave
	// it referencing a vertex the model does not contain.
	addSynth := func(id string) bool {
		if _, ok := seen[id]; ok {
			return true
		}
		if len(m.Key) >= caps.vertices {
			return false
		}
		appendVertex(id, id, "", "", "", 0, nil, 0)
		return true
	}

	// DonutStart is offsets with a leading zero (the list layout of ADR-0232
	// §SD2); it is dropped again below when no vertex declared a ring, so the
	// common case carries no offsets slice at all.
	m.DonutStart = append(m.DonutStart, 0)

	if vertRec != nil && vc.idCol >= 0 {
		var donut []float32
		rows := vertRec.NumRows()
		for row := range rows {
			if len(m.Key) >= caps.vertices {
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
			label := id
			if vc.labelCol >= 0 {
				if l := formatCell(vertRec, vc.labelCol, row); l != "" {
					label = l
				}
			}
			var shape, tone, group string
			if vc.shapeCol >= 0 {
				shape = formatCell(vertRec, vc.shapeCol, row)
			}
			if vc.toneCol >= 0 {
				tone = formatCell(vertRec, vc.toneCol, row)
			}
			if vc.groupCol >= 0 {
				group = formatCell(vertRec, vc.groupCol, row)
				noteGroup(group)
			}
			var weight float64
			if vc.weightCol >= 0 {
				if w, ok := quantityCellValue(vertRec, vc.weightCol, row); ok && w > 0 {
					weight = w
					m.maxNodeWeight = max(m.maxNodeWeight, w)
				}
			}
			var ring []float32
			if vc.donutCol >= 0 {
				// The scratch buffer is reused for the next row; the values
				// are copied into the model's own column by appendVertex.
				if got, ok := netDonutAt(vertRec.Column(vc.donutCol), int(row), donut[:0]); ok && len(got) > 0 {
					donut = got
					ring = got
				}
			}
			var total float32
			if vc.donutTotalCol >= 0 {
				if t, ok := quantityCellValue(vertRec, vc.donutTotalCol, row); ok && t > 0 {
					total = float32(t)
				}
			}
			appendVertex(id, label, group, shape, tone, weight, ring, total)
		}
	}

	edgeSeen := make(map[[2]string]struct{}, 64)
	if edgesRec != nil {
		rows := edgesRec.NumRows()
		for row := range rows {
			if m.NumEdges() >= caps.edges {
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
			var label, tone string
			if ec.labelCol >= 0 {
				label = formatCell(edgesRec, ec.labelCol, row)
			}
			if ec.toneCol >= 0 {
				tone = formatCell(edgesRec, ec.toneCol, row)
			}
			var weight float64
			if ec.weightCol >= 0 {
				// A non-positive or unreadable cell leaves the weight at 0,
				// which both widgets read as *unknown* and draw as an ordinary
				// edge (ADR-0167 §SD2).
				if w, ok := quantityCellValue(edgesRec, ec.weightCol, row); ok && w > 0 {
					weight = w
					m.maxWeight = max(m.maxWeight, w)
				}
			}
			m.From = append(m.From, m.names.intern(src))
			m.To = append(m.To, m.names.intern(tgt))
			m.FromID = append(m.FromID, src)
			m.ToID = append(m.ToID, tgt)
			m.EdgeLabel = append(m.EdgeLabel, label)
			m.EdgeTone = append(m.EdgeTone, tone)
			m.EdgeWeight = append(m.EdgeWeight, weight)
		}
	}
	if len(m.DonutValues) == 0 {
		m.DonutStart = nil // no vertex declared a ring
	}
	m.sortByKey()
	return
}

// sortByKey puts the vertex columns in ascending interned-id order, which is
// what makes the model's row order the CSR's slot order and the widget's
// (ADR-0232 §SD9). Edge columns are untouched: they address vertices by key,
// not by row.
//
// The permutation is applied by building each column afresh rather than by
// swapping in place, because the ragged donut column cannot be swapped
// elementwise and a second shape for it would be the bug this whole change
// exists to avoid.
func (inst *netModel) sortByKey() {
	n := inst.NumVertices()
	perm := make([]int32, n)
	for i := range perm {
		perm[i] = int32(i)
	}
	slices.SortFunc(perm, func(a, b int32) int {
		return cmp.Compare(inst.Key[a], inst.Key[b])
	})
	sorted := true
	for i, p := range perm {
		if int(p) != i {
			sorted = false
			break
		}
	}
	if sorted {
		return // already ascending: the common case of a query that ORDERed by id
	}
	inst.Key = permuteSlice(inst.Key, perm)
	inst.ID = permuteSlice(inst.ID, perm)
	inst.Label = permuteSlice(inst.Label, perm)
	inst.Group = permuteSlice(inst.Group, perm)
	inst.Shape = permuteSlice(inst.Shape, perm)
	inst.Tone = permuteSlice(inst.Tone, perm)
	inst.Weight = permuteSlice(inst.Weight, perm)
	inst.DonutTotal = permuteSlice(inst.DonutTotal, perm)
	if inst.DonutStart == nil {
		return
	}
	start := make([]int32, 0, n+1)
	values := make([]float32, 0, len(inst.DonutValues))
	start = append(start, 0)
	for _, p := range perm {
		values = append(values, inst.DonutValues[inst.DonutStart[p]:inst.DonutStart[p+1]]...)
		start = append(start, int32(len(values)))
	}
	inst.DonutStart, inst.DonutValues = start, values
}

// permuteSlice returns src reordered by perm.
func permuteSlice[T any](src []T, perm []int32) (out []T) {
	out = make([]T, len(perm))
	for i, p := range perm {
		out[i] = src[p]
	}
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

// graphChannelInputs is what the two graph panels' Render methods read off
// the filled channels: the edges batch and claim, and the vertices batch and
// claim when the query has a vertices CTE, else noVerticesClaim.
type graphChannelInputs struct {
	edges    arrow.RecordBatch
	ec       networkEdgesClaim
	vertices arrow.RecordBatch
	vc       networkVerticesClaim
	// opts is the Graphview tab's settings row (ADR-0231 §SD5). The layered
	// panel ignores it, the way it ignores `donut` — one contract, two panels
	// honouring different subsets (ADR-0227 §SD1).
	opts arrow.RecordBatch
	gc   networkGraphOptsClaim
}

func graphChannelsToClaims(filled map[ChannelID]ChannelResult) (in graphChannelInputs, ok bool) {
	edges, has := filled[chEdges]
	if !has {
		return
	}
	in.ec, ok = edges.Claim.(networkEdgesClaim)
	if !ok {
		return
	}
	in.edges = edges.Rec
	in.vc = noVerticesClaim()
	if v, has := filled[chVertices]; has {
		if got, isC := v.Claim.(networkVerticesClaim); isC {
			in.vc = got
			in.vertices = v.Rec
		}
	}
	in.gc = noGraphOptsClaim()
	if o, has := filled[chGraphOpts]; has {
		if got, isC := o.Claim.(networkGraphOptsClaim); isC {
			in.gc = got
			in.opts = o.Rec
		}
	}
	return
}

// graphviewFrozen is the freeze rule the live panels share: a layout that
// has run its step budget without settling is held, so a graph that never
// converges stops costing a step per frame. Read after the Render, so the
// verdict is the frame just drawn and takes effect on the next one.
func graphviewFrozen(v *graphview.View, steps uint64) bool {
	m := v.Metrics()
	return m.Steps >= steps && !v.IsSettled()
}

// graphviewSettleStatus is the simulation readout the live panels share: the
// hold, the freeze, the rest, or the motion.
func graphviewSettleStatus(v *graphview.View, paused, frozen bool) string {
	m := v.Metrics()
	switch {
	case paused:
		return " · paused"
	case frozen:
		return fmt.Sprintf(" · frozen after %d steps, still moving (%.3f) — settle or re-lay-out",
			m.Steps, m.LastDisplacement)
	case v.IsSettled():
		return " · settled"
	case m.Steps > 0:
		return fmt.Sprintf(" · settling (%.3f)", m.LastDisplacement)
	}
	return ""
}
