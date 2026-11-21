// Package tiff reads a TIFF or BigTIFF file into an [imaging.Stack]
// (ADR-0398 §SD6), written from the TIFF 6.0 specification, Adobe's
// Technical Note 3 and PageMaker supplement, the BigTIFF description,
// GeoTIFF 1.1 and GDAL's documentation of its private tags. It reads from
// an [io.ReaderAt] (§SD5), so that a ranged source can sit under it.
//
// # What is read
//
// Classic TIFF and BigTIFF in either byte order; strips and tiles, tiles
// cut at the image's edge; chunky and planar layouts; no compression,
// PackBits, LZW and Deflate; predictor 2 on 8-, 16- and 32-bit integers and
// predictor 3 on 32- and 64-bit floats. Samples are unsigned 8- and 16-bit,
// signed 16- and 32-bit, and 32- and 64-bit float, every sample of a pixel
// alike, and are kept in their stored kind.
//
// # What comes out
//
// A BlackIsZero or WhiteIsZero file is a stack whose band axis is
// [imaging.ColorModelGray], an RGB file one whose band axis is
// [imaging.ColorModelRGB]; one extra sample of unassociated alpha makes
// them GrayAlpha and RGBA. Any other layout — more extra samples,
// associated alpha, extras of unspecified meaning — is a band axis of one
// position per sample without a colour model, and a note says why. A
// WhiteIsZero file's samples are as stored, not inverted, and the
// provenance says so. A palette file is expanded through its colour map to
// 16-bit red, green and blue ([imaging.DerivationPaletteExpanded]).
//
// The first directory is the image. Later directories of its size, sample
// kind and layout are pages, positions of a selector axis named "page";
// reduced-resolution directories, masks, SubIFDs and directories unlike the
// first are counted in the provenance's notes and not read. [Inspect] lists
// every directory and what [Decode] does with it.
//
// GDAL_NODATA becomes the planes' invalid value where their kind holds it
// exactly; GDAL_METADATA's per-band descriptions, scales and offsets become
// band labels and value transforms. GeoTIFF's pixel scale and tiepoint, or
// its transformation matrix, become the placement's affine, moved half a
// pixel for a PixelIsPoint raster, with the EPSG code of the reference
// system; without GeoTIFF tags, a resolution in inches or centimetres is a
// physical placement. What the file declares and the placement does not
// evaluate is listed in [imaging.Placement.Unapplied] or the notes.
//
// # What is refused
//
// Everything else, by name, under [ErrUnsupported]: other compressions
// (JPEG, Zstandard, LERC, JPEG 2000 …), other sample widths and formats
// (1-, 4- and 12-bit, signed 8-bit, unsigned 32-bit, 64-bit integers,
// 16-bit float, complex), mixed per-sample formats, other photometric
// interpretations (YCbCr, CMYK, CIELab, LogLuv …), and every orientation
// other than 1 (top-left) — a deliberate narrow cut: rows and columns are
// read as stored and never reordered. A file over [Options] is refused
// under [ErrLimit] from its directories, before any sample is read, and a
// file that is not well formed under [ErrMalformed].
package tiff
