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
// # Pixels
//
// Mapped pixels are 0xRRGGBBAA with straight alpha, the form the paintImage
// opcode takes. A colour's alpha is the alpha plane scaled to 8 bits by its
// nominal range and is never stretched.
package display
