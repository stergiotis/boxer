---
type: reference
audience: end-user
status: draft
title: Operating image scope as an agent
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operating image scope as an agent

Image scope holds no model grant of its own. An agent reaches a window
through its operations catalog under a task grant the person approved
(ADR-0269, ADR-0318); the person sees every change in the window and in the
command log, and the same handlers run when they click.
`keelson('operations')` lists the operations with their arguments; this page
says how to use them together and what each refuses.

No operation opens a file. The file picker is the person's: an agent that
needs a particular image asks the person to open it with **Open…**, then
reads `get_view`. What opens is what the host registered: `list_formats`
lists the formats — name, title, the endings their files carry, the
reader's version and what it reads and refuses — and a file is recognised
by its first bytes. A file over the file broker's read limit — 64 MiB
unless the host raises it — is refused before the window sees it. A file a
reader refuses ends in `get_view`'s `Error` with the reader's own sentence,
which names the feature, the limit or the defect, and `ErrorKind` says
which refusal it was: `unknown-format` (no reader claims the file; the
sentence names the formats), `ambiguous` (two do), `unsupported`, `limit`,
`malformed`.

For scenes and headless runs, `BOXER_IMAGESCOPE_SAMPLE` opens a new window on
a sample (`field`, `colour`, `small`, `float`, `scaled`, `elevation`, `double`,
`counts`, or the stack `bands`) and `BOXER_IMAGESCOPE_REGION` marks a
region on it: column, row, width, height. A value that names no sample or
does not parse leaves the window as it would otherwise be and says so in the
status row.

## The resources

- `image` — the image open: name, size, colour model, sample kind, plane
  names, the stored value its planes declare invalid if any
  (`HasInvalid`, `InvalidValue`), where its samples came from, including
  whether the source was lossy, and its `Placement` when the file places
  its pixels. Written by `open_sample`, and by the person's **Open…**.
- `view` — the plane shown or the composite, the display recipe, the
  marked region and, for a stack, the selection asked for. A composite is
  there whole, as `set_composite` takes it, linked or not, so restoring the
  resource shows it again. Written by `set_plane`, `set_recipe`,
  `set_region`, `set_position`, `set_bands`, `set_entry`, `set_composite`
  and `set_layer`.
- `regions` — the window's regions, as the regions document, and the one
  selected. Written by `add_region`, `update_region`, `remove_region`,
  `select_region` and `set_regions_document`.
- `images` — the window's images: the one opened and the results of
  operations, each with an id, and the one shown. Written by
  `run_operation`, `cancel_operation`, `show_image` and `drop_image`.

Nothing of the three outlives the window. Regions, results and their
records are held in memory while it is open and are gone when it closes;
the regions document and a record are kept only by saving them.

## The phases

`image_state` gives the window's phase and the edges open from it;
`image_machine` the whole graph.

- *empty* — no image. Every operation but `open_sample` and `get_view` is
  refused, naming those two ways on.
- *opening* — a file is being chosen, read or decoded, or a sample made.
- *viewing* — an image is shown and no region is measured.
- *selecting* — a position or bands of the stack open, or another entry
  of the file, were set and that selection is being taken; the image shown
  stays until it lands.
- *measuring* — a region is marked and its statistics are being computed.
- *measured* — the marked region's statistics are computed.
- *measuring-regions* — regions are being measured, by `measure_region`,
  `measure_regions` or the person's **Measure**.
- *profiling* — a line's profile is being taken for the window's chart.
- *running* — an operation is being run; its result is shown as it fills.

The last three are the window's work on the image shown, each a task with
a row of its own; they rank below an open and a selection, and above the
marked rectangle's measurement, which goes on beside them.

An open that fails or is cancelled returns to the phase it interrupted;
`get_view`'s `Error` or `Note` says which. A selection that fails returns
the same way, with the reason in `SelectionError`.

## Reading

- `get_view` — the phase, the `image` and `view` resources, the
  magnification, the level drawn (`0` is the samples; `k` means each drawn
  cell is the mean of a 2^k × 2^k block) and what the last open, measurement
  and handover came to. For a stack, `StackSelection` is true and `Stack`
  lists its axes — name, role, size, unit, and at most 32 labels and
  coordinates each, `Truncated` when there are more — the selection shown,
  the one asked for, and `Pending` while they differ. `SelectionNote` says
  what the last selection change kept of the view. For an image read from
  a file, `File` names it, its format with the reader's version — and the
  `Variant` read, in the format's own word, where one format covers
  several — the entry shown and how many the file holds. `Mode` says how the
  image is shown: `as stored`, `single plane` or `composite`; `Composite`
  lists a composite's layers — label, band or plane, colour, whether
  hidden, and the recipe with the limits in force — whether it is `Linked`,
  the layer `InHand`, and the `Statement` the window draws beside the image
  while it is not linked. `Clipped` is the share of the valid samples the
  limits in force clip, below and above, of the single plane shown or the
  layer in hand, with `Exact` false when it is read off the histogram's
  bins rather than counted; it is absent until the histogram is counted.
- `get_axis` — the labels and coordinates of one axis from a position, at
  most 256: the rest of an axis `get_view` cut short.
- `list_formats` — the formats this host reads.
- `get_entries` — the entries of the file open, from `From`, at most
  `Count` (256; 0 lists 64), `Truncated` when there are more: each with its
  `Id`, title, size, planes and kind as the header says them, whether the
  reader reads it or, when not, the `Refusal` — `unsupported` for what the
  reader does not read, `limit` for a bound of its own, `malformed` for an
  entry that does not follow its format — and the `Reason`, the `Default`
  and the one `Shown`. Refused when no file is open.
- `read_pixels` — a block of samples, every plane, as values in row-major
  order, through the plane's value transform; an invalid sample — NaN, or
  the declared value — is 0 and counted in `Invalid`. At most 1024 pixels: a larger block is cut to whole rows that fit
  and `Truncated` is set. Both sizes zero reads one pixel. The values come
  from the samples, whatever the recipe shows.
- `measure` — statistics of every plane over a region, with the method
  stated in `Method`. The zero region means the marked one, and is refused
  when none is marked. A region that reaches outside the image is refused
  naming the image's size. The numbers are computed from the samples at full
  resolution; see **What the numbers are** for what the standard deviation
  and the percentiles are.
- `to_world` — the frame coordinates of a point given in continuous pixel
  coordinates (`Col`, `Row`), with the frame's axis names, units and code,
  and the text the readout writes. On the sky they are decimal degrees of
  longitude and latitude.
- `to_pixel` — the other way: the continuous pixel coordinates of a frame
  point (`A`, `B`), the pixel it falls in and whether that pixel is the
  image's. A sky position on the hemisphere opposite the reference point
  is refused: the gnomonic projection does not show it.

Both are refused, by name, for an image without a placement.

For a stack, `read_pixels` and `measure` name in `Selection` the selection
their numbers are of — the file's `Entry` among it: the one shown, which is
the one asked for unless a change is still pending. Read it before quoting a
number.

Pixel (column, row) covers the square from (column, row) to (column + 1,
row + 1); row 0 is the top. A region is `Col`, `Row`, `Width`, `Height`.
In continuous coordinates the centre of pixel (3, 4) is (3.5, 4.5).

## Changing the view

Every command sets a value; none toggles, so a repeat leaves the same view.

- `open_sample` — open `field`, `colour`, `small`, `float`, `scaled`,
  `elevation`, `double`, `counts` or the stack `bands` in place of what is
  shown. It settles when the image is shown. The view returns to the image
  as stored under its default recipe and the region is cleared.
- `set_position` — `Axis`, a selector axis's name, and `Position` on it.
  Refused, naming the remedy, when no stack is open, for the band axis
  (that is `set_bands`) and for a position outside the axis.
- `set_bands` — `Bands`: one index, shown as a single plane through a
  colour map; three, shown as red, green and blue under one recipe; or none,
  for the image as stored when the stack's bands are a colour model. Three
  bands of a stack whose bands are no colour model open over the range of
  their data together, linear; `SelectionNote` says so. Three bands of
  signed 32-bit or float samples, which the colour mapping does not take,
  open as the linked composite of the three in red, green and blue over the
  range of their data — the same picture — and the outcome's `Note` says
  so.
- `set_entry` — `Id`, an entry of the file open from `get_entries`: it is
  read from the bytes the window holds and replaces the image opened as an
  open does — its default selection, fitted, under the default recipe; the
  regions and the results stay. It settles when the entry is shown, the
  outcome's `Selection.Entry` naming it. An entry the reader does not read
  is refused with its reason, an id the file lacks naming the ones it has;
  an entry the reader refuses while reading it leaves the one shown, and
  the outcome's `Error` says why, `get_view`'s `SelectionErrorKind` which
  refusal it was. While it is read, `set_position`, `set_bands` and
  `set_composite` are refused.
- `set_position` and `set_bands` settle when the selection is shown; the
  outcome names it, and `Note` says what was kept. The view, the marked
  region and, while the view stays of one kind, the plane and the recipe are
  kept; limits taken from the data are taken again from the new plane. From
  one band to three, or back, the view opens under that kind's default
  recipe.
- `set_plane` — `as stored`, or a plane name from `get_view`: a band's
  label for an image taken from a stack, otherwise a colour. Two planes of
  one band share a name; `ByIndex` with `Index` names one of them. The
  recipe returns to that view's default, keeping the colour map, so call
  `set_recipe` after it, not before.
- `set_recipe` — the whole recipe. Read `get_view`, change fields of
  `View.Recipe` and send it back. `Limits` is `data`, `nominal`,
  `percentile` (with `PercentLo` and `PercentHi`), `zscale` (with
  `ZScaleContrast` and `ZScaleSamples`, 0 for 0.25 and 1000) or `manual`
  (with `Lo` and `Hi`); percentile and zscale apply to a single plane or a
  composite's layer. Percentile limits need the plane's histogram, which is
  counted off the frame, and zscale limits a sample of about a thousand of
  its values, read off the frame for an image that is a store of tiles:
  until they land, `get_view`'s `LimitsPending` is true and the limits in
  force stay. The contrast and the sample count used are in the recipe
  reported. Percentiles that fall on one value, and a zscale sample whose
  values are one value, leave the limits in force.
  Of a composite the recipe is the layer in hand's — every layer's when the
  composite is linked: its data limits are then the range of the shown
  layers' planes together, and percentile and zscale limits are found from
  the layer in hand's plane. A layer takes no colour map and no clipping
  marks; `FlagClipped` is refused for one.
  A name that is no transfer function or colour map is refused naming the
  ones that are.
- `set_composite` — show the image as a composite (see **What the numbers
  are**): `Layers`, one to eight, each a `Band` of the stack — or a `Plane`
  of an image that is no stack's — a `Colour` (red, green, blue, cyan,
  magenta, yellow, white) and `Hidden`; `Linked`; and the layer `InHand`.
  Linked, the composite's one recipe is `Recipe`, absent for the range of
  the data of the shown layers' planes together, and a layer's own `Recipe`
  is refused. Unlinked, each layer's `Recipe` is its own, absent for the
  range of its plane's data, and `Recipe` is refused. For a stack the
  selection holding the layers' bands is taken first, as an image of their
  bands: at most four different bands, three when the stack declares an
  invalid value — the fourth plane of an image is alpha, which declares
  none. It settles when the composite is shown; `Error` says why it could
  not be, and what was shown stays. `set_position` afterwards keeps the
  layers, their colours and recipes, and takes limits found from the data
  again from the new samples.
- `set_layer` — one layer, by `Index`, given whole as the view resource
  has it: `get_view`'s `View.Composite.Layers`, which leaves a linked
  composite's recipes out (the top-level `Composite.Layers` reports the
  same layers with their labels and the limits in force). `InHand` also
  makes it the layer in hand. Its `Recipe` is refused while the composite
  is linked: `set_recipe` sets every layer's then. Refused when no
  composite is shown; it settles as `set_composite` does.
- `set_region` — mark a region; the zero region clears the mark. The window
  then computes its statistics, and `get_view`'s `Measured` says when
  `measure` returns them without recomputing.
- `set_zoom` — `fit` or `actual`.

## Regions

A region is a shape by its parameters — `point`, `line`, `rectangle`,
`ellipse` or `polygon` — in one stated frame, with an anchor, a label, a
note and an origin. The frame is `pixel` (continuous pixel coordinates) or
the frame of the image's placement, named as `get_region` reports it;
`image` in a `RegionSpec` means the frame of the image shown. The anchor
names positions of the stack opened a region belongs to; empty is every
position, and `AnchorShown` takes the position shown. Angles are degrees;
a positive angle turns clockwise on screen in pixel coordinates.

The rectangle marked with `set_region` is not a region. It stays the
transient selection `measure` and `send_to_play` read; the person's **Keep
as region** makes a region of it.

- `list_regions` — the regions, at most 64 at a time from `From`,
  `Truncated` when more follow, with the tool in the person's hand. Each says
  whether it `Applies` to the image shown and whether it is `Drawn` — it
  applies and reached the image's pixels — and otherwise the `Reason`:
  another frame, another stack position, a stack it was not made for. Its
  `Figures` are *none*, *current* or *stale*.
- `get_region` — one region, with its `Spec`: what `update_region` takes back.
- `add_region` — a `RegionSpec`; returns the new id and selects it. A window
  holds at most 256 regions, and a polygon made here at most 4096 vertices.
- `update_region` — `Id` and the whole `Region` spec: read `get_region`,
  change what should change, send it back. The numbers are taken in the
  frame the spec names; nothing is converted.
- `remove_region` and `select_region` — by `Id`; an empty id selects none.
- `measure_region` — `Id`, and `Area` for the figures by covered area. It
  runs as a task and settles with the figures: per plane the valid and
  invalid counts, range, mean, standard deviation and the 1st, 50th and 99th
  percentiles, the `Method` identifier, the coverage `Rule` of the figures,
  the `Mapping` that brought the region to the pixels, the `Image` and, for a
  stack, the `Selection` measured. A region not drawn on the image shown is
  refused with the reason. Figures go *stale* when the region, the image
  shown or its selection changes.
- `measure_regions` — every region drawn on the image shown, in one task,
  as **Measure all** does; `Area` as for `measure_region`. It settles with
  a line per region, at most 32 — per plane the valid and invalid counts,
  the range and the mean — and how many regions it left out as not drawn
  (`NotDrawn`). Each region's figures, whole, are then `measure_region`'s.
  Refused when no region is drawn on the image shown.
- `profile_region` — a line's pixels in order, at most 1024 from `From`,
  each with its column, row, the length of the line inside it, the distance
  to the middle of that length and every plane's value.
- `get_regions_document` and `set_regions_document` — the regions document,
  JSON of version 1, at most 1 MiB. Setting replaces every region and drops
  the figures and the profile.

`get_view` gives the number of `Regions`, the `SelectedRegion`, the `Tool`
in the person's hand, and whether `RegionMeasuring` or `Profiling` is under
way.

A region's `Origin` is set by the window, never by the caller: *person* for
the person's gestures, the caller's task (`task:<id>`) for an agent's call,
*agent* for a call that names none, and *document* for a region the person
loaded from a document that recorded none. A document an agent sets takes
the agent's origin for every region in it. A region another than the person
made is drawn dashed, its label starting with ◇.

`send_regions_to_play` measures every region drawn on the image shown and
publishes the figures as the dataset `regions`, one row per region and
plane; `send_profile_to_play` publishes a line's profile as the dataset
`profile`, one row per pixel. Both write outside the app and are confirmed
by the person; their outcomes are in `get_view`'s `RegionsHandover` and
`ProfileHandover`.

## Operations on images

An operation is an operator applied once, at full resolution, to images of
the window; its result is a new image of the window with the record of what
it did. `list_operators` gives every operator as it is declared — id,
version, summary, the inputs it takes, each parameter with its type, unit,
range, choices, default and whether it is required — and the functions an
expression may call: enough to call `run_operation` without this page.

- `plan_operation` — the same arguments as `run_operation`; states the
  result's size, kind, planes, neighbourhood, border and first passes and
  what it would take in memory, or why it cannot run. Nothing is read.
- `run_operation` — `Operator`, `Params` (each a `Name` and its `Value` as
  text), `Inputs` and an optional `Name`. An input names an `Image` by id
  (empty is the image shown), and for the stack opened a `Band` by its label
  and `Positions` on its selector axes, an axis not named taking the
  position shown; `Over` makes one entry stand for every position of an axis,
  named `<Name>_<position>`, as `axis-stat` over time wants. For an image
  that is no stack's, `Band` names one of its planes. It runs as a task, one
  at a time per window, and settles with the result's id, its description
  and a summary of its record.
- `cancel_operation` — stops the run under way; a cancelled or failed run
  leaves nothing, and what was shown before is shown again.
- `list_images` — the images by id: `opened`, and `r1`, `r2`, … for results
  in the order they were made, never reused. Results may hold 1 GiB with
  their levels; a run past it is refused, and `drop_image` frees one.
- `show_image` and `drop_image` — by `Id`. An image of the same size keeps
  the view and the marked region. The image opened is not dropped.
- `get_record` — a result's record, the versioned JSON document, at most
  256 KiB at a time from the byte `From`. A result made from results
  carries their records, so the chain back to the files is in it.

Setting a position or the bands of the stack while a result is shown shows
the stack's new selection in its place. `get_view` names the image shown in
`ShownImage` and says whether a run is under way in `Running`.

## Sending the pixels to play

`send_to_play` publishes the stored samples of the marked region, or of the
whole image when none is marked, as the dataset `pixels` — columns `col`,
`row` and one per plane, named by the plane and holding the stored sample
in its own kind, invalid ones included — and opens the playground on it
with a query that leaves the invalid samples out. It writes outside
the app, so the host asks the person to confirm it every time. More than
about four million pixels are refused; mark a smaller region. The outcome is
in `get_view`'s `Handover`.
