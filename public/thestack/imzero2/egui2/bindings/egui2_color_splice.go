//go:build llm_generated_opus47

package bindings

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// PutColorAsRetainedColor32 emits Color32 construction opcodes into the
// retained builder so an [EvaluatedArg(Color32)]-transport widget method
// can consume a unified [color.Color] argument (ADR-0052 SD3).
//
// When the Color carries an externally-constructed holder (SD7 escape-hatch
// path; see [color.FromRetainedHolder]), the holder's bytes are spliced
// directly — byte-identical to the pre-refactor `r.SpliceRetained(fg.Untype())`
// emission.
//
// Otherwise the opcodes are synthesised inline from the Color's literal u32
// using `FromRgbaUnmultiplied` semantics, matching SD8 (Go-side literals are
// sRGB non-premultiplied; the Rust side premultiplies at decode). The inline
// form costs ~+5 bytes relative to a pre-retained splice, matching SD3.
//
// Lives in the `components` package rather than `color` because the opcode
// IDs (`FuncProcIdColor`, `ColorMethodIdFromRgbaUnmultiplied`, `ColorMethodIdBuild`)
// are package-local generated constants; lifting this helper into `color`
// would create a cycle with the generated factories that consume color.Color.
func PutColorAsRetainedColor32(r *typed.RetainedFffiBuilder, col color.Color) {
	if h := col.Holder(); h != nil {
		r.SpliceRetained(h)
		return
	}
	lit := col.Literal()
	r.WriteOpCode(uint32(FuncProcIdColor))
	r.WriteOpCode(uint32(ColorMethodIdFromRgbaUnmultiplied))
	r.WriteUint8(uint8(lit >> 24))
	r.WriteUint8(uint8(lit >> 16))
	r.WriteUint8(uint8(lit >> 8))
	r.WriteUint8(uint8(lit))
	r.WriteOpCode(uint32(ColorMethodIdBuild))
}
