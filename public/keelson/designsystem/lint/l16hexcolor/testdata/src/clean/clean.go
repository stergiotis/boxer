package clean

import "color"

// A token stand-in: the argument is a call, not a literal.
type token struct{ packed uint32 }

func (t token) AsHex() (packed uint32) { packed = t.packed; return }

var accent = token{}

// Colour-named declarations that take their value from a token or a
// variable, not a literal.
var (
	accentColor              = color.Hex(accent.AsHex())
	accentColorPacked        = accent.AsHex()
	bandColor         uint32 = accentColorPacked
)

// A uint32 literal that is not a colour by name.
const maxRows uint32 = 0x1000

// An untyped decimal const with a colour-ish name is not flagged: the
// untyped check requires the hex spelling.
const toneCount = 12

func fine() {
	_ = color.Hex(accent.AsHex())
	_ = color.Hex(bandColor)
	_ = color.Other(0x44ff44ff) // not the hex constructor
	_ = color.RGB(1, 2, 3)      // L2's shape, not L16's
	_ = color.Hex(0)            // the transparent sentinel
	_ = color.Hex(0x00000000)   // the transparent sentinel, spelled out
	var noFillColor uint32 = 0  // the sentinel in a declaration
	color.Fill(noFillColor)
	var limit uint32 = 0xffff // not a colour by name
	color.Fill(limit)
}
