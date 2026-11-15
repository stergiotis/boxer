// Package decode reads a PNG or a JPEG file into an [imaging.Image] without
// passing its samples through 8 bits or through premultiplied alpha (ADR-0373
// §SD10). The pixel decoding is the standard library's; what this package
// adds is the way from a decoded picture to planes, and the record of what
// the file stored.
//
// # What comes out
//
// The colour model follows the file, not the decoder's choice of Go type: a
// greyscale file is one plane, a colour file three, and a file with alpha — or
// one that names a transparent colour — one more. Samples are 16-bit where
// the file's are and 8-bit otherwise. Alpha is straight.
//
// Three kinds of file do not store the planes they yield, and the image's
// provenance says so: a palette file is expanded through its palette, a JPEG's
// luma and chroma are converted to red, green and blue by the standard
// matrix, and a CMYK JPEG is converted without a profile. A PNG with fewer
// than 8 bits per sample reaches this package already scaled to the 8-bit
// range by the decoder. An animated PNG yields its default image.
//
// # Into tiles, row by row
//
// [DecodeTiled] reads the same files into a store of tiles with every
// reduced level built (ADR-0374 §SD7). A non-interlaced PNG goes through a
// row decoder of this package's own, written from the PNG specification:
// it holds two rows of the file and a band of rows besides the store, and
// hands the store over before the first row is read, so a viewer can show
// tiles as they are built. Its planes and provenance are those [Decode]
// gives, but for the decoder's name; a test over every colour type and bit
// depth and a fuzz target against the standard decoder hold it to that. An
// interlaced PNG and a JPEG are decoded whole and then stored.
//
// # What is refused
//
// Whatever the standard library's decoders do not read is refused with their
// reason and the file's own description of itself: a 12-bit or lossless JPEG,
// an arithmetic-coded one. So is a file whose pixel count exceeds
// [Options.MaxPixels], before any pixel is decoded.
//
// # Colour encoding
//
// A file's statement about its colour encoding — an sRGB chunk, a gamma, a
// profile — is recorded in [imaging.Provenance.ColorDeclaration] and not
// applied (§SD5).
package decode
