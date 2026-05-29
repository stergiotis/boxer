//go:build llm_generated_opus47

package colorscale

import (
	"fmt"
	"math"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// HoverBand is a treemap.ColoringI decorator that dims cells whose color
// metric falls outside a ±halfWidth normalized band around an active center
// value. Pair with a colorscale.ColorScale's OnHover callback to highlight
// the treemap cells whose value sits near whatever the user is pointing at
// on the legend.
//
// When no band is active (initial state, or after ClearBand), Colors falls
// through to the wrapped base ColoringI unchanged. When SetBand is active,
// cells outside the band get Fill / HoverFill collapsed to DimFill and
// AccentBorder collapsed to Border, so the highlighted band reads as the
// only saturated swatch. Inside the band the cells are returned as-is.
//
// Band width and center are kept in the colormap's normalized 0..1 space
// (via treemap.Colormap.Normalize) so the same band-half-width is visually
// consistent across linear and log colormaps.
type HoverBand struct {
	cmap     *treemap.Colormap
	base     treemap.ColoringI
	valueFn  func(*layout.Node) float64
	halfW    float64
	active   bool
	centerT  float64
}

// DefaultHalfWidth is the initial band half-width used by NewHoverBand —
// 5% of the colormap's normalized axis on each side of the center value.
const DefaultHalfWidth float64 = 0.05

var _ treemap.ColoringI = (*HoverBand)(nil)

// NewHoverBand constructs a band-highlight decorator over base. cmap and
// valueFn must match what base was built from — cmap so HoverBand can
// normalize the cell's value into the same 0..1 space the legend's hover
// produced, and valueFn so each cell's raw value can be looked up. base is
// invoked for every Colors call (the band logic only ever modifies the
// returned colors, never the ok flag), so a fall-through-friendly base
// (e.g. treemap.ContinuousColoringFromMap) is recommended.
//
// Panics on nil arguments — they're programmer errors that would otherwise
// turn into a nil dereference inside Colors.
func NewHoverBand(cmap *treemap.Colormap, base treemap.ColoringI, valueFn func(*layout.Node) float64) (inst *HoverBand) {
	if cmap == nil {
		panic("colorscale: NewHoverBand requires a non-nil Colormap")
	}
	if base == nil {
		panic("colorscale: NewHoverBand requires a non-nil base ColoringI")
	}
	if valueFn == nil {
		panic("colorscale: NewHoverBand requires a non-nil valueFn")
	}
	inst = &HoverBand{
		cmap:    cmap,
		base:    base,
		valueFn: valueFn,
		halfW:   DefaultHalfWidth,
	}
	return
}

// SetHalfWidth overrides the band half-width. Stays sticky across SetBand
// calls, unlike the demo precursor that re-asserted the default each hover.
// Panics on non-positive input.
func (inst *HoverBand) SetHalfWidth(halfWidth float64) {
	if !(halfWidth > 0) {
		panic(fmt.Sprintf("colorscale: SetHalfWidth requires halfWidth > 0 (got %v)", halfWidth))
	}
	inst.halfW = halfWidth
}

// SetBand activates the band centered on value. value is mapped through the
// bound Colormap.Normalize, so callers can pass the raw hover value from
// ColorScale.HoverInfo.Value directly — no manual log/lin handling needed.
func (inst *HoverBand) SetBand(value float64) {
	inst.active = true
	inst.centerT = inst.cmap.Normalize(value)
}

// ClearBand deactivates the highlight; subsequent Colors calls fall through
// to base unchanged.
func (inst *HoverBand) ClearBand() {
	inst.active = false
}

// Active reports whether a band is currently active. Useful for callers
// that want to draw an auxiliary hover marker outside the treemap.
func (inst *HoverBand) Active() bool { return inst.active }

// Colors implements treemap.ColoringI. Delegates to base, then — when a
// band is active — replaces out-of-band cells' Fill / HoverFill /
// AccentBorder with their dim counterparts so the band reads as the only
// saturated region.
func (inst *HoverBand) Colors(info treemap.CellInfo) (cs treemap.CellColors, ok bool) {
	cs, ok = inst.base.Colors(info)
	if !ok {
		return
	}
	if !inst.active {
		return
	}
	cellT := inst.cmap.Normalize(inst.valueFn(info.Node))
	if math.Abs(cellT-inst.centerT) <= inst.halfW {
		return
	}
	cs.Fill = cs.DimFill
	cs.HoverFill = cs.DimFill
	cs.AccentBorder = cs.Border
	return
}
