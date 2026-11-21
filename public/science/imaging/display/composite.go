package display

import (
	"image"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// LayerColorE is the colour a composite layer is drawn in (ADR-0401 §SD4):
// one of a fixed set whose sums stay readable. Each is full scale in some of
// red, green and blue and zero in the others. The zero value is no colour
// and is refused.
type LayerColorE uint8

const (
	LayerColorRed LayerColorE = iota + 1
	LayerColorGreen
	LayerColorBlue
	LayerColorCyan
	LayerColorMagenta
	LayerColorYellow
	LayerColorWhite
)

// AllLayerColors lists the layer colours, in the order [DefaultLayerColors]
// cycles through.
var AllLayerColors = []LayerColorE{LayerColorRed, LayerColorGreen, LayerColorBlue, LayerColorCyan, LayerColorMagenta, LayerColorYellow, LayerColorWhite}

var layerColorNames = [...]string{"", "red", "green", "blue", "cyan", "magenta", "yellow", "white"}

// layerColorLanes is each colour's channels as lanes of a composite
// accumulator: see [CompositeMapper.Map].
var layerColorLanes = [...]uint32{
	0,
	laneR,
	laneG,
	laneB,
	laneG | laneB,
	laneR | laneB,
	laneR | laneG,
	laneR | laneG | laneB,
}

func (inst LayerColorE) isValid() bool { return inst >= LayerColorRed && inst <= LayerColorWhite }

func (inst LayerColorE) String() string {
	if inst.isValid() {
		return layerColorNames[inst]
	}
	return "unknown"
}

// MarshalText writes the colour's name, so a stored composite reads as text.
func (inst LayerColorE) MarshalText() ([]byte, error) {
	if !inst.isValid() {
		return nil, eb.Build().Uint8("color", uint8(inst)).Errorf("layer colour has no name")
	}
	return []byte(layerColorNames[inst]), nil
}

// UnmarshalText reads a colour's name.
func (inst *LayerColorE) UnmarshalText(text []byte) error {
	for i, name := range layerColorNames {
		if i > 0 && string(text) == name {
			*inst = LayerColorE(i)
			return nil
		}
	}
	return eb.Build().Str("name", string(text)).Errorf("no layer colour of that name")
}

// Pixel is the colour at full level as a 0xRRGGBBAA pixel, opaque: the
// swatch a legend draws.
func (inst LayerColorE) Pixel() uint32 {
	if !inst.isValid() {
		return 0
	}
	l := layerColorLanes[inst]
	var px uint32 = 0xff
	if l&laneR != 0 {
		px |= 0xff << 24
	}
	if l&laneG != 0 {
		px |= 0xff << 16
	}
	if l&laneB != 0 {
		px |= 0xff << 8
	}
	return px
}

// MaxLayers is the most layers a composite holds (ADR-0401 §SD1).
const MaxLayers = 8

// DefaultLayerColors is the colours n layers start in (ADR-0401 §SD4): one
// layer white, which shows its plane in grey; two green and magenta; three
// red, green and blue; more cycle through [AllLayerColors] from red.
func DefaultLayerColors(n int) (colors []LayerColorE) {
	switch n {
	case 1:
		return []LayerColorE{LayerColorWhite}
	case 2:
		return []LayerColorE{LayerColorGreen, LayerColorMagenta}
	}
	colors = make([]LayerColorE, max(n, 0))
	for i := range colors {
		colors[i] = AllLayerColors[i%len(AllLayerColors)]
	}
	return colors
}

// Layer is one layer of a composite (ADR-0401 §SD1): how its plane is
// mapped to a level, the colour the level is drawn in, and whether it is
// drawn. Which plane it shows is not part of it: [CompositeMapper.Map]
// takes the planes in layer order.
type Layer struct {
	// Recipe maps the layer's plane to a level: its limits, transfer
	// function and parameter, and Invert. A layer has no palette and flags
	// nothing — its colour is Color — so a recipe that names a palette or
	// sets FlagClipped is refused.
	Recipe Recipe
	Color  LayerColorE
	// Hidden leaves the layer out of every pixel: its level is not added
	// and its invalid samples do not make a pixel invalid.
	Hidden bool
}

// Composite is an ordered list of layers, shown together (ADR-0401 §SD1).
type Composite []Layer

// NewComposite is a linked composite of n layers (ADR-0401 §SD2): every
// layer under one recipe, in [DefaultLayerColors], shown. The recipe's
// palette and clipping flag, which a layer has no use for, are cleared.
func NewComposite(n int, recipe Recipe) (c Composite) {
	c = make(Composite, 0, max(n, 0))
	for _, color := range DefaultLayerColors(n) {
		c = append(c, Layer{Recipe: layerRecipe(recipe), Color: color})
	}
	return c
}

// layerRecipe is a recipe without what a layer does not use.
func layerRecipe(r Recipe) Recipe {
	r.Palette, r.FlagClipped = "", false
	return r
}

// IsLinked reports whether every shown layer has one recipe (ADR-0401
// §SD2): a composite that is not linked stretches its layers separately,
// which changes the colour balance, and a view says so. A composite with
// fewer than two shown layers is linked.
func (inst Composite) IsLinked() bool {
	var first *Recipe
	for i := range inst {
		if inst[i].Hidden {
			continue
		}
		if first == nil {
			first = &inst[i].Recipe
			continue
		}
		if inst[i].Recipe != *first {
			return false
		}
	}
	return true
}

// WithRecipe is the composite with every layer, shown or hidden, under one
// recipe: linked. The recipe's palette and clipping flag are cleared.
func (inst Composite) WithRecipe(recipe Recipe) (c Composite) {
	c = make(Composite, len(inst))
	for i, l := range inst {
		l.Recipe = layerRecipe(recipe)
		c[i] = l
	}
	return c
}

// Validate reports why a composite cannot be compiled: no layers or more
// than [MaxLayers], a layer of no known colour, and a layer recipe that
// cannot be compiled or names a palette or sets FlagClipped.
func (inst Composite) Validate() error {
	if len(inst) == 0 {
		return eb.Build().Errorf("a composite needs at least one layer")
	}
	if len(inst) > MaxLayers {
		return eb.Build().Int("layers", len(inst)).Int("max", MaxLayers).Errorf("a composite holds at most eight layers")
	}
	for i, l := range inst {
		if !l.Color.isValid() {
			return eb.Build().Int("layer", i).Uint8("color", uint8(l.Color)).Errorf("layer has no colour of the set")
		}
		if l.Recipe.Palette != "" {
			return eb.Build().Int("layer", i).Str("palette", l.Recipe.Palette).Errorf("a layer is drawn in its colour and takes no palette")
		}
		if l.Recipe.FlagClipped {
			return eb.Build().Int("layer", i).Errorf("a layer has no colour to spare for flagging clipped values")
		}
		if err := l.Recipe.Validate(); err != nil {
			return eb.Build().Int("layer", i).Errorf("layer recipe cannot be compiled: %w", err)
		}
	}
	return nil
}

// The accumulator of a composite pixel is a uint32 in one of two layouts.
// When no channel is shared by two shown layers it is the pixel itself: an
// 8-bit lane per channel at the pixel's own position, which a single level
// cannot overflow, and the invalid flag in bit 0. Otherwise it holds three
// 10-bit lanes, blue from bit 0, and the flag in bit 31.
const (
	// narrowLanes picks, from a colour's pixel, bit 0 of each channel byte.
	narrowLanes uint32 = 0x01010100
	narrowFlag  uint32 = 1
	laneB       uint32 = 1 << 0
	laneG       uint32 = 1 << 10
	laneR       uint32 = 1 << 20
	// laneLow is bit 0 of each lane.
	laneLow = laneR | laneG | laneB
	// wideFlag marks a pixel at which a shown layer is invalid.
	wideFlag uint32 = 1 << 31
	// lanesBeforeSaturating is how many full-scale levels a 10-bit lane holds
	// without overflowing.
	lanesBeforeSaturating = 4
)

// compositeLayer is one compiled shown layer.
type compositeLayer struct {
	spec PlaneSpec
	// index is the layer's position in the composite, which is the
	// position of its plane in Map's argument.
	index int
	// lanes is the colour's channels in the mapper's layout, flag its
	// invalid flag.
	lanes, flag uint32
	// codes is the 8-bit level of each code of a kind with codes, indexed
	// by stored − lo.
	codes []uint8
	// window is the 8-bit level of each step of the window of a binned
	// kind; a stored sample falls at step (s − lo) · scale.
	window    []uint8
	lo, scale float64
}

// CompositeMapper maps the planes of a composite to pixels (ADR-0401 §SD1,
// §SD3, §SD5). It is compiled for one composite and one [PlaneSpec] per
// layer, holds no state that a mapping changes, and may be used from
// several goroutines at once.
type CompositeMapper struct {
	layers []compositeLayer
	n      int
	// wide is set when a channel is shared by two shown layers, so that
	// levels are summed in 10-bit lanes and cut to 255.
	wide bool
}

// CompileComposite compiles a composite for planes of the given specs, one
// per layer, in layer order (ADR-0401 §SD3): each shown layer through the
// mapping of a single plane — a kind with codes ([imaging.SampleKindE.Codes])
// to a table of one 8-bit level per code, signed 32-bit and the float kinds
// to the window between the layer's limits divided into 65536 steps, as
// [CompilePlaneFor] does — so a level here is the index the grey palette of
// a plane mapper reads for the same sample and recipe. Layers may differ in
// sample kind, value transform and declared invalid value. Hidden layers
// are validated and not compiled.
func CompileComposite(layers []Layer, specs []PlaneSpec) (mapper *CompositeMapper, err error) {
	if err = Composite(layers).Validate(); err != nil {
		return nil, err
	}
	if len(specs) != len(layers) {
		return nil, eb.Build().Int("layers", len(layers)).Int("specs", len(specs)).Errorf("a composite needs one plane spec per layer")
	}
	mapper = &CompositeMapper{n: len(layers)}
	var shared uint32
	for _, l := range layers {
		if l.Hidden {
			continue
		}
		if shared&l.Color.Pixel()&narrowLanes != 0 {
			mapper.wide = true
		}
		shared |= l.Color.Pixel() & narrowLanes
	}
	for i, l := range layers {
		spec := specs[i]
		if err = spec.Validate(); err != nil {
			return nil, eb.Build().Int("layer", i).Errorf("layer plane spec: %w", err)
		}
		if l.Hidden {
			continue
		}
		c := compositeLayer{spec: spec, index: i, lanes: l.Color.Pixel() & narrowLanes, flag: narrowFlag}
		if mapper.wide {
			c.lanes, c.flag = layerColorLanes[l.Color], wideFlag
		}
		switch spec.Kind {
		case imaging.SampleKindU8, imaging.SampleKindU16, imaging.SampleKindI16:
			lo, n, _ := spec.Kind.Codes()
			c.codes = codeLevels(l.Recipe, spec.Transform, lo, n)
		case imaging.SampleKindF32, imaging.SampleKindF64, imaging.SampleKindI32:
			c.lo, c.scale = windowOf(l.Recipe, spec.Transform)
			c.window = windowLevels(l.Recipe)
		default:
			return nil, eb.Build().Int("layer", i).Stringer("kind", spec.Kind).Errorf("no mapping for planes of this sample kind")
		}
		mapper.layers = append(mapper.layers, c)
	}
	return mapper, nil
}

// Map writes the composite's pixels inside rect into dst, row by row, as
// 0xRRGGBBAA. planes holds one plane per layer, in layer order; a hidden
// layer's plane is not read and may be the zero plane. Every shown layer's
// plane must be of its spec's kind, declare its spec's invalid value or
// none (a plane that declares none is drawn under the spec's declaration,
// as the single-plane mapper does), and be of one size with the others
// (ADR-0401 §SD6).
//
// The arithmetic of a pixel: each shown layer k gives an 8-bit level L_k —
// round(255 · y) of [Recipe.Level], from the same table a plane mapper
// compiles, so for a kind with codes it is exactly what [ColorMapper]
// gives a colour plane; for a binned kind it is the level of the window
// step nearest the sample, the ends for a value outside the limits, as
// [PlaneMapper] draws it, within one level of round(255 · y). Each
// channel c of red, green and blue is min(255, Σ L_k over the shown layers
// whose colour has c): a saturating sum of 8-bit levels, the colours being
// full scale or zero in each channel. Alpha is 255. A pixel at which any
// shown layer's sample is invalid — NaN, or its spec's declared value — is
// 0x00000000, fully transparent, as [ColorMapper] draws an invalid pixel
// (ADR-0401 §SD5). With no shown layer every pixel is opaque black. So
// three layers red, green and blue under one recipe are, bit for bit, the
// colour mapping of the same three planes (ADR-0401 §SD1).
func (inst *CompositeMapper) Map(dst []uint32, planes []imaging.Plane, rect image.Rectangle) error {
	if len(planes) != inst.n {
		return eb.Build().Int("planes", len(planes)).Int("layers", inst.n).Errorf("a composite maps one plane per layer")
	}
	w, h := 0, 0
	for _, l := range inst.layers {
		p := planes[l.index]
		if p.Kind() != l.spec.Kind {
			return eb.Build().Int("layer", l.index).Stringer("plane", p.Kind()).Stringer("mapper", l.spec.Kind).
				Errorf("mapper was compiled for another sample kind")
		}
		if !l.spec.matches(p) {
			inv, _ := p.Invalid()
			return eb.Build().Int("layer", l.index).Float64("invalid", inv).Bool("mapperDeclares", l.spec.HasInvalid).Float64("mapperInvalid", l.spec.Invalid).
				Errorf("mapper was compiled for another invalid value")
		}
		if w == 0 {
			w, h = p.Width(), p.Height()
		} else if p.Width() != w || p.Height() != h {
			return eb.Build().Int("layer", l.index).Int("width", p.Width()).Int("height", p.Height()).Int("firstWidth", w).Int("firstHeight", h).
				Errorf("the planes of a composite share one grid")
		}
	}
	if w == 0 {
		// No shown layer: the region is checked against any plane given.
		for _, p := range planes {
			if !p.IsZero() {
				w, h = p.Width(), p.Height()
				break
			}
		}
	}
	if w == 0 {
		if rect.Empty() || rect.Min.X < 0 || rect.Min.Y < 0 || len(dst) != rect.Dx()*rect.Dy() {
			return eb.Build().Stringer("region", rect).Int("pixels", len(dst)).Errorf("region to map is empty or does not match the destination")
		}
	} else if err := checkRegion(rect, w, h, len(dst)); err != nil {
		return err
	}
	rw := rect.Dx()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		out := dst[(y-rect.Min.Y)*rw : (y-rect.Min.Y+1)*rw]
		clear(out)
		at := y*w + rect.Min.X
		for k := range inst.layers {
			l := &inst.layers[k]
			// Four full-scale levels fill a lane; before a fifth is added the
			// lanes are cut back to 255, and again before an eighth, which
			// the sum of non-negative levels does not distinguish from
			// cutting once at the end.
			if inst.wide && k >= lanesBeforeSaturating && (k-lanesBeforeSaturating)%(lanesBeforeSaturating-1) == 0 {
				saturateRow(out)
			}
			p := planes[l.index]
			switch l.spec.Kind {
			case imaging.SampleKindU8:
				addCodesU8(out, p.U8()[at:at+rw], (*[256]uint8)(l.codes), l.lanes)
				if l.spec.HasInvalid {
					flagRow(out, p.U8()[at:at+rw], uint8(l.spec.Invalid), l.flag)
				}
			case imaging.SampleKindU16:
				addCodesU16(out, p.U16()[at:at+rw], (*[65536]uint8)(l.codes), l.lanes)
				if l.spec.HasInvalid {
					flagRow(out, p.U16()[at:at+rw], uint16(l.spec.Invalid), l.flag)
				}
			case imaging.SampleKindI16:
				addCodesI16(out, p.I16()[at:at+rw], (*[65536]uint8)(l.codes), l.lanes)
				if l.spec.HasInvalid {
					flagRow(out, p.I16()[at:at+rw], int16(l.spec.Invalid), l.flag)
				}
			case imaging.SampleKindF32:
				addWindow(out, p.F32()[at:at+rw], l)
			case imaging.SampleKindF64:
				addWindow(out, p.F64()[at:at+rw], l)
			case imaging.SampleKindI32:
				addWindow(out, p.I32()[at:at+rw], l)
			}
		}
		if inst.wide {
			packWide(out)
		} else {
			packNarrow(out)
		}
	}
	return nil
}

// addCodesU8 adds one row of an unsigned 8-bit layer: a level per code.
func addCodesU8(out []uint32, samples []uint8, table *[256]uint8, lanes uint32) {
	for x, s := range samples {
		out[x] += uint32(table[s]) * lanes
	}
}

// addCodesU16 adds one row of an unsigned 16-bit layer.
func addCodesU16(out []uint32, samples []uint16, table *[65536]uint8, lanes uint32) {
	for x, s := range samples {
		out[x] += uint32(table[s]) * lanes
	}
}

// addCodesI16 indexes by stored − (−32768), the bit pattern with its sign
// bit flipped.
func addCodesI16(out []uint32, samples []int16, table *[65536]uint8, lanes uint32) {
	for x, s := range samples {
		out[x] += uint32(table[uint16(s)^0x8000]) * lanes
	}
}

// flagRow marks the pixels whose sample is the declared invalid code.
func flagRow[T uint8 | uint16 | int16](out []uint32, samples []T, inv T, flag uint32) {
	for x, s := range samples {
		if s == inv {
			out[x] |= flag
		}
	}
}

// addWindow adds one row of a binned layer as mapWindow draws it: placed in
// the window in float64, quantised to the nearest step, the ends for a
// value outside, NaN and the declared value flagged invalid.
func addWindow[T windowSampleI](out []uint32, samples []T, l *compositeLayer) {
	table := (*[floatTableSize]uint8)(l.window)
	under, over := uint32(table[0])*l.lanes, uint32(table[floatTableSize-1])*l.lanes
	hasInv := l.spec.HasInvalid
	// The declared value is one the kind holds exactly (PlaneSpec.Validate),
	// so the conversion is exact.
	skip := T(l.spec.Invalid)
	lo, scale, lanes, flag := l.lo, l.scale, l.lanes, l.flag
	for x, s := range samples {
		t := (float64(s) - lo) * scale
		switch {
		case s != s || (hasInv && s == skip):
			out[x] |= flag
		case t < 0:
			out[x] += under
		case t > floatTableSize-1:
			out[x] += over
		default:
			out[x] += uint32(table[int(t+0.5)]) * lanes
		}
	}
}

// saturateRow cuts every lane holding 256 or more to 255. A lane holds at
// most 1020 when it is cut — four levels before the first cut, 255 plus
// three levels before the next — so bits 8 and 9 of a lane say whether it
// overflowed.
func saturateRow(out []uint32) {
	for x, a := range out {
		over := (a>>8 | a>>9) & laneLow
		out[x] = a&^(over*0x300) | over*0xff
	}
}

// packNarrow turns accumulators of the narrow layout into pixels: opaque,
// and a flagged pixel fully transparent.
func packNarrow(out []uint32) {
	for x, a := range out {
		// All ones unless the flag, bit 0, is set.
		keep := a&narrowFlag - 1
		out[x] = (a | 0xff) & keep
	}
}

// packWide turns accumulators of the wide layout into pixels: each lane cut
// to 255, opaque, and a flagged pixel fully transparent.
func packWide(out []uint32) {
	for x, a := range out {
		r := min(a>>20&0x3ff, 0xff)
		g := min(a>>10&0x3ff, 0xff)
		b := min(a&0x3ff, 0xff)
		// All ones unless the flag, the sign bit, is set.
		keep := ^uint32(int32(a) >> 31)
		out[x] = (r<<24 | g<<16 | b<<8 | 0xff) & keep
	}
}
