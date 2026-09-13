package graphview

import (
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// NodeColumns is the frame's node declaration as columns (ADR-0232 §SD2):
// Ids, and beside it one optional slice per NodeSpec field, each either nil
// — absent for every row — or as long as Ids. It is the shape a caller whose
// data is already columnar hands over without building a spec per row, and
// the two forms reconcile to the same retained state and paint the same
// picture.
//
// In a float column NaN is "not declared for this row" and every other value
// is the value (§SD4): an Opacity of 0 paints nothing, a Strength of 0 on an
// edge pulls nothing, a Radius of 0 is a node that is only its label and its
// pick radius. That is where the row form's zero-as-default reading is not
// available, since a result's NULL needs the spelling. A bool column absent
// is false for every row; a string column absent is empty.
//
// A node is pinned when both PinX and PinY are declared, and pulled on an
// axis when both its target and its strength are declared and the strength
// is not zero. The ragged columns — aura membership, donut slices — use
// Arrow's list layout: an offsets slice of len(Ids)+1 into a values slice, so
// a list column from a result is handed over without re-nesting.
//
// The slot order of the retained graph is the declaration's row order
// (§SD3), duplicates of an id folded into its first row with the later row's
// attributes winning. A caller that declares in ascending id order therefore
// has the slot order of the analytics engine's CSR, and a column the engine
// returns indexes this declaration with no join.
type NodeColumns struct {
	Ids []uint64

	Label   []string
	Color   []color.Color
	Radius  []float32 // world units; NaN takes the style default
	Opacity []float32 // 0..1; NaN paints as declared
	NoPick  []bool
	// LabelAlways paints the node's label whatever the hover and selection
	// state, the per-node form of Options.LabelsAlways.
	LabelAlways []bool

	PinX, PinY []float32 // world units; the node is pinned where both are declared
	// PullX and PullStrengthX pull the node toward a column, PullY and
	// PullStrengthY toward a row (ADR-0224 §SD16); an axis pulls when both
	// its target and its strength are declared and the strength is not 0.
	PullX, PullY                 []float32
	PullStrengthX, PullStrengthY []float32

	// AuraOffsets is len(Ids)+1 offsets into AuraIds, the aura groups of each
	// node; nil declares none. An empty id is skipped.
	AuraOffsets []int32
	AuraIds     []string

	// DonutOffsets is len(Ids)+1 offsets into DonutValues, the ring slices
	// of each node; nil draws none. DonutColors, when given, pairs with
	// DonutValues by index and a 0 takes the qualitative cycle; DonutTotal,
	// when given, is per node, and a value past the slice sum leaves the
	// remainder as a track.
	DonutOffsets []int32
	DonutValues  []float32
	DonutColors  color.Colors
	DonutTotal   []float32
}

// EdgeColumns is the frame's edge declaration as columns: From and To, and
// beside them one optional slice per EdgeSpec field under the rules of
// NodeColumns. Width, Length, Strength and Opacity are float columns with
// NaN as the unset value; a Length must be positive and a non-positive one
// reads as unset, since a zero ideal length has no meaning; a negative
// Strength is zero.
type EdgeColumns struct {
	From, To []uint64

	Id       []uint64
	Label    []string
	Color    []color.Color
	Width    []float32 // screen pixels; NaN takes the style default
	Length   []float32 // multiplier on the ideal edge length
	Strength []float32 // multiplier on the attraction; 0 pulls nothing
	Opacity  []float32
	NoPick   []bool
}

// Len is the number of rows.
func (nc *NodeColumns) Len() int { return len(nc.Ids) }

// Len is the number of rows.
func (ec *EdgeColumns) Len() int { return len(ec.From) }

// Validate reports the first way the columns disagree with Ids: a column of
// another length, an offsets column that is not len(Ids)+1, monotone and
// ending at its values' length, or a colour column not paired with its
// values. RenderColumns validates for the caller; a caller that builds its
// columns once per rebuild may validate there instead and skip the per-frame
// check.
func (nc *NodeColumns) Validate() (err error) {
	n := len(nc.Ids)
	if err = colLen("Label", len(nc.Label), n); err != nil {
		return
	}
	if err = colLen("Color", len(nc.Color), n); err != nil {
		return
	}
	if err = colLen("Radius", len(nc.Radius), n); err != nil {
		return
	}
	if err = colLen("Opacity", len(nc.Opacity), n); err != nil {
		return
	}
	if err = colLen("NoPick", len(nc.NoPick), n); err != nil {
		return
	}
	if err = colLen("LabelAlways", len(nc.LabelAlways), n); err != nil {
		return
	}
	if err = colLen("PinX", len(nc.PinX), n); err != nil {
		return
	}
	if err = colLen("PinY", len(nc.PinY), n); err != nil {
		return
	}
	if err = colLen("PullX", len(nc.PullX), n); err != nil {
		return
	}
	if err = colLen("PullY", len(nc.PullY), n); err != nil {
		return
	}
	if err = colLen("PullStrengthX", len(nc.PullStrengthX), n); err != nil {
		return
	}
	if err = colLen("PullStrengthY", len(nc.PullStrengthY), n); err != nil {
		return
	}
	if err = colLen("DonutTotal", len(nc.DonutTotal), n); err != nil {
		return
	}
	if err = listLen("Aura", nc.AuraOffsets, n, len(nc.AuraIds)); err != nil {
		return
	}
	if err = listLen("Donut", nc.DonutOffsets, n, len(nc.DonutValues)); err != nil {
		return
	}
	if nc.DonutColors != nil && len(nc.DonutColors) != len(nc.DonutValues) {
		err = eb.Build().Int("colors", len(nc.DonutColors)).Int("values", len(nc.DonutValues)).Errorf("graphview: DonutColors is not paired with DonutValues")
	}
	return
}

// Validate is NodeColumns.Validate for the edge columns.
func (ec *EdgeColumns) Validate() (err error) {
	n := len(ec.From)
	if len(ec.To) != n {
		return eb.Build().Int("from", n).Int("to", len(ec.To)).Errorf("graphview: From and To differ in length")
	}
	if err = colLen("Id", len(ec.Id), n); err != nil {
		return
	}
	if err = colLen("Label", len(ec.Label), n); err != nil {
		return
	}
	if err = colLen("Color", len(ec.Color), n); err != nil {
		return
	}
	if err = colLen("Width", len(ec.Width), n); err != nil {
		return
	}
	if err = colLen("Length", len(ec.Length), n); err != nil {
		return
	}
	if err = colLen("Strength", len(ec.Strength), n); err != nil {
		return
	}
	if err = colLen("Opacity", len(ec.Opacity), n); err != nil {
		return
	}
	return colLen("NoPick", len(ec.NoPick), n)
}

func colLen(name string, have, n int) error {
	if have != 0 && have != n {
		return eb.Build().Str("column", name).Int("len", have).Int("rows", n).Errorf("graphview: column length differs from the row count")
	}
	return nil
}

func listLen(name string, offsets []int32, n, values int) error {
	if offsets == nil {
		return nil
	}
	if len(offsets) != n+1 {
		return eb.Build().Str("column", name).Int("offsets", len(offsets)).Int("rows", n).Errorf("graphview: list offsets are not rows+1 long")
	}
	if offsets[0] != 0 || int(offsets[n]) != values {
		return eb.Build().Str("column", name).Int("last", int(offsets[n])).Int("values", values).Errorf("graphview: list offsets do not span the values")
	}
	for i := 1; i <= n; i++ {
		if offsets[i] < offsets[i-1] {
			return eb.Build().Str("column", name).Int("row", i-1).Errorf("graphview: list offsets are not monotone")
		}
	}
	return nil
}

// fromSpecs rewrites a row-shaped declaration into the scratch columns,
// resolving the row form's zero-as-default fields to the columnar unset
// value so that one reconcile serves both forms. The backing slices are
// reused across frames.
func (nc *NodeColumns) fromSpecs(nodes []NodeSpec) {
	n := len(nodes)
	nc.Ids = growTo(nc.Ids, n)
	nc.Label = growTo(nc.Label, n)
	nc.Color = growTo(nc.Color, n)
	nc.Radius = growTo(nc.Radius, n)
	nc.Opacity = growTo(nc.Opacity, n)
	nc.NoPick = growTo(nc.NoPick, n)
	nc.LabelAlways = growTo(nc.LabelAlways, n)
	nc.PinX = growTo(nc.PinX, n)
	nc.PinY = growTo(nc.PinY, n)
	nc.PullX = growTo(nc.PullX, n)
	nc.PullY = growTo(nc.PullY, n)
	nc.PullStrengthX = growTo(nc.PullStrengthX, n)
	nc.PullStrengthY = growTo(nc.PullStrengthY, n)
	nc.AuraOffsets = growTo(nc.AuraOffsets, n+1)
	nc.AuraIds = nc.AuraIds[:0]
	nc.DonutOffsets = growTo(nc.DonutOffsets, n+1)
	nc.DonutValues = nc.DonutValues[:0]
	nc.DonutColors = nc.DonutColors[:0]
	nc.DonutTotal = growTo(nc.DonutTotal, n)
	anyColors := false
	for i := range nodes {
		sp := &nodes[i]
		nc.Ids[i] = sp.Id
		nc.Label[i] = sp.Label
		nc.Color[i] = sp.Color
		nc.Radius[i] = nanIfNotPositive(sp.Radius)
		nc.Opacity[i] = nanIfNotPositive(sp.Opacity)
		nc.NoPick[i] = sp.NoPick
		nc.LabelAlways[i] = sp.LabelAlways
		if sp.Pinned {
			nc.PinX[i], nc.PinY[i] = sp.PinX, sp.PinY
		} else {
			nc.PinX[i], nc.PinY[i] = nan32, nan32
		}
		nc.PullX[i], nc.PullY[i] = sp.Pull.X, sp.Pull.Y
		nc.PullStrengthX[i], nc.PullStrengthY[i] = sp.Pull.StrengthX, sp.Pull.StrengthY
		nc.AuraOffsets[i] = int32(len(nc.AuraIds))
		nc.AuraIds = append(nc.AuraIds, sp.Auras...)
		nc.DonutOffsets[i] = int32(len(nc.DonutValues))
		nc.DonutValues = append(nc.DonutValues, sp.Donut.Values...)
		// Colours pair by index; a spec whose colours run short of its
		// values, or that has none, takes the cycle for the rest, which is
		// what a 0 means in the column.
		for j := range sp.Donut.Values {
			var col uint32
			if j < len(sp.Donut.Colors) {
				col = sp.Donut.Colors[j]
				anyColors = anyColors || col != 0
			}
			nc.DonutColors = append(nc.DonutColors, col)
		}
		nc.DonutTotal[i] = sp.Donut.Total
	}
	nc.AuraOffsets[n] = int32(len(nc.AuraIds))
	nc.DonutOffsets[n] = int32(len(nc.DonutValues))
	if !anyColors {
		nc.DonutColors = nc.DonutColors[:0]
	}
}

// fromSpecs is NodeColumns.fromSpecs for the edges.
func (ec *EdgeColumns) fromSpecs(edges []EdgeSpec) {
	n := len(edges)
	ec.From = growTo(ec.From, n)
	ec.To = growTo(ec.To, n)
	ec.Id = growTo(ec.Id, n)
	ec.Label = growTo(ec.Label, n)
	ec.Color = growTo(ec.Color, n)
	ec.Width = growTo(ec.Width, n)
	ec.Length = growTo(ec.Length, n)
	ec.Strength = growTo(ec.Strength, n)
	ec.Opacity = growTo(ec.Opacity, n)
	ec.NoPick = growTo(ec.NoPick, n)
	for i := range edges {
		e := &edges[i]
		ec.From[i], ec.To[i], ec.Id[i] = e.From, e.To, e.Id
		ec.Label[i] = e.Label
		ec.Color[i] = e.Color
		ec.Width[i] = nanIfNotPositive(e.Width)
		ec.Length[i] = nanIfNotPositive(e.Length)
		ec.Strength[i] = nanIfNotPositive(e.Strength)
		ec.Opacity[i] = nanIfNotPositive(e.Opacity)
		ec.NoPick[i] = e.NoPick
	}
}

// nanIfNotPositive maps the row form's unset value — zero, and by the same
// rule anything negative — to the columnar unset value.
func nanIfNotPositive(v float32) float32 {
	if v > 0 {
		return v
	}
	return nan32
}

// colF32 reads a float column at row i, NaN when the column is absent.
func colF32(col []float32, i int) float32 {
	if col == nil {
		return nan32
	}
	return col[i]
}

func colBool(col []bool, i int) bool { return col != nil && col[i] }

func colStr(col []string, i int) string {
	if col == nil {
		return ""
	}
	return col[i]
}

func colColor(col []color.Color, i int) color.Color {
	if col == nil {
		return color.Color{}
	}
	return col[i]
}

func colU64(col []uint64, i int) uint64 {
	if col == nil {
		return 0
	}
	return col[i]
}

// sameF32 is equality that holds between two NaNs, for change detection
// on a column whose unset value is NaN.
func sameF32(a, b float32) bool {
	return a == b || (isNaN32(a) && isNaN32(b))
}
