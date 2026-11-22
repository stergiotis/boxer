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
[ADR-0398](../adr/0398-images-beyond-a-colour-model-more-sample-kinds-invalid-samples-stacks-a-placement-and-a-seam-for-readers.md)
without the `imagescope` app: to read PNG and JPEG files, and the formats
you register a reader for, into samples, to compute numbers from them, to map them to colours, or to
show them in an imzero2 window. Each package's doc comment has the API; this
page is which package to reach for and the contracts that cross them.

## Which package

| You want to… | Use | Needs imzero2 |
| --- | --- | --- |
| hold an image as planes of samples in their own kind, a stack of planes along named axes, and where the pixels are | [`imaging`](../../public/science/imaging/doc.go) | no |
| read a PNG or JPEG file into an image; register a reader of another format and read any registered format into a stack, entry by entry | [`imaging/decode`](../../public/science/imaging/decode/doc.go) | no |
| check a reader against the seam's contract, or give a test a file of several entries without a real format | [`decode/decodetest`](../../public/science/imaging/decode/decodetest/doc.go) | no |
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

`decode.Sniff` tells PNG and JPEG from a file's first bytes, and
`decode.Decode` reads one from a byte slice, refusing an image over
`Options.MaxPixels` — 64 megapixels unless set — from its header, before
decoding it. The chunk and marker scanners and the decode are fuzzed
against the standard library's reading of the same file
(`decode/fuzz_test.go`); run them with `scripts/ci/gofuzz.sh
./public/science/imaging/decode/` after changing the decoder. What the
decoder changed on the way — a palette expanded, chroma converted, an
animation cut to its first frame, a lossy source — is in `Provenance`, and
a project that shows numbers should show those notes beside them, as the
app does.

## Registering a reader

Every other format comes through a reader you supply: a value implementing
`decode.ReaderI` that describes its format (`Format`: a name, a title, the
endings its files carry, a version of its own reading and a doc line),
claims its files from their first `decode.SniffLen` bytes, lists what a
file holds as entries — a directory, a header unit, an array, each with an
id, its shape and kind — and decodes an entry into a stack under
`decode.Limits`. It reads from an `io.ReaderAt`, delivers row 0 at the top
and records each convention it converted in `Provenance`, checks the limits
from the header before reading samples (`Limits.Check`), and refuses by
name with `decode.Unsupported`, `OverLimit`, `Malformed` or `NoEntry`
(`decode.Refuse` for a kind held as a value); the contract is in the
package doc of [`imaging/decode`](../../public/science/imaging/decode/doc.go).
PNG and JPEG are readers too (`decode.BuiltinReaders`).

An entry the reader does not read is listed with the refusal a decode of it
would be: `entry.Refused(err)` sets `Readable` false, `Refusal` to the kind
— unsupported, limit or malformed — and `Reason` to the refusal's bare
words, without the kind's closing sentence (`decode.ReasonOf` gives them
back from any refusal). Exactly one entry of a file is the `Default`: the
one a plain open reads, or, in a file with nothing readable — a file of
tables, an archive of other members — the one it would have read. Decode
finds its entry with `decode.SelectEntry`, which refuses an unreadable one,
the default among them, with its own kind and its title, so a reader never
picks an entry or a refusal for itself.

Read the file through `decode.ReadFullAt`, so that a file that ends early is
malformed and a read that fails for any other reason reaches the caller as
it is, no refusal: a failing disk does not make a file malformed. Read a
stream — a section of the file, a decompressor over it — from
`decode.MarkReadFailures` of the file and pass what it returns to
`decode.ReadError`: the mark survives the decompressor, so a failed read
stays no refusal while a corrupt block, a checksum or the stream's early
end is malformed, without your knowing the decompressor's errors. Where one
format name covers several variants — wider offsets, an archive of arrays,
a compressed wrapping — set `Provenance.Variant` to the variant's word and
keep `Provenance.Format` the reader's name.

Register readers with `decode.Register` where your host wires its apps,
before any window opens a file — not from an `init` of the reader's
package, so that the formats a deployment parses are listed in one place.
`decode.Default()` is the registry that holds them; `Registry.Decode`
hands a file to the one reader that claims it and refuses a file none
claims (`ErrUnknownFormat`, its sentence naming the formats registered) or
two claim (`ErrAmbiguous`). An app shows a refusal's sentence as it stands
and tells the kinds apart with `decode.RefusalOf`. A test builds its own
registry with `decode.NewRegistry`.

Run [`decodetest.Check`](../../public/science/imaging/decode/decodetest/doc.go)
from your reader's tests over a few valid files whose samples are larger
than their headers, and a file with no readable entry where your format has
one (`Expect.Refusal` states the kind it is listed with). It checks the
contract — sniffing, entries and their refusals, entries that decode to
what they announce, each limit refused before samples are read, cancel,
refusals that wrap the seam's sentinels, every truncation survived, a
failing read kept apart from a malformed file — and not that the format was
read correctly, which only your fixtures, held against a reference reader,
can show. Bump `Format.Version` whenever the same bytes would decode to
something else.

Three things the kit cannot judge for you. "Before samples are read" is
measured against what `Entries` reads: a reader of a compressed container
inflates a block of input to reach a member's header, so it sets
`File.HeaderSlack` to its decompressor's input buffer and the member's own
header; a reader that decompresses a whole file to list it reads everything
in `Entries`, so the bound proves nothing of it, and it owes a test of its
own that a decode over a limit stops by what it decompresses. And the three
`Limits` are the caller's: a bound of your own — on a chunk or a tile held
at once, on what listing inflates — is refused with `OverLimit` naming it as
the reader's, and must not refuse a stack the caller's limits admit because
one chunk is larger than a plane. Derive it from the request rather than
fix it: no lower than `MaxPixels` and `MaxBytes`, and no lower than a floor
that holds the largest chunk the format's writers put around a small image,
so that the kit's exact limits still decode.

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
