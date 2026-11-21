---
type: how-to
audience: developer using boxer's image packages from another module
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Written against ADR-0373, ADR-0374
> and ADR-0398 (proposed). Their exported API is a Tier 1 surface: a change
> to it is recorded in their dated updates. Boxer is pre-1.0, so pin the
> version you build against.

# Reading, measuring and showing an image with boxer's image packages

This page is for a project that uses the image engine of
[ADR-0373](../adr/0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md)
and its extension in
[ADR-0398](../adr/0398-images-beyond-a-colour-model-more-sample-kinds-invalid-samples-stacks-a-placement-and-three-readers.md)
without the `imagescope` app: to read PNG, JPEG, TIFF, FITS and NPY files
into samples, to compute numbers from them, to map them to colours, or to
show them in an imzero2 window. Each package's doc comment has the API; this
page is which package to reach for and the contracts that cross them.

## Which package

| You want to… | Use | Needs imzero2 |
| --- | --- | --- |
| hold an image as planes of samples in their own kind, a stack of planes along named axes, and where the pixels are | [`imaging`](../../public/science/imaging/doc.go) | no |
| read a PNG or JPEG file into an image, or a TIFF, FITS, NPY or NPZ file into a stack | [`imaging/decode`](../../public/science/imaging/decode/doc.go) | no |
| read one of those three formats with its reader's own options — a FITS HDU, an NPZ member — or list what a file holds without its samples | [`decode/tiff`](../../public/science/imaging/decode/tiff/doc.go), [`decode/fits`](../../public/science/imaging/decode/fits/doc.go), [`decode/npy`](../../public/science/imaging/decode/npy/doc.go) | no |
| keep an image as 512-pixel tiles with every reduced level, and read a region's histogram and samples from them | [`imaging/tiles`](../../public/science/imaging/tiles/doc.go) | no |
| count samples, take percentiles, mean and standard deviation of a plane or a region | [`imaging/stats`](../../public/science/imaging/stats/doc.go) | no |
| reduce an image for showing it smaller — means, minima, maxima | [`imaging/levels`](../../public/science/imaging/levels/doc.go) | no |
| turn samples into 0xRRGGBBAA pixels under limits, a transfer function and a colour map; several planes as a composite of coloured layers; limits by zscale; the share of samples limits clip | [`imaging/display`](../../public/science/imaging/display/doc.go) | no |
| state a region — a point, a line, a rectangle, an ellipse, a polygon — in a frame, and measure or profile the pixels it covers | [`imaging/regions`](../../public/science/imaging/regions/doc.go) | no |
| run a declared operator over images into a new tiled image, with a record of what it did | [`imaging/ops`](../../public/science/imaging/ops/doc.go) | no |
| try any of the above without a file — `ByName` for an image of each kind, `StackByName("bands")` for a placed stack of three times and four bands | [`imaging/samples`](../../public/science/imaging/samples/doc.go) | no |
| show an image in a window, with pan, zoom, readout, histogram and a marked region — as stored, one plane, or a composite of layers | [`widgets/imageview`](../../public/thestack/imzero2/egui2/widgets/imageview/doc.go) | yes |

All but the last depend on boxer's error packages and BLAKE3 and nothing of
the GUI, so a headless service can use them alone.

## Bringing your own samples

A file is not the only way in. Make planes from slices you already hold and
assemble an image:

```go
p, err := imaging.NewPlaneF32(width, height, samples) // NaN marks an invalid sample
p, err = p.WithTransform(imaging.ValueTransform{Scale: 0.01, Offset: -40})
img, err := imaging.NewImage(imaging.ColorModelGray, []imaging.Plane{p}, imaging.Provenance{SourceName: "run 17"})
```

A plane is one of six kinds: unsigned 8- and 16-bit, signed 16- and
32-bit, 32- and 64-bit float, stored as given and never narrowed. A kind
with at most 2^16 codes (`SampleKindE.Codes`) has a histogram bin and a
mapping-table entry per code; signed 32-bit and the floats are binned and
windowed. A `switch` over `SampleKindE` written for the first three kinds
meets three it did not list.

Contracts that come with planes:

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
- **A plane may declare one stored value invalid**, with
  `Plane.WithInvalid` — a raster's nodata, FITS `BLANK` — as NaN is in a
  float plane. The kernels leave it out of every range, histogram,
  percentile and statistic and count it apart; a reduced cell is the mean of
  its valid samples. The planes of an image or a stack declare one value or
  none, and an alpha plane none. The declaration is not part of the digest.
- **Pixel (col, row) covers [col, col+1) × [row, row+1)**, row 0 at the top,
  samples row-major. A region is an `image.Rectangle` in those pixels.

## Stacks

Planes beyond a colour model — a dozen spectral bands, a time series, a
cube — are an `imaging.Stack`: planes of one size and kind along named
axes, each a *band* axis (positions shown one at a time or three as red,
green and blue; at most one per stack) or a *selector* axis (one position
shown), with an optional unit, a coordinate and a label per position. A
stack whose band axis is a colour model says so (`Stack.Model`).

An image is taken from a stack, sharing its planes:
`Stack.Image(at, bands)` with a position per axis and one or three band
positions, or `Stack.AsStored(at)` for the colour model's image. The image
carries the bands' labels (`Image.PlaneLabels`), so a readout names B8 and
not red, and the stack's placement. Its digest is the one the same planes
give when assembled directly, computed from digests the stack holds. A
stack is resident in memory.

## Where the pixels are

An `imaging.Placement` maps continuous pixel coordinates to a frame: a
coordinate reference system named by the file's code (`EPSG:2056`), whose
coordinates are given as that system defines them and never converted; a
gnomonic (TAN) projection of the sky, which `Placement.World` evaluates to
longitude and latitude in decimal degrees; or a physical scale.
`Placement.Pixel` is the inverse, refusing a sky position on the far
hemisphere. What a file declares and the engine does not evaluate — another
projection, a distortion — is listed in `Unapplied`, and where the affine
alone would misplace pixels the reader leaves the placement empty.
`Source` names the statement it came from. Images and stacks carry one; it
is not part of the digest.

## Decoding untrusted files

`decode.Sniff` tells the six formats from a file's first bytes, and
`FormatE.IsStack` which way each goes. `decode.Decode` reads a PNG or JPEG
from a byte slice and refuses an image over `Options.MaxPixels` — 64
megapixels unless set — from its header, before decoding it.

`decode.DecodeStack` reads a TIFF or BigTIFF, a FITS file (gzip-wrapped
too), an NPY array or an NPZ archive's only array from an `io.ReaderAt`
into a stack, through the reader of its format. `Options.MaxPixels`,
`MaxBytes` and `MaxPlanes` are the readers' own limits — a plane, the whole
stack's samples, its plane count — checked from the header before a sample
is read. What each reader reads is in its package's doc comment; what it
does not read it refuses by name rather than guessing: in TIFF, JPEG,
Zstandard and LERC compression and an orientation other than top-left; in
FITS, 64-bit integers, tile compression and random groups; in NPY, object
arrays and native byte order; and in each, a sample type outside the six
kinds — unsigned 32-bit, 64-bit integer, complex — where the format has one. A refusal keeps the reader's error, so `errors.Is`
matches the reader's `ErrUnsupported`, `ErrLimit` or `ErrMalformed` and the
message names the feature, the limit or the defect: show it as it stands. A
reader converts its format's conventions to the pixel convention above —
FITS rows flipped, a point-registered raster shifted half a pixel, a
Fortran-ordered array reordered — and records it in `Provenance`
(`Derivations`, and `Notes` for the rest). The chunk and marker scanners and the decode are fuzzed
against the standard library's reading of the same file
(`decode/fuzz_test.go`); run them with `scripts/ci/gofuzz.sh
./public/science/imaging/decode/` after changing the decoder. What the reader
changed on the way — a palette expanded, chroma converted, an animation cut
to its first frame, a lossy source — is in `Provenance`, and a project that
shows numbers should show those notes beside them, as the app does.

## A large PNG, into tiles

`decode.DecodeTiled` reads a PNG or JPEG into an `imaging/tiles` store:
level 0 cut into 512-pixel tiles, every reduced level built in the same
pass. A
non-interlaced PNG is decoded row by row, so the image is never held whole
besides the store; the store is handed to an `onStore` callback before the
first row, and `imageview.StoreSource(store)` shows it as it fills, a tile
not built yet being waited for off the frame. The store answers a region's
histogram (`Histogram`) and samples (`Read`) from its tiles, and its
provenance carries the image's digest once it is finished. An interlaced
PNG and a JPEG take the whole decode first. `tiles.FromImage` stores an
image in memory — an image taken from a stack among them — by copying its
planes. What a store says beside its samples is set through
`tiles.Options`: `Invalid` and `HasInvalid`, `PlaneLabels` and `Placement`,
which `FromImage` takes from the image when they are left unset.

## Mapping a plane yourself

`display.CompilePlane` and `display.CompileColor` take a kind and a
transform, for planes that declare no invalid value. For any other, compile
from the plane's whole description:
`display.CompilePlaneFor(recipe, display.PlaneSpecOf(p), palette)`, and
`display.CompileColorFor(recipe, spec)` for a colour image. The colour
mapping takes only the kinds with codes and refuses the others by name; a
composite of the colour planes shows such an image whole.

## Bands together

A `display.Composite` is a list of `display.Layer` — a recipe, a colour of
the fixed set (`display.AllLayerColors`) and whether it is shown — with one
plane per layer
([ADR-0401](../adr/0401-showing-bands-together-a-recipe-per-band-an-additive-composite-and-limits-by-zscale.md)).
`display.CompileComposite(layers, specs)` compiles it for one
`display.PlaneSpec` per layer, of any sample kind; `CompositeMapper.Map`
takes the planes in layer order, all of one size, and writes pixels with no
alpha — a hidden layer's plane is not read. `display.NewComposite` makes a
linked one in the default colours, and `Composite.IsLinked` says whether the
shown layers share a recipe; a view of an unlinked one should say so, since
its colours are not comparable with another view's. Three layers red, green
and blue under one recipe are the colour mapping of the same planes.

## Limits found from the data

A `display.Recipe` whose `Limits.Source` is `LimitsSourcePercentile` or
`LimitsSourceZScale` is a request: its `Lo` and `Hi` are what a mapper
compiles with, and something has to find them. Without the widget, count a
histogram with `stats.Of` and call `display.PercentileLimits`, or call
`display.ZScaleLimits` on the plane; a reader of a tiled image collects
`display.ZScaleGrid`'s points first, reads each tile they fall in once, and
calls `display.ZScaleLimitsAt` with the values. zscale's steps are written
out on `ZScaleLimitsAt`; it records the contrast and the sample count it
used. With the widget, pass the recipe to `Viewer.SetRecipe`, which finds
the limits — at once for an image in memory, off the frame otherwise — and
reports `LimitsPending` until they land.

`display.ClippedShares(histogram, limits)` is the share of the valid
samples below the lower limit and above the upper one: exact for a kind
with one bin per code, interpolated within a bin otherwise, and
`ClippedShare.Exact` says which.

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

To replace the image shown and keep the view — another plane of a stack, of
the same size — pass `imageview.ImageSource(img)` to
`SetSource(src, true)`; `SetImage` fits the view again. The recipe and the
marked region are the host's to put back: imagescope keeps the recipe while
the view stays a single plane or stays a colour image, and takes limits
that came from the data again from the new plane.

A composite is a third view beside `SetAsStored` and `SetPlane`:
`Viewer.SetComposite(layers, linked)` takes `imageview.CompositeLayer`s —
a plane of the source by index and a `display.Layer` — and maps them from
the samples the tiles carry, so a change of a layer asks a source nothing
it has cached. Layers are planes of one source; to composite bands of a
stack, take them as the planes of one image first, as imagescope does. The
layer in hand (`SetLayerInHand`) is what `SetRecipe`, the histogram strip
and its handles act on; linked (`SetCompositeLinked`), a recipe set reaches
every layer. Unlinked with two layers shown, the viewer draws
`imageview.CompositeUnlinkedText` beside the image, and
`CompositeStatement` reports it to the host. `ClippedShares` gives what the
limits in force clip, of the single plane or the layer in hand, once its
histogram is counted; `imageview.ClippedText` writes it as the strip does.

For an image with a placement, the readout line gives the pointer's frame
coordinates beside its column and row, unless `Options.HideFrameCoordinates`
is set; `Events.HoverFrame` and `Viewer.HoverFrame` carry them as an
`imageview.FramePosition`. `imageview.FramePositionAt` gives the same text
for any point, and `imageview.DescribePlacement` a line naming the frame,
the pixel size and the source.

## Showing something that is not an image in memory

A renderer — a document page, a plot, anything that can draw a rectangle at
a scale — is an `imageview.SourceI`: it describes its size, model, kind and
the levels it answers, below and above full resolution, and returns the
samples of one tile at a level when asked, on the viewer's workers, honouring
the context. Its `SourceInfo` also carries what the samples cannot say: the
invalid value its colour planes declare (`Invalid`, `HasInvalid`), the
planes' labels and the placement. `Viewer.SetSource(src, keepView)` shows it; keep the view when
the next page replaces this one. Mark it `Rendered`, return 8-bit RGB, and
the default recipe shows it as drawn. The viewer keeps its tiles in a cache
bounded by `Options.SampleCacheBytes`, so a contrast change asks the
renderer nothing; call `Invalidate` when what it draws changes. For a
document, `Options.WheelScrolls` makes the wheel scroll; `View`, `SetView`
and `Reveal` move the view in image pixels, and `Options.Overlay` paints
marks and labelled hit regions over the image through a `Projector`. The bound is
the memory a procedural source costs the viewer
([ADR-0374](../adr/0374-imageview-shows-a-tile-source-and-the-decoder-streams-into-one.md)
§SD9). The histogram and percentile limits need every sample: they are
offered for a source that counts a histogram itself
(`imageview.HistogramSourceI`), as `imageview.StoreSource` does over a
tiles store, and not for one that only draws tiles.

## Keeping up

The packages are pre-1.0 like the rest of boxer. On a bump, the compiler
finds a renamed or removed symbol; a contract above that changed is recorded
in a dated entry under `## Updates` of ADR-0373, ADR-0374, ADR-0398 or
ADR-0401.
