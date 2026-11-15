---
type: adr
status: accepted
date: 2026-11-14
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-15
---

# ADR-0373: An image viewer — samples apart from presentation, mapped in Go, shipped as tiles

## Context

The tree can put pixels on screen and cannot look at an image. Its one pixel
path is RGBA8 through the `image` and `paintImage` opcodes; the decode helper
its consumers share produces 8 bits per channel, and
[`imagedecode.Pack`](../../public/thestack/imzero2/egui2/widgets/imagedecode/imagedecode.go)
keeps the high byte of a 16-bit sample. There is no type for an image's
samples, no place where a display range lives, and no widget that pans and
zooms a raster the caller supplies — although
[portolan](../../public/thestack/imzero2/egui2/widgets/portolan/doc.go) has
carried a flat pixel coordinate system, `Simple`, since
[ADR-0204](./0204-leaflet-map-core-port.md), with nothing using it.

What a viewer for measured images has to get right is well recorded outside
this tree, mostly by the projects that got it wrong first. Three lessons carry
the decisions below:

- **Display state must not reach the data.** ImageJ converts a 16-bit image to
  8 bits by scaling from whatever range the brightness dialog shows, unless a
  global option is unchecked
  ([ImageJ User Guide, Image › Type](https://imagej.net/ij/docs/guide/146-28.html));
  the result of a data operation then depends on a view setting no output
  records. The systems that avoided this keep the display mapping as a
  separate, stored value — DICOM's ordered modality, VOI and presentation
  transforms ([PS3.4 N.2](https://dicom.nema.org/medical/dicom/current/output/chtml/part04/sect_N.2.html)),
  astropy's interval and stretch
  ([normalization](https://docs.astropy.org/en/stable/visualization/normalization.html)).
- **A pixel convention left implicit is paid for later.** ImageJ stated where
  integer coordinates lie only in 1.52u (2020) and fixed regressions from it
  for two years ([release notes](https://imagej.net/ij/notes.html)); GDAL
  changed its reading of a GeoTIFF flag by half a pixel
  ([RFC 33](https://gdal.org/en/stable/development/rfc/rfc33_gtiff_pixelispoint.html)).
- **Draw what is available and refine.** BigDataViewer renders whatever
  resolution is cached and replaces it as finer data arrives
  ([Pietzsch et al.](https://arxiv.org/abs/1412.0488)); a viewer whose every
  change re-renders the whole image is interactive only while the image is
  small.

The cost of the existing path was measured for this decision. The
[image upload-cost trial](../trials/image-upload-cost/README.md) §0, on one
busy machine and the two headless hosts only (2026-11-14): replacing an image
every frame as 512 px tiles took 17 ms per cycle for 16 MiB of RGBA and 58 ms
for 64 MiB on the wgpu host, 22 and 64 ms on the cpu rasteriser; one texture
of the same size cost more than tiles at every size; a resident texture cost
nothing per frame; and mapping a 3840 × 2160 viewport of 16-bit samples to
RGBA8 through a table took about 3.5 ms on one core. The desktop host and the
browser tab are not measured.

The first version's scope was set by the owner (2026-11-14): whatever a PNG
or a JPEG file can hold — greyscale and colour, with or without alpha, at 8
or 16 bits — shown the way an ordinary viewer shows it and open to
inspection as measured samples; the native and headless hosts.

## Design space (QOC)

**Question.** Where does the mapping from samples to screen colours run?

**Options.**

- **O1** — in Go: samples stay in Go at full depth, visible tiles are mapped
  to RGBA8 and shipped through `paintImage`.
- **O2** — an indexed texture and a palette: Go ships 8-bit indices, the host
  holds a palette and looks colours up when drawing.
- **O3** — in the host: samples are uploaded in their own type and limits,
  transfer function and colour map are evaluated where the frame is drawn.

**Criteria.**

- **C1** — the stretch sees full-depth samples: limits and transfer function
  are applied before anything is cut to 8 bits.
- **C2** — cost of changing the recipe on a still view, by the trial's figures
  where they apply.
- **C3** — surfaces that change shape: IDL opcodes, the three renderers
  (wgpu, the cpu rasteriser, the WebGL painter on the mesh lane), the mesh
  wire.
- **C4** — one implementation of the mapping, so a capture, a remote viewer
  and the desktop show the same colours.
- **C5** — what it costs to change one's mind later.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | −  | ++ |
| C2 | −  | +  | ++ |
| C3 | ++ | −  | −− |
| C4 | ++ | +  | −  |
| C5 | ++ | +  | −− |

O2 quantises before the palette, so a change of limits or transfer function
still re-ships the indices and only a change of colour map is free. O3 is what
the newer viewers in other ecosystems do; here it is a new textured primitive
in three renderers and on the mesh wire, and
[ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) §SD3 treats a paint
callback, the usual vehicle for it, as a sentinel.

## Decision

We will build the viewer as O1: an image is immutable planes of samples held
in Go; what is shown is those planes seen through a small recipe — limits,
transfer function, and for a single plane a colour map — evaluated in Go for
the tiles in view and shipped as RGBA8 through the existing `paintImage`
opcode. The widget is
portolan's view model over its `Simple` coordinate system with tiles supplied
as pixels, and the app is an ordinary keelson app around it. No opcode, no
renderer and no wire changes.

### Subsidiary design decisions

- **SD1 — An image is planes and a colour model, shown as stored or one plane of it.**
  An image is one to four planes of one size and
  one sample kind, with a colour model that says what they are: grey, grey
  with alpha, RGB, RGBA. A colour image opens as a colour image — its planes
  composited as red, green and blue, alpha as transparency over a
  checkerboard — which is what the file means and what any other viewer
  shows. Any single plane of it, alpha included, can be shown instead as a
  scalar plane with a colour map. A luminance or any other combination of
  planes is derived data and therefore an operation with a name, not a view
  mode.
- **SD2 — The plane.** Samples are stored one plane per channel, in the kind
  the file had — 8-bit unsigned, 16-bit unsigned or 32-bit float in the first
  version — and are never narrowed implicitly; alpha is kept straight, not
  multiplied into the colour planes. A plane carries, beside its samples and
  size, a value transform (scale and offset, the identity for PNG and JPEG)
  and which samples are invalid (NaN in a float plane; none otherwise). The
  image carries its provenance: source name, a hash of the decoded samples,
  the decoder, the bit depth and colour type as stored, whether the source
  was lossy, and what the file said about its colour encoding. A property the
  file did not state is recorded as unknown, not guessed. Nothing mutates a
  plane; an operation makes a new one.
- **SD3 — One pixel convention, written here.** Image coordinates are
  continuous, in pixels; pixel (col, row) covers [col, col+1) × [row, row+1),
  its centre is at +0.5, row 0 is the top row, and both indices start at 0. A
  readout's index is the floor of the coordinate. This is the texture
  convention the hosts draw with, so the tile under the cursor and the sample
  read out are the same one by construction; a format with another convention
  is converted at its reader.
- **SD4 — The recipe.** Limits, a transfer function (linear, logarithmic,
  square root, asinh, gamma), and for a single plane a colour map with its
  colours for invalid and clipped samples. A colour image has one set of
  limits and one transfer function for all its colour planes: stretching
  channels separately changes the colours, so it is not offered as a display
  setting. Limits are stored as two numbers together with how they were
  obtained — typed in, the nominal range of the sample kind, the range of the
  data, or a named percentile rule — so a displayed range can always be read
  and repeated. The default shows a colour image over the nominal range of
  its sample kind, linear, which is the image as stored; a single plane over
  the range of its data, linear, greyscale. A robust stretch is one gesture
  away and is named on screen when active. A recipe is a value: it is saved
  with the view, travels in an export, and no operation reads it.
- **SD5 — Mapping is a table lookup.** For integer planes the recipe compiles
  to one table indexed by the sample; for float planes to a window and a
  table that holds the transfer function; a colour image uses the same table
  for each of its colour planes. A recipe change rebuilds the table and
  re-maps tiles; the mapped pixels are not kept beyond what was sent. A
  single plane's colour map is a plain grey ramp by default — under which an
  8-bit greyscale file looks as it is stored — or one of the design system's
  sequential and diverging scales
  ([ADR-0031](./0031-imzero2-design-system-color.md)), which are the
  perceptually uniform, colour-vision-safe ones; the viewer offers no palette
  outside that set. Samples are taken to
  be sRGB-encoded, which is what the hosts assume of the pixels they are
  given; a file that declares another encoding is shown the same way and the
  view says the declaration was not applied.
- **SD6 — Tiles and levels.** An image is cut into 512 px tiles; below full
  resolution it is shown from levels the viewer builds by halving, each
  holding the mean of the samples it covers and their minimum and maximum
  beside it. The level drawn is the coarsest one that still has at least one
  sample per screen pixel. Only tiles in view are mapped and shipped; after a
  recipe change they are replaced coarse level first, under a per-frame
  budget of bytes, with the previous pixels left on screen until their
  replacement lands. The view says which level it is showing. Levels are
  built off the frame; until they are, a tile below full resolution is
  reduced from the samples when it is asked for, to the same cells, so
  nothing on screen changes when the build lands.
- **SD7 — The widget.** `imageview` is semi-retained
  ([ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md))
  and owns no navigation of its own: view state, gestures, tile life cycle
  and overlays are portolan's. Portolan gains one seam — a tile supplied as
  pixels by the host widget instead of fetched by URL and decoded — and a way
  to say that every tile's content is out of date while it stays on screen.
  Rulers, colour bar and histogram sit around the canvas the way
  [`spectrumdisplay`](../../public/thestack/imzero2/egui2/widgets/spectrumdisplay/spectrumdisplay.go)
  composes its parts.
- **SD8 — Numbers come from the samples.** The cursor readout, the histogram,
  the limits a percentile rule yields and a region's statistics are computed
  in Go from the planes, exactly, over the whole image or the stated region —
  never from a texture and never from a reduced level. Each says what it was
  computed over.
- **SD9 — Operations are declared, and results leave as tables.** Opening,
  setting each part of the recipe, navigating, reading a pixel and measuring
  a region are operations in the app's catalog, so the person's control, an
  agent's call and the record are one declaration. A histogram, a region's
  statistics and a line profile are handed to play as ad-hoc datasets. An
  operation that computes a new plane follows three rules fixed now, ahead of
  the pipeline that will carry such operations: it makes a new plane, it does
  not read a recipe, and its parameters are declared once.
- **SD10 — The app and its readers.** A regular keelson app (working name
  `imagescope`): files arrive through the file broker and decode on a
  background lane into planes without passing through 8 bits or through
  premultiplied alpha. The readers cover what the two formats define and the
  standard library's decoders read: for PNG every colour type and bit depth,
  interlaced or not, a palette image expanded through its palette and
  recorded as such, an animated file opened as its default image; for JPEG
  greyscale and colour at 8 bits, baseline or progressive, a CMYK file
  converted to RGB without a profile and recorded as such. What a decoder
  does not read — 12-bit and lossless JPEG among them — is refused by name.
  The app states on screen when a source was lossy.
- **SD11 — Memory.** The planes and their levels stay resident in Go; textures
  leave the host by its idle eviction. The upload budget of SD6 is a named
  constant set from the trial, not a per-machine measurement.

### Milestones

- **M1** ✓ — the image and its planes, PNG and JPEG readers that keep bit
  depth and straight alpha, the recipe and the mapping kernels for a single
  plane and for a colour image, with their tests:
  [`public/science/imaging`](../../public/science/imaging/doc.go) and its
  `decode` and `display` packages.
- **M2** ✓ — portolan's pixel-tile seam and `imageview` showing an image at
  full resolution, as stored or one plane of it: pan, zoom, readout, colour
  bar. The seam is `portolan.TilePixelsI` on the loader and
  `Map.InvalidateTiles`; the widget is
  [`widgets/imageview`](../../public/thestack/imzero2/egui2/widgets/imageview/doc.go).
  A transparent sample is laid over a checkerboard fixed to the image's
  pixel grid when its tile is mapped, so no tile leaves Go translucent.
- **M3** ✓ — levels
  ([`imaging/levels`](../../public/science/imaging/levels/doc.go)), the
  per-frame budget and coarse-first replacement; the histogram with
  draggable limits, counted over the whole plane
  ([`imaging/stats`](../../public/science/imaging/stats/doc.go)), and
  percentile limits. The minimum and maximum beside each level are built;
  nothing drawn reads them yet.
- **M4** ✓ — the app, [`apps/imagescope`](../../apps/imagescope/app_register.go)
  under its working name: files through the file broker's read dialog, a
  phase machine mirrored from the open and the measurement, an operations
  catalog, a help book with a method page, two scenes and a tour entry.
  Region statistics are per plane over the region's samples at full
  resolution: count, range, mean, the standard deviation with divisor n, and
  nearest-rank percentiles, which are sample values
  ([`imaging/stats`](../../public/science/imaging/stats/doc.go)). *Send to
  play* publishes the region's samples, one row per pixel, at most about four
  megapixels, and a larger region is refused rather than cut. No operation
  opens a file: the picker is the person's. Nothing persists, and there is no
  launch configuration (Deferred).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `public/science/imaging` with `decode` and `display` (image, plane, recipe, mapping, levels, PNG/JPEG readers) | added | their tests; the package-properties table |
| Exported Go API: portolan's options | a tile source that supplies pixels, and content invalidation, added beside the URL source | portolan's pyramid tests; no existing caller changes |
| Exported Go API: `widgets/imageview` | added | `widgets/conformance`, a gallery demo, a scene |
| App registry | one app added, `apps/imagescope`; two seed variables, `BOXER_IMAGESCOPE_SAMPLE` and `BOXER_IMAGESCOPE_REGION` | its operations catalog, help book, scenes and tour entry; the environment-variable registry |
| egui2 IDL, FFFI2 frame, mesh wire, renderers | none | — |

## Alternatives

- **O2, an indexed texture and a palette.** Rejected: it buys a free colour
  map change and nothing for limits or transfer function, which are the
  changes a person drags; it needs an opcode and three renderer
  implementations to do so.
- **O3, mapping in the host.** Rejected for the first version, on C3 and C5:
  three renderers and the mesh wire for a path the trial does not yet show to
  be needed at the first version's sizes. The seam it would use is SD5's — a
  plane and a recipe go in, colours come out — so adopting it later replaces
  the mapping and the tile payload, not the model.
- **A new widget with its own view and tile cache, on the
  [waveform](./0208-audio-waveform-player-widget.md) pattern.** Rejected:
  it would write portolan's gestures, inertia, anchored zoom, tile retention
  and overlay protocol a second time. What that pattern has and portolan
  lacks — reductions that bound the samples they summarise — is taken into
  SD6 as the minimum and maximum kept beside each level.
- **One texture per image, redrawn larger under a clip.** Rejected: it works
  until the host's texture limit and costs a whole-image upload per recipe
  change; in the trial one texture cost more than the same pixels as tiles at
  every size measured, more than twice as much at 64 MiB on the wgpu host.
- **RGBA8 as the working type**, as every image consumer in the tree does
  now. Rejected: it is the display format, and an operation or a readout on
  it reads what was shown instead of what was measured.
- **Rainbow and other non-uniform colour maps** (the tree's older 8-stop
  palette set includes one). Rejected: a map whose lightness is not monotone
  draws boundaries the data does not have
  ([Crameri et al. 2020](https://pmc.ncbi.nlm.nih.gov/articles/PMC7595127/)).
- **A robust automatic stretch as the first view.** Rejected as the default:
  a non-linear or clipped view nobody asked for is the thing the publishing
  guidelines ask to be disclosed
  ([Rossner & Yamada 2004](https://pmc.ncbi.nlm.nih.gov/articles/PMC2172141/)).
  It stays one gesture away.
- **An Arrow tensor type for planes.** Rejected: arrow-go implemented neither
  canonical tensor extension (its documentation, read 2026-11-14), and the
  ad-hoc dataset gate maps a fixed-size list to a plain array; what leaves
  the app is tables of results, per SD9.

## Consequences

### Positive

- A displayed range is always a pair of numbers with a stated origin, and no
  data operation can depend on it.
- The same Go code maps the pixels for every host, so a capture and a remote
  viewer cannot disagree with the desktop about a colour.
- Portolan's `Simple` coordinate system gets its first caller, and a pixel
  tile seam other rasters can use.
- A recipe change costs the tiles in view, not the image.

### Negative

- Every recipe change is Go CPU time and an upload. The trial measured the
  mapping for one plane only; a colour image maps three. By the trial's cells a
  view of about 4 megapixels is replaced in 17–22 ms on the headless hosts; a
  3840 × 2160 view, twice the bytes, falls between measured cells and is at
  or past the edge of a 30 Hz drag there. The desktop host is unmeasured and
  a browser tab was left out of scope partly for this.
- A tile is four bytes per pixel on the pipe whatever the sample kind.
- Textures leave only by idle eviction, so host memory after a long pan is
  bounded by time, not by a byte budget the widget controls.
- Portolan carries Leaflet's vocabulary — zoom as a power of two, latitude
  for y — into a widget whose users think in pixels and magnification;
  `imageview` has to translate at its boundary.
- The whole image and its levels are resident; the levels, with a minimum
  and a maximum beside each mean, take about as much again as the samples.

### Neutral

- The pixel convention of SD3 differs from FITS's and from array libraries'
  centre-at-integer habit; readers for such formats convert.
- A JPEG's samples are what its decoder produced; the viewer shows them
  faithfully and says the source was lossy.
- Colour is not managed: a profile or gamma a file carries is recorded and
  reported, not applied. Two files with different declared encodings and the
  same samples look the same.
- Bit depths below 8 reach the viewer already scaled to the 8-bit range by
  the decoder; the stored depth is in the provenance, the original codes are
  not recoverable from the plane.

## Verification plan — Tier 1

- **Lane.** Default `go test`: readers against golden files of every colour
  type and bit depth the two formats define, alpha kept straight; the mapping kernels against a direct evaluation; level building
  against a brute-force reduction, including that each level's minimum and
  maximum bound its samples. `widgets/conformance` for the widget. One scene
  that opens an image, changes the limits on a still view and reads the
  cursor value back.
- **What would fail.** A 16-bit sample arriving as 8 bits; a colour image
  whose default view differs from its stored pixels; a semi-transparent pixel
  darkened by a second multiplication; a readout that
  differs from the source array; a recipe change that leaves the old pixels
  on screen (the case the cpu rasteriser's cache missed until
  [ADR-0205](./0205-imzero2-cpu-rasterized-pixel-host.md)'s 2026-11-14
  update); a level whose extremes do not bound its samples.
- **Gap.** A scene cannot set the transfer parameter: the headless host
  sends a `set_value` as text, which egui's number box ignores and its slider
  does not handle at all (observed 2026-11-15, egui 0.36), so the slider's
  sync is tested in Go only. Interactive cost is not gated; the
  [trial](../trials/image-upload-cost/README.md) is rerun when the path or
  the widget's tiling changes. The desktop host is covered by no lane.

## Deferred

- **Bands beyond a colour model, and extra axes** (z, time, an animated
  file's frames): band selection, false colour, a stretch per band. The
  image is planes with a model, and the recipe is per view, so that these
  add models, views and selectors rather than a second design.
- **Colour management** — applying a profile, a wide-gamut or high-range
  display.
- **Images larger than memory**, and the ranged file reads they need.
- **TIFF, FITS and chunked formats.** Each is a reader that produces planes;
  proprietary formats are converted out of process.
- **The browser tab.** After the trial's tab milestone.
- **Mapping in the host (O3).** Revisit when a measured recipe change at the
  sizes in use misses the rate people need, or when the tab is in scope.
- **An operation pipeline** — operators with a declared input region, scale
  behaviour, border rule and output kind, pulled by the view and by export.
  Its own ADR; SD9's three rules are what this one fixes for it.
- **Regions beyond a rectangle, and compared or linked views.**
- **A launch configuration for the app** — opening a window on an image
  another app holds. It waits for the first app that has an image to hand
  over; until then a window opens empty or on a sample.
- **Line profiles.** SD9 names them beside the histogram and region
  statistics as results that leave as tables; the first cut has region
  statistics only.
- **An explicit release for `paintImage` textures.** An opcode change; taken
  up if idle eviction proves too slow under real panning.

## Open questions

1. **The app's name.** Working name `imagescope`.

## Status

Accepted 2026-11-15.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [Image upload-cost trial](../trials/image-upload-cost/README.md) — the measurement behind O1 and SD6.
- [ADR-0204](./0204-leaflet-map-core-port.md) — portolan and its `Simple` coordinate system.
- [ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md) — the widget contract `imageview` follows.
- [ADR-0058](./0058-imzero2-scrolling-texture-widget.md) §SD1 — the standing choice of colour mapping in Go.
- [ADR-0128](./0128-imzero2-mesh-draw-stream-codec-lane.md) §SD3 — paint callbacks as a sentinel.
- [ADR-0149](./0149-implot-core-port-painter-lane.md) — `paintImage`.
- [ADR-0205](./0205-imzero2-cpu-rasterized-pixel-host.md) — the cpu rasteriser and its texture revision.
- [ADR-0031](./0031-imzero2-design-system-color.md) — the palettes.
