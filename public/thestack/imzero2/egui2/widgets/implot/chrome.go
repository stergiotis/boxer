package implot

import "github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"

// ChromeE selects the plot chrome palette: backgrounds, grid, border,
// axis/legend/readout text, the box-zoom rect, error-bar whiskers and the
// contrast-text pair. It does NOT touch the data-series palette
// (paletteDeep) — series colors and their legend swatches are part of the
// port's ImPlot identity and stay identical under both chromes.
type ChromeE uint8

const (
	// ChromeIDS derives the chrome from the IDS semantic palette
	// (styletokens, ADR-0031 neutral spine + accent role). The default.
	ChromeIDS ChromeE = iota
	// ChromeClassic is the hand-matched dark palette the port shipped
	// with (the M1–SD7 captures), kept selectable for continuity. Its
	// values are visually close to the IDS spine but predate it.
	ChromeClassic
)

// Chrome colors, assigned by SetChrome (package init applies ChromeIDS).
// Vars, not consts, so the palette is swappable; every value is a packed
// 0xRRGGBBAA as color.Hex expects. Font sizes and tick geometry are not
// part of the chrome and stay consts in plot.go.
var (
	colPlotBg       uint32 // canvas background outside the plot area
	colAreaBg       uint32 // plot-area fill
	colBorder       uint32 // plot border + tick marks
	colGridMajor    uint32
	colGridMinor    uint32
	colTickLabel    uint32
	colAxisLabel    uint32
	colTitle        uint32
	colReadout      uint32 // bottom-right hover readout
	colBoxFill      uint32 // box-zoom selection rect
	colBoxStroke    uint32
	colErrorBar     uint32 // fixed whisker foreground (doc.go deviation)
	colLegendBg     uint32
	colLegendHidden uint32 // legend row text of a hidden series
	colContrastDark uint32 // contrastText dark-on-light branch
	colContrastLite uint32 // contrastText light-on-dark branch
)

func init() { SetChrome(ChromeIDS) }

// SetChrome switches the chrome palette. Call it at application startup
// (or between frames); it reassigns package state and is not safe against
// a concurrently rendering plot. Selecting a chrome is process-wide —
// per-plot styling remains out of scope until a style system exists
// (doc.go).
func SetChrome(chrome ChromeE) {
	if chrome == ChromeClassic {
		colPlotBg = 0x111318ff
		colAreaBg = 0x1a1d24ff
		colBorder = 0x3a3f4bff
		colGridMajor = 0x2c313cff
		colGridMinor = 0x21252eff
		colTickLabel = 0xaab2c0ff
		colAxisLabel = 0xcdd3ddff
		colTitle = 0xe6e9eeff
		colReadout = 0x8891a0ff
		colBoxFill = 0x4c72b028
		colBoxStroke = 0x4c72b0cc
		colErrorBar = 0xc9cfdaff
		colLegendBg = 0x14171dee
		colLegendHidden = 0x667080ff
		colContrastDark = 0x10131aff
		colContrastLite = 0xe6e9eeff
		return
	}
	alpha := func(hex uint32, a uint32) uint32 { return hex&^0xff | a }
	colPlotBg = styletokens.NeutralBgFaint.AsHex()
	colAreaBg = styletokens.NeutralBgSurface.AsHex()
	colBorder = styletokens.NeutralBorderFaint.AsHex()
	// The spine has no grid tier; grid lines are the faint border blended
	// into the area fill via alpha.
	colGridMajor = alpha(styletokens.NeutralBorderFaint.AsHex(), 0x40)
	colGridMinor = alpha(styletokens.NeutralBorderFaint.AsHex(), 0x1e)
	colTickLabel = styletokens.NeutralTextSecondary.AsHex()
	colAxisLabel = styletokens.NeutralTextPrimary.AsHex()
	colTitle = styletokens.NeutralTextPrimary.AsHex()
	colReadout = alpha(styletokens.NeutralTextSecondary.AsHex(), 0xb4)
	// Box-zoom is a selection affordance → the accent role, not a data
	// color (the classic palette borrowed paletteDeep[0]).
	colBoxFill = alpha(styletokens.AccentDefault.AsHex(), 0x28)
	colBoxStroke = alpha(styletokens.AccentDefault.AsHex(), 0xcc)
	colErrorBar = styletokens.NeutralTextPrimary.AsHex()
	colLegendBg = alpha(styletokens.NeutralBgPanel.AsHex(), 0xee)
	colLegendHidden = styletokens.NeutralTextDisabled.AsHex()
	colContrastDark = styletokens.NeutralBgFaint.AsHex()
	colContrastLite = styletokens.NeutralTextPrimary.AsHex()
}
