---
type: adr
status: proposed
date: 2026-11-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0398: Images beyond a colour model — more sample kinds, declared invalid samples, stacks with axes, a placement, and a seam for readers

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
  decided here is the smallest shape that readers of the formats above fill
  completely.
- **A reader's interpretation is behaviour.** Which georeferencing statement
  a reader used when a file carries several, whether it shifted a half pixel
  ([GDAL RFC 33](https://gdal.org/en/stable/development/rfc/rfc33_gtiff_pixelispoint.html)),
  whether it flipped rows: each changes every coordinate downstream and none
  is visible in the samples.
- **Nothing found may be guessed at.** ADR-0373 §SD2 records an unstated
  property as unknown; the same holds for a projection a reader does not
  implement or a compression it does not decode.

A fourth lesson is about where readers live. The formats that hold such
images are many, each is a parser of untrusted bytes with fixtures and a
fuzz corpus of its own, and which of them a deployment needs is that
deployment's business. Readers for three of them were first written in this
tree, under this decision, and then moved out to the module that uses them:
what this tree owes a format is a way to be given one, and a check that a
reader keeps the engine's rules.

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
axis order and per-axis transforms are all open at once, and nothing that
fills the model needs any of it yet.

## Decision

We will take O2. The engine gains three sample kinds and a declared invalid
sample on the plane; a **stack** of planes along named axes, from which an
image is taken; a **placement** that says where pixels are; and a **seam**
through which a host registers the readers that fill them. An image remains
one to four planes under a colour model, and everything that shows or
measures an image goes on taking one. Besides PNG and JPEG, this tree holds
no reader of its own.

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
- **SD5 — A reader converts conventions and says so.** A reader delivers the
  pixel convention of ADR-0373 §SD3 — row 0 at the top, a pixel covering
  [col, col+1) — whatever its format uses, and records what it did: rows
  flipped, a half-pixel shift for a point-registered raster, an axis order
  reversed. It takes its input as random-access bytes, so that a ranged
  source can be put under it later without a new signature. It refuses a
  file over a pixel, byte or plane limit from the header, before reading
  samples, and it refuses by name what it does not read; it guesses at
  nothing. Where one reader's name covers several variants of its format —
  wider offsets, an archive of arrays, a compressed wrapping — the variant
  read is recorded in the provenance in the format's own word. A file that
  ends before what is read is malformed; a read that fails for another
  reason is no refusal and reaches the caller as it is.
- **SD6 — The seam.** A reader is a value that states its format — a name,
  a title for a person, the endings its files usually carry, and a version
  of its own reading — recognises its files from their first bytes, says
  what a file holds, and decodes into a stack under the caller's limits.
  Readers are held in a registry. A host fills the process's registry where
  it wires its apps, in one visible place, and not by the side effect of an
  import: a reader that is linked is a parser of untrusted bytes, and which
  ones a deployment exposes should be readable at one site. A name is
  registered once. A file that two readers claim is refused, naming both;
  no order of registration decides. PNG and JPEG are registered as readers
  too, so that one call opens anything the host reads. The app goes on
  streaming those two as ADR-0374 §SD7 has it, and recognises them before it
  asks the registry: a second reader a host registered for their bytes is
  not asked by the app.
- **SD7 — A file's entries.** A file may hold several images: the header
  units of one format, the arrays of an archive of another. A reader lists
  them as entries — each with an identifier stable for that file, a title,
  its shape and kind, and whether it can be read or, with the kind of the
  refusal, why not. Exactly one entry is the default: the one a plain open
  reads, or, in a file with nothing readable, the one it would have read,
  whose refusal is then the file's. Decoding names an entry or takes that
  default. An
  app that holds a file's bytes can so show another entry without the
  person opening the file again, and an agent can ask which there are.
- **SD8 — The refusals are the seam's own.** No reader claims the file, or
  more than one does; the file uses something its reader does not read; the
  file is over a limit; the file is malformed; the file has no entry of the
  name asked for. A reader wraps one of the last four and its message names
  the feature, the limit, the defect or the entry, so an app shows the
  reader's sentence and still knows which kind of refusal it is without
  knowing the reader. A refusal's bare words and its kind can be read back
  from it. The seam also supplies the reads that keep a failed read apart
  from a defect, through a decompressor too, by marking the failures of the
  file's source.
- **SD9 — A conformance kit.** What SD5 to SD8 ask of a reader is checked by
  a kit a reader's own tests run against it: that it describes itself
  completely, recognises what it claims and nothing shorter than its own
  signature, lists entries that decode to what they announce, refuses over
  each limit before reading samples, stops when cancelled, wraps the seam's
  refusals, survives every truncation of a valid file without a panic, and
  does not report a read that failed as a file that is malformed. It takes
  a file with nothing readable, too, and holds its default to the refusal it
  announced.
  "Before reading samples" is measured against what the reader reads to
  list a file's entries, so a reader that must read a whole file to list
  them is not held by it and owes a test of its own; a reader's own working
  bounds, on a chunk or a tile, are named as its own and set so that a file
  within the caller's limits still decodes; and the kit cannot know a
  format's signature, so
  it offers a reader the empty input, zeros, noise and other formats' first
  bytes.
  The kit carries a format of its own, used by this tree's tests only, so
  that the app's paths for stacks, entries and refusals are exercised
  without a real format.

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
- **M3** ✓ — the seam: the reader contract, the registry, entries, the
  refusals and the conformance kit with its test format, in
  [`imaging/decode`](../../public/science/imaging/decode/doc.go). The
  readers for TIFF, FITS and NPY built under this decision on 2026-11-21
  leave the tree with it.
- **M4** ✓ — the app opens what its host registered: a band and a position
  chosen from a stack, the readout in frame coordinates. A stack opens on
  the image as stored where its bands are a colour model and on its first
  band otherwise. A change of selection runs off the frame as its own
  phase, a newer one superseding the one under way; it keeps the view and
  the marked region, keeps the recipe while the colour model stays the
  same, and says which limits it recomputed. Every reading and measurement
  names the selection it was computed on. Three bands of a binned kind were
  refused with the remedy until
  [ADR-0401](./0401-showing-bands-together-a-recipe-per-band-an-additive-composite-and-limits-by-zscale.md)
  gave them a view. What opens through the app is bounded by the file
  broker's read limit.
- **M5** ✓ — the app lists the formats of its host, offers a file's entries,
  and shows another entry from the bytes it holds. It holds a file's bytes
  only while the file has more than one entry and is the one open. While an
  entry is being read, a change of position or bands is refused rather than
  superseding it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `public/science/imaging` | three sample kinds, a plane's invalid value, `Stack` and its axes, `Placement`; plane labels and a placement on an image | the digest, `samples`, the package-properties table, the image engine how-to |
| Exported Go API: `imaging/display`, `imaging/stats`, `imaging/levels`, `imaging/tiles` | every kernel takes the six kinds and honours an invalid value | their tests, each against a direct evaluation |
| Exported Go API: `imaging/decode` | the reader contract, a registry and the process's own, entries, limits and four refusals; PNG and JPEG as readers | a conformance kit (`decode/decodetest`) with a test format; the image engine how-to |
| A host's wiring site | registers the readers it exposes | whatever lists what a host mounts |
| Exported Go API: `widgets/imageview` | the readout and the source description carry the new kinds, an invalid value and plane labels | `widgets/conformance`, the gallery demo |
| App operations: `apps/imagescope` | reads report the new kinds, invalid samples and labels; the formats of the host and a file's entries are listed, and an entry is chosen | its help book |
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
- **A projection library, or EPSG codes resolved to parameters.** Rejected
  here: a dependency and a registry for what a viewer of one image needs
  only as a name. Coordinates are given in the file's system and labelled
  with it.
- **Readers for TIFF, FITS and NPY in this tree.** Built first, under this
  decision, and moved out: three parsers of untrusted input, their fixtures
  and their fuzz corpora are a standing cost to a library whose viewer needs
  only to be handed a stack, and the next format would have raised the same
  question again.
- **Registration by importing a reader's package.** Rejected: it is how the
  apps of this tree register, and it suits code a host chooses by linking
  it. A parser of files is better named where the host is wired, where a
  test can also give an app a registry of its own.
- **The first registered reader wins a file two of them claim.** Rejected:
  it makes what a file opens as depend on the order of two lines.
- **A closed list of formats in the engine.** Rejected: it is what the seam
  replaces; a format is known by the name its reader registers.
- **Readers out of process, or compiled to WebAssembly behind one
  interface.** Not decided here: a reader behind this seam may be built
  either way, and the seam does not ask.
- **A registry that recovers a reader's panic.** Not taken: the kit holds a
  reader to not panicking on truncated input, and a panic that gets past it
  is a defect to be seen, not a refusal to be shown.

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
- This tree reads no measured-image format itself: out of the box the app
  opens PNG and JPEG, and what else it opens is the host's doing.
- A reader's faithfulness is its module's to show; the kit checks the
  contract, not that a format was read correctly.
- A stack is resident: a time series or a cube that does not fit in memory
  does not open.
- Coordinates in a projected system are labelled, not converted: two images
  in different systems cannot be compared by position.

### Neutral

- A consumer's own `switch` over sample kinds meets three kinds it did not
  list.
- A format that stores its bottom row first is read top row first; an
  image's digest is of the samples in the engine's order.
- A reduced level of a plane with an invalid value can differ by one code
  from the mean of its valid samples.

## Migration — Tier 1

- **Breaks.** In the model and the kernels nothing stops compiling.
  `SampleKindE.IsInteger` is true for
  the signed kinds too; a caller that used it to mean "has one histogram bin
  per code" needs `Codes`, which says so. `NominalMax` goes on answering for
  unsigned kinds only, and `NominalRange` answers for every integer kind.
  The packages `decode/tiff`, `decode/fits` and `decode/npy`, the four
  format values named after them and the dispatch that knew them, all
  published for a day, are removed.
- **Path.** Re-read each `switch` over `SampleKindE` in consuming code. A
  module that read those formats through this tree registers readers with
  the seam; the three packages are in this repository's history at the
  commit before their removal.
- **Regeneration.** The package-properties table and the surface document.
- **Old shape.** The model's is kept, the additions beside it; the
  readers' is removed outright.

## Verification plan — Tier 1

- **Lane.** Default `go test`: each kernel over the six kinds, with and
  without an invalid value, against a direct evaluation; a stack's
  selections sharing its planes; the placement against closed-form cases and
  its own inverse; the registry's refusals; the conformance kit over the
  test format and over PNG and JPEG; the app opening the test format's
  stacks, entries and refusals.
- **What would fail.** A signed sample read as unsigned; an invalid sample
  counted in a mean or drawn as data; a reader that passes the kit while
  reading samples over a limit, or ignoring a cancel; a file two readers
  claim opening as either; a digest of an existing image that changed.
- **Gap.** No measured-image format is read in this tree, so no lane here
  holds a placement or an invalid value to a real file; the samples and the
  test format stand in.

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
- **A reader over a ranged source**, and a registry entry that says a
  reader can use one.

## Status

Proposed — awaiting review by the repository owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md) — the engine this extends; §SD2, §SD3, §SD8 and its Deferred section.
- [ADR-0374](./0374-imageview-shows-a-tile-source-and-the-decoder-streams-into-one.md) — the tile source and the memory bound.
- [GeoTIFF 1.1](https://docs.ogc.org/is/19-008r4/19-008r4.html) and GDAL's [raster data model](https://gdal.org/en/stable/user/raster_data_model.html) — a placement and an invalid value as formats state them.
- [FITS Standard 4.0](https://fits.gsfc.nasa.gov/fits_standard.html); Greisen & Calabretta 2002 and Calabretta & Greisen 2002, [representations of world coordinates in FITS](https://fits.gsfc.nasa.gov/fits_wcs.html).
