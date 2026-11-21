// Package npy reads a NumPy NPY array, or one array of an NPZ archive, into
// an [imaging.Stack] (ADR-0398 §SD8, milestone M5). It is written from the
// NPY format description in numpy's documentation, on the standard
// library alone; it never runs or translates numpy.
//
// # What is read
//
// NPY format versions 1.0, 2.0 and 3.0. The header dictionary is parsed by a
// small parser of exactly the Python literal syntax a header uses —
// strings, integers, True, False, None, tuples, lists, dictionaries —
// tolerant of key order, whitespace and trailing commas, and it must hold
// the keys descr, fortran_order and shape and no other. The element types
// read are |u1 (u8), and in either byte order u2 (u16), i2 (i16), i4 (i32),
// f4 (f32) and f8 (f64); NaN in a float array is an invalid sample, as the
// model has it.
//
// A descr with the native byte order '=' is refused: the format description
// says a file reads the same on any machine and states nothing about native
// order, numpy writes '<', '>' or '|', and '=' would name the order of a
// machine the reader cannot know.
//
// # Axes
//
// The last two dimensions are rows and columns; NPY rows are stored top
// row first, so none are flipped. Each leading dimension is a selector
// axis named "axis0", "axis1", … in the array's order; the format carries
// no names, units or coordinates, and the stack has no placement, value
// transform or declared invalid value. An array whose bands are its last
// dimension — (rows, cols, bands), as images are often held — reads as a
// stack of one plane per row, each cols × bands, which is not the image:
// its producer has to move the band dimension to the front
// (numpy.moveaxis) before saving. A Fortran-ordered array (fortran_order: True), whose
// first index varies fastest, is converted to row-major planes and the
// provenance records [imaging.DerivationAxesReordered].
//
// # NPZ
//
// An NPZ archive is a zip of NPY members, stored or deflated.
// [InspectArchive] lists its members and each array's header, read from the
// start of the member without inflating the rest. [Decode] reads the member
// [Options.Member] names, or the only .npy member; an archive of several
// arrays without a choice is refused with their names. The limits are
// enforced from the member's own NPY header, never from the zip's sizes,
// and no more of the member is inflated than the header's shape asks for
// — plus one byte, to reach the end where the zip reader checks the
// member's CRC; a member with bytes after its array data is therefore
// refused, where a plain NPY file's trailing bytes are ignored.
//
// # What is refused
//
// By name, under [ErrUnsupported]: every other element type — bool, int8,
// uint32, the 64-bit integers, float16, extended precision, complex,
// strings, datetimes, structured and raw records — and an object array,
// which would need an unpickler, which this reader never runs; an array of
// fewer than two dimensions or with no elements; a format version other
// than 1.0, 2.0 and 3.0; a zip member compressed other than stored or
// deflated, or encrypted. Over a limit, under [ErrLimit], before any sample
// is read: a plane over [Options.MaxPixels], samples over
// [Options.MaxBytes], planes over [Options.MaxPlanes], a header over
// [MaxHeaderLen]. Anything else that breaks the format, under
// [ErrMalformed].
package npy
