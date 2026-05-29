//go:build llm_generated_opus47

// SPDX-License-Identifier: MIT

package styletokens

// AsHex packs the RGBA8 as a 0xRRGGBBAA uint32 in the same byte order the
// color package's Hex constructor expects.
//
// Bridge for widgets that need a color.Color from a styletokens palette
// entry without tripping designlint L2 (which flags raw color.RGB /
// color.RGBA calls): call `color.Hex(token.AsHex())`. designlint allowlists
// neither styletokens importing color (would violate ADR-0035 layering) nor
// extra color calls in widget internals — `color.Hex` is the canonical
// non-flagged constructor.
func (inst RGBA8) AsHex() (packed uint32) {
	packed = uint32(inst.R)<<24 | uint32(inst.G)<<16 | uint32(inst.B)<<8 | uint32(inst.A)
	return
}
