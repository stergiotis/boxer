// Package imaging holds an image as measured samples, apart from any way of
// showing it (ADR-0373 §SD1, §SD2): one to four planes of one size and one
// sample kind, a colour model that says what the planes are, and the
// provenance of the whole. Showing an image is the display package's job and
// reading one from a file the decode package's; nothing here knows about
// either.
//
// # Planes are not modified
//
// A [Plane] is immutable by contract: its accessors return the backing
// slices, and a caller must not write through them. An operation that changes
// samples makes a new plane. Go cannot enforce this for a slice, so the rule
// is stated here once and every accessor repeats it.
//
// # Samples keep their kind
//
// A plane stores samples in the kind the source had — [SampleKindU8],
// [SampleKindU16] or [SampleKindF32] — and nothing in this package narrows
// one kind into another. The value a sample stands for is the stored sample
// through the plane's [ValueTransform]; for a PNG or a JPEG that is the
// identity.
//
// # Coordinates
//
// Image coordinates are continuous and in pixels (§SD3): pixel (col, row)
// covers [col, col+1) × [row, row+1), its centre is at +0.5, row 0 is the top
// row and both indices start at 0. Samples are stored row-major, so the
// sample of pixel (col, row) is at index row·width + col.
package imaging
