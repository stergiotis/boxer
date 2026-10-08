package grib

import (
	"bytes"
	"errors"
	"image"
	"image/png"
	"math"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/science/geo/grib/aec"
	"github.com/stergiotis/boxer/public/science/geo/grib/jpeg2000"
)

// ValuesE decodes the field into dst (grown as needed) in stored order:
// one value per grid point, NaN where the bitmap or the packing's missing
// value management leaves no value (ADR-0292 §R2). The result is the
// double the reference implementation computes: (R + X·2^E) × 10^−D.
//
// Packings outside the subset — edition 1's spectral and second-order
// packings among them — and bitmaps other than present or absent are
// [ErrUnsupported]; a data section shorter than its bit count is
// [ErrInconsistent].
func (inst *Field) ValuesE(dst []float64) (values []float64, err error) {
	err = inst.Packing.supportedE()
	if err != nil {
		return
	}
	n := int(inst.Grid.NumPoints)
	if cap(dst) < n {
		dst = make([]float64, n)
	}
	values = dst[:n]
	coded := inst.Bitmap.Count()
	if inst.Packing.Template != 4 && inst.Packing.Template != 41 && inst.Packing.Template != 40 && inst.Packing.Template != jpegPreStandard && uint32(coded) != inst.Packing.NumValues {
		// Templates 5.40 and 5.41 count the image's samples; 5.4 the raw floats.
		err = eb.Build().Int("bitmapCount", coded).Uint32("numValues", inst.Packing.NumValues).Errorf("bitmap and data representation disagree on the number of coded values: %w", ErrInconsistent)
		return
	}
	if inst.Packing.Grib1MissingField {
		for i := range values {
			values[i] = math.NaN()
		}
		return
	}
	// Decode the coded values into the front of a scratch slice, then
	// spread them over the bitmap.
	var packed []float64
	if inst.Bitmap.Present {
		packed = make([]float64, coded)
	} else {
		packed = values
	}
	switch inst.Packing.Template {
	case 0:
		err = inst.unpackSimpleE(packed)
	case 2, 3:
		err = inst.unpackComplexE(packed)
	case 4:
		err = inst.unpackIEEEE(packed)
	case 41:
		err = inst.unpackPNGE(packed)
	case 42:
		err = inst.unpackCCSDSE(packed)
	case 40, jpegPreStandard:
		err = inst.unpackJPEGE(packed)
	}
	if err != nil {
		return
	}
	if inst.Bitmap.Present {
		k := 0
		for i := range values {
			if inst.Bitmap.Set(i) {
				values[i] = packed[k]
				k++
			} else {
				values[i] = math.NaN()
			}
		}
	}
	return
}

// unpackSimpleE is template 5.0. A width of 0 codes a constant field whose
// every value is the reference, scaled: R × 10^−D, with the decimal scale
// applied — the omission that has shipped in a reference library.
func (inst *Field) unpackSimpleE(out []float64) (err error) {
	p := &inst.Packing
	bin, dec := p.scaleFactors()
	ref := float64(p.Reference)
	if p.Bits == 0 {
		v := ref * dec
		for i := range out {
			out[i] = v
		}
		return
	}
	need := (uint64(len(out))*uint64(p.Bits) + 7) / 8
	if uint64(len(inst.data)) < need {
		err = eb.Build().Int("bytes", len(inst.data)).Uint64("needed", need).Uint8("bits", p.Bits).Int("values", len(out)).Errorf("data section shorter than its bit count: %w", ErrInconsistent)
		return
	}
	br := bitReader{b: inst.data}
	for i := range out {
		out[i] = (ref + float64(br.bits(p.Bits))*bin) * dec
	}
	return
}

// unpackIEEEE is template 5.4: raw IEEE floats, single or double.
func (inst *Field) unpackIEEEE(out []float64) (err error) {
	p := &inst.Packing
	var width int
	switch p.IEEEPrecision {
	case 1:
		width = 4
	case 2:
		width = 8
	default:
		err = unsupportedE("ieee precision " + strconv.Itoa(int(p.IEEEPrecision)))
		return
	}
	if len(inst.data) < width*len(out) {
		err = eb.Build().Int("bytes", len(inst.data)).Int("needed", width*len(out)).Errorf("data section shorter than its values: %w", ErrInconsistent)
		return
	}
	r := rd{b: inst.data}
	for i := range out {
		if width == 4 {
			out[i] = float64(r.f32())
		} else {
			out[i] = math.Float64frombits(r.u64())
		}
	}
	return
}

// unpackPNGE is template 5.41: the packed integers are the samples of a
// PNG image whose depth is the bit count (1–16 as greyscale, 24 as RGB, 32
// as RGBA), read through the standard library. The image's dimensions must
// multiply to the value count, and its depth must agree with Section 5 —
// the mismatch that produced "invalid grid values" silently elsewhere.
func (inst *Field) unpackPNGE(out []float64) (err error) {
	p := &inst.Packing
	bin, dec := p.scaleFactors()
	ref := float64(p.Reference)
	if p.Bits == 0 {
		v := ref * dec
		for i := range out {
			out[i] = v
		}
		return
	}
	img, err := png.Decode(bytes.NewReader(inst.data))
	if err != nil {
		err = eb.Build().Errorf("png data section: %w: %w", ErrMalformed, err)
		return
	}
	b := img.Bounds()
	if b.Dx()*b.Dy() != len(out) {
		err = eb.Build().Int("width", b.Dx()).Int("height", b.Dy()).Int("values", len(out)).Errorf("png dimensions do not multiply to the value count: %w", ErrInconsistent)
		return
	}
	k := 0
	switch m := img.(type) {
	case *image.Gray:
		if p.Bits > 8 {
			err = eb.Build().Uint8("bits", p.Bits).Int("depth", 8).Errorf("png depth disagrees with bits per value: %w", ErrInconsistent)
			return
		}
		for y := 0; y < b.Dy(); y++ {
			row := m.Pix[y*m.Stride:]
			for x := 0; x < b.Dx(); x++ {
				out[k] = (ref + float64(row[x])*bin) * dec
				k++
			}
		}
	case *image.Gray16:
		if p.Bits <= 8 || p.Bits > 16 {
			err = eb.Build().Uint8("bits", p.Bits).Int("depth", 16).Errorf("png depth disagrees with bits per value: %w", ErrInconsistent)
			return
		}
		for y := 0; y < b.Dy(); y++ {
			row := m.Pix[y*m.Stride:]
			for x := 0; x < b.Dx(); x++ {
				v := uint64(row[2*x])<<8 | uint64(row[2*x+1])
				out[k] = (ref + float64(v)*bin) * dec
				k++
			}
		}
	case *image.NRGBA:
		if p.Bits != 24 && p.Bits != 32 {
			err = eb.Build().Uint8("bits", p.Bits).Int("depth", 32).Errorf("png depth disagrees with bits per value: %w", ErrInconsistent)
			return
		}
		for y := 0; y < b.Dy(); y++ {
			row := m.Pix[y*m.Stride:]
			for x := 0; x < b.Dx(); x++ {
				px := row[4*x : 4*x+4]
				v := uint64(px[0])<<16 | uint64(px[1])<<8 | uint64(px[2])
				if p.Bits == 32 {
					v = v<<8 | uint64(px[3])
				}
				out[k] = (ref + float64(v)*bin) * dec
				k++
			}
		}
	case *image.RGBA:
		if p.Bits != 24 && p.Bits != 32 {
			err = eb.Build().Uint8("bits", p.Bits).Int("depth", 32).Errorf("png depth disagrees with bits per value: %w", ErrInconsistent)
			return
		}
		for y := 0; y < b.Dy(); y++ {
			row := m.Pix[y*m.Stride:]
			for x := 0; x < b.Dx(); x++ {
				px := row[4*x : 4*x+4]
				v := uint64(px[0])<<16 | uint64(px[1])<<8 | uint64(px[2])
				if p.Bits == 32 {
					v = v<<8 | uint64(px[3])
				}
				out[k] = (ref + float64(v)*bin) * dec
				k++
			}
		}
	default:
		err = unsupportedE("png colour model " + strconv.Quote(strconv.Itoa(int(p.Bits))+" bits"))
	}
	return
}

// unpackComplexE is templates 5.2 and 5.3: the values are split into
// groups, each with its own reference, width and length; the group
// descriptors are packed in three runs, each padded to an octet, then the
// values follow group by group. Template 5.3 first stores the initial
// values and the overall minimum of the differenced series, and the
// decoded integers are the differences to be undone (ADR-0292 §R6).
//
// Missing-value management (code table 5.5) marks a missing value with all
// ones at the group's width, and a whole missing group with all ones at the
// field's width when the group has zero width. The spatial differences
// skip missing points: the chain runs over the values that are present.
func (inst *Field) unpackComplexE(out []float64) (err error) {
	p := &inst.Packing
	c := p.Complex
	bin, dec := p.scaleFactors()
	ref := float64(p.Reference)
	n := len(out)
	if c.SplittingMethod != 1 {
		err = unsupportedE("group splitting method " + strconv.Itoa(int(c.SplittingMethod)))
		return
	}
	if c.MissingManagement > 2 {
		err = unsupportedE("missing value management " + strconv.Itoa(int(c.MissingManagement)))
		return
	}
	if p.Template == 3 && (c.SpatialOrder < 1 || c.SpatialOrder > 2) {
		err = unsupportedE("spatial differencing order " + strconv.Itoa(int(c.SpatialOrder)))
		return
	}
	if p.Template == 3 && (c.SpatialOctets < 1 || c.SpatialOctets > 4) {
		err = eb.Build().Uint8("octets", c.SpatialOctets).Errorf("spatial differencing descriptor width: %w", ErrMalformed)
		return
	}
	if n == 0 {
		return
	}
	ng := int(c.NumGroups)
	if ng == 0 || ng > n {
		err = eb.Build().Uint32("groups", c.NumGroups).Int("values", n).Errorf("group count not credible for the value count: %w", ErrInconsistent)
		return
	}
	br := bitReader{b: inst.data}
	// Template 5.3's extra descriptors: the first `order` values and the
	// overall minimum, each SpatialOctets wide; the minimum is
	// sign-magnitude.
	var first [2]int64
	var minimum int64
	if p.Template == 3 {
		w := c.SpatialOctets * 8
		for k := 0; k < int(c.SpatialOrder); k++ {
			first[k] = int64(br.bits(w))
		}
		raw := br.bits(w)
		sign := uint64(1) << (w - 1)
		minimum = int64(raw &^ sign)
		if raw&sign != 0 {
			minimum = -minimum
		}
	}
	// Group references.
	refs := make([]uint64, ng)
	for g := range refs {
		refs[g] = br.bits(p.Bits)
	}
	br.alignByte()
	widths := make([]uint8, ng)
	for g := range widths {
		w := uint64(c.WidthReference) + br.bits(c.WidthBits)
		if w > 64 {
			err = eb.Build().Uint64("width", w).Errorf("group width above 64 bits: %w", ErrMalformed)
			return
		}
		widths[g] = uint8(w)
	}
	br.alignByte()
	lengths := make([]uint32, ng)
	var total uint64
	for g := range lengths {
		var l uint64
		if g == ng-1 {
			l = uint64(c.LastGroupLength)
			br.bits(c.LengthBits)
		} else {
			l = uint64(c.LengthReference) + br.bits(c.LengthBits)*uint64(c.LengthIncrement)
		}
		if l > uint64(n) {
			err = eb.Build().Uint64("length", l).Int("values", n).Errorf("group longer than the field: %w", ErrInconsistent)
			return
		}
		lengths[g] = uint32(l)
		total += l
	}
	br.alignByte()
	if total != uint64(n) {
		err = eb.Build().Uint64("sumOfGroups", total).Int("values", n).Errorf("group lengths do not sum to the value count: %w", ErrInconsistent)
		return
	}
	if br.bad {
		err = eb.Build().Int("bytes", len(inst.data)).Errorf("data section shorter than its group descriptors: %w", ErrInconsistent)
		return
	}
	// Unpack the integers, marking missing ones. A group of zero width has
	// every value equal to its reference.
	ints := make([]int64, n)
	missing := make([]bool, n)
	allOnesField := uint64(1)<<p.Bits - 1
	k := 0
	for g := 0; g < ng; g++ {
		w := widths[g]
		allOnes := uint64(1)<<w - 1
		gref := refs[g]
		l := int(lengths[g])
		if w == 0 && c.MissingManagement != 0 && p.Bits != 0 && (gref == allOnesField || (c.MissingManagement == 2 && gref == allOnesField-1)) {
			for j := 0; j < l; j++ {
				missing[k] = true
				k++
			}
			continue
		}
		for j := 0; j < l; j++ {
			x := br.bits(w)
			if w != 0 && c.MissingManagement != 0 && (x == allOnes || (c.MissingManagement == 2 && x == allOnes-1)) {
				missing[k] = true
			} else {
				ints[k] = int64(gref + x)
			}
			k++
		}
	}
	if br.bad {
		err = eb.Build().Int("bytes", len(inst.data)).Errorf("data section shorter than its packed values: %w", ErrInconsistent)
		return
	}
	// Undo the spatial differencing over the present values.
	if p.Template == 3 {
		order := int(c.SpatialOrder)
		// The first `order` present values are the stored initial values;
		// the rest are differences offset by the minimum.
		var prev [2]int64
		seen := 0
		for i := 0; i < n; i++ {
			if missing[i] {
				continue
			}
			if seen < order {
				ints[i] = first[seen]
			} else {
				d := ints[i] + minimum
				if order == 1 {
					ints[i] = prev[0] + d
				} else {
					ints[i] = 2*prev[0] - prev[1] + d
				}
			}
			prev[1] = prev[0]
			prev[0] = ints[i]
			seen++
		}
	}
	for i := 0; i < n; i++ {
		if missing[i] {
			out[i] = math.NaN()
		} else {
			out[i] = (ref + float64(ints[i])*bin) * dec
		}
	}
	return
}

// unpackCCSDSE is template 5.42: the packed integers are the samples of a
// CCSDS 121.0-B-3 coded stream at the field's bit width (ADR-0292 §R6,
// M2). The options mask's data-layout bits (signed, three-byte, MSB) say
// how the samples would be laid out in bytes and do not touch the integers
// themselves; the coder bits (preprocessor, restricted set, interval
// padding) do, and are passed through. A bit width of 0 is a constant
// field as under simple packing.
func (inst *Field) unpackCCSDSE(out []float64) (err error) {
	p := &inst.Packing
	bin, dec := p.scaleFactors()
	ref := float64(p.Reference)
	if p.Bits == 0 {
		v := ref * dec
		for i := range out {
			out[i] = v
		}
		return
	}
	if p.Bits > 32 {
		err = unsupportedE("ccsds resolution above 32 bits")
		return
	}
	c := p.CCSDS
	params := aec.Params{
		Bits:              p.Bits,
		BlockSize:         c.BlockSize,
		ReferenceInterval: c.ReferenceInterval,
		Signed:            c.Flags&0x01 != 0,
		Preprocess:        c.Flags&0x08 != 0,
		Restricted:        c.Flags&0x10 != 0,
		PadInterval:       c.Flags&0x20 != 0,
	}
	samples, err := aec.Decode(nil, inst.data, len(out), params)
	if err != nil {
		switch {
		case errors.Is(err, aec.ErrParams):
			err = eb.Build().Uint8("flags", c.Flags).Uint8("blockSize", c.BlockSize).Uint16("rsi", c.ReferenceInterval).Errorf("ccsds parameters: %w: %w", ErrMalformed, err)
		default:
			err = eb.Build().Errorf("ccsds data section: %w: %w", ErrInconsistent, err)
		}
		return
	}
	for i := range out {
		out[i] = (ref + float64(samples[i])*bin) * dec
	}
	return
}

// unpackJPEGE is template 5.40: the packed integers are the samples of a
// JPEG 2000 codestream whose precision is the field's bit width
// (ADR-0292 §R6). The image's sample count must be the coded value
// count, and its precision must agree with Section 5 — the mismatches
// behind silent wrong values in other decoders' PNG and JPEG paths.
func (inst *Field) unpackJPEGE(out []float64) (err error) {
	p := &inst.Packing
	bin, dec := p.scaleFactors()
	ref := float64(p.Reference)
	if p.Bits == 0 {
		v := ref * dec
		for i := range out {
			out[i] = v
		}
		return
	}
	img, err := jpeg2000.Decode(inst.data)
	if err != nil {
		if feature, ok := jpeg2000.UnsupportedFeature(err); ok {
			err = unsupportedE("jpeg 2000 " + feature)
			return
		}
		err = eb.Build().Errorf("jpeg 2000 data section: %w: %w", ErrInconsistent, err)
		return
	}
	if len(img.Samples) != len(out) {
		err = eb.Build().Int("width", img.Width).Int("height", img.Height).Int("values", len(out)).Errorf("jpeg 2000 dimensions do not multiply to the value count: %w", ErrInconsistent)
		return
	}
	if img.Precision != p.Bits {
		err = eb.Build().Uint8("bits", p.Bits).Uint8("precision", img.Precision).Errorf("jpeg 2000 precision disagrees with bits per value: %w", ErrInconsistent)
		return
	}
	for i, v := range img.Samples {
		out[i] = (ref + float64(v)*bin) * dec
	}
	return
}
