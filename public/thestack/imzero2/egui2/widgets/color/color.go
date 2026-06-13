// Package color provides the unified ImZero2 color type per ADR-0052.
//
// Callers construct Color values via [Hex], [RGB], [RGBA], or [Gray] and pass
// them to any widget method that takes a color. Under the hood the generated
// FFFI2 encoder picks the right wire transport (raw u32 or retained Color32)
// based on the per-arg IDL annotation; callers never see that distinction.
//
// Call [Color.Keep] to promote a literal into a retained variant so a single
// Color value can be passed to multiple widget calls without re-emitting
// construction opcodes. The retained variant still carries the originating
// u32, so flattening onto a PlainArg(U32)-transport method is zero-cost.
//
// Array-valued color payloads use the companion type [Colors] (literal only
// per ADR-0052 SD9).
//
// Package structure note: color.Color deliberately does not hold a typed
// components.Color32S retained holder, because components imports color
// (generated factories take color.Color arguments). The retained state is
// therefore a kind flag plus the originating u32; the actual opcode splice
// for EvaluatedArg(Color32) transport is synthesised by
// [components.PutColorAsRetainedColor32] at wire time.
package color

import (
	imageColor "image/color"

	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
)

// ColorKindE distinguishes the storage variant of a [Color].
type ColorKindE uint8

const (
	// ColorKindNone is the zero-value kind; a Color of this kind carries no data.
	ColorKindNone ColorKindE = 0
	// ColorKindLiteral holds an inline u32 RGBA value.
	ColorKindLiteral ColorKindE = 1
	// ColorKindRetained is a flag that tells the encoder to splice retained
	// Color32 opcodes (synthesised inline from the Literal unless [Color.Holder]
	// is non-nil, in which case the pre-built holder is spliced).
	ColorKindRetained ColorKindE = 2
)

// Color is a discriminated union of a literal u32 RGBA and a retained-variant
// flag. Literal values interpret their bytes as sRGB non-premultiplied per
// ADR-0052 SD8.
type Color struct {
	kind    ColorKindE
	literal uint32
	// holder is populated only for the SD7 escape-hatch path where callers
	// construct a retained Color32 via the legacy fluent factory
	// (components.Color().FromRgbaUnmultiplied(...).Keep()) and wrap it via
	// [FromRetainedHolder]. The encoder prefers holder-splice when set,
	// and falls back to inline synthesis from literal otherwise.
	holder *typed.RetainedFffiHolder
}

// Hex constructs a literal Color from a packed 0xRRGGBBAA value.
func Hex(rgba uint32) (ret Color) {
	ret = Color{kind: ColorKindLiteral, literal: rgba}
	return
}

// RGB constructs an opaque (alpha=0xff) literal Color.
func RGB(r uint8, g uint8, b uint8) (ret Color) {
	ret = Color{
		kind:    ColorKindLiteral,
		literal: uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | 0xff,
	}
	return
}

// RGBA constructs a literal Color with explicit alpha.
func RGBA(r uint8, g uint8, b uint8, a uint8) (ret Color) {
	ret = Color{
		kind:    ColorKindLiteral,
		literal: uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | uint32(a),
	}
	return
}

// Gray constructs an opaque gray literal Color with all channels set to v.
func Gray(v uint8) (ret Color) {
	ret = RGB(v, v, v)
	return
}

// Transparent is the fully-transparent sentinel (RGBA 0/0/0/0). Reach for
// this when an API requires a Color argument but no fill or stroke should
// render — placeholder backgrounds in panels, "no tint" cells, RichText
// runs without a background, etc.
//
// Prefer Transparent over the literal RGBA(0,0,0,0) so designlint L2 stays
// quiet and the intent ("no color") reads at the call site. .Keep() works
// on the value: `color.Transparent.Keep()`.
var Transparent = RGBA(0, 0, 0, 0)

// FromImage constructs a Color from any standard image/color.Color by
// reading its 16-bit RGBA channels and packing them as 8-bit sRGB.
// Bridges runtime-supplied palettes (image/color.Palette indexing, custom
// colormaps, hash-distributed accent rotations) into the egui2 color
// surface without tripping designlint L2 in widget internals — the lint
// flags raw literals, not color computations from data inputs, and this
// helper lives in the color package (already L2-allowlisted).
func FromImage(src imageColor.Color) (ret Color) {
	r, g, b, a := src.RGBA()
	ret = RGBA(uint8(r>>8), uint8(g>>8), uint8(b>>8), uint8(a>>8))
	return
}

// FromRetainedHolder adopts a pre-built Color32 retained holder as a Color.
// Intended for the SD7 escape hatch: callers who need non-premultiplied or
// FromBlackAlpha semantics construct via the legacy components.Color()
// fluent factory and wrap the result here before passing to a color-annotated
// argument. If the caller also has the source u32 RGBA, pass it as literal
// so flattening onto PlainArg(U32) transports remains zero-cost; pass 0 if
// unknown (flattening will then panic).
func FromRetainedHolder(h *typed.RetainedFffiHolder, literal uint32) (ret Color) {
	ret = Color{kind: ColorKindRetained, literal: literal, holder: h}
	return
}

// Kind reports the storage variant.
func (inst Color) Kind() (ret ColorKindE) {
	ret = inst.kind
	return
}

// Literal returns the source u32 RGBA. Valid for literal variants and for
// retained variants whose originating u32 was tracked; may be zero for
// [FromRetainedHolder]-constructed values whose caller did not provide one.
func (inst Color) Literal() (ret uint32) {
	ret = inst.literal
	return
}

// Holder returns the adopted retained holder when one was supplied via
// [FromRetainedHolder]; nil otherwise. Used by the encoder to prefer
// holder-splice over inline synthesis.
func (inst Color) Holder() (ret *typed.RetainedFffiHolder) {
	ret = inst.holder
	return
}

// Keep promotes a literal Color into a retained variant so it can be reused
// across many widget calls without re-emitting construction opcodes. Calling
// Keep on an already-retained Color is idempotent. Panics on a zero-value
// Color (kind=None).
func (inst Color) Keep() (ret Color) {
	if inst.kind == ColorKindRetained {
		ret = inst
		return
	}
	if inst.kind != ColorKindLiteral {
		panic("egui2/color: Keep() called on zero-value Color")
	}
	ret = Color{kind: ColorKindRetained, literal: inst.literal}
	return
}
