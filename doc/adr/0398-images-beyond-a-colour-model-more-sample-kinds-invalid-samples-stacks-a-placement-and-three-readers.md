---
type: adr
status: proposed
date: 2026-11-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0398: Images beyond a colour model — more sample kinds, declared invalid samples, stacks with axes, a placement, and readers for TIFF, FITS and NPY

## Context

[ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md)
scoped the image engine to what a PNG or a JPEG holds: one to four planes
under a colour model, in 8-bit, 16-bit unsigned or 32-bit float samples. Its
Deferred section lists what that leaves out — bands beyond a colour model,
extra axes, TIFF and FITS — and says how they are meant to arrive: "the image
is planes with a model, and the recipe is per view, so that these add models,
views and selectors rather than a second design."

Measured images from remote sensing, astronomy and microscopy need four
things the engine has no place for.

- **Other sample kinds.** Elevation models and calibrated detector frames are
  signed 16-bit; FITS has 32-bit integers and 64-bit floats as ordinary
  types ([FITS Standard 4.0](https://fits.gsfc.nasa.gov/fits_standard.html)
  §4.4.1.1, `BITPIX`); GeoTIFF products come in all of them.
- **Samples declared invalid.** An integer raster marks "no measurement" with
  a reserved code: FITS `BLANK`, GDAL's nodata value
  ([raster data model](https://gdal.org/en/stable/user/raster_data_model.html)).
  The engine knows one invalid sample, NaN in a float plane. A statistic that
  counts a border of zeros as measurements is wrong without saying so.
- **More planes than a colour model names, along more than one axis.** A
  multispectral scene has a dozen bands; a spectral cube, a focus series and
  a time series have one plane per position. They are one file, or one
  acquisition, and a person looks at one, or three, of the planes at a time.
- **A place.** Where a pixel is: an affine to a projected coordinate system
  ([GeoTIFF 1.1](https://docs.ogc.org/is/19-008r4/19-008r4.html)), a world
  coordinate system on the sky
  ([FITS WCS](https://fits.gsfc.nasa.gov/fits_wcs.html)), a physical pixel
  size. ADR-0373 §SD3 fixed the pixel convention so that this could be added
  without a second one.

Three lessons from systems that carry such data shape the decision.

- **A general model left unfinished costs more than a narrow one finished.**
  napari carried axis labels and units as inert metadata from 0.5.0 (2024)
  until 0.7.0 (2026) made them effective
  ([release notes](https://napari.org/stable/release/index.html)). What is
  decided here is the smallest shape the three readers below fill completely.
- **A reader's interpretation is behaviour.** Which georeferencing statement
  a reader used when a file carries several, whether it shifted a half pixel
  ([GDAL RFC 33](https://gdal.org/en/stable/development/rfc/rfc33_gtiff_pixelispoint.html)),
  whether it flipped rows: each changes every coordinate downstream and none
  is visible in the samples.
- **Nothing found may be guessed at.** ADR-0373 §SD2 records an unstated
  property as unknown; the same holds for a projection a reader does not
  implement or a compression it does not decode.

The tree's rules bound the readers: no cgo, dependencies that pass the
licence gate, and a refusal that names what was refused. `golang.org/x/image/tiff`,
already a dependency, reads unsigned 8- and 16-bit samples from the first
directory of a classic TIFF and nothing else of what is needed here (its
package documentation and source, read 2026-11-14).

## Design space (QOC)

**Question.** How do planes beyond a colour model, and axes beyond rows and
columns, enter the model?

**Options.**

- **O1** — widen the image: a colour model "bands" with any number of planes,
  and axes on the image.
- **O2** — a stack above the image: many planes along named axes, of which an
  image — one plane, or three, under the existing colour models — is taken
  without copying.
- **O3** — an N-dimensional array type with named axes, of which rows and
  columns are two, and the image becomes a view of it.

**Criteria.**

- **C1** — what stays unchanged: the tile source, the tiled store, the
  mapping kernels, the viewer, which all take an image of at most four
  planes.
- **C2** — memory and tile size when a dataset has a dozen planes and a view
  shows one.
- **C3** — the meaning of "the image as stored" (ADR-0373 §SD1) stays one
  thing.
- **C4** — room for what is deferred: a recipe per band, sources larger than
  memory, axes with coordinates.
- **C5** — how much is decided now that no reader exercises.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | ++ | −− |
| C2 | −  | ++ | +  |
| C3 | −  | ++ | −  |
| C4 | −  | +  | ++ |
| C5 | +  | ++ | −− |

Under O1 every tile of a twelve-band scene carries twelve planes to show one,
and a colour image and a band image differ only by a model that names
nothing. O3 is what array libraries do and is the larger design: chunking,
axis order and per-axis transforms are all open at once, and the three
readers need none of it yet.

## Decision

We will take O2. The engine gains three sample kinds and a declared invalid
sample on the plane; a **stack** of planes along named axes, from which an
image is taken; a **placement** that says where pixels are; and three readers
that fill them. An image remains one to four planes under a colour model, and
everything that shows or measures an image goes on taking one.

### Subsidiary design decisions

- **SD1 — Three more sample kinds.** Signed 16-bit, signed 32-bit and 64-bit
  float join the three of ADR-0373 §SD2, stored as read and never narrowed.
  An integer kind of at most 16 bits has a histogram with one bin per code
  and compiles to a lookup table; signed 32-bit and 64-bit float are binned
  and windowed as 32-bit float is. A reduced level of an integer plane holds
  rounded means in the plane's own kind. The identifiers of the existing
  kinds do not change, so the digest of an image that existed before is the
  same. Unsigned 32-bit, 64-bit integers and complex samples are not kinds: a
  reader refuses them by name.
- **SD2 — A plane may declare one stored value invalid.** Besides NaN in a
  float plane, a plane of any kind may name one stored value that marks "no
  measurement". An invalid sample is left out of every range, histogram,
  percentile and statistic and counted as invalid; it is drawn as an invalid
  sample is; a reduced cell is the mean, minimum and maximum of its valid
  samples and invalid when it has none. A mean that would round onto the
  declared value is moved to the neighbouring code: a reduced level is for
  drawing, and no number is read from one (ADR-0373 §SD8). The declaration is
  not part of an image's digest, as the value transform is not. The planes of
  an image or a stack share one declared value or none, and an alpha plane
  declares none: where a file states both transparency and an invalid value,
  the value is recorded and not applied.
- **SD3 — The stack.** A stack is planes of one size and one kind arranged
  along named axes other than rows and columns. An axis has a name, a size, a
  role, and optionally a unit with a coordinate per position (a wavelength, a
  time, a depth) and a label per position. The role is one of two: a **band**
  axis, whose positions are measured together and may be shown together; or
  a **selector** axis, of which a view shows one position. A stack has at
  most one band axis. An image is taken from a stack by naming a position on
  every selector axis and one or three positions on the band axis; it shares
  the stack's planes, and it carries the labels of the positions it was
  taken from, so a readout names a band and not "red". A stack whose band
  axis is a colour model says so, and the image as stored is that selection.
  Planes that differ in size or kind are separate stacks. A stack is resident
  in memory and bounded as an image is (ADR-0374 §SD9).
- **SD4 — The placement.** A placement is an affine from the continuous pixel
  coordinates of ADR-0373 §SD3 to coordinates in a frame, and what the frame
  is: a coordinate reference system named by the code the file gave
  (recorded, and its coordinates are whatever that system defines — the
  engine holds no projection library); a gnomonic projection of the sky about
  a reference point, which the engine evaluates to longitude and latitude; a
  physical scale in a unit; or nothing. It records which statement of the
  file it was derived from. What a file declares and the engine cannot
  evaluate — another projection, a distortion polynomial, a second coordinate
  system — is recorded as declared and not applied, and where the affine
  alone would then misplace pixels the placement is left empty rather than
  approximated. A reduced level's placement follows from its factor. Images
  and stacks carry one placement; it is not part of the digest.
- **SD5 — Readers convert conventions and say so.** Each reader delivers the
  pixel convention of ADR-0373 §SD3 — row 0 at the top, a pixel covering
  [col, col+1) — whatever its format uses, and records what it did: rows
  flipped, a half-pixel shift for a point-registered raster, an axis order
  reversed. A reader takes its input as random-access bytes, so that a ranged
  source can be put under it later without a new signature. It refuses a
  file over a pixel or byte limit from the header, before reading samples.
- **SD6 — TIFF.** Classic TIFF and BigTIFF in either byte order; strips and
  tiles; chunky and planar layouts; no compression, PackBits, LZW and
  Deflate; horizontal and floating-point predictors; unsigned 8- and 16-bit,
  signed 16- and 32-bit, 32- and 64-bit float samples. A greyscale or RGB
  file, with unassociated alpha or none, is a stack whose band axis is that
  colour model; any other sample layout is a stack of bands. A palette file
  is expanded and recorded as such. Directories of the size, kind and layout
  of the first form a selector axis; reduced-resolution and mask directories
  are counted and not read. From GeoTIFF: the affine from the pixel scale
  and tiepoint or from the transformation matrix, the raster-type half
  pixel, and the reference system's code; from GDAL's private tags: the
  invalid value, and band names, scales and offsets where stated. Every
  other compression — JPEG, Zstandard, LERC, JPEG 2000 among them — and
  every other sample format is refused by name; so are an orientation other
  than top-left, which the reader does not convert, and a file with strips
  or tiles left unwritten. A later directory that would be a page and cannot
  be read refuses the file, so that no position shifts.
- **SD7 — FITS.** The primary array and image extensions, `BITPIX` 8, 16,
  32, −32 and −64; `BSCALE` and `BZERO` as the value transform, `BLANK` as
  the invalid value; axes beyond two as selector axes named by their
  `CTYPE`; rows flipped so that row 0 is the top. A linear coordinate system
  and a gnomonic one (`TAN`) become the placement; any other projection, a
  distortion convention and alternate systems are recorded as declared.
  64-bit integers, tile-compressed images and random groups are refused by
  name; table extensions are counted and not read.
- **SD8 — NPY and NPZ.** The NPY array format, versions 1 to 3, of the six
  kinds in either byte order and either index order; two dimensions are one
  plane, further leading dimensions are selector axes. An NPZ archive lists
  its members and reads the one asked for. An object array, which would need
  an unpickler, is refused; so is every other element type, by name.

### Milestones

- **M1** ✓ — the model: kinds, the invalid value, the stack, the placement and
  the digest of a stack, in [`imaging`](../../public/science/imaging/doc.go).
  An image taken from a stack has the digest the same planes give when
  assembled directly, from plane digests computed once; a stack's own digest
  cannot equal an image's.
- **M2** ✓ — the kernels and their callers honour them: mapping, histograms and
  statistics, levels, the tiled store, the viewer's readout, the app's
  reads. A mean that lands on the declared value moves to the neighbouring
  code on the side of the exact mean, and up when the two are equal; in a
  float plane to the next float. In a colour image a pixel with an invalid
  sample in any colour plane is drawn as transparent pixels are. A colour
  image of a binned kind — 32-bit integer, either float — is not mapped as a
  composite, as a 32-bit float one was not before: the viewer opens it on its
  first plane (Deferred, with the recipe per band).
- **M3** ✓ — the TIFF reader, with GeoTIFF
  ([`decode/tiff`](../../public/science/imaging/decode/tiff/doc.go)). A
  raster type the file does not state is read as area-registered, and the
  placement's source says so. Every stack it makes has a band axis, of one
  position for a greyscale file.
- **M4** ✓ — the FITS reader
  ([`decode/fits`](../../public/science/imaging/decode/fits/doc.go)). A
  cube's axes beyond the second become selector axes slowest first, so the
  planes keep the file's order and the axis numbering is reversed. Where a
  header carries both forms of the linear matrix, which the standard
  forbids, one is used and the reader says which. A celestial axis in
  arcseconds, arcminutes or radians is converted to degrees, though the
  standard asks for degrees. A gzip-wrapped file is decompressed in memory
  under the byte limit.
- **M5** ✓ — the NPY and NPZ reader
  ([`decode/npy`](../../public/science/imaging/decode/npy/doc.go)). Native
  byte order (`=`) is refused by name: the format description promises a
  file that reads the same on any machine. An archive member is bounded by
  its own array header, not by the archive's directory.
- All three readers also bound the number of planes: a byte limit alone
  admits a shape of a hundred million one-pixel planes.
- **M6** ✓ — the app opens all three: a band and a position chosen from a
  stack, the readout in frame coordinates. A stack opens on the image as
  stored where its bands are a colour model and on its first band otherwise.
  A change of selection runs off the frame as its own phase, a newer one
  superseding the one under way; it keeps the view and the marked region,
  keeps the recipe while the colour model stays the same, and says which
  limits it recomputed. Every reading and measurement names the selection it
  was computed on. Three bands of a binned kind were refused with the remedy
  until [ADR-0401](./0401-showing-bands-together-a-recipe-per-band-an-additive-composite-and-limits-by-zscale.md)
  gave them a view.
  What opens through the app is bounded by the file broker's read limit,
  which is below the readers' own.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `public/science/imaging` | three sample kinds, a plane's invalid value, `Stack` and its axes, `Placement`; plane labels and a placement on an image | the digest, `samples`, the package-properties table, the image engine how-to |
| Exported Go API: `imaging/display`, `imaging/stats`, `imaging/levels`, `imaging/tiles` | every kernel takes the six kinds and honours an invalid value | their tests, each against a direct evaluation |
| Exported Go API: readers under `imaging/decode` | three packages added | fuzz targets; fixtures with their sources and licences |
| Exported Go API: `widgets/imageview` | the readout and the source description carry the new kinds, an invalid value and plane labels | `widgets/conformance`, the gallery demo |
| App operations: `apps/imagescope` | reads report the new kinds, invalid samples and labels | its help book |
| egui2 IDL, FFFI2 frame, renderers | none | — |

## Alternatives

- **O1, a colour model for bands.** Rejected on C1 and C2: every consumer of
  an image would meet images of any plane count, and tiles would carry planes
  no view shows.
- **O3, an N-dimensional array.** Rejected for now on C5; the stack's axes
  are named and typed so that it can become a view of one.
- **Convert an integer plane with an invalid value to float and NaN.**
  Rejected: it changes the kind the file had, gives up the exact histogram of
  a 16-bit plane, and doubles the memory of the common case.
- **A validity mask plane beside each plane.** Rejected: a byte per sample
  for what the formats here state as one value. A mask a file stores as its
  own raster is a plane of its own and is deferred.
- **Unsigned 16-bit FITS read as unsigned.** A FITS file stores unsigned
  16-bit as signed with `BZERO` 32768. Rejected as a special case: the stored
  sample is the signed one and the value transform already gives the value.
- **A projection library, or EPSG codes resolved to parameters.** Rejected
  here: a dependency and a registry for what a viewer of one image needs
  only as a name. Coordinates are given in the file's system and labelled
  with it.
- **`golang.org/x/image/tiff` extended, or a third-party TIFF package.**
  Rejected: the first decodes through `image.Image`, which has no float,
  signed or planar form; the pure-Go packages found either stop at the same
  place or are unmaintained. The reader is written from the TIFF 6.0 and
  BigTIFF descriptions over the standard library's `compress` packages.
- **A decoder compiled to WebAssembly and run in-process**, as the tree does
  for other parsers. Not taken for these three formats, whose containers are
  simple; it remains the route for codecs that are not.

## Consequences

### Positive

- A file from an instrument opens with its own sample kind, its invalid
  samples excluded from every number, and its pixels placed.
- The viewer, the store and the mapping kernels keep taking an image; a
  stack is selection in front of them.
- A reader that is put over a ranged source later keeps its signature.

### Negative

- Every kernel has six kinds and an invalid-value branch where it had three
  kinds and NaN; each addition is a place a kind can be handled wrongly, held
  by tests against a direct evaluation.
- Three readers are code the ecosystem has elsewhere; each is held to
  fixtures written by a reference implementation, and a difference is a
  defect of ours.
- A stack is resident: a time series or a cube that does not fit in memory
  does not open.
- Coordinates in a projected system are labelled, not converted: two images
  in different systems cannot be compared by position.

### Neutral

- A consumer's own `switch` over sample kinds meets three kinds it did not
  list.
- A FITS image is stored bottom row first and is read top row first; its
  digest is of the samples in the engine's order.
- A reduced level of a plane with an invalid value can differ by one code
  from the mean of its valid samples.

## Migration — Tier 1

- **Breaks.** Nothing stops compiling. `SampleKindE.IsInteger` is true for
  the signed kinds too; a caller that used it to mean "has one histogram bin
  per code" needs `Codes`, which says so. `NominalMax` goes on answering for
  unsigned kinds only, and `NominalRange` answers for every integer kind.
- **Path.** Re-read each `switch` over `SampleKindE` in consuming code.
- **Regeneration.** None.
- **Old shape.** Kept; the additions are beside it.

## Verification plan — Tier 1

- **Lane.** Default `go test`: each kernel over the six kinds, with and
  without an invalid value, against a direct evaluation; a stack's
  selections sharing its planes; the placement against closed-form cases and
  its own inverse; each reader against fixtures written by a reference
  implementation of its format, sample for sample, and under the fuzz lane.
- **What would fail.** A signed sample read as unsigned; an invalid sample
  counted in a mean or drawn as data; a row order or a half pixel a reader
  did not convert; a compression or projection applied that the reader does
  not implement; a digest of an existing image that changed.
- **Gap.** The fixtures are written outside the lanes, by hand, from scripts
  kept beside them; no lane re-derives them.

## Deferred

- **A recipe per band, and more than three bands composited.** A display
  decision of its own.
- **Stacks and files larger than memory**: ranged reads, a file's own
  reduced-resolution directories, chunked formats.
- **Unsigned 32-bit samples and label images**, which are identifiers and
  not measurements: nearest reduction, no arithmetic, a colour per id.
- **Other projections, distortion polynomials, resolving a reference
  system's code.**
- **Mask rasters, and a mask per band.**
- **JPEG, Zstandard and LERC in TIFF; tile-compressed FITS.**

## Status

Proposed — awaiting review by the repository owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md) — the engine this extends; §SD2, §SD3, §SD8 and its Deferred section.
- [ADR-0374](./0374-imageview-shows-a-tile-source-and-the-decoder-streams-into-one.md) — the tile source and the memory bound.
- [TIFF 6.0](https://www.itu.int/itudoc/itu-t/com16/tiff-fx/docs/tiff6.pdf), [BigTIFF](https://www.awaresystems.be/imaging/tiff/bigtiff.html), [GeoTIFF 1.1](https://docs.ogc.org/is/19-008r4/19-008r4.html).
- [FITS Standard 4.0](https://fits.gsfc.nasa.gov/fits_standard.html); Greisen & Calabretta 2002 and Calabretta & Greisen 2002, [representations of world coordinates in FITS](https://fits.gsfc.nasa.gov/fits_wcs.html).
- [NPY format](https://numpy.org/doc/stable/reference/generated/numpy.lib.format.html).
