package grib

import (
	"math"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ComplexPacking is the group description of templates 5.2 and 5.3.
type ComplexPacking struct {
	SplittingMethod   uint8
	MissingManagement uint8
	PrimaryMissing    uint32
	SecondaryMissing  uint32
	NumGroups         uint32
	WidthReference    uint8
	WidthBits         uint8
	LengthReference   uint32
	LengthIncrement   uint8
	LastGroupLength   uint32
	LengthBits        uint8
	// SpatialOrder and SpatialOctets are template 5.3's two extra octets;
	// zero for 5.2.
	SpatialOrder  uint8
	SpatialOctets uint8
}

// Packing is Section 5. Reference, BinaryScale, DecimalScale and Bits are
// the fields every grid-point template shares; a value is
// (Reference + X·2^BinaryScale) × 10^−DecimalScale. Complex is set for 5.2
// and 5.3, IEEEPrecision for 5.4; 5.41 has no parameters beyond the shared
// ones. Templates the reader refuses (5.40, 5.42, 5.50, 5.51, 5.200 and
// any other) still parse the shared prefix where they have one, and Raw
// holds the template for all.
type Packing struct {
	Template     uint16
	NumValues    uint32
	Reference    float64
	BinaryScale  int16
	DecimalScale int16
	Bits         uint8
	// OriginalType is code table 5.1: 0 floating point, 1 integer. It
	// decides how the missing-value substitutes of 5.2/5.3 are read.
	OriginalType  uint8
	Complex       *ComplexPacking
	IEEEPrecision uint8
	CCSDS         *CCSDSPacking
	// JPEG is template 5.40's two parameters: the compression type of code
	// table 5.40 (0 lossless, 1 lossy) and the target ratio for lossy.
	JPEG *JPEGPacking
	// Grib1Flags is edition 1's data section flag octet (flag table 11);
	// Grib1 says the packing came from edition 1, where Template 0 stands
	// for its simple grid-point packing and the flags name the rest.
	Grib1Flags uint8
	Grib1      bool
	// Grib1MissingField marks an edition 1 field coded with no values
	// (binary scale and reference value all ones): every point is missing.
	Grib1MissingField bool
	Raw               []byte
}

// JPEGPacking is template 5.40's parameters.
type JPEGPacking struct {
	CompressionType uint8
	TargetRatio     uint8
}

// jpegPreStandard is the template number NCEP used for JPEG 2000 before
// 5.40 was assigned; files from that era are still on disk and the layout
// is 5.40's.
const jpegPreStandard = 40000

// CCSDSPacking is template 5.42's coder parameters: the options mask whose
// bits are the ones libaec documents (1 signed, 2 three-byte samples, 4
// MSB first, 8 preprocessor, 16 restricted option set, 32 pad each
// reference interval), the block size and the reference sample interval.
type CCSDSPacking struct {
	Flags             uint8
	BlockSize         uint8
	ReferenceInterval uint16
}

// supported reports whether the reader decodes this template's data, and
// otherwise names the feature (ADR-0292 §R1, §R6).
func (inst *Packing) supported() (err error) {
	if inst.Grib1 {
		switch {
		case inst.Grib1Flags&0x80 != 0:
			err = unsupported("grib1 spectral packing")
		case inst.Grib1Flags&0x40 != 0:
			err = unsupported("grib1 second-order packing")
		case inst.Grib1Flags&0x10 != 0:
			err = unsupported("grib1 additional data flags")
		}
		return
	}
	switch inst.Template {
	case 0, 2, 3, 4, 40, 41, 42, jpegPreStandard:
	default:
		err = unsupported("packing template 5." + strconv.Itoa(int(inst.Template)))
	}
	return
}

func parsePacking(body []byte) (p Packing, err error) {
	r := rd{b: body}
	p.NumValues = r.u32()
	p.Template = r.u16()
	err = r.truncation("data representation section")
	if err != nil {
		return
	}
	p.Raw = r.rest()
	t := rd{b: p.Raw}
	switch p.Template {
	case 0, 1, 2, 3, 40, 41, 42, 61, jpegPreStandard:
		p.Reference = float64(t.f32())
		p.BinaryScale = t.s16()
		p.DecimalScale = t.s16()
		p.Bits = t.u8()
		p.OriginalType = t.u8()
	case 4:
		p.IEEEPrecision = t.u8()
	}
	switch p.Template {
	case 2, 3:
		c := &ComplexPacking{}
		c.SplittingMethod = t.u8()
		c.MissingManagement = t.u8()
		c.PrimaryMissing = t.u32()
		c.SecondaryMissing = t.u32()
		c.NumGroups = t.u32()
		c.WidthReference = t.u8()
		c.WidthBits = t.u8()
		c.LengthReference = t.u32()
		c.LengthIncrement = t.u8()
		c.LastGroupLength = t.u32()
		c.LengthBits = t.u8()
		if p.Template == 3 {
			c.SpatialOrder = t.u8()
			c.SpatialOctets = t.u8()
		}
		p.Complex = c
	case 42:
		p.CCSDS = &CCSDSPacking{Flags: t.u8(), BlockSize: t.u8(), ReferenceInterval: t.u16()}
	case 40, jpegPreStandard:
		p.JPEG = &JPEGPacking{CompressionType: t.u8(), TargetRatio: t.u8()}
	}
	err = t.truncation("data representation template 5." + strconv.Itoa(int(p.Template)))
	if err != nil {
		return
	}
	if p.Bits > 64 {
		err = eb.Build().Uint8("bits", p.Bits).Errorf("bits per value above 64: %w", ErrMalformed)
		return
	}
	if math.IsNaN(p.Reference) || math.IsInf(p.Reference, 0) {
		err = eb.Build().Errorf("reference value is not a number: %w", ErrMalformed)
	}
	return
}

// scaleFactors returns the multipliers of the value formula. The binary
// scale is an exact power of two; the decimal one is 10^−D, which is how
// the reference implementation applies it, and the digest lane holds this
// reader to its exact doubles.
func (inst *Packing) scaleFactors() (bin float64, dec float64) {
	bin = math.Ldexp(1, int(inst.BinaryScale))
	dec = math.Pow(10, -float64(inst.DecimalScale))
	return
}
