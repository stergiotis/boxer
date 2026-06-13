package color

// Colors is a bulk-color payload type. Literal-only per ADR-0052 SD9; retained
// values are a scalar-only concept and cannot enter a Colors slice.
//
// Memory shape is identical to []uint32 (zero-overhead new type). Wire format
// is the standard ctabb.U32h packed-u32 layout; .AsColors() is an IDL-level
// annotation that only changes the Go-facing parameter type.
type Colors []uint32

// NewColors returns a pre-sized Colors slice. The backing array is guaranteed
// non-nil even for n==0, so the FFFI2 nil-slice sentinel (0xFFFFFFFF) is never
// emitted by callers that use this constructor.
func NewColors(n int) (ret Colors) {
	if n == 0 {
		ret = Colors{}
		return
	}
	ret = make(Colors, n)
	return
}

// ColorsFromU32 adopts an existing []uint32 as a Colors (zero-copy borrow).
// A nil input is coerced to an empty-non-nil slice so the FFFI2 nil-slice
// sentinel cannot slip onto the wire through this path.
func ColorsFromU32(s []uint32) (ret Colors) {
	if s == nil {
		ret = Colors{}
		return
	}
	ret = Colors(s)
	return
}

// ColorsFromSlice packs the literal values of a []Color into a Colors.
// Panics if any element is retained (SD9: retained is scalar-only).
func ColorsFromSlice(cs []Color) (ret Colors) {
	ret = make(Colors, len(cs))
	for i, c := range cs {
		if c.kind != ColorKindLiteral {
			panic("egui2/color: ColorsFromSlice rejects non-literal Color (SD9: retained is scalar-only)")
		}
		ret[i] = c.literal
	}
	return
}

// AsU32 returns the underlying []uint32 for interop with existing FFFI2
// marshalling paths.
func (inst Colors) AsU32() (ret []uint32) {
	ret = []uint32(inst)
	return
}

// SetHex writes a packed 0xRRGGBBAA value at index i.
func (inst Colors) SetHex(i int, rgba uint32) {
	inst[i] = rgba
}

// SetRGB writes an opaque (alpha=0xff) RGB color at index i.
func (inst Colors) SetRGB(i int, r uint8, g uint8, b uint8) {
	inst[i] = uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | 0xff
}

// SetRGBA writes an RGBA color at index i.
func (inst Colors) SetRGBA(i int, r uint8, g uint8, b uint8, a uint8) {
	inst[i] = uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | uint32(a)
}

// SetGray writes an opaque gray color at index i.
func (inst Colors) SetGray(i int, v uint8) {
	inst.SetRGB(i, v, v, v)
}
