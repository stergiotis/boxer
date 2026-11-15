---
type: how-to
audience: developer using boxer's image packages from another module
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Written against ADR-0373 and
> ADR-0374. Their exported API is a Tier 1 surface: a change to it is
> recorded in their dated updates. Boxer is pre-1.0, so pin the version you
> build against.

# Reading, measuring and showing an image with boxer's image packages

This page is for a project that uses the image engine of
[ADR-0373](../adr/0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md)
without the `imagescope` app: to read PNG and JPEG files into samples, to
compute numbers from them, to map them to colours, or to show them in an
imzero2 window. Each package's doc comment has the API; this page is which
package to reach for and the contracts that cross them.

## Which package

| You want to… | Use | Needs imzero2 |
| --- | --- | --- |
| hold an image as planes of samples at their own bit depth | [`imaging`](../../public/science/imaging/doc.go) | no |
| read a PNG or JPEG file into one | [`imaging/decode`](../../public/science/imaging/decode/doc.go) | no |
| count samples, take percentiles, mean and standard deviation of a plane or a region | [`imaging/stats`](../../public/science/imaging/stats/doc.go) | no |
| reduce an image for showing it smaller — means, minima, maxima | [`imaging/levels`](../../public/science/imaging/levels/doc.go) | no |
| turn samples into 0xRRGGBBAA pixels under limits, a transfer function and a colour map | [`imaging/display`](../../public/science/imaging/display/doc.go) | no |
| try any of the above without a file | [`imaging/samples`](../../public/science/imaging/samples/doc.go) | no |
| show an image in a window, with pan, zoom, readout, histogram and a marked region | [`widgets/imageview`](../../public/thestack/imzero2/egui2/widgets/imageview/doc.go) | yes |

The first six depend on boxer's error packages and BLAKE3 and nothing of the
GUI, so a headless service can use them alone.

## Bringing your own samples

A file is not the only way in. Make planes from slices you already hold and
assemble an image:

```go
p, err := imaging.NewPlaneF32(width, height, samples) // NaN marks an invalid sample
p, err = p.WithTransform(imaging.ValueTransform{Scale: 0.01, Offset: -40})
img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "run 17"})
```

Three contracts come with that:

- **The planes own the samples.** `NewPlane*` keeps the slice it is given and
  the accessors return it. Write to it afterwards and the image's digest, any
  levels built from it and any histogram counted from it are silently wrong.
  Copy first if the slice is still in use.
- **Stored samples and values are different numbers.** A plane stores
  samples; a value is a sample through the plane's `ValueTransform`. Limits,
  statistics, the histogram's `Range`, `Mean` and `Percentile` and the
  widget's colour bar are in values; a histogram's bins and `StoredRange`
  are in stored samples. A file decoded from PNG or JPEG has the identity
  transform, so the two coincide there and nowhere else.
- **Pixel (col, row) covers [col, col+1) × [row, row+1)**, row 0 at the top,
  samples row-major. A region is an `image.Rectangle` in those pixels.

## Decoding untrusted files

`decode.Decode` reads the whole file from a byte slice and refuses an image
over `Options.MaxPixels` — 64 megapixels unless set — from its header,
before decoding it. The chunk and marker scanners and the decode are fuzzed
against the standard library's reading of the same file
(`decode/fuzz_test.go`); run them with `scripts/ci/gofuzz.sh
./public/science/imaging/decode/` after changing the decoder. What the reader
changed on the way — a palette expanded, chroma converted, an animation cut
to its first frame, a lossy source — is in `Provenance`, and a project that
shows numbers should show those notes beside them, as the app does.

## A large PNG, into tiles

`decode.DecodeTiled` reads a file into an `imaging/tiles` store: level 0
cut into 512-pixel tiles, every reduced level built in the same pass. A
non-interlaced PNG is decoded row by row, so the image is never held whole
besides the store; the store is handed to an `onStore` callback before the
first row, and `imageview.StoreSource(store)` shows it as it fills, a tile
not built yet being waited for off the frame. The store answers a region's
histogram (`Histogram`) and samples (`Read`) from its tiles, and its
provenance carries the image's digest once it is finished. An interlaced
PNG and a JPEG take the whole decode first.

## Percentile limits

A `display.Recipe` whose `Limits.Source` is `LimitsSourcePercentile` is a
request: its `Lo` and `Hi` are what a mapper compiles with, and something has
to find them from a histogram. Without the widget, count one with
`stats.Of` and call `display.PercentileLimits`. With it, pass the recipe to
`Viewer.SetRecipe`, which resolves it when the histogram is counted and
reports `LimitsPending` until then.

## In a window

`imageview.Viewer` is a semi-retained widget
([ADR-0267](../adr/0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md)):
make one per view with `New`, give it an image with `SetImage`, call
`Render` or `RenderFill` every frame, and `Close` it when the view goes. Its
level build and histogram counts are background jobs: pass the app's task
API in `Options.Tasks` (`task.ForApp(ctx)` in `Mount`, before `New`) and
each runs as a task of the window, which the host lists, can cancel and
drains on close; draw `LevelJob()` and `HistogramJob()` with
`widgets/bgjobrow` for the standard progress row. Without a task API they
run untracked. `Close` cancels them without waiting. The tile workers are
the map's own and stop with `Close`. The gallery demo
`egui2_hl_imageview_demo.go` and [`apps/imagescope`](../../apps/imagescope/app_register.go)
are the two callers to copy from.

## Showing something that is not an image in memory

A renderer — a document page, a plot, anything that can draw a rectangle at
a scale — is an `imageview.SourceI`: it describes its size, model, kind and
the levels it answers, below and above full resolution, and returns the
samples of one tile at a level when asked, on the viewer's workers, honouring
the context. `Viewer.SetSource(src, keepView)` shows it; keep the view when
the next page replaces this one. Mark it `Rendered`, return 8-bit RGB, and
the default recipe shows it as drawn. The viewer keeps its tiles in a cache
bounded by `Options.SampleCacheBytes`, so a contrast change asks the
renderer nothing; call `Invalidate` when what it draws changes. For a
document, `Options.WheelScrolls` makes the wheel scroll; `View`, `SetView`
and `Reveal` move the view in image pixels, and `Options.Overlay` paints
marks and labelled hit regions over the image through a `Projector`. The bound is
the memory a procedural source costs the viewer
([ADR-0374](../adr/0374-imageview-shows-a-tile-source-and-the-decoder-streams-into-one.md)
§SD9). The histogram and percentile limits are not offered for a source.

## Keeping up

The packages are pre-1.0 like the rest of boxer. On a bump, the compiler
finds a renamed or removed symbol; a contract above that changed is recorded
in a dated entry under `## Updates` of ADR-0373 or ADR-0374.
