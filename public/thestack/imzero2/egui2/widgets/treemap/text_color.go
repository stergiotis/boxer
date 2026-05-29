//go:build llm_generated_opus47

package treemap

import "math"

// W3C WCAG 2.0 relative luminance, used to pick a readable text color (black
// or white) over an arbitrary cell fill. Spec:
// https://www.w3.org/TR/WCAG20/#relativeluminancedef
//
// Cell fills come from caller-supplied palettes (Viridis8, user colormaps,
// etc.) whose lightness varies widely; a static text color is unreadable at
// one end of any non-trivial palette. Computing this once per palette entry
// at construction time keeps the renderer hot-path free of math.

const (
	wcagRC = 0.2126
	wcagGC = 0.7152
	wcagBC = 0.0722

	// sRGB gamma split point and low-gamma slope.
	wcagSplit = 0.03928
	wcagLowK  = 1.0 / 12.92

	// Crossover luminance where contrast against pure white equals contrast
	// against pure black: solve (1.05)/(L+0.05) == (L+0.05)/0.05 → L ≈ 0.1791.
	// Fills brighter than this read better with black text; darker with white.
	textLumThreshold = 0.1791
)

// linearizeChannel undoes the sRGB transfer function on an 8-bit channel,
// returning a linear-light value in [0,1].
func linearizeChannel(c8 uint8) float64 {
	c := float64(c8) / 255.0
	if c <= wcagSplit {
		return c * wcagLowK
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// relativeLuminance returns the W3C-defined Y in [0,1] for an sRGB triple.
func relativeLuminance(r, g, b uint8) float64 {
	return wcagRC*linearizeChannel(r) + wcagGC*linearizeChannel(g) + wcagBC*linearizeChannel(b)
}

// pickTextColor returns an 0xRRGGBBAA value (opaque black or opaque white)
// that maximizes WCAG contrast against the given fill.
func pickTextColor(r, g, b uint8) uint32 {
	if relativeLuminance(r, g, b) > textLumThreshold {
		return 0x000000ff
	}
	return 0xffffffff
}
