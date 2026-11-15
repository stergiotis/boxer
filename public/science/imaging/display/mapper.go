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

// floatTableSize is how finely a float plane's window is divided. The log
// transfer function is steepest at the low end, where one step of this table
// is about half a palette level; a coarser table would band there.
const floatTableSize = 1 << 16

// PlaneMapper maps one plane to colours through a palette. It is compiled for
// one sample kind and value transform and may be used from several
// goroutines at once.
type PlaneMapper struct {
	kind    imaging.SampleKindE
	table   []uint32 // indexed by the stored sample, for integer kinds
	invalid uint32

	// Float planes: a window onto a table that holds transfer and palette.
	floatTable  []uint32
	lo, scale   float64 // table index = (stored − lo) · scale
	under, over uint32  // what a value outside the window is drawn as
}

// CompilePlane compiles a recipe for planes of one kind and value transform.
// A nil palette is the grey ramp.
func CompilePlane(recipe Recipe, kind imaging.SampleKindE, transform imaging.ValueTransform, palette *Palette) (mapper *PlaneMapper, err error) {
	if err = recipe.Validate(); err != nil {
		return nil, err
	}
	if palette == nil {
		palette = GrayPalette()
	}
	colour := func(value float64) uint32 {
		x, under, over := recipe.position(value)
		if recipe.FlagClipped && under {
			return palette.Under
		}
		if recipe.FlagClipped && over {
			return palette.Over
		}
		return palette.Colors[int(math.Round(recipe.level(x)*255))]
	}
	mapper = &PlaneMapper{kind: kind, invalid: palette.Invalid}
	switch kind {
	case imaging.SampleKindU8, imaging.SampleKindU16:
		hi, _ := kind.NominalMax()
		mapper.table = make([]uint32, int(hi)+1)
		for s := range mapper.table {
			mapper.table[s] = colour(transform.Apply(float64(s)))
		}
	case imaging.SampleKindF32:
		// The table divides the window between the limits evenly, in stored
		// units: entry 0 is the value Lo and the last entry the value Hi,
		// whatever the sign of the value transform's scale, so an index
		// below the table is a value below the limits.
		a := (recipe.Limits.Lo - transform.Offset) / transform.Scale
		b := (recipe.Limits.Hi - transform.Offset) / transform.Scale
		mapper.lo, mapper.scale = a, float64(floatTableSize-1)/(b-a)
		mapper.floatTable = make([]uint32, floatTableSize)
		for i := range mapper.floatTable {
			x := float64(i) / float64(floatTableSize-1)
			mapper.floatTable[i] = palette.Colors[int(math.Round(recipe.level(x)*255))]
		}
		mapper.under, mapper.over = mapper.floatTable[0], mapper.floatTable[floatTableSize-1]
		if recipe.FlagClipped {
			mapper.under, mapper.over = palette.Under, palette.Over
		}
	default:
		return nil, eb.Build().Stringer("kind", kind).Errorf("no mapping for planes of this sample kind")
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
// result.
func (inst *PlaneMapper) Map(dst []uint32, plane imaging.Plane, alpha imaging.Plane, rect image.Rectangle) error {
	if plane.Kind() != inst.kind {
		return eb.Build().Stringer("plane", plane.Kind()).Stringer("mapper", inst.kind).
			Errorf("mapper was compiled for another sample kind")
	}
	if err := checkRegion(rect, plane.Width(), plane.Height(), len(dst)); err != nil {
		return err
	}
	w, rw := plane.Width(), rect.Dx()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		out := dst[(y-rect.Min.Y)*rw : (y-rect.Min.Y+1)*rw]
		at := y*w + rect.Min.X
		switch inst.kind {
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
		case imaging.SampleKindF32:
			table := (*[floatTableSize]uint32)(inst.floatTable)
			for x, s := range plane.F32()[at : at+rw] {
				t := (float64(s) - inst.lo) * inst.scale
				switch {
				case s != s:
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
	}
	if alpha.IsZero() {
		return nil
	}
	if alpha.Kind() != inst.kind || alpha.Width() != plane.Width() || alpha.Height() != plane.Height() {
		return eb.Build().Errorf("alpha plane does not match the plane it belongs to")
	}
	applyAlpha(dst, alpha, rect)
	return nil
}

// alpha8 scales a 16-bit alpha sample to 8 bits by the nominal range, rounded.
func alpha8(a uint16) uint32 { return (uint32(a)*255 + 32767) / 65535 }

// applyAlpha replaces each pixel's alpha by the alpha plane's sample, except
// where the pixel is already fully transparent: an invalid or flagged sample
// stays so.
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

// ColorMapper maps a colour image to pixels: one table takes a stored sample
// of any colour plane to its 8-bit level. It may be used from several
// goroutines at once.
type ColorMapper struct {
	kind  imaging.SampleKindE
	table []uint8
}

// CompileColor compiles a recipe for colour images of one integer sample
// kind. The recipe's palette and clipping flag do not apply.
func CompileColor(recipe Recipe, kind imaging.SampleKindE, transform imaging.ValueTransform) (mapper *ColorMapper, err error) {
	if err = recipe.Validate(); err != nil {
		return nil, err
	}
	hi, ok := kind.NominalMax()
	if !ok {
		return nil, eb.Build().Stringer("kind", kind).Errorf("colour images of this sample kind are not mapped")
	}
	mapper = &ColorMapper{kind: kind, table: make([]uint8, int(hi)+1)}
	for s := range mapper.table {
		x, _, _ := recipe.position(transform.Apply(float64(s)))
		mapper.table[s] = uint8(math.Round(recipe.level(x) * 255))
	}
	return mapper, nil
}

// Map writes the pixels of a colour image inside rect into dst, row by row:
// red, green and blue through the recipe, alpha as stored and opaque when the
// image has none.
func (inst *ColorMapper) Map(dst []uint32, img imaging.Image, rect image.Rectangle) error {
	if !img.Model().IsColor() {
		return eb.Build().Stringer("model", img.Model()).Errorf("image has no red, green and blue planes")
	}
	if img.Kind() != inst.kind {
		return eb.Build().Stringer("image", img.Kind()).Stringer("mapper", inst.kind).
			Errorf("mapper was compiled for another sample kind")
	}
	if err := checkRegion(rect, img.Width(), img.Height(), len(dst)); err != nil {
		return err
	}
	w, rw := img.Width(), rect.Dx()
	r, g, b := img.Plane(0), img.Plane(1), img.Plane(2)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		out := dst[(y-rect.Min.Y)*rw : (y-rect.Min.Y+1)*rw]
		at := y*w + rect.Min.X
		switch inst.kind {
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
		}
	}
	if alpha, ok := img.Alpha(); ok {
		applyAlphaAlways(dst, alpha, rect)
	}
	return nil
}

// applyAlphaAlways replaces each pixel's alpha by the alpha plane's sample.
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
