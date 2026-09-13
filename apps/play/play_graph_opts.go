package play

import (
	"fmt"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview"
)

// play_graph_opts.go owns the `graph_opts` CTE — Seam B of ADR-0231 §SD5: the
// one-row settings table carrying what is a property of the whole drawing and
// still the query's under §SD1.
//
// Columns rather than keys, which is that record's kill-reason against a
// key/value table: a column name is schema-visible, so completion offers it and
// the claim type-checks it, and a misspelling is an unclaimed column rather
// than a silently ignored key. Every column is optional and a value outside a
// column's vocabulary is refused with a reason in the status line, leaving the
// default in charge — a settings table must not be able to blank a drawing.
//
// The CTE is an ordinary split node, so its cells may read signals:
// `{gv_zoom:Float64} < 0.3 AS hide_edges` is a level of detail the query owns.

const (
	// graph_opts columns (chGraphOpts).
	graphOptLayoutCol       = "layout"
	graphOptOrientationCol  = "orientation"
	graphOptRingDistCol     = "ring_dist"
	graphOptRowDistCol      = "row_dist"
	graphOptColDistCol      = "col_dist"
	graphOptKScaleCol       = "k_scale"
	graphOptGravityCol      = "gravity"
	graphOptForceModelCol   = "force_model"
	graphOptExaggerationCol = "exaggeration"
	graphOptHideEdgesCol    = "hide_edges"
	graphOptUndirectedCol   = "undirected"
	graphOptPinOnDragCol    = "pin_on_drag"
	graphOptSizeByCol       = "size_by"
	graphOptToneByCol       = "tone_by"
	graphOptOpacityByCol    = "opacity_by"
	graphOptAuraByCol       = "aura_by"
	graphOptDistanceFromCol = "distance_from"

	// networkGraphOptsNodeID is the CTE the channel binds to — a node of the
	// user's own split, demanded on its own lane like the other two.
	networkGraphOptsNodeID NodeID = "graph_opts"
)

// graphviewSeedE names where the seeded metrics measure from (§SD5's
// `distance_from`).
type graphviewSeedE uint8

const (
	// graphviewSeedSelection is the default: the selected vertices.
	graphviewSeedSelection graphviewSeedE = iota
	graphviewSeedHover
)

// networkGraphOptsClaim is the resolved column indices; -1 marks an absent
// column, which is every column's normal state.
type networkGraphOptsClaim struct {
	layoutCol, orientationCol                 int
	ringDistCol, rowDistCol, colDistCol       int
	kScaleCol, gravityCol                     int
	forceModelCol, exaggerationCol            int
	hideEdgesCol, undirectedCol, pinOnDragCol int
	sizeByCol, toneByCol, opacityByCol        int
	auraByCol, distanceFromCol                int
}

// noGraphOptsClaim is the claim of a query without a `graph_opts` CTE.
func noGraphOptsClaim() networkGraphOptsClaim {
	return networkGraphOptsClaim{
		layoutCol: -1, orientationCol: -1, ringDistCol: -1, rowDistCol: -1, colDistCol: -1,
		kScaleCol: -1, gravityCol: -1, forceModelCol: -1, exaggerationCol: -1,
		hideEdgesCol: -1, undirectedCol: -1, pinOnDragCol: -1,
		sizeByCol: -1, toneByCol: -1, opacityByCol: -1, auraByCol: -1, distanceFromCol: -1,
	}
}

// resolveGraphOpts applies the §SD5 contract to a schema. Every column is
// optional, so the CTE is never rejected for its shape: a `graph_opts` naming
// nothing the panel knows is a table of ordinary result columns and the
// drawing keeps every default.
//
// The numeric and boolean columns are claimed only when they can carry what
// they promise, the guard the rest of the contract already uses: a `gravity`
// that is not numeric is far likelier to be a column that happens to share the
// name than a setting the author meant.
func resolveGraphOpts(schema *arrow.Schema) (gc networkGraphOptsClaim) {
	gc = noGraphOptsClaim()
	if schema == nil {
		return
	}
	for ci, f := range schema.Fields() {
		numeric := isNumericType(f.Type)
		switch f.Name {
		case graphOptLayoutCol:
			gc.layoutCol = ci
		case graphOptOrientationCol:
			gc.orientationCol = ci
		case graphOptForceModelCol:
			gc.forceModelCol = ci
		case graphOptSizeByCol:
			gc.sizeByCol = ci
		case graphOptToneByCol:
			gc.toneByCol = ci
		case graphOptOpacityByCol:
			gc.opacityByCol = ci
		case graphOptAuraByCol:
			gc.auraByCol = ci
		case graphOptDistanceFromCol:
			gc.distanceFromCol = ci
		case graphOptRingDistCol:
			if numeric {
				gc.ringDistCol = ci
			}
		case graphOptRowDistCol:
			if numeric {
				gc.rowDistCol = ci
			}
		case graphOptColDistCol:
			if numeric {
				gc.colDistCol = ci
			}
		case graphOptKScaleCol:
			if numeric {
				gc.kScaleCol = ci
			}
		case graphOptGravityCol:
			if numeric {
				gc.gravityCol = ci
			}
		case graphOptExaggerationCol:
			if numeric {
				gc.exaggerationCol = ci
			}
		case graphOptHideEdgesCol:
			if isBooleanType(f.Type) {
				gc.hideEdgesCol = ci
			}
		case graphOptUndirectedCol:
			if isBooleanType(f.Type) {
				gc.undirectedCol = ci
			}
		case graphOptPinOnDragCol:
			if isBooleanType(f.Type) {
				gc.pinOnDragCol = ci
			}
		}
	}
	return
}

// graphOpts is the resolved row: what the query said about the drawing as a
// whole. Each field carries an "unset" — a zero for the positive quantities, a
// Set flag for the enumerations and the booleans — because the chrome's auto
// position has to tell "the query said force" from "the query said nothing"
// (§SD1's precedence rule).
type graphOpts struct {
	Layout         graphviewLayoutE
	LayoutSet      bool
	Orientation    graphview.OrientationE
	OrientationSet bool

	RingDist, RowDist, ColDist float32
	KScale, Gravity            float32
	Exaggeration               float32

	ForceModel    graphview.ForceModelE
	ForceModelSet bool

	HideEdges, HideEdgesSet   bool
	Undirected, UndirectedSet bool
	PinOnDrag, PinOnDragSet   bool

	SizeBy, ToneBy, OpacityBy, AuraBy string

	DistanceFrom    graphviewSeedE
	DistanceFromSet bool

	// Reasons are the vocabulary refusals, for the status line. A refused
	// value leaves its default in charge.
	Reasons []string
	// ExtraRows records a CTE that returned more than one row: a settings
	// table with many rows is a query bug worth saying out loud rather than a
	// set of settings.
	ExtraRows bool
}

// buildGraphOpts reads the first row of the `graph_opts` result. A nil record,
// an unclaimed schema or an empty result all yield the zero value, which is
// "the query said nothing" and leaves every default in charge.
func buildGraphOpts(rec arrow.RecordBatch, gc networkGraphOptsClaim) (o graphOpts) {
	if rec == nil || rec.NumRows() == 0 {
		return
	}
	if rec.NumRows() > 1 {
		o.ExtraRows = true
	}
	const row = 0
	refuse := func(col, got string, vocab ...string) {
		o.Reasons = append(o.Reasons, fmt.Sprintf("`%s = %s` is not one of %s",
			col, got, strings.Join(vocab, ", ")))
	}
	text := func(ci int) string {
		if ci < 0 {
			return ""
		}
		return strings.TrimSpace(formatCell(rec, ci, row))
	}
	number := func(ci int) float32 {
		if ci < 0 {
			return 0
		}
		v, ok := quantityCellValue(rec, ci, row)
		if !ok || v <= 0 {
			return 0 // a non-positive setting is unset, as it is in the widget
		}
		return float32(v)
	}
	boolean := func(ci int) (val, set bool) {
		if ci < 0 {
			return
		}
		return booleanCellValue(rec, ci, row)
	}

	if s := text(gc.layoutCol); s != "" {
		if l, ok := parseGraphviewLayout(s); ok {
			o.Layout, o.LayoutSet = l, true
		} else {
			refuse(graphOptLayoutCol, s, "force", "force_gravity", "hierarchical", "radial", "random")
		}
	}
	if s := text(gc.orientationCol); s != "" {
		switch s {
		case "top_down":
			o.Orientation, o.OrientationSet = graphview.OrientationTopDown, true
		case "left_right":
			o.Orientation, o.OrientationSet = graphview.OrientationLeftRight, true
		default:
			refuse(graphOptOrientationCol, s, "top_down", "left_right")
		}
	}
	if s := text(gc.forceModelCol); s != "" {
		switch s {
		case "fr":
			o.ForceModel, o.ForceModelSet = graphview.ForceModelFR, true
		// Both spellings, because the record writes one and the widget's own
		// identifier the other, and a reader should not have to know which.
		case "neighbour_embedding", "neighbor_embedding":
			o.ForceModel, o.ForceModelSet = graphview.ForceModelNeighborEmbedding, true
		default:
			refuse(graphOptForceModelCol, s, "fr", "neighbour_embedding")
		}
	}
	if s := text(gc.distanceFromCol); s != "" {
		switch s {
		case "selection":
			o.DistanceFrom, o.DistanceFromSet = graphviewSeedSelection, true
		case "hover":
			o.DistanceFrom, o.DistanceFromSet = graphviewSeedHover, true
		default:
			refuse(graphOptDistanceFromCol, s, "selection", "hover")
		}
	}

	o.RingDist, o.RowDist, o.ColDist = number(gc.ringDistCol), number(gc.rowDistCol), number(gc.colDistCol)
	o.KScale, o.Gravity = number(gc.kScaleCol), number(gc.gravityCol)
	o.Exaggeration = number(gc.exaggerationCol)

	o.HideEdges, o.HideEdgesSet = boolean(gc.hideEdgesCol)
	o.Undirected, o.UndirectedSet = boolean(gc.undirectedCol)
	o.PinOnDrag, o.PinOnDragSet = boolean(gc.pinOnDragCol)

	o.SizeBy, o.ToneBy = text(gc.sizeByCol), text(gc.toneByCol)
	o.OpacityBy, o.AuraBy = text(gc.opacityByCol), text(gc.auraByCol)
	return
}

// parseGraphviewLayout resolves the `layout` vocabulary. An unrecognised value
// leaves the panel's own default in charge rather than picking one, which is
// what keeps a typo from silently re-laying a graph out.
func parseGraphviewLayout(s string) (l graphviewLayoutE, ok bool) {
	switch s {
	case "force":
		return graphviewLayoutForce, true
	case "force_gravity":
		return graphviewLayoutGravity, true
	case "hierarchical":
		return graphviewLayoutTree, true
	case "radial":
		return graphviewLayoutRadial, true
	case "random":
		return graphviewLayoutRandom, true
	}
	return graphviewLayoutAuto, false
}

// statusNote is what the panel appends to its status line: the refusals and
// the extra-rows note, or the empty string when the query's settings were all
// understood.
func (inst *graphOpts) statusNote() string {
	var parts []string
	if inst.ExtraRows {
		parts = append(parts, "`graph_opts` returned more than one row; the first is the settings")
	}
	parts = append(parts, inst.Reasons...)
	if len(parts) == 0 {
		return ""
	}
	return " · " + strings.Join(parts, " · ")
}

// isBooleanType reports whether a column can carry a flag: Arrow's own
// boolean, or the small integer a ClickHouse Bool arrives as on the paths that
// widen it. A numeric column here is read as 0-or-not, which is what a query
// writing `1 AS undirected` means.
func isBooleanType(dt arrow.DataType) bool {
	return dt.ID() == arrow.BOOL || isNumericType(dt)
}

// booleanCellValue reads a flag cell. It is its own reader rather than a
// quantityCellValue call because that one falls back to parsing the formatted
// text, and a boolean formats as "true" — which parses as no number at all.
func booleanCellValue(rec arrow.RecordBatch, col int, row int64) (val, set bool) {
	arr := rec.Column(col)
	if row < 0 || int(row) >= arr.Len() || arr.IsNull(int(row)) {
		return
	}
	if b, isBool := arr.(*array.Boolean); isBool {
		return b.Value(int(row)), true
	}
	if v, isNum := numericCellValue(arr, row); isNum {
		return v != 0, true
	}
	return
}
