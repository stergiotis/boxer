// Package fits reads an image HDU of a FITS file into an [imaging.Stack]
// (ADR-0398 §SD7, milestone M4). It is written from the FITS Standard 4.0
// and the world-coordinate papers of Greisen & Calabretta (2002, Paper I)
// and Calabretta & Greisen (2002, Paper II), on the standard library alone.
//
// # What is read
//
// The primary array and IMAGE extensions with BITPIX 8, 16, 32, −32 and −64,
// which become planes of kind u8, i16, i32, f32 and f64, converted from the
// file's big-endian order. NAXIS1 is the columns and NAXIS2 the rows. A FITS
// image is stored bottom row first; the planes hold row 0 at the top
// (ADR-0373 §SD3) and the provenance records [imaging.DerivationRowsFlipped].
// Axes 3 and up become selector axes. They are listed slowest first, the
// order of the stack's planes, which is the reverse of the FITS numbering:
// a cube of NAXIS3 = 4 and NAXIS4 = 2 has axes ["axis4" (2), "axis3" (4)],
// and its planes are in the file's order, so no sample moves. An axis is
// named by its CTYPE when that is present and unique, otherwise "axisN"
// with its FITS number. Where CRVAL, CRPIX and a scale (CDELT, or the
// diagonal CD element) are given, the axis is linear and it is not coupled
// to another by the matrix, its coordinates are filled in from them, with
// CUNIT as the unit.
//
// BSCALE and BZERO become every plane's value transform, so an "unsigned
// 16-bit" file — stored signed with BZERO 32768 — reads as i16 with an
// offset of 32768, as the standard describes it. BLANK is the declared
// invalid value of an integer image; on a float image, where NaN marks an
// undefined sample, it is ignored with a note. BUNIT has no field in the
// model and is recorded as a note.
//
// # Choosing an HDU
//
// [Options.UseHDU] reads the HDU of that index. Without it the first HDU
// this reader reads is chosen: an image with at least two axes and data. When
// there is none, the first image the reader refuses — random groups, a
// tile-compressed image, BITPIX 64 — is refused by name. [Inspect] lists
// every HDU, its header cards and whether [Decode] reads it.
//
// # Where the pixels are
//
// Axes 1 and 2 become the placement. The engine's pixel (col, row) is FITS
// pixel (col + 0.5, NAXIS2 + 0.5 − row), and the affine is built from
// CRPIX with the first of these the header gives: the CD matrix, the PC
// matrix with CDELT, CROTA2 with CDELT, or CDELT alone (Standard §8.1;
// CDELT and CROTA2 beside a CD matrix are ignored as the standard says).
// [imaging.Placement.Source] says which.
//
//   - A longitude–latitude pair in the TAN projection (RA---TAN/DEC--TAN,
//     GLON-TAN/GLAT-TAN, ELON/ELAT, …, in either axis order) is a
//     [imaging.FrameKindCelestialGnomonic] frame: the affine gives Paper II's
//     intermediate world coordinates, x along the longitude axis, in
//     degrees; RefLon and RefLat are CRVAL; LonPole is LONPOLE or Paper II's
//     default; the frame's code is RADESYS (or its default from EQUINOX)
//     with the equinox, or GALACTIC, ECLIPTIC, …. A celestial CUNIT in
//     arcsec, arcmin, mas or rad is converted to degrees, with a note.
//   - Two linear axes, or none named, with a scale are a
//     [imaging.FrameKindPhysical] frame in CUNIT, named by CTYPE.
//   - Anything else the header declares for axes 1 and 2 — another
//     projection (SIN, ZEA, CAR, AIT, …), a distortion (a -SIP suffix, TPV,
//     PV terms that alter TAN, the distortion-paper keywords), a celestial
//     axis coupled to a third axis, a non-linear algorithm on a linear axis —
//     leaves the placement zero, with a note naming each declaration: the
//     reader never approximates. Alternate descriptions (keywords with a
//     letter suffix) are noted and not read.
//   - No world-coordinate keywords: no placement, no note.
//
// # What is refused
//
// By name, under [ErrUnsupported]: BITPIX 64, random groups (GROUPS = T),
// a tile-compressed image (ZIMAGE = T in a BINTABLE) when it is the selected
// or the only image, a table, an image of fewer than two axes or no data, and
// SIMPLE = F. Over a limit, under [ErrLimit], before any sample is read: a
// plane over [Options.MaxPixels], samples over [Options.MaxBytes], planes
// over [Options.MaxPlanes]. Anything the standard does not allow that this
// reader needs, under [ErrMalformed]. Table extensions are counted in the
// provenance's notes.
//
// A gzip-wrapped file is decompressed into memory, refusing one that
// decompresses to more than MaxBytes and 16 MiB, and noted. All other
// input is read through [io.ReaderAt], header by header and in chunks of
// samples, and every count, offset and size is checked against the file's
// size before anything is allocated for it.
package fits
