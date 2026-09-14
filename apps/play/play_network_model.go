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

	// Seam A (ADR-0231 §SD2): placement, emphasis and state. Every one is
	// optional and claimed on name AND type, and a NULL cell is "not declared
	// for this row" — which is how a query pins some vertices and leaves the
	// rest to the layout, and which reaches the widget as the NaN of
	// ADR-0232 §SD4. Because NULL is the unset value a DECLARED ZERO IS A
	// ZERO: `opacity = 0` paints nothing, `strength = 0` is an edge that is
	// drawn and does not pull — neither of which the row-shaped spec could
	// say.
	networkOpacityCol     = "opacity"
	networkPickCol        = "pick"
	networkSelectedCol    = "selected"
	networkFitCol         = "fit"
	networkRadiusCol      = "radius"
	networkLabelAlwaysCol = "label_always"
	networkPinXCol        = "pin_x"
	networkPinYCol        = "pin_y"
	networkLatCol         = "lat"
	networkLonCol         = "lon"
	networkStartXCol      = "start_x"
	networkStartYCol      = "start_y"
	networkPullXCol       = "pull_x"
	networkPullYCol       = "pull_y"
	networkPullSCol       = "pull_strength"
	networkPullSXCol      = "pull_strength_x"
	networkPullSYCol      = "pull_strength_y"
	networkGroupsCol      = "groups"
	networkDonutTonesCol  = "donut_tones"
	networkCenterCol      = "center"

	// Edge-only additions. `id` tells parallel edges apart in hover,
	// selection and events; `length` and `strength` are the force step's
	// per-edge terms, which `weight` deliberately does not become — an edge's
	// magnitude and its physics are different claims (§SD2).
	networkEdgeIDCol   = "id"
	networkLengthCol   = "length"
	networkStrengthCol = "strength"

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
		donutCol: -1, donutTotalCol: -1,
		opacityCol: -1, pickCol: -1, selectedCol: -1, fitCol: -1,
		radiusCol: -1, labelAlwaysCol: -1, centerCol: -1,
		pinXCol: -1, pinYCol: -1, latCol: -1, lonCol: -1,
		startXCol: -1, startYCol: -1,
		pullXCol: -1, pullYCol: -1, pullSCol: -1, pullSXCol: -1, pullSYCol: -1,
		groupsCol: -1, donutTonesCol: -1}
}

// noEdgesClaim is the all-absent edge claim; the two required columns are
// filled in by the resolver.
func noEdgesClaim() networkEdgesClaim {
	return networkEdgesClaim{srcCol: -1, tgtCol: -1, labelCol: -1, toneCol: -1, weightCol: -1,
		idCol: -1, opacityCol: -1, pickCol: -1, selectedCol: -1, lengthCol: -1, strengthCol: -1}
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
	// Seam A (§SD2).
	idCol, opacityCol, pickCol, selectedCol int
	lengthCol, strengthCol                  int
}

type networkVerticesClaim struct {
	idCol, labelCol, groupCol, shapeCol, toneCol, weightCol int
	// donutCol / donutTotalCol are spent by the live panel alone (§SD5); the
	// layered panel resolves them and ignores them, so one claim serves both.
	donutCol, donutTotalCol int
	// Seam A (§SD2): emphasis, state, placement and the two set-valued
	// columns. The layered panel honours opacity and selected and ignores the
	// placement ones, since Graphviz owns positions there (ADR-0231 §SD10);
	// pick and the edge id wait on the layered view, which has no per-node
	// hit exclusion and one edge per ordered pair.
	opacityCol, pickCol, selectedCol, fitCol           int
	radiusCol, labelAlwaysCol, centerCol               int
	pinXCol, pinYCol, latCol, lonCol                   int
	startXCol, startYCol                               int
	pullXCol, pullYCol, pullSCol, pullSXCol, pullSYCol int
	groupsCol, donutTonesCol                           int
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
	ec = noEdgesClaim()
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
		case networkEdgeIDCol:
			// Any type: read as text and interned like a vertex id, so a
			// UUID, an integer key and a name all tell parallel edges apart.
			ec.idCol = ci
		case networkOpacityCol:
			if isNumericType(f.Type) {
				ec.opacityCol = ci
			}
		case networkLengthCol:
			if isNumericType(f.Type) {
				ec.lengthCol = ci
			}
		case networkStrengthCol:
			if isNumericType(f.Type) {
				ec.strengthCol = ci
			}
		case networkPickCol:
			if isBooleanType(f.Type) {
				ec.pickCol = ci
			}
		case networkSelectedCol:
			if isBooleanType(f.Type) {
				ec.selectedCol = ci
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
	vc = noVerticesClaim()
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
		case networkOpacityCol:
			if isNumericType(f.Type) {
				vc.opacityCol = ci
			}
		case networkRadiusCol:
			if isNumericType(f.Type) {
				vc.radiusCol = ci
			}
		case networkPinXCol:
			if isNumericType(f.Type) {
				vc.pinXCol = ci
			}
		case networkPinYCol:
			if isNumericType(f.Type) {
				vc.pinYCol = ci
			}
		case networkLatCol:
			if isNumericType(f.Type) {
				vc.latCol = ci
			}
		case networkLonCol:
			if isNumericType(f.Type) {
				vc.lonCol = ci
			}
		case networkStartXCol:
			if isNumericType(f.Type) {
				vc.startXCol = ci
			}
		case networkStartYCol:
			if isNumericType(f.Type) {
				vc.startYCol = ci
			}
		case networkPullXCol:
			if isNumericType(f.Type) {
				vc.pullXCol = ci
			}
		case networkPullYCol:
			if isNumericType(f.Type) {
				vc.pullYCol = ci
			}
		case networkPullSCol:
			if isNumericType(f.Type) {
				vc.pullSCol = ci
			}
		case networkPullSXCol:
			if isNumericType(f.Type) {
				vc.pullSXCol = ci
			}
		case networkPullSYCol:
			if isNumericType(f.Type) {
				vc.pullSYCol = ci
			}
		case networkPickCol:
			if isBooleanType(f.Type) {
				vc.pickCol = ci
			}
		case networkSelectedCol:
			if isBooleanType(f.Type) {
				vc.selectedCol = ci
			}
		case networkFitCol:
			if isBooleanType(f.Type) {
				vc.fitCol = ci
			}
		case networkLabelAlwaysCol:
			if isBooleanType(f.Type) {
				vc.labelAlwaysCol = ci
			}
		case networkCenterCol:
			if isBooleanType(f.Type) {
				vc.centerCol = ci
			}
		case networkGroupsCol:
			// A set of aura ids, or nothing: a scalar column that happens to
			// be called `groups` is a name collision, not a membership.
			if netIsStringList(f.Type) {
				vc.groupsCol = ci
			}
		case networkDonutTonesCol:
			if netIsStringList(f.Type) {
				vc.donutTonesCol = ci
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
	// DonutTones is one tone family per ring slice, paired with DonutValues
	// by index (§SD2); the panel resolves the family to a colour.
	DonutToneStart  []int32
	DonutToneValues []string

	// Seam A (ADR-0231 §SD2). Every float column spells "not declared for
	// this row" as NaN, which is the widget's own unset (ADR-0232 §SD4) and
	// what makes a DECLARED ZERO a zero: an `opacity` of 0 paints nothing.
	// Every flag column spells it as false, which is each flag's "the query
	// did not ask".
	Opacity     []float32
	Radius      []float32 // absolute size, winning over `weight`'s share
	NoPick      []bool    // the inverse of the `pick` column the query writes
	Selected    []bool
	Fit         []bool
	LabelAlways []bool
	Center      []bool // a radial-layout centre
	PinX, PinY  []float32
	// StartX / StartY place a node once and then leave it free — what a
	// stored layout is restored from.
	StartX, StartY []float32
	PullX, PullY   []float32
	PullSX, PullSY []float32
	// AuraStart / AuraValues are the resolved aura membership: the `groups`
	// set, or `group` alone where the query declared only that (§SD4), so a
	// panel spends one column rather than re-deciding the rule.
	AuraStart  []int32
	AuraValues []string

	// Located reports that some vertex carried `lat`/`lon`, and GeoOrigin is
	// the projected centroid its pins were measured from (§SD3) — what a
	// panel needs to publish a gesture back in the units the query wrote.
	Located                bool
	GeoOriginX, GeoOriginY float64
	// Lat / Lon are the located rows' declared coordinates, NaN elsewhere:
	// what a host frames the picture by and what a geographic read-back is
	// checked against.
	Lat, Lon []float64
	// GroupsDeclared reports that the query wrote a `groups` column, which is
	// the query asking for auras (§SD4) — distinct from `group` alone, where
	// the reader still judges whether the grouping is spatial.
	GroupsDeclared bool
	// Extra carries the vertices columns an encoding selector named that are
	// not part of the contract (ADR-0231 §SD6: "naming a column spends that
	// column on the channel"). Read at build for the names the settings row
	// and the chrome asked for, one row per vertex, NaN and "" for a
	// synthesised endpoint.
	Extra map[string]netExtraColumn

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
	// Seam A's edge half. EdgeID tells parallel edges apart in hover,
	// selection and events; Length and Strength are the force step's per-edge
	// terms, which `weight` deliberately does not become.
	EdgeID       []uint64
	EdgeOpacity  []float32
	EdgeLength   []float32
	EdgeStrength []float32
	EdgeNoPick   []bool
	EdgeSelected []bool

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
	return buildNetModelWith(edgesRec, ec, vertRec, vc, caps, nil)
}

// netExtraColumn is one selector-named vertices column as the build read it:
// a numeric column arrives in Num, a string-like one in Str, and a list of
// strings in the list layout of ListStart / ListValues. Exactly one of the
// three shapes is set.
type netExtraColumn struct {
	Num        []float64 // NaN: not declared for this row
	Str        []string
	ListStart  []int32
	ListValues []string
}

// IsNumeric reports the column carries quantities.
func (inst netExtraColumn) IsNumeric() bool { return inst.Num != nil }

// IsList reports the column carries a set per row.
func (inst netExtraColumn) IsList() bool { return inst.ListStart != nil }

// buildNetModelWith is buildNetModel that also reads the named vertices
// columns into netModel.Extra, for the encoding selectors that name a column
// of the query's own (§SD6). A name that is not a column of the vertices
// result, or names a type no channel can spend, is left out; the selector's
// own refusal says so.
func buildNetModelWith(edgesRec arrow.RecordBatch, ec networkEdgesClaim, vertRec arrow.RecordBatch, vc networkVerticesClaim, caps netCaps, extra []string) (m netModel) {
	m.groupIdx = make(map[string]int, 8)
	m.names = newNetIds(64)

	// vertexRef is one vertex before its columns are written: the declared
	// id, its interned key, and the vertices row it came from — -1 for an
	// endpoint the edge list synthesised.
	//
	// Collecting refs, sorting THEM, and only then writing the columns is what
	// removes the permutation this build used to apply per column after the
	// fact. With Seam A the vertex side carries two dozen columns including
	// two ragged ones, and a permutation that missed one would put a pin on
	// the wrong node — silently, since every column would still be the right
	// length.
	type vertexRef struct {
		id  string
		key uint64
		row int64
	}
	refs := make([]vertexRef, 0, 64)
	seen := make(map[string]struct{}, 64)
	addVertex := func(id string, row int64) bool {
		if _, dup := seen[id]; dup {
			return true
		}
		if len(refs) >= caps.vertices {
			return false
		}
		seen[id] = struct{}{}
		refs = append(refs, vertexRef{id: id, key: m.names.intern(id), row: row})
		return true
	}
	noteGroup := func(g string) {
		if g == "" {
			return
		}
		if _, ok := m.groupIdx[g]; !ok {
			m.groupIdx[g] = len(m.groupIdx)
		}
	}

	// Pass A — the vertices rows, in declaration order, so `group` claims its
	// palette position in the order the query named them (§SD1) whatever the
	// sort does next.
	if vertRec != nil && vc.idCol >= 0 {
		rows := vertRec.NumRows()
		for row := range rows {
			if len(refs) >= caps.vertices {
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
			addVertex(id, row)
			if vc.groupCol >= 0 {
				noteGroup(formatCell(vertRec, vc.groupCol, row))
			}
		}
	}

	// Pass B — the edges, which may synthesise an endpoint the vertices CTE
	// never named.
	type edgeRef struct{ row int64 }
	edgeRows := make([]edgeRef, 0, 64)
	edgeSeen := make(map[[3]string]struct{}, 64)
	if edgesRec != nil {
		rows := edgesRec.NumRows()
		for row := range rows {
			if len(edgeRows) >= caps.edges {
				m.capped = true
				break
			}
			src := formatCell(edgesRec, ec.srcCol, row)
			tgt := formatCell(edgesRec, ec.tgtCol, row)
			if src == "" || tgt == "" {
				continue
			}
			// The dedup key carries the edge id, so parallel edges that
			// declare distinct ids are distinct edges (§SD2) where before
			// every repeat of a pair collapsed.
			var eid string
			if ec.idCol >= 0 {
				eid = formatCell(edgesRec, ec.idCol, row)
			}
			key := [3]string{src, tgt, eid}
			if _, dup := edgeSeen[key]; dup {
				continue
			}
			if !addVertex(src, -1) || !addVertex(tgt, -1) {
				m.capped = true
				continue // a dangling endpoint (vertex cap reached) drops the edge
			}
			edgeSeen[key] = struct{}{}
			edgeRows = append(edgeRows, edgeRef{row: row})
		}
	}

	m.GroupsDeclared = vc.groupsCol >= 0

	// Pass C — ascending interned id, which is the CSR's slot order and the
	// widget's (ADR-0232 §SD9).
	slices.SortFunc(refs, func(a, b vertexRef) int { return cmp.Compare(a.key, b.key) })

	// Pass D — the vertex columns, written once each in final order.
	n := len(refs)
	m.Key = make([]uint64, n)
	m.ID = make([]string, n)
	m.Label = make([]string, n)
	m.Group = make([]string, n)
	m.Shape = make([]string, n)
	m.Tone = make([]string, n)
	m.Weight = make([]float64, n)
	m.DonutTotal = make([]float32, n)
	m.Opacity = make([]float32, n)
	m.Radius = make([]float32, n)
	m.Lat, m.Lon = make([]float64, n), make([]float64, n)
	m.PinX, m.PinY = make([]float32, n), make([]float32, n)
	m.StartX, m.StartY = make([]float32, n), make([]float32, n)
	m.PullX, m.PullY = make([]float32, n), make([]float32, n)
	m.PullSX, m.PullSY = make([]float32, n), make([]float32, n)
	m.NoPick = make([]bool, n)
	m.Selected = make([]bool, n)
	m.Fit = make([]bool, n)
	m.LabelAlways = make([]bool, n)
	m.Center = make([]bool, n)
	m.DonutStart = make([]int32, 0, n+1)
	m.AuraStart = make([]int32, 0, n+1)
	m.DonutToneStart = make([]int32, 0, n+1)
	m.DonutStart = append(m.DonutStart, 0)
	m.AuraStart = append(m.AuraStart, 0)
	m.DonutToneStart = append(m.DonutToneStart, 0)

	// geo collects the located vertices' projected coordinates before the
	// origin is known, since the origin is their centroid (§SD3).
	type geoPoint struct {
		i    int
		x, y float64
	}
	var geo []geoPoint

	// The selector-named columns, resolved once against the schema.
	type extraRef struct {
		name string
		col  int
		kind uint8 // 1 numeric, 2 string, 3 string list
	}
	var extras []extraRef
	if vertRec != nil && len(extra) > 0 {
		for _, name := range extra {
			ci := vertRec.Schema().FieldIndices(name)
			if len(ci) == 0 {
				continue
			}
			f := vertRec.Schema().Field(ci[0])
			switch {
			case isNumericType(f.Type):
				extras = append(extras, extraRef{name: name, col: ci[0], kind: 1})
			case isStringLikeType(f.Type):
				extras = append(extras, extraRef{name: name, col: ci[0], kind: 2})
			case netIsStringList(f.Type):
				extras = append(extras, extraRef{name: name, col: ci[0], kind: 3})
			}
		}
	}
	if len(extras) > 0 {
		m.Extra = make(map[string]netExtraColumn, len(extras))
		for _, x := range extras {
			var col netExtraColumn
			switch x.kind {
			case 1:
				col.Num = make([]float64, n)
			case 2:
				col.Str = make([]string, n)
			default:
				col.ListStart = make([]int32, 1, n+1)
			}
			m.Extra[x.name] = col
		}
	}
	writeExtras := func(i int, row int64) {
		for _, x := range extras {
			col := m.Extra[x.name]
			switch x.kind {
			case 1:
				col.Num[i] = math.NaN()
				if row >= 0 {
					if v, ok := numericCellValue(vertRec.Column(x.col), row); ok {
						col.Num[i] = v
					}
				}
			case 2:
				if row >= 0 {
					col.Str[i] = formatCell(vertRec, x.col, row)
				}
			default:
				if row >= 0 {
					col.ListValues = netStringsAt(vertRec, x.col, row, col.ListValues)
				}
				col.ListStart = append(col.ListStart, int32(len(col.ListValues)))
			}
			m.Extra[x.name] = col
		}
	}

	for i := range refs {
		r := &refs[i]
		m.Key[i], m.ID[i], m.Label[i] = r.key, r.id, r.id
		// An unset float column is NaN, not zero: "not declared for this row".
		m.Opacity[i], m.Radius[i] = graphviewUnsetF32, graphviewUnsetF32
		m.Lat[i], m.Lon[i] = math.NaN(), math.NaN()
		m.PinX[i], m.PinY[i] = graphviewUnsetF32, graphviewUnsetF32
		m.StartX[i], m.StartY[i] = graphviewUnsetF32, graphviewUnsetF32
		m.PullX[i], m.PullY[i] = graphviewUnsetF32, graphviewUnsetF32
		m.PullSX[i], m.PullSY[i] = graphviewUnsetF32, graphviewUnsetF32
		writeExtras(i, r.row)
		if r.row < 0 {
			// A synthesised endpoint: it is its own label and nothing else.
			m.DonutStart = append(m.DonutStart, int32(len(m.DonutValues)))
			m.AuraStart = append(m.AuraStart, int32(len(m.AuraValues)))
			m.DonutToneStart = append(m.DonutToneStart, int32(len(m.DonutToneValues)))
			continue
		}
		row := r.row
		if vc.labelCol >= 0 {
			if l := formatCell(vertRec, vc.labelCol, row); l != "" {
				m.Label[i] = l
			}
		}
		if vc.shapeCol >= 0 {
			m.Shape[i] = formatCell(vertRec, vc.shapeCol, row)
		}
		if vc.toneCol >= 0 {
			m.Tone[i] = formatCell(vertRec, vc.toneCol, row)
		}
		if vc.groupCol >= 0 {
			m.Group[i] = formatCell(vertRec, vc.groupCol, row)
		}
		if vc.weightCol >= 0 {
			if w, ok := quantityCellValue(vertRec, vc.weightCol, row); ok && w > 0 {
				m.Weight[i] = w
				m.maxNodeWeight = max(m.maxNodeWeight, w)
			}
		}
		if vc.donutTotalCol >= 0 {
			if t, ok := quantityCellValue(vertRec, vc.donutTotalCol, row); ok && t > 0 {
				m.DonutTotal[i] = float32(t)
			}
		}
		m.Opacity[i] = netFloatAt(vertRec, vc.opacityCol, row)
		m.Radius[i] = netFloatAt(vertRec, vc.radiusCol, row)
		m.PinX[i], m.PinY[i] = netFloatAt(vertRec, vc.pinXCol, row), netFloatAt(vertRec, vc.pinYCol, row)
		m.StartX[i], m.StartY[i] = netFloatAt(vertRec, vc.startXCol, row), netFloatAt(vertRec, vc.startYCol, row)
		m.PullX[i], m.PullY[i] = netFloatAt(vertRec, vc.pullXCol, row), netFloatAt(vertRec, vc.pullYCol, row)
		m.PullSX[i], m.PullSY[i] = netPullStrength(vertRec, vc.pullSXCol, vc.pullSCol, row),
			netPullStrength(vertRec, vc.pullSYCol, vc.pullSCol, row)
		// `pick` is the positive spelling — absent is pickable — and the
		// widget takes the negative, so the claim inverts once here.
		if vc.pickCol >= 0 {
			if v, set := booleanCellValue(vertRec, vc.pickCol, row); set {
				m.NoPick[i] = !v
			}
		}
		m.Selected[i] = netFlagAt(vertRec, vc.selectedCol, row)
		m.Fit[i] = netFlagAt(vertRec, vc.fitCol, row)
		m.LabelAlways[i] = netFlagAt(vertRec, vc.labelAlwaysCol, row)
		m.Center[i] = netFlagAt(vertRec, vc.centerCol, row)
		if lat, okLat := netFloat64At(vertRec, vc.latCol, row); okLat {
			if lon, okLon := netFloat64At(vertRec, vc.lonCol, row); okLon {
				m.Lat[i], m.Lon[i] = lat, lon
				x, y := netProjectWebMercator(lat, lon)
				geo = append(geo, geoPoint{i: i, x: x, y: y})
			}
		}
		if vc.donutCol >= 0 {
			m.DonutValues, _ = netDonutAt(vertRec.Column(vc.donutCol), int(row), m.DonutValues)
		}
		m.DonutToneValues = netStringsAt(vertRec, vc.donutTonesCol, row, m.DonutToneValues)
		// Aura membership: the `groups` set, or `group` alone when the query
		// declared only that (§SD4).
		before := len(m.AuraValues)
		m.AuraValues = netStringsAt(vertRec, vc.groupsCol, row, m.AuraValues)
		for _, g := range m.AuraValues[before:] {
			noteGroup(g)
		}
		if len(m.AuraValues) == before && m.Group[i] != "" {
			m.AuraValues = append(m.AuraValues, m.Group[i])
		}
		m.DonutStart = append(m.DonutStart, int32(len(m.DonutValues)))
		m.AuraStart = append(m.AuraStart, int32(len(m.AuraValues)))
		m.DonutToneStart = append(m.DonutToneStart, int32(len(m.DonutToneValues)))
	}
	if len(m.DonutValues) == 0 {
		m.DonutStart = nil
	}
	if len(m.AuraValues) == 0 {
		m.AuraStart = nil
	}
	if len(m.DonutToneValues) == 0 {
		m.DonutToneStart = nil
	}
	// §SD3: the world is measured from the located set's centroid, so a
	// country-level graph does not spend float32's mantissa on its distance
	// from the antimeridian.
	if len(geo) > 0 {
		var sx, sy float64
		for _, p := range geo {
			sx, sy = sx+p.x, sy+p.y
		}
		m.Located = true
		m.GeoOriginX, m.GeoOriginY = sx/float64(len(geo)), sy/float64(len(geo))
		for _, p := range geo {
			// A `pin_x`/`pin_y` declared on the same row wins (§SD12): the
			// world-unit pin is the more deliberate statement, and a query
			// that wants geography does not write one. The row still counts
			// as located, so the geographic read-back covers it.
			if !math.IsNaN(float64(m.PinX[p.i])) && !math.IsNaN(float64(m.PinY[p.i])) {
				continue
			}
			m.PinX[p.i] = float32(p.x - m.GeoOriginX)
			m.PinY[p.i] = float32(p.y - m.GeoOriginY)
		}
	}

	// Pass E — the edge columns, in declaration order.
	ne := len(edgeRows)
	m.From, m.To = make([]uint64, ne), make([]uint64, ne)
	m.FromID, m.ToID = make([]string, ne), make([]string, ne)
	m.EdgeLabel, m.EdgeTone = make([]string, ne), make([]string, ne)
	m.EdgeWeight = make([]float64, ne)
	m.EdgeID = make([]uint64, ne)
	m.EdgeOpacity = make([]float32, ne)
	m.EdgeLength, m.EdgeStrength = make([]float32, ne), make([]float32, ne)
	m.EdgeNoPick = make([]bool, ne)
	m.EdgeSelected = make([]bool, ne)
	for i := range edgeRows {
		row := edgeRows[i].row
		src := formatCell(edgesRec, ec.srcCol, row)
		tgt := formatCell(edgesRec, ec.tgtCol, row)
		m.FromID[i], m.ToID[i] = src, tgt
		m.From[i], m.To[i] = m.names.intern(src), m.names.intern(tgt)
		if ec.labelCol >= 0 {
			m.EdgeLabel[i] = formatCell(edgesRec, ec.labelCol, row)
		}
		if ec.toneCol >= 0 {
			m.EdgeTone[i] = formatCell(edgesRec, ec.toneCol, row)
		}
		if ec.weightCol >= 0 {
			// A non-positive or unreadable cell leaves the weight at 0, which
			// both widgets read as *unknown* and draw as an ordinary edge
			// (ADR-0167 §SD2).
			if w, ok := quantityCellValue(edgesRec, ec.weightCol, row); ok && w > 0 {
				m.EdgeWeight[i] = w
				m.maxWeight = max(m.maxWeight, w)
			}
		}
		if ec.idCol >= 0 {
			if s := formatCell(edgesRec, ec.idCol, row); s != "" {
				// Interned in the same table as the vertex ids: an edge id
				// need only be unique among the edges of one ordered pair,
				// and one table keeps that true without a second convention.
				m.EdgeID[i] = m.names.intern(s)
			}
		}
		m.EdgeOpacity[i] = netFloatAt(edgesRec, ec.opacityCol, row)
		m.EdgeLength[i] = netFloatAt(edgesRec, ec.lengthCol, row)
		m.EdgeStrength[i] = netFloatAt(edgesRec, ec.strengthCol, row)
		if ec.pickCol >= 0 {
			if v, set := booleanCellValue(edgesRec, ec.pickCol, row); set {
				m.EdgeNoPick[i] = !v
			}
		}
		m.EdgeSelected[i] = netFlagAt(edgesRec, ec.selectedCol, row)
	}
	return
}

// netPullStrength resolves one axis's pull strength: the per-axis column when
// the query gave one, else the shared `pull_strength`, else the default. A
// target named without a strength still pulls — "naming an axis turns the pull
// on" (§SD2) — and the default is CenterGravity's, so a strength reads on the
// scale the widget's own centre pull already uses rather than on a new one.
func netPullStrength(rec arrow.RecordBatch, perAxis, shared int, row int64) float32 {
	if v := netFloatAt(rec, perAxis, row); !math.IsNaN(float64(v)) {
		return v
	}
	if v := netFloatAt(rec, shared, row); !math.IsNaN(float64(v)) {
		return v
	}
	return networkDefaultPullStrength
}

// networkDefaultPullStrength is what an axis pulls with when the query named a
// target and no strength. It is ForceParams.CenterGravity's default, which is
// the scale ADR-0224 §SD16 says a per-node pull reads on.
const networkDefaultPullStrength = 0.3

// netWebMercatorZoom is the reference zoom the geographic pins are projected
// at (§SD3). The value only sets the world's scale — every located vertex is
// projected at the same one and measured from their centroid — so it is
// chosen to put a country-sized graph in the same range as a free layout's
// world units rather than for any tiling reason.
const netWebMercatorZoom = 8

// netProjectWebMercator projects degrees to world units at the reference zoom.
// Latitudes past the Mercator limit are clamped rather than sent to infinity.
func netProjectWebMercator(lat, lon float64) (x, y float64) {
	const limit = 85.05112878
	lat = min(max(lat, -limit), limit)
	lon = min(max(lon, -180), 180)
	scale := float64(int(1)<<netWebMercatorZoom) * 256
	x = (lon + 180) / 360 * scale
	s := math.Sin(lat * math.Pi / 180)
	y = (0.5 - math.Log((1+s)/(1-s))/(4*math.Pi)) * scale
	return
}

// netUnprojectWebMercator is netProjectWebMercator inverted, for publishing a
// located graph's gestures back in the units the query wrote (§SD3).
func netUnprojectWebMercator(x, y float64) (lat, lon float64) {
	scale := float64(int(1)<<netWebMercatorZoom) * 256
	lon = x/scale*360 - 180
	n := math.Pi * (1 - 2*y/scale)
	lat = math.Atan(math.Sinh(n)) * 180 / math.Pi
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

// The Seam A cell readers (ADR-0231 §SD2). Each spells "not declared for this
// row" the way its channel does: NaN for a float, false for a flag, an empty
// slice for a set. A NULL cell and an absent column are the same thing, which
// is what lets a query pin some vertices and leave the rest to the layout.

// netIsStringList reports whether a column can carry a set of names: a list of
// a string-like type. A scalar column of the same name is a collision, not a
// set.
func netIsStringList(dt arrow.DataType) bool {
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
	return isStringLikeType(elem)
}

// netFloatAt reads a numeric cell, NaN for an absent column, a NULL cell or an
// unreadable one. NaN is the widget's own "not declared" (ADR-0232 §SD4), so
// the value travels to the declaration without a second convention.
func netFloatAt(rec arrow.RecordBatch, col int, row int64) float32 {
	if col < 0 {
		return graphviewUnsetF32
	}
	v, ok := numericCellValue(rec.Column(col), row)
	if !ok {
		return graphviewUnsetF32
	}
	return float32(v)
}

// netFloat64At is netFloatAt in double precision, for the geographic columns:
// a latitude rounded to float32 moves a node by metres, and the projection
// runs before the world units are narrowed.
func netFloat64At(rec arrow.RecordBatch, col int, row int64) (v float64, ok bool) {
	if col < 0 {
		return 0, false
	}
	return numericCellValue(rec.Column(col), row)
}

// netFlagAt reads a flag cell; an absent column or a NULL cell is false, which
// is every flag's "the query did not ask".
func netFlagAt(rec arrow.RecordBatch, col int, row int64) bool {
	if col < 0 {
		return false
	}
	v, _ := booleanCellValue(rec, col, row)
	return v
}

// netStringsAt appends a string-list cell's non-empty elements to dst. An
// absent column, a NULL cell or a column that is not a list appends nothing.
func netStringsAt(rec arrow.RecordBatch, col int, row int64, dst []string) []string {
	if col < 0 {
		return dst
	}
	arr := rec.Column(col)
	if row < 0 || int(row) >= arr.Len() || arr.IsNull(int(row)) {
		return dst
	}
	var values arrow.Array
	var start, end int
	switch a := arr.(type) {
	case *array.List:
		values = a.ListValues()
		start, end = int(a.Offsets()[row]), int(a.Offsets()[row+1])
	case *array.LargeList:
		values = a.ListValues()
		start, end = int(a.Offsets()[row]), int(a.Offsets()[row+1])
	case *array.FixedSizeList:
		values = a.ListValues()
		n := int(a.DataType().(*arrow.FixedSizeListType).Len())
		start, end = int(row)*n, (int(row)+1)*n
	default:
		return dst
	}
	for i := start; i < end; i++ {
		if values.IsNull(i) {
			continue
		}
		if s := formatArrayElem(values, int64(i)); s != "" {
			dst = append(dst, s)
		}
	}
	return dst
}
