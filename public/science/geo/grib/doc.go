// Package grib reads GRIB edition 1 and 2 messages — the format every
// numerical weather model is published in — without cgo, without a C
// codec, and without descent from any existing GRIB library (ADR-0292).
//
// It is a reader for a subset, on purpose, and the subset is the corpus of
// files real producers publish (ADR-0292 §R1): grid templates 3.0, 3.1,
// 3.10, 3.20, 3.30, 3.40, 3.90 and 3.101; the 4.0 prefix of every product
// template that has one, with the tails of 4.0–4.2, 4.8, 4.11, 4.12 and
// 4.15; packings 5.0, 5.2, 5.3, 5.4, 5.40, 5.41 and 5.42 (the CCSDS
// 121.0-B-3 coder is the sub-package aec, the JPEG 2000 profile the
// sub-package jpeg2000);
// edition 1 with simple grid-point packing on lat/lon, rotated and regular
// or reduced Gaussian grids, ECMWF's local definition 1, and the
// large-message length convention, mapped onto the same [Grid] and
// [Packing] with the product in [Message.Grib1]. Everything else is refused with [ErrUnsupported] and
// the feature's name ([UnsupportedFeature]); bytes that are not GRIB fail
// with [ErrMalformed]; a message whose sections contradict each other — a
// data section shorter than its bit count, a bitmap of the wrong length —
// fails with [ErrInconsistent]. No input makes the reader panic, which a
// property test holds. The failure the package is designed against is not
// a refusal but a misread, which is what the survey behind the ADR found in
// every decoder it examined.
//
// [Scan], [ScanBytes] and [ScanFrom] yield [Message] values in file order,
// skipping the GTS headers, blocking records and padding that surround
// messages in the wild and reporting a truncated tail as that message's
// error. A message's [Field] values hold their sections parsed; [Field.ValuesE]
// decodes the data in the order it is stored, with NaN for missing points,
// and nothing is reordered for the caller (ADR-0292 §R2): the scan flags
// are exposed, [Grid.PointsE] follows them to give each stored point its
// latitude and longitude, and [Field.RasterE] applies them once, on
// request, for grids with rectangular dimensions.
//
// Every signed octet is decoded sign-magnitude with all ones as missing
// (ADR-0292 §R5); time is a reference instant, a coded unit and a coded
// offset, converted to a duration only when the unit has one (§R7);
// parameters are their coded triplet, never named (§R8).
//
// [IndexE] lists every field with its offset and identity as JSON lines
// for readers that fetch by byte range (§R9); the sub-package tables
// names parameters, centres and code-table values from the WMO's own
// tables (§R8).
//
// The fixtures under testdata are real producers' messages and ecCodes'
// public test files, held field by field against what ecCodes 2.49.0
// decoded from the same bytes (testdata/expect.tsv, produced by
// testdata/gen_fixtures.py.txt); testdata/SOURCES.txt records their origin
// and licences. EXPLANATION.md derives the value formula, the complex
// packing layout and the rotated-pole geometry.
package grib
