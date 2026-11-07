package violator

import "color"

// A packed colour typed uint32 at package scope.
const bandColor uint32 = 0x4c9f7038 // want `L16: colour literal 0x4c9f7038 initialises bandColor`

// An untyped const written in hex, named as a colour.
const accentColour = 0xff4444ff // want `L16: colour literal 0xff4444ff initialises accentColour`

// A grouped const block: the second spec inherits no type and is hex.
const (
	weekendBandColor     uint32 = 0x1f202450 // want `L16: colour literal 0x1f202450 initialises weekendBandColor`
	officeHoursBandColor        = 0xf0c97014 // want `L16: colour literal 0xf0c97014 initialises officeHoursBandColor`
)

// A var, and a named type whose underlying type is uint32.
type packed uint32

var (
	highlightHex packed = 0xffee44ff // want `L16: colour literal 0xffee44ff initialises highlightHex`
	warnTone     uint32 = 4294901760 // want `L16: colour literal 4294901760 initialises warnTone`
)

func raw() {
	_ = color.Hex(0x44ff44ff)             // want `L16: raw color\.Hex\(0x44ff44ff\) literal`
	color.Paint(color.Hex(0xffffffff), 1) // want `L16: raw color\.Hex\(0xffffffff\) literal`
	var markerColor uint32 = 0x202020ff   // want `L16: colour literal 0x202020ff initialises markerColor`
	color.Fill(markerColor)
}
