---
type: adr
status: accepted
date: 2026-11-15
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-15
---

# ADR-0374: imageview shows a tile source, and the decoder streams into one

## Context

[ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md)
gave `imageview` one way in: `SetImage` with an `imaging.Image`, every sample
in memory, from which the viewer cuts its own tiles and builds its own
levels. Two consumers show what that leaves out.

**A page renderer.** A PDF viewer built downstream on `imageview` has a
backend that renders any rectangle of a page at any scale and leaves "tiling,
levels of detail and texture caching" to the image viewer. Through
`SetImage` it can only render each page whole, once, at one scale chosen in
advance. Magnified past that scale the page shows enlarged pixels; a page
costs its whole raster and its levels whatever is in view; and rendering
again at a sharper scale would be a new image, which `SetImage` fits to the
pane, throwing the reader's view away. Its authors list, besides, what a
document reader needs that `imageview` keeps private: an overlay painted in
image coordinates (every search hit, a text selection, links a scene can
find by name), the view read and set from code (go to a point, keep the
zoom across pages, say when the person zoomed), the wheel scrolling instead
of zooming, and keys reaching the host.

**A large file.** `decode` reads PNG and JPEG through Go's standard
decoders, which hold the whole decoded picture; the engine then copies it
into planes, so the peak is two whole images, and the cap is 64 megapixels.
A clean-room survey of how other systems decode these two formats in pieces
(documentation, specifications, issue prose only; no source read) found:

- No documented system seeks into a plain PNG or JPEG. Viewers of large
  images pre-tile them, and the tiler that does it reads the file once, top
  to bottom, and writes every level of the pyramid in that one pass
  ([libvips `dzsave`](https://www.libvips.org/API/current/method.Image.dzsave.html),
  [pyramids](https://www.libvips.org/API/8.17/making-image-pyramids.html)).
  The one documented random access into a large JPEG relies on restart
  markers, which common encoders leave out and which must be found by a
  scan first ([OpenSlide](https://openslide.org/formats/hamamatsu/)).
- A non-interlaced PNG and a baseline JPEG decode in memory proportional to
  the width: a PNG row filter reads only the previous row
  ([spng](https://libspng.org/docs/decode/)); libjpeg works in strips of a
  few rows ([libjpeg.txt](https://raw.githubusercontent.com/libjpeg-turbo/libjpeg-turbo/main/doc/libjpeg.txt)).
  An interlaced PNG and a progressive JPEG do not: the first needs most of
  the image before any final row, the second a coefficient buffer of 2 to 6
  bytes per pixel before any output.
- JPEG can decode at 1/2, 1/4 and 1/8 scale cheaply, but still reads the
  whole stream, and its reduced output is a low-frequency reconstruction,
  not a block mean, and has no minimum or maximum.
- Go's decoders offer `Decode` and `DecodeConfig` and nothing incremental;
  scaled decoding has been asked for since 2014
  ([#8055](https://github.com/golang/go/issues/8055), and the decoding-options
  proposal [#27830](https://github.com/golang/go/issues/27830) is on hold).

Both consumers want the same seam: the viewer asks for a tile at a level and
something answers it — a renderer that draws it, or a store a decoder filled.
The boundary to [portolan](./0204-leaflet-map-core-port.md) decides how
much of that is new: portolan already schedules tiles per level, renders
above a source's native level, carries overlays, sets and reports the view,
and can leave the wheel and the keys alone.

## Design space

- **Q1 — What does a source hand back: samples, or display pixels?**
  O1a samples (planes of a sample kind) for every source; O1b display
  pixels for a rendered source, samples for a raster one. Criteria: one
  tile path; the recipe (invert, a stretch) applying to a rendered page;
  what the numbers can claim.
- **Q2 — Who holds which pixels?** O2a portolan's display tiles are the only
  cache; O2b imageview keeps a bounded cache of sample tiles beside it.
  Criteria: a recipe change must not ask a renderer again; memory bounded.
- **Q3 — What does "tile-aware decoder" do?** O3a decode on demand per tile;
  O3b decode once, streaming, into tiles and all levels; O3c decode whole as
  now, then cut tiles. Criteria: peak memory, time to first tile, how far
  the prior art supports it, how much decoder Go lacks.
- **Q4 — How does a host reach the view?** O4a through portolan's types;
  O4b through imageview's own, in image pixels.

O1a wins: the rendered page is RGB samples of kind u8, which the default
recipe of a colour image shows unchanged (ADR-0373 §SD4), and one path means
one cache, one upload budget and one readout. O1b is rejected because it
splits every downstream feature in two for a saving of one conversion per
tile. O2b wins: under O2a a contrast change would re-render a PDF page.
O3b wins for the formats that stream (below); O3a is rejected because
without an index each tile costs every row above it, and an index is new
ground no documented PNG reader has taken. O4b wins: a host speaks pixels,
and portolan's geographic types stay portolan's.

## Decision

`imageview` shows a **source**: something that answers a tile of samples at
a level. An image in memory is one source; a renderer and a decoded store are
others. The decoder decodes once, streaming where the format allows, into a
store of tiles and levels that is itself a source.

### Subsidiary design decisions

- **SD1 — The source.** A source describes itself once — its size at level
  0 in pixels, its colour model and sample kind, each plane's value
  transform, the levels it answers natively (how many halvings below level
  0, how many doublings above), whether it holds samples or renders them,
  and its provenance — and answers `Tile(ctx, level, x, y)` with the planes
  of one tile, at most 512 px square and cut at the edges. It is called on
  the loader's workers and must honour `ctx`. Level 0 is the source's own
  pixel grid; level −k is a 2^k reduction, level +k a 2^k magnification.
  `SetImage` stays, as a source over an image in memory and the levels built
  from it.
- **SD2 — Samples everywhere.** Every tile is samples (O1a). A source that
  renders — a page, a plot — returns RGB or RGBA samples of kind u8 and says
  so; the viewer then shows no histogram and no colour bar unless asked, and
  the readout calls its values rendered colour. Nothing about a rendered
  source is presented as a measurement.
- **SD3 — Two caches, one each.** imageview keeps the sample tiles it has
  been given, bounded in bytes and evicted least recently used; portolan
  keeps the display tiles it has shipped. A source whose tiles are in memory
  already — an image, the decoder's store — says so, and its tiles are read
  in place and not cached again. A recipe change re-maps from the
  sample cache and asks no source. `Invalidate` — the source's content
  changed, its geometry did not — drops the sample cache and has portolan
  replace the tiles in view coarse first, the old ones standing meanwhile
  (ADR-0373 §SD6). `SetSource` keeps the view when asked to; `SetImage`
  goes on fitting.
- **SD4 — The host's view and overlay, in pixels.** imageview passes
  through what portolan has, in its own terms: the view as the centre pixel
  and the magnification, read and set; a call that brings a rectangle of
  pixels into view; a `ViewChanged` event; an overlay callback painted after
  the image with a projector from image pixels to the canvas, through which
  the paint and hit-region calls work as in a portolan overlay; and while a
  region is dragged, the drag as it stands. No portolan type appears in
  imageview's API.
- **SD5 — The boundary to portolan.** portolan schedules tiles per level,
  renders above the native level, holds display tiles, ships them under the
  upload budget, owns gestures and the view. imageview owns the pixel
  convention, the source, the sample cache, the recipe and every number.
  imageview's level k is portolan's zoom k, so no level is translated.
  portolan gains two things: a wheel that pans, with the wheel and a
  modifier zooming — the reading interaction a document wants — and the box
  being dragged, reported while it is dragged and not only on release. Keys
  stay off in the map and reach the host's frame. The sample cache does not
  follow portolan's tile lifetime: it is bounded and evicted on its own, so
  portolan needs no callback when it drops a tile.
- **SD6 — Numbers from a source.** ADR-0373 §SD8 holds wherever a source
  holds samples: a whole-image histogram and a region's statistics read the
  level-0 tiles through the source, as a background task whose cost is
  linear in the pixels read. A rendering source has no whole-image
  statistics; a region over one is measured at the level stated in the
  result and called rendered colour.
- **SD7 — The decoder streams into a tiled store.** Decoding a file is one
  pass, top to bottom, that cuts level-0 tiles and builds every reduced level
  as 512-row bands complete — mean, minimum and maximum, exactly as
  `levels.Build` defines them — and offers each tile as soon as it is final,
  so the first tiles show while the rest decode. For a non-interlaced PNG
  the pass is a row decoder written from the PNG specification over
  `compress/zlib`, with working memory proportional to the width and the
  standard library's decoder as the test oracle. Interlaced PNG, and JPEG,
  are decoded whole by the standard library as now and then cut into tiles
  — a JPEG row decoder is not written; their peak memory stays the whole
  image and their cap stays. The store holds its tiles in memory.
- **SD8 — The seam before the decoder.** The source seam with the
  in-memory source and the sample cache comes first and changes nothing a
  caller of `SetImage` sees; the view and overlay surface follow, then the
  streaming decoder and its store, then a rendering source the tests and the
  gallery use in place of a page renderer.
- **SD9 — Memory is bounded, per kind of source.** A procedural source — a
  renderer, a generator — holds nothing of its own beyond what it chooses;
  what the viewer holds for it is the sample cache, bounded in bytes by an
  option, and the display tiles of the view, which portolan prunes to the
  view and its keep buffer. A source in memory — an image, the decoder's
  store — is bounded by the decode limit the host sets, in pixels: its size
  is the samples, about a third more for the means, and as much again for
  the minima and maxima. Over the limit a file is refused, naming the limit.
  No source is held twice.

### Milestones

- **M1 ✓ — The source and the sample cache.** `SetImage` over an in-memory
  source; `Invalidate`; `SetSource` keeping the view
  ([`imageview.SourceI`](../../public/thestack/imzero2/egui2/widgets/imageview/sourcei.go)).
- **M2 ✓ — The view and the overlay.** View, overlay, `ViewChanged` and the
  drag in pixels; portolan's wheel-pans interaction and box in progress. The
  call that brings a rectangle into view is `Reveal`, since the widget
  contract reserves `Show` for a show-and-hide pair.
- **M3 ✓ — The streaming decoder.** The PNG row decoder and the tiled store
  with one-pass levels; JPEG and interlaced PNG through the whole decoder
  into the same store; numbers through the source
  ([`imaging/tiles`](../../public/science/imaging/tiles/doc.go),
  `decode.DecodeTiled`, `imageview.StoreSource`). A source that counts its
  own histogram (`imageview.HistogramSourceI`) gets the histogram strip and
  percentile limits. The app opens every file into a store and shows it as
  it fills. For the store's digest to be computed as rows arrive, an image's
  digest is now BLAKE3 over a header and each plane's own digest, rather
  than over the planes in one stream. Measured informally on one machine
  (2026-11-15), the row decoder's peak memory is about the store's size,
  well below decoding whole and then building levels, and it is slower than
  that path.
- **M4 ✓ — A rendering source.** One in the gallery with magnified levels
  (`egui2_hl_imagerender_demo.go`) and a scene that zooms past full
  resolution, finds the overlay's marks by name, and checks that a recipe
  change renders nothing again
  ([`imageview/scenes`](../../public/thestack/imzero2/egui2/widgets/imageview/scenes/rendered-source.scene.md));
  the image engine how-to updated.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `widgets/imageview` | a source interface, `SetSource`, `Invalidate`, view and overlay calls, a `ViewChanged` event; `SetImage` kept | `widgets/conformance`, its tests, the gallery demo, `apps/imagescope` |
| Exported Go API: `imaging/decode` | a streaming decode into a tiled store beside `Decode` | the fuzz targets, with the streaming path held to the standard library |
| Exported Go API: `imaging/levels` | levels built band by band | its tests against `Reduce` |
| Exported Go API: portolan | a wheel-pans interaction; the box in progress in `Events` | portolan's handler tests |
| egui2 IDL, FFFI2 frame, renderers | none | — |

## Alternatives

- **Display pixels from a rendering source (O1b).** Rejected: two tile
  paths, two caches and a readout that means different things by source, to
  save one conversion per tile.
- **Decode on demand per tile (O3a).** Rejected for now: without an index a
  tile costs every row above it; with one — deflate checkpoints for PNG, a
  position per JPEG row — it is new ground no documented reader has taken,
  and Go's `flate` may not resume from a checkpoint. Kept as the way past
  memory (Deferred).
- **A JPEG row decoder in Go.** Rejected for this decision: baseline JPEG
  would stream in memory proportional to the width, but the decoder is the
  larger part of the work and progressive files still need the whole image.
- **A second viewer on portolan for rendered content.** Rejected: it would
  repeat imageview's pixel convention, upload budget and tile scheduling,
  which is what the downstream viewer's authors asked not to do.
- **Scaled JPEG decode for the reduced levels.** Rejected as a level: it is
  not a block mean and has no minimum or maximum. It is a candidate for a
  first preview (Deferred).

## Consequences

### Positive

- A renderer shows sharp at every magnification its levels reach, and pays
  for what is in view.
- A recipe change asks no source again.
- A non-interlaced PNG decodes in memory proportional to its width plus its
  tiles, and shows its first tiles before the last row is read.
- A document reader gets its overlay, its view and its wheel without
  reaching into portolan.

### Negative

- The tiled store holds what the image and its levels held, so an image
  larger than memory still does not open; the decoder change moves the peak,
  not the total.
- A PNG row decoder is code the standard library had; it is held to the
  standard library by fuzzing, and every difference found is a bug of ours.
- JPEG and interlaced PNG keep the whole-image peak.
- Numbers over a source that is not in memory cost a read of every tile.

### Neutral

- `SetImage` callers see no change.
- The sample cache bound is a constant, like the upload budget.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the in-memory source against today's tiles,
  pixel for pixel; the sample cache's bound and eviction; `Invalidate`
  keeping the old tiles until their replacements land; the view round-trip
  in pixels; the PNG row decoder against `image/png` on every colour type and
  bit depth, interlaced files refused to the whole path, and under the fuzz
  lane; band-built levels against `levels.Reduce`. A scene with a rendering
  source zoomed past level 0.
- **What would fail.** A tile from a source that differs from the tile the
  same samples gave through `SetImage`; a recipe change that calls the
  source; a PNG row that differs from the standard decoder's; a level built
  in bands that differs from one built whole.

## Deferred

- **Images larger than memory**: spilling the store to disk, or dropping
  level 0 and decoding it again on demand through an index recorded on the
  first pass. Trigger: the first image that does not fit.
- **A JPEG row decoder, and scaled JPEG decoding for a first preview.**
  Trigger: a JPEG whose whole decode is the problem.
- **Restart-marker random access.** Its prevalence is unmeasured.

## Decided in review

- The tile store may live in memory, provided procedural producers are
  served and memory can be bounded (SD9). Repository owner, 2026-11-15.
- The JPEG row decoder waits (Deferred). Repository owner, 2026-11-15.

## Open questions

1. Whether a rendering source should show its histogram when asked — of
   what, at which level.

## Status

Accepted 2026-11-15.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## Updates

### 2026-11-15 — keys reach the host through the map; a replaced source's tiles are dropped

§SD5's "keys stay off in the map and reach the host's frame" did not hold:
the canvas is drawn inside `Render`, so a host has no frame around it to
take Page Up or Page Down with. The map's key-capturing frame now takes the
keys the host names (`imageview.Options.Keys`, portolan's
`Options.HostKeys`) and reports their presses in `Events.Keys` without
acting on them; the map's own arrows stay off in imageview.

Adopting §SD1 in a PDF viewer met a defect at the seam: a tile under way
when the source was replaced could be answered by the old source and
stored for the new one. portolan's `Map.SetSource` now starts a content
revision, an answer requested under an older one is not shown and the tile
is asked again, and a call no request waits on has its context cancelled
and leaves the loader's in-flight index — which is what §SD1's "must honour
`ctx`" presupposed. imageview publishes a new source to its workers before
the map asks for its tiles.

## References

- [ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md) — the viewer this extends.
- [ADR-0204](./0204-leaflet-map-core-port.md) — portolan.
- [ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md) — the widget contract.
- [libvips: how it opens files](https://www.libvips.org/API/current/how-it-opens-files.html), [access modes](https://www.libvips.org/API/current/enum.Access.html).
- [Wuffs image decoders](https://raw.githubusercontent.com/google/wuffs/main/doc/std/image-decoders.md).
