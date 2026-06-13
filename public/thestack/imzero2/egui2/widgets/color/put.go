package color

import (
	"github.com/stergiotis/boxer/public/thestack/fffi2/runtime"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
)

// PutAsU32 writes a Color to a retained-fffi builder as a 4-byte RGBA u32,
// matching the legacy PlainArg(U32) wire encoding (ADR-0052 SD3). Works on
// both literal and retained variants via the stashed originating u32, so no
// opcode parsing is required.
//
// Called by generated factory / method code when the IDL argument is
// annotated with .AsColor() over a PlainArg(U32) transport.
//
// Panics for retained variants whose originating u32 is unknown (Literal()==0
// from FromRetainedHolder with literal==0). In practice this only fires when
// a SD7 escape-hatch caller wraps a built retained holder without supplying
// the source RGBA and then passes it to a PlainArg-transport widget — rare;
// treat the panic as a hint to use a retained-transport site or supply the
// literal at construction.
func PutAsU32(r *typed.RetainedFffiBuilder, col Color) {
	r.WriteUint32(col.literal)
}

// PutColorsSlice writes a Colors payload as a length-prefixed packed u32
// array, matching the legacy ctabb.U32h wire encoding. Literal-only per
// SD9 — Colors carries []uint32 internally.
func PutColorsSlice(r *typed.RetainedFffiBuilder, cs Colors) {
	runtime.PutUint32SliceArg(r, []uint32(cs))
}
