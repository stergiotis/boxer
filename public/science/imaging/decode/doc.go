// Package decode reads image files: a PNG or a JPEG into an
// [imaging.Image] without passing its samples through 8 bits or through
// premultiplied alpha (ADR-0373 §SD10), and any format a host registers a
// reader for into an [imaging.Stack], through the seam of ADR-0398
// §SD5–§SD8 (see Readers). The pixel decoding of PNG and JPEG is the
// standard library's; what this package adds is the way from a decoded
// picture to planes, the record of what the file stored, and the seam.
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
//
// # Readers
//
// A reader ([ReaderI]) is a value that reads one format into stacks. It is
// held in a [Registry], which hands a file to the one reader that claims
// it. This package holds two, PNG and JPEG ([BuiltinReaders]); a host
// supplies the rest. The contract, in the order a registry calls it:
//
//  1. Describe. Format returns the reader's [Format]: a name unique in a
//     registry and recorded as the Provenance.Format of every stack it
//     makes, a title for a person, the endings its files carry, a doc of a
//     sentence or two, and a version. The version is of the reader's own
//     reading: bump it when the same bytes would decode to something else
//     — other samples, another placement, other notes — so that whatever
//     was derived from an earlier reading can be told apart from a new one.
//  2. Sniff. Sniff says from a file's first [SniffLen] bytes whether the
//     file is its format. It claims nothing shorter than its signature,
//     the empty input among them, and no file of another format: a file
//     two readers claim is refused, whichever was registered first.
//  3. Entries. Entries lists the images a file holds ([Entry]) from its
//     headers alone: a stable id each, a title, the shape and kind as far
//     as the header says, whether the reader reads it or, when not, the
//     refusal a decode of it is — its kind and its bare words
//     ([Entry.Refused]) — and exactly one Default: what a plain open
//     reads, or, in a file with nothing readable, the entry it would have
//     read, whose refusal is then the file's (ADR-0398 §SD7).
//  4. Decode. Decode reads one entry — the default for an empty id — into
//     a stack, under the request's [Limits]. [SelectEntry] finds the entry
//     and refuses an unreadable one with its kind, so a reader goes
//     through it rather than choosing for itself.
//
// What a decode delivers (§SD5). The pixel convention of ADR-0373 §SD3,
// whatever the format's: row 0 at the top, pixel (col, row) covering
// [col, col+1) × [row, row+1). A convention converted on the way — rows
// flipped, an axis order reversed, a half-pixel shift for a raster
// registered at pixel centres — is recorded: in
// [imaging.Provenance.Derivations] where a flag names it, in
// Provenance.Notes otherwise, and in [imaging.Placement.Source] for the
// statement a placement was derived from. Where one reader's format covers
// several variants — a container with wider offsets, an archive of arrays,
// a compressed wrapping — the variant read is
// [imaging.Provenance.Variant], in the format's own word, and Format stays
// the reader's name. Samples come in the kind the file stores them, with
// its invalid value declared on the planes, and nothing the reader does
// not evaluate is applied as if it did.
//
// What a decode refuses (§SD8). A request over its limits, from the
// header, before a sample is read: [Limits.Check] does the arithmetic. What
// the reader does not read, by name — a compression, a sample type, a
// projection — rather than a guess at it. A file that does not follow its
// format. Each is an error built with [Unsupported], [OverLimit],
// [Malformed] or [MalformedWith] — [Refuse] for a kind held as a value —
// and an unknown entry id is [NoEntry]: an app shows the sentence as it
// stands and tells the refusals apart with errors.Is or [RefusalOf],
// without knowing the reader, and [ReasonOf] gives back a refusal's bare
// words and kind. A reader reads through the io.ReaderAt it is given, no
// more than it needs to answer, and honours ctx between reads of samples.
// It reads through [ReadFullAt] — and a stream, a decompressor among them,
// over [MarkReadFailures] of the file, its errors through [ReadError] — so
// that a file cut short or a corrupt block is malformed and a read that
// fails for another reason is returned as it is: no refusal, since the file
// may be sound.
//
// A registry is filled where a host wires its apps, in one visible place:
// [Default] is the process's, holding PNG and JPEG, and [Register] adds to
// it. A reader is a parser of untrusted bytes, and which ones a deployment
// exposes should be readable at that one site, so a reader's package does
// not register itself from an init function (ADR-0398 §SD6). A test that
// wants other readers builds its own registry with [NewRegistry].
//
// The package decode/decodetest is the conformance kit (§SD9): a reader's
// tests run decodetest.Check over a few files of its format, and it checks
// the contract above — not that the format was read correctly, which only
// the reader's own fixtures can show.
package decode
