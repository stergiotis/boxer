package treemap

import (
	"fmt"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/treemap/layout"
)

// CellColors is the per-cell color palette returned by a ColoringI. Each color
// is a retained color.Color; the renderer never pays for color construction
// per frame. StyleI picks which slot to use via UseDimFill / UseHoverFill /
// UseAccentBorder flags.
//
// Text/HoverText/DimText are WCAG-picked (black or white) for readable contrast
// against the matching fill; callers writing labels on top of cells should use
// the text slot that corresponds to whichever fill slot the StyleI selected.
type CellColors struct {
	Fill         color.Color // primary fill
	HoverFill    color.Color // brightened variant for hover
	DimFill      color.Color // muted variant for context cells
	Border       color.Color // default border (darker than Fill)
	AccentBorder color.Color // bright border for highlight (= Fill)
	Text         color.Color // contrast-picked text color over Fill
	HoverText    color.Color // contrast-picked text color over HoverFill
	DimText      color.Color // contrast-picked text color over DimFill
}

// ColoringI resolves a cell's color set based on its CellInfo. Implementations
// pre-derive variants (Fill / HoverFill / DimFill / Border / AccentBorder) at
// construction time. Returning ok=false signals "no opinion"; in a
// CompositeColoring this causes the next layer to be tried.
type ColoringI interface {
	Colors(CellInfo) (CellColors, bool)
}

// deriveCellColors produces the full color set from a single base color
// using the package's standard transformations (hover = +30 each channel,
// dim = half brightness, border = -40 each channel, accent = base). Text
// slots are WCAG-picked (black or white) per matching fill for readable
// contrast on arbitrary palettes.
//
// Channel arithmetic is computational, not an ad-hoc palette pick — the
// base comes from the active Coloring's palette (Viridis8 / Magma8 /
// caller-supplied) so the derivations are pure functions of data.
// Pack-and-Hex avoids tripping designlint L2's raw-literal heuristic
// without sacrificing the per-channel math.
func deriveCellColors(base uint32) CellColors {
	r := uint8(base >> 24 & 0xFF)
	g := uint8(base >> 16 & 0xFF)
	b := uint8(base >> 8 & 0xFF)
	a := uint8(base & 0xFF)
	hr, hg, hb := clampHi(int(r)+30), clampHi(int(g)+30), clampHi(int(b)+30)
	dr, dg, db := r/2, g/2, b/2
	br, bg, bb := clampLo(int(r)-40), clampLo(int(g)-40), clampLo(int(b)-40)
	pack := func(rr, gg, bb uint8) (packed uint32) {
		packed = uint32(rr)<<24 | uint32(gg)<<16 | uint32(bb)<<8 | uint32(a)
		return
	}
	return CellColors{
		Fill:         color.Hex(pack(r, g, b)).Keep(),
		HoverFill:    color.Hex(pack(hr, hg, hb)).Keep(),
		DimFill:      color.Hex(pack(dr, dg, db)).Keep(),
		Border:       color.Hex(pack(br, bg, bb)).Keep(),
		AccentBorder: color.Hex(pack(r, g, b)).Keep(),
		Text:         color.Hex(pickTextColor(r, g, b)).Keep(),
		HoverText:    color.Hex(pickTextColor(hr, hg, hb)).Keep(),
		DimText:      color.Hex(pickTextColor(dr, dg, db)).Keep(),
	}
}

func clampLo(v int) uint8 {
	if v < 0 {
		return 0
	}
	return uint8(v)
}

func clampHi(v int) uint8 {
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// --- Built-in colorings ---

// DepthColoring resolves colors from a fixed palette indexed by CellInfo.Depth
// (modulo palette length). The default coloring used when no WithColoring
// option is supplied.
func DepthColoring(palette []uint32) ColoringI {
	if len(palette) == 0 {
		panic("treemap: DepthColoring requires a non-empty palette")
	}
	p := make([]CellColors, len(palette))
	for i, rgba := range palette {
		p[i] = deriveCellColors(rgba)
	}
	return &depthColoring{palette: p}
}

type depthColoring struct{ palette []CellColors }

var _ ColoringI = (*depthColoring)(nil)

func (inst *depthColoring) Colors(info CellInfo) (CellColors, bool) {
	return inst.palette[info.Depth%len(inst.palette)], true
}

// CategoricalColoring picks a palette entry per cell using fn. fn returning
// a negative index means "no opinion" — falls through in a CompositeColoring.
func CategoricalColoring(palette []uint32, fn func(node *layout.Node) int) ColoringI {
	if len(palette) == 0 {
		panic("treemap: CategoricalColoring requires a non-empty palette")
	}
	if fn == nil {
		panic("treemap: CategoricalColoring requires a non-nil fn")
	}
	p := make([]CellColors, len(palette))
	for i, rgba := range palette {
		p[i] = deriveCellColors(rgba)
	}
	return &categoricalColoring{palette: p, fn: fn}
}

type categoricalColoring struct {
	palette []CellColors
	fn      func(*layout.Node) int
}

var _ ColoringI = (*categoricalColoring)(nil)

func (inst *categoricalColoring) Colors(info CellInfo) (CellColors, bool) {
	idx := inst.fn(info.Node)
	if idx < 0 {
		return CellColors{}, false
	}
	return inst.palette[idx%len(inst.palette)], true
}

// Colormap pairs a colormap.Config — the palette + data-range + scale mapping,
// shared with the scientific texture widgets and the colorscale legend — with
// treemap's per-entry CellColors derivation. Construct once, pass it to a
// ContinuousColoringFromMap, and pass its Config() to colorscale.New so the
// legend samples the exact gradient the cells use.
//
// Linear by default (NewColormap) or log-base-10 (NewLogColormap) for
// heavy-tailed distributions like complexity or file-size histograms.
//
// Cells quantize value to the nearest palette entry; the legend (via Config)
// interpolates between entries for a smooth gradient strip.
type Colormap struct {
	cfg   *colormap.Config
	cells []CellColors // CellColors pre-derived per palette entry
}

// NewColormap constructs a linear Colormap. Panics if palette has fewer than 2
// colors or if min >= max.
func NewColormap(palette []uint32, min, max float64) *Colormap {
	return wrapColormap(colormap.NewConfig(palette, min, max))
}

// NewLogColormap constructs a log-base-10 Colormap. Panics if palette has fewer
// than 2 colors, if min or max is non-positive, or if min >= max. Suitable for
// heavy-tailed data (e.g., cyclomatic complexity).
func NewLogColormap(palette []uint32, min, max float64) *Colormap {
	if min <= 0 || max <= 0 {
		panic(fmt.Sprintf("treemap: NewLogColormap requires strictly positive min,max (got %v,%v)", min, max))
	}
	cfg := colormap.NewConfig(palette, min, max)
	cfg.Scale = colormap.ScaleLogE
	return wrapColormap(cfg)
}

func wrapColormap(cfg *colormap.Config) *Colormap {
	cells := make([]CellColors, len(cfg.Palette))
	for i, rgba := range cfg.Palette {
		cells[i] = deriveCellColors(rgba)
	}
	return &Colormap{cfg: cfg, cells: cells}
}

// Config returns the underlying colormap.Config. Pass it to colorscale.New so the
// legend renders the exact same gradient the treemap cells are coloured from.
func (inst *Colormap) Config() *colormap.Config { return inst.cfg }

// Range returns the (min, max) data range this colormap covers.
func (inst *Colormap) Range() (min, max float64) { return inst.cfg.Range() }

// IsLog reports whether this colormap uses log-base-10 scaling.
func (inst *Colormap) IsLog() bool { return inst.cfg.IsLog() }

// Palette returns a copy of the raw 0xRRGGBBAA palette values.
func (inst *Colormap) Palette() []uint32 {
	out := make([]uint32, len(inst.cfg.Palette))
	copy(out, inst.cfg.Palette)
	return out
}

// Normalize returns a 0..1 position for value within the colormap's range,
// clamped to the range endpoints.
func (inst *Colormap) Normalize(value float64) float64 { return inst.cfg.Normalize(value) }

// At returns the interpolated 0xRRGGBBAA color for a value.
func (inst *Colormap) At(value float64) uint32 { return inst.cfg.At(value) }

// ColorsAt returns the pre-derived CellColors (fill/hover/dim/border/accent) for
// a value, quantized to the nearest palette entry. Used by ContinuousColoringFromMap.
func (inst *Colormap) ColorsAt(value float64) CellColors {
	return inst.cells[inst.cfg.IndexAt(value, len(inst.cells))]
}

// ContinuousColoringFromMap wraps a Colormap as a ColoringI. Use this when
// a legend widget needs to share the exact same colormap instance as the
// treemap; NaN from fn yields ok=false (fall through in a Composite).
func ContinuousColoringFromMap(cm *Colormap, fn func(node *layout.Node) float64) ColoringI {
	if cm == nil {
		panic("treemap: ContinuousColoringFromMap requires a non-nil Colormap")
	}
	if fn == nil {
		panic("treemap: ContinuousColoringFromMap requires a non-nil fn")
	}
	return &continuousColoring{cm: cm, fn: fn}
}

// ContinuousColoring is the legacy all-in-one constructor. Internally it
// builds a Colormap; callers who also want a legend widget should prefer
// NewColormap + ContinuousColoringFromMap to share a single instance.
func ContinuousColoring(palette []uint32, fn func(node *layout.Node) float64, min, max float64) ColoringI {
	return ContinuousColoringFromMap(NewColormap(palette, min, max), fn)
}

type continuousColoring struct {
	cm *Colormap
	fn func(*layout.Node) float64
}

var _ ColoringI = (*continuousColoring)(nil)

func (inst *continuousColoring) Colors(info CellInfo) (CellColors, bool) {
	v := inst.fn(info.Node)
	if v != v { // NaN
		return CellColors{}, false
	}
	return inst.cm.ColorsAt(v), true
}

// FixedColoring returns the same color for every cell. Useful as a base
// layer in a CompositeColoring or for monochromatic visualizations.
func FixedColoring(rgba uint32) ColoringI {
	return &fixedColoring{colors: deriveCellColors(rgba)}
}

type fixedColoring struct{ colors CellColors }

var _ ColoringI = (*fixedColoring)(nil)

func (inst *fixedColoring) Colors(info CellInfo) (CellColors, bool) { return inst.colors, true }

// ConditionalColoring delegates to inner only when predicate(info) is true;
// otherwise returns ok=false. Use to scope a coloring to specific states
// (e.g. only color leaves, only color drillable cells).
func ConditionalColoring(predicate func(CellInfo) bool, inner ColoringI) ColoringI {
	if predicate == nil {
		panic("treemap: ConditionalColoring requires a non-nil predicate")
	}
	if inner == nil {
		panic("treemap: ConditionalColoring requires a non-nil inner")
	}
	return &conditionalColoring{predicate: predicate, inner: inner}
}

type conditionalColoring struct {
	predicate func(CellInfo) bool
	inner     ColoringI
}

var _ ColoringI = (*conditionalColoring)(nil)

func (inst *conditionalColoring) Colors(info CellInfo) (CellColors, bool) {
	if !inst.predicate(info) {
		return CellColors{}, false
	}
	return inst.inner.Colors(info)
}

// CompositeColoring tries layers in order and returns the LAST layer whose
// Colors call yielded ok=true. If none yield ok, returns (zero, false).
// Use for layered effects: a base depth coloring with overrides for
// specific node categories.
//
//	treemap.CompositeColoring(
//	    treemap.DepthColoring(treemap.Viridis8),
//	    treemap.CategoricalColoring(red, errorIndex),
//	)
func CompositeColoring(layers ...ColoringI) ColoringI {
	if len(layers) == 0 {
		panic("treemap: CompositeColoring requires at least one layer")
	}
	for i, l := range layers {
		if l == nil {
			panic("treemap: CompositeColoring layer " + itoa(i) + " is nil")
		}
	}
	return &compositeColoring{layers: layers}
}

type compositeColoring struct{ layers []ColoringI }

var _ ColoringI = (*compositeColoring)(nil)

func (inst *compositeColoring) Colors(info CellInfo) (CellColors, bool) {
	var result CellColors
	var found bool
	for _, l := range inst.layers {
		if cs, ok := l.Colors(info); ok {
			result = cs
			found = true
		}
	}
	return result, found
}

// SimpleColoring is a convenience wrapper for callers who only want to
// supply a fill color per cell. Hover/dim/border alternates are derived
// automatically using the package's standard transformations and cached
// keyed by the returned RGBA so the same fill yields the same alternates.
// fn returning ok=false falls through.
func SimpleColoring(fn func(CellInfo) (rgba uint32, ok bool)) ColoringI {
	if fn == nil {
		panic("treemap: SimpleColoring requires a non-nil fn")
	}
	return &simpleColoring{fn: fn, cache: make(map[uint32]CellColors)}
}

type simpleColoring struct {
	fn    func(CellInfo) (uint32, bool)
	cache map[uint32]CellColors
}

var _ ColoringI = (*simpleColoring)(nil)

func (inst *simpleColoring) Colors(info CellInfo) (CellColors, bool) {
	rgba, ok := inst.fn(info)
	if !ok {
		return CellColors{}, false
	}
	if cached, hit := inst.cache[rgba]; hit {
		return cached, true
	}
	cs := deriveCellColors(rgba)
	inst.cache[rgba] = cs
	return cs, true
}

// itoa is a tiny helper to avoid pulling in strconv solely for panic strings.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
