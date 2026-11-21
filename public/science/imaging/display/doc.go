// Package display turns an image's samples into the colours a screen shows,
// under a recipe that is a value of its own (ADR-0373 §SD4, §SD5). A [Recipe]
// says which range of values is shown and through which transfer function; a
// mapper compiled from it does the work as a table lookup. Nothing here
// changes a sample, and nothing outside this package needs to know a recipe
// to work on an image.
//
// # The mapping
//
// A value v becomes a position x = (v − Lo) / (Hi − Lo), clamped to [0, 1];
// the transfer function takes x to y in [0, 1]; a single plane looks y up in
// a 256-entry palette, and a colour image scales y to 0…255 in each of its
// colour planes, with one recipe for all three. The transfer functions are:
//
//	linear  y = x
//	log     y = ln(a·x + 1) / ln(a + 1)        a = 1000 unless given
//	sqrt    y = √x
//	asinh   y = asinh(x / a) / asinh(1 / a)    a = 0.1 unless given
//	gamma   y = x^g                             g = 1 unless given
//
// Every one of them maps 0 to 0 and 1 to 1 and is monotone, so a brighter
// sample is never drawn darker. [Recipe.Invert] replaces y by 1 − y.
//
// # Two default views
//
// [DefaultRecipe] shows a colour image over the nominal range of its sample
// kind, linear: with 8-bit samples that reproduces the stored pixels exactly.
// It shows a single plane over the range of its data, linear. Both record in
// [Limits.Source] where the range came from, so a displayed range can be read
// and repeated.
//
// # Sample kinds and invalid samples
//
// A kind with at most 2^16 codes — unsigned 8- and 16-bit, signed 16-bit —
// compiles to a table with one entry per code, indexed by stored − lo
// (ADR-0398 §SD1). Signed 32-bit and the float kinds compile to a window
// between the limits divided into 65536 steps; a sample is placed in it in
// float64 and then quantised. NaN in a float plane and the stored value a
// plane declares invalid (ADR-0398 §SD2, [PlaneSpec]) are drawn in the
// palette's invalid colour; in a table it is the declared code's entry. A
// colour image is mapped for the kinds with codes only, and a pixel at which
// any colour plane is invalid is drawn fully transparent.
//
// # Bands together
//
// A [Composite] is a third way of showing, beside the image as stored and a
// single plane (ADR-0401): up to [MaxLayers] layers, each one plane, a
// recipe of its own and a colour from a fixed set — red, green, blue, cyan,
// magenta, yellow, white (§SD4) — summed. A [CompositeMapper] maps each
// shown layer through the mapping of a single plane, a table for a kind
// with codes and the window otherwise, so bands of any kind composite
// (§SD3), to an 8-bit level; each channel is the sum of the levels of the
// layers whose colour has it, cut at 255. A pixel at which any shown layer
// is invalid is fully transparent, and a composite has no alpha (§SD5).
// Three layers red, green and blue under one recipe are the colour mapping
// of the same planes, bit for bit (§SD1). A composite is linked while its
// shown layers share one recipe ([Composite.IsLinked], §SD2): stretching
// them separately changes the colour balance, and a view says so.
//
// # Limits by zscale
//
// [ZScaleLimits] finds limits around the bulk of a plane's values from a
// sample of them on a regular grid, robust to a few bright sources where a
// percentile is not (ADR-0401 §SD7): a line is fitted to the sorted sample
// with outliers rejected, and the limits are the median less and plus the
// line's slope across the sample, divided by a contrast. The steps are
// written out at [ZScaleLimitsAt]; [Limits] records the contrast and the
// sample count, as it records percentiles.
//
// # What limits clip
//
// [ClippedShares] reads off a histogram the share of the valid samples below
// the lower limit and above the upper one (ADR-0401 §SD8): counted exactly
// for a kind with one bin per code, to a bin otherwise, and it says which.
//
// # Pixels
//
// Mapped pixels are 0xRRGGBBAA with straight alpha, the form the paintImage
// opcode takes. A colour's alpha is the alpha plane scaled to 8 bits by its
// nominal range and is never stretched.
package display
