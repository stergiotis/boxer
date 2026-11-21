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
// [SampleKindU16], [SampleKindI16], [SampleKindI32], [SampleKindF32] or
// [SampleKindF64] (ADR-0398 §SD1) — and nothing in this package narrows one
// kind into another. A float64 holds every sample of every kind exactly,
// which is why [Plane.StoredAt] reports one. The value a sample stands for
// is the stored sample through the plane's [ValueTransform]; for a PNG or a
// JPEG that is the identity. A kind with at most 2^16 codes
// ([SampleKindE.Codes]) has a histogram with one bin per code; the others
// are binned.
//
// # Invalid samples
//
// NaN is an invalid sample in a float plane. Besides it, a plane of any kind
// may declare one stored value invalid — a file's "no measurement" code
// ([Plane.WithInvalid], ADR-0398 §SD2). An invalid sample has no value: it
// is left out of the stored range and of every number computed from the
// plane. The declaration, like the value transform, is not part of a digest.
//
// # Stacks
//
// A [Stack] is planes of one size and one kind along named axes other than
// rows and columns (ADR-0398 §SD3): at most one band axis, whose positions
// are measured together, and any number of selector axes, of which a view
// shows one position. An [Image] is taken from a stack by naming a position
// on every selector axis and one or three bands; it shares the stack's
// planes and carries the bands' labels. A stack whose band axis is a colour
// model has an image as stored. A stack is resident in memory.
//
// # Coordinates
//
// Image coordinates are continuous and in pixels (ADR-0373 §SD3): pixel
// (col, row) covers [col, col+1) × [row, row+1), its centre is at +0.5, row
// 0 is the top row and both indices start at 0. Samples are stored
// row-major, so the sample of pixel (col, row) is at index row·width + col.
//
// A [Placement] says where those coordinates are (ADR-0398 §SD4): an affine
// to a frame that is a physical scale, a coordinate reference system named
// by its code and not resolved, or a gnomonic projection of the sky, which
// is evaluated to longitude and latitude. What a file declares and the
// package does not evaluate is recorded as unapplied. An image and a stack
// carry one placement; it is not part of a digest.
package imaging
