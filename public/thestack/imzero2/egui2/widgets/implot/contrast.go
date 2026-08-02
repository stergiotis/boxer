package implot

import "math"

// Luminance — the lane's shared answer to "how bright is this fill", for the
// widgets that have to put a label on top of one.
//
// The same reasoning as text.go's width estimate applies: what this exists to
// stop is every widget inventing its own formula. A frame label and a pie
// label sitting in the same pane, picking opposite inks for the same fill,
// reads as a bug.
//
// This package's own contrastText is deliberately *not* written over it, and
// the difference is worth stating: contrastText keeps upstream ImPlot's
// CalcTextColor shape — Rec.601 weights applied to the gamma-encoded bytes —
// so that a port of an ImPlot chart looks like the chart it was ported from.
// The two rules disagree on about 7% of the 24-bit colour space. Where a
// widget is not reproducing an upstream look, RelativeLuminance is the one to
// use.

// RelativeLuminance is WCAG 2.x relative luminance of an 0xRRGGBBAA colour,
// in [0,1]: each channel is linearised out of the sRGB transfer function and
// then weighted by the Rec.709 primaries.
//
// Alpha is ignored — a translucent fill composites against something this
// function cannot see, so the caller has to resolve that first if it matters.
//
// The usual reason to want it is picking an ink: the contrast ratio between
// two colours is (L1+0.05)/(L2+0.05) with L1 the lighter, and the luminance
// at which two candidate inks contrast equally is
// sqrt((Ldark+0.05)*(Llight+0.05)) - 0.05.
func RelativeLuminance(rgba uint32) float64 {
	lin := func(shift uint) float64 {
		v := float64(rgba>>shift&0xff) / 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(24) + 0.7152*lin(16) + 0.0722*lin(8)
}

// ContrastRatio is the WCAG contrast ratio between two colours, from 1 (the
// same luminance) to 21 (black against white). It is the number a threshold
// like "4.5:1 for body text" refers to.
func ContrastRatio(a uint32, b uint32) float64 {
	la, lb := RelativeLuminance(a), RelativeLuminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}
