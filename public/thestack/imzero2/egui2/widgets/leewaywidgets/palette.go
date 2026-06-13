package leewaywidgets

// ColorPaletteE selects the section-accent colour ladder used by Card and
// Table emitters. Distinct from the card-package ColorPaletteE so this
// library has no dependency back on boxerstaging/leeway/card.
type ColorPaletteE int

const (
	// ColorPaletteInferno is the default — warm, high-contrast.
	ColorPaletteInferno ColorPaletteE = iota
	ColorPaletteViridis
	ColorPaletteMagma
	ColorPalettePlasma
)
