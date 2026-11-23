package display

import (
	"image"
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// Palette is the colour map of a single plane: 256 colours from the lowest
// level to the highest, and the colours for what the ramp cannot show. All
// are 0xRRGGBBAA, straight alpha.
type Palette struct {
	Colors [256]uint32
	// Invalid is drawn for an invalid sample.
	Invalid uint32
	// Under and Over are drawn for values outside the limits when the recipe
	// flags them.
	Under, Over uint32
}

// GrayPalette is the plain grey ramp: level i is the grey i, opaque, so an
// 8-bit greyscale image shown over its nominal range is its stored pixels.
// Its colours for invalid and flagged samples are fully transparent: whatever
// lies under the image shows through.
func GrayPalette() *Palette {
	p := &Palette{}
	for i := range p.Colors {
		v := uint32(i)
		p.Colors[i] = v<<24 | v<<16 | v<<8 | 0xff
	}
	return p
}

// floatTableSize is how finely a binned plane's window is divided. The log
// transfer function is steepest at the low end, where one step of this table
// is about half a palette level; a coarser table would band there.
//
// A sample gets the level of the step nearest to it, so the mapping differs
// from the function itself by what the function moves across one step. That
// is under a level for the functions at their usual parameters, and is not
// for a gamma exponent far below one: at 0.02 the first step alone spans
// some two hundred levels, and a sample just above the window's low end is
// shown at the low end's level.
const floatTableSize = 1 << 16

// PlaneSpec is what a mapper is compiled for: a sample kind, a value
// transform, and the stored value the planes declare invalid, if any
// (ADR-0398 §SD2). [PlaneSpecOf] reads it off a plane.
type PlaneSpec struct {
	Kind      imaging.SampleKindE
	Transform imaging.ValueTransform
	// Invalid is the stored value declared invalid when HasInvalid is set.
	Invalid    float64
	HasInvalid bool
}

// PlaneSpecOf is the spec of a plane: its kind, value transform and declared
// invalid value.
func PlaneSpecOf(p imaging.Plane) PlaneSpec {
	inv, ok := p.Invalid()
	return PlaneSpec{Kind: p.Kind(), Transform: p.Transform(), Invalid: inv, HasInvalid: ok}
}

// Validate refuses a declared invalid value the spec's kind cannot hold, as
// [imaging.Plane.WithInvalid] does: not finite, outside an integer kind's
// range or fractional for it.
func (inst PlaneSpec) Validate() error {
	if !inst.HasInvalid {
		return nil
	}
	v := inst.Invalid
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return eb.Build().Float64("invalid", v).Errorf("the invalid value must be finite")
	}
	if lo, hi, ok := inst.Kind.NominalRange(); ok && (v != math.Trunc(v) || v < lo || v > hi) {
		return eb.Build().Float64("invalid", v).Stringer("kind", inst.Kind).Errorf("the invalid value is not a sample this kind can hold")
	}
	return nil
}

// matches reports whether a plane's declaration is one a mapper compiled
// for this spec draws right: the same value, or none.
func (inst PlaneSpec) matches(p imaging.Plane) bool {
	inv, ok := p.Invalid()
	return !ok || (inst.HasInvalid && inv == inst.Invalid)
}

// PlaneMapper maps one plane to colours through a palette. It is compiled for
// one [PlaneSpec] and may be used from several goroutines at once.
type PlaneMapper struct {
	spec    PlaneSpec
	table   []uint32 // indexed by stored − lo, for a kind with codes
	invalid uint32

	// Binned kinds: a window onto a table that holds transfer and palette.
	floatTable  []uint32
	lo, scale   float64 // table index = (stored − lo) · scale
	under, over uint32  // what a value outside the window is drawn as
}

// CompilePlane compiles a recipe for planes of one kind and value transform
// that declare no invalid value. A nil palette is the grey ramp.
func CompilePlane(recipe Recipe, kind imaging.SampleKindE, transform imaging.ValueTransform, palette *Palette) (mapper *PlaneMapper, err error) {
	return CompilePlaneFor(recipe, PlaneSpec{Kind: kind, Transform: transform}, palette)
}

// CompilePlaneFor compiles a recipe for planes of one spec (ADR-0398 §SD1,
// §SD2). A nil palette is the grey ramp. A kind with codes
// ([imaging.SampleKindE.Codes]) compiles to a table with an entry per code,
// indexed by stored − lo, in which the declared invalid code is the
// palette's invalid colour, so drawing it costs nothing per pixel. Signed
// 32-bit and the float kinds compile to a table over the window between the
// limits; a sample is placed in it in float64 before it is quantised, and NaN
// and the declared value are drawn as the palette's invalid colour.
func CompilePlaneFor(recipe Recipe, spec PlaneSpec, palette *Palette) (mapper *PlaneMapper, err error) {
	if err = recipe.Validate(); err != nil {
		return nil, err
	}
	if err = spec.Validate(); err != nil {
		return nil, err
	}
	if palette == nil {
		palette = GrayPalette()
	}
	mapper = &PlaneMapper{spec: spec, invalid: palette.Invalid}
	switch spec.Kind {
	case imaging.SampleKindU8, imaging.SampleKindU16, imaging.SampleKindI16:
		lo, n, _ := spec.Kind.Codes()
		mapper.table = make([]uint32, n)
		for i := range mapper.table {
			level, under, over := recipe.level8(spec.Transform.Apply(float64(lo + int64(i))))
			switch {
			case recipe.FlagClipped && under:
				mapper.table[i] = palette.Under
			case recipe.FlagClipped && over:
				mapper.table[i] = palette.Over
			default:
				mapper.table[i] = palette.Colors[level]
			}
		}
		if spec.HasInvalid {
			mapper.table[int64(spec.Invalid)-lo] = palette.Invalid
		}
	case imaging.SampleKindF32, imaging.SampleKindF64, imaging.SampleKindI32:
		mapper.lo, mapper.scale = windowOf(recipe, spec.Transform)
		levels := windowLevels(recipe)
		mapper.floatTable = make([]uint32, floatTableSize)
		for i, level := range levels {
			mapper.floatTable[i] = palette.Colors[level]
		}
		mapper.under, mapper.over = mapper.floatTable[0], mapper.floatTable[floatTableSize-1]
		if recipe.FlagClipped {
			mapper.under, mapper.over = palette.Under, palette.Over
		}
	default:
		return nil, eb.Build().Stringer("kind", spec.Kind).Errorf("no mapping for planes of this sample kind")
	}
	return mapper, nil
}

func checkRegion(rect image.Rectangle, width, height, dstLen int) error {
	if rect.Empty() || !rect.In(image.Rect(0, 0, width, height)) {
		return eb.Build().Stringer("region", rect).Int("width", width).Int("height", height).
			Errorf("region to map is empty or not inside the image")
	}
	if dstLen != rect.Dx()*rect.Dy() {
		return eb.Build().Int("pixels", dstLen).Stringer("region", rect).
			Errorf("destination does not hold one pixel per pixel of the region")
	}
	return nil
}

// Map writes the colours of the plane's pixels inside rect into dst, row by
// row. A valid alpha plane, of the image's own kind, sets each pixel's alpha
// to that plane's sample scaled to 8 bits; pass the zero plane for an opaque
// result. A plane that declares an invalid value other than the one the
// mapper was compiled for is refused, since its invalid samples would be
// drawn as data; a plane that declares none is drawn under the mapper's.
func (inst *PlaneMapper) Map(dst []uint32, plane imaging.Plane, alpha imaging.Plane, rect image.Rectangle) error {
	if plane.Kind() != inst.spec.Kind {
		return eb.Build().Stringer("plane", plane.Kind()).Stringer("mapper", inst.spec.Kind).
			Errorf("mapper was compiled for another sample kind")
	}
	if !inst.spec.matches(plane) {
		inv, _ := plane.Invalid()
		return eb.Build().Float64("invalid", inv).Bool("mapperDeclares", inst.spec.HasInvalid).Float64("mapperInvalid", inst.spec.Invalid).
			Errorf("mapper was compiled for another invalid value")
	}
	if err := checkRegion(rect, plane.Width(), plane.Height(), len(dst)); err != nil {
		return err
	}
	w, rw := plane.Width(), rect.Dx()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		out := dst[(y-rect.Min.Y)*rw : (y-rect.Min.Y+1)*rw]
		at := y*w + rect.Min.X
		switch inst.spec.Kind {
		case imaging.SampleKindU8:
			table := (*[256]uint32)(inst.table)
			for x, s := range plane.U8()[at : at+rw] {
				out[x] = table[s]
			}
		case imaging.SampleKindU16:
			table := (*[65536]uint32)(inst.table)
			for x, s := range plane.U16()[at : at+rw] {
				out[x] = table[s]
			}
		case imaging.SampleKindI16:
			// stored − (−32768) is the bit pattern with its sign bit flipped.
			table := (*[65536]uint32)(inst.table)
			for x, s := range plane.I16()[at : at+rw] {
				out[x] = table[uint16(s)^0x8000]
			}
		case imaging.SampleKindF32:
			mapWindow(inst, out, plane.F32()[at:at+rw])
		case imaging.SampleKindF64:
			mapWindow(inst, out, plane.F64()[at:at+rw])
		case imaging.SampleKindI32:
			mapWindow(inst, out, plane.I32()[at:at+rw])
		}
	}
	if alpha.IsZero() {
		return nil
	}
	if alpha.Kind() != inst.spec.Kind || alpha.Width() != plane.Width() || alpha.Height() != plane.Height() {
		return eb.Build().Errorf("alpha plane does not match the plane it belongs to")
	}
	applyAlpha(dst, alpha, rect)
	return nil
}

// windowSampleI is a sample kind mapped through a window onto a table.
type windowSampleI interface {
	float32 | float64 | int32
}

// mapWindow maps one row of a binned plane: the sample is placed in the
// window in float64, then quantised to the table. NaN and the declared
// invalid value are the invalid colour.
func mapWindow[T windowSampleI](inst *PlaneMapper, out []uint32, samples []T) {
	table := (*[floatTableSize]uint32)(inst.floatTable)
	hasInv := inst.spec.HasInvalid
	// The declared value is one the kind holds exactly (PlaneSpec.Validate), so
	// the conversion is exact.
	skip := T(inst.spec.Invalid)
	for x, s := range samples {
		t := (float64(s) - inst.lo) * inst.scale
		switch {
		case s != s || (hasInv && s == skip):
			out[x] = inst.invalid
		case t < 0:
			out[x] = inst.under
		case t > floatTableSize-1:
			out[x] = inst.over
		default:
			out[x] = table[int(t+0.5)]
		}
	}
}

// alpha8 scales a 16-bit alpha sample to 8 bits by the nominal range, rounded.
func alpha8(a uint16) uint32 { return (uint32(a)*255 + 32767) / 65535 }

// applyAlpha replaces each pixel's alpha by the alpha plane's sample, except
// where the pixel is already fully transparent: an invalid or flagged sample
// stays so. Alpha of a kind other than unsigned 8- and 16-bit is not applied:
// no reader here gives one a meaning.
func applyAlpha(dst []uint32, alpha imaging.Plane, rect image.Rectangle) {
	w, rw := alpha.Width(), rect.Dx()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		out := dst[(y-rect.Min.Y)*rw : (y-rect.Min.Y+1)*rw]
		at := y*w + rect.Min.X
		switch alpha.Kind() {
		case imaging.SampleKindU8:
			for x, a := range alpha.U8()[at : at+rw] {
				if out[x]&0xff != 0 {
					out[x] = out[x]&^0xff | uint32(a)
				}
			}
		case imaging.SampleKindU16:
			for x, a := range alpha.U16()[at : at+rw] {
				if out[x]&0xff != 0 {
					out[x] = out[x]&^0xff | alpha8(a)
				}
			}
		}
	}
}

// applyAlphaAlways replaces each pixel's alpha by the alpha plane's sample,
// for the kinds [applyAlpha] applies.
func applyAlphaAlways(dst []uint32, alpha imaging.Plane, rect image.Rectangle) {
	w, rw := alpha.Width(), rect.Dx()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		out := dst[(y-rect.Min.Y)*rw : (y-rect.Min.Y+1)*rw]
		at := y*w + rect.Min.X
		switch alpha.Kind() {
		case imaging.SampleKindU8:
			for x, a := range alpha.U8()[at : at+rw] {
				out[x] = out[x]&^0xff | uint32(a)
			}
		case imaging.SampleKindU16:
			for x, a := range alpha.U16()[at : at+rw] {
				out[x] = out[x]&^0xff | alpha8(a)
			}
		}
	}
}

// ColorMapper maps a colour image to pixels: one table takes a stored sample
// of any colour plane to its 8-bit level. It may be used from several
// goroutines at once.
type ColorMapper struct {
	spec  PlaneSpec
	table []uint8 // indexed by stored − lo
}

// CompileColor compiles a recipe for colour images of one sample kind with
// codes whose planes declare no invalid value. The recipe's palette and
// clipping flag do not apply.
func CompileColor(recipe Recipe, kind imaging.SampleKindE, transform imaging.ValueTransform) (mapper *ColorMapper, err error) {
	return CompileColorFor(recipe, PlaneSpec{Kind: kind, Transform: transform})
}

// CompileColorFor compiles a recipe for colour images whose colour planes
// have one spec (ADR-0398 §SD1, §SD2). The kind must have codes
// ([imaging.SampleKindE.Codes]): unsigned 8- and 16-bit and signed 16-bit;
// the binned kinds are refused by name, and a viewer shows such an image one
// plane at a time. A pixel at which any colour plane holds the declared
// invalid value is drawn fully transparent, as the grey palette draws an
// invalid sample. The recipe's palette and clipping flag do not apply.
func CompileColorFor(recipe Recipe, spec PlaneSpec) (mapper *ColorMapper, err error) {
	if err = recipe.Validate(); err != nil {
		return nil, err
	}
	if err = spec.Validate(); err != nil {
		return nil, err
	}
	lo, n, ok := spec.Kind.Codes()
	if !ok {
		return nil, eb.Build().Stringer("kind", spec.Kind).Errorf("colour images of this sample kind are not mapped")
	}
	return &ColorMapper{spec: spec, table: codeLevels(recipe, spec.Transform, lo, n)}, nil
}

// level8 is the 8-bit level a value is drawn at — [Recipe.Level] scaled to
// 0…255 and rounded half away from zero — and whether it lay below or above
// the limits. Every mapper here quantises a level through it, so a palette
// index, a colour plane's level and a composite layer's level of one value
// under one recipe are the same number.
func (inst Recipe) level8(value float64) (level uint8, under, over bool) {
	x, under, over := inst.position(value)
	return uint8(math.Round(inst.level(x) * 255)), under, over
}

// codeLevels is the 8-bit level of every code lo … lo+n−1 of a kind with
// codes, through the value transform, indexed by stored − lo.
func codeLevels(recipe Recipe, transform imaging.ValueTransform, lo int64, n int) (levels []uint8) {
	levels = make([]uint8, n)
	for i := range levels {
		levels[i], _, _ = recipe.level8(transform.Apply(float64(lo + int64(i))))
	}
	return levels
}

// windowOf places the window between a recipe's limits in stored units: a
// stored sample s falls at table index (s − lo) · scale. The table divides
// the window evenly: entry 0 is the value Lo and the last entry the value
// Hi, whatever the sign of the value transform's scale, so an index below
// the table is a value below the limits.
func windowOf(recipe Recipe, transform imaging.ValueTransform) (lo, scale float64) {
	a := (recipe.Limits.Lo - transform.Offset) / transform.Scale
	b := (recipe.Limits.Hi - transform.Offset) / transform.Scale
	return a, float64(floatTableSize-1) / (b - a)
}

// windowLevels is the 8-bit level of each of the window's [floatTableSize]
// steps, the first at Lo and the last at Hi.
func windowLevels(recipe Recipe) (levels []uint8) {
	levels = make([]uint8, floatTableSize)
	for i := range levels {
		x := float64(i) / float64(floatTableSize-1)
		levels[i] = uint8(math.Round(recipe.level(x) * 255))
	}
	return levels
}

// Map writes the pixels of a colour image inside rect into dst, row by row:
// red, green and blue through the recipe, alpha as stored and opaque when the
// image has none, and a pixel any colour plane of which is invalid fully
// transparent. An image whose planes declare an invalid value other than
// the mapper's is refused.
func (inst *ColorMapper) Map(dst []uint32, img imaging.Image, rect image.Rectangle) error {
	if !img.Model().IsColor() {
		return eb.Build().Stringer("model", img.Model()).Errorf("image has no red, green and blue planes")
	}
	if img.Kind() != inst.spec.Kind {
		return eb.Build().Stringer("image", img.Kind()).Stringer("mapper", inst.spec.Kind).
			Errorf("mapper was compiled for another sample kind")
	}
	if !inst.spec.matches(img.Plane(0)) {
		inv, _ := img.Plane(0).Invalid()
		return eb.Build().Float64("invalid", inv).Bool("mapperDeclares", inst.spec.HasInvalid).Float64("mapperInvalid", inst.spec.Invalid).
			Errorf("mapper was compiled for another invalid value")
	}
	if err := checkRegion(rect, img.Width(), img.Height(), len(dst)); err != nil {
		return err
	}
	w, rw := img.Width(), rect.Dx()
	r, g, b := img.Plane(0), img.Plane(1), img.Plane(2)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		out := dst[(y-rect.Min.Y)*rw : (y-rect.Min.Y+1)*rw]
		at := y*w + rect.Min.X
		switch inst.spec.Kind {
		case imaging.SampleKindU8:
			table := (*[256]uint8)(inst.table)
			rs, gs, bs := r.U8()[at:at+rw], g.U8()[at:at+rw], b.U8()[at:at+rw]
			for x := range out {
				out[x] = uint32(table[rs[x]])<<24 | uint32(table[gs[x]])<<16 | uint32(table[bs[x]])<<8 | 0xff
			}
		case imaging.SampleKindU16:
			table := (*[65536]uint8)(inst.table)
			rs, gs, bs := r.U16()[at:at+rw], g.U16()[at:at+rw], b.U16()[at:at+rw]
			for x := range out {
				out[x] = uint32(table[rs[x]])<<24 | uint32(table[gs[x]])<<16 | uint32(table[bs[x]])<<8 | 0xff
			}
		case imaging.SampleKindI16:
			table := (*[65536]uint8)(inst.table)
			rs, gs, bs := r.I16()[at:at+rw], g.I16()[at:at+rw], b.I16()[at:at+rw]
			for x := range out {
				out[x] = uint32(table[uint16(rs[x])^0x8000])<<24 | uint32(table[uint16(gs[x])^0x8000])<<16 | uint32(table[uint16(bs[x])^0x8000])<<8 | 0xff
			}
		}
	}
	if alpha, ok := img.Alpha(); ok {
		applyAlphaAlways(dst, alpha, rect)
	}
	if inst.spec.HasInvalid {
		markInvalid(dst, img, rect, inst.spec.Invalid)
	}
	return nil
}

// markInvalid draws fully transparent every pixel at which a colour plane
// holds the declared invalid value. It runs after alpha, which would
// otherwise give such a pixel back its opacity.
func markInvalid(dst []uint32, img imaging.Image, rect image.Rectangle, inv float64) {
	w, rw := img.Width(), rect.Dx()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		out := dst[(y-rect.Min.Y)*rw : (y-rect.Min.Y+1)*rw]
		at := y*w + rect.Min.X
		for i := range 3 {
			p := img.Plane(i)
			switch p.Kind() {
			case imaging.SampleKindU8:
				markRow(out, p.U8()[at:at+rw], uint8(inv))
			case imaging.SampleKindU16:
				markRow(out, p.U16()[at:at+rw], uint16(inv))
			case imaging.SampleKindI16:
				markRow(out, p.I16()[at:at+rw], int16(inv))
			}
		}
	}
}

func markRow[T uint8 | uint16 | int16](out []uint32, samples []T, inv T) {
	for x, s := range samples {
		if s == inv {
			out[x] = 0
		}
	}
}
