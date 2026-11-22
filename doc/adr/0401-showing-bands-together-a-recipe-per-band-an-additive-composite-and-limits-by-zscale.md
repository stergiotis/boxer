---
type: adr
status: proposed
date: 2026-11-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0401: Showing bands together — a recipe per band, an additive composite, and limits by zscale

## Context

[ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md)
§SD4 gives a colour image one set of limits and one transfer function for
all its colour planes, "stretching channels separately changes the colours,
so it is not offered as a display setting", and defers "band selection,
false colour, a stretch per band". With
[ADR-0398](./0398-images-beyond-a-colour-model-more-sample-kinds-invalid-samples-stacks-a-placement-and-a-seam-for-readers.md)
three bands of a stack can be taken as a colour image, which is false colour
under that one shared recipe. Three things remain out of reach.

- **Bands that do not share a range.** Near-infrared reflectance of
  vegetation is several times the red; two narrowband filters differ by
  orders of magnitude; fluorescence channels are separate measurements with
  nothing in common but the grid. Under one pair of limits all but the
  brightest band are black.
- **Bands that are not 8- or 16-bit integers.** A colour image of a float or
  32-bit kind is not mapped at all (ADR-0398, M2): the mapper for colour
  images is a table indexed by the sample.
- **Colours other than red, green and blue.** Two channels are read most
  easily, and by most people, as green and magenta; a fourth channel has no
  primary left.

What other tools record about this:

- A stretch per channel is the documented way to change a colour balance
  without saying so. The convention where it is offered is one shared
  stretch unless the person asks otherwise, with the choice visible
  ([astropy, RGB images](https://docs.astropy.org/en/stable/visualization/rgb.html)).
- Viewers of multichannel microscopy converge on one record per channel —
  visible, window, colour — summed
  ([OME-NGFF, omero metadata](https://ngff.openmicroscopy.org/latest/#omero-md)).
- For limits, astronomy's default is zscale, which finds a range around the
  sky background from a sample of the pixels and is robust to a few bright
  sources where percentiles are not
  ([astropy `ZScaleInterval`](https://docs.astropy.org/en/stable/api/astropy.visualization.ZScaleInterval.html)).

The mapping runs in Go and ships tiles (ADR-0373, Decision), so what is
decided here is a kernel and a view mode, not a renderer.

## Decision

We will add a third way of showing an image, beside the image as stored and
a single plane: a **composite** of layers. A layer is one plane, a recipe of
its own, and a colour; the colours of the layers are added. A composite
starts with one recipe shared by its layers and says on screen when it does
not. Limits can also be found by zscale, and the view states the share of
samples its limits clip.

### Subsidiary design decisions

- **SD1 — The composite.** A composite is an ordered list of at most eight
  layers. A layer names one plane of the source, carries limits and a
  transfer function, a colour, and whether it is shown. A pixel is the sum,
  over the shown layers, of the layer's level between its limits times its
  colour, cut at full scale. Three layers coloured red, green and blue under
  one recipe give, pixel for pixel, what ADR-0373 §SD4 gives for the same
  planes as a colour image.
- **SD2 — Linked unless unlinked.** A composite is created linked: its
  layers share one recipe, and changing it changes all of them. Unlinking is
  a setting, not a side effect of a gesture, and while a composite is
  unlinked the view says so beside the image, in what an operation reports,
  and in anything exported. Two unlinked views of the same data are not
  comparable by colour, and the help says it.
- **SD3 — Any sample kind.** Each layer maps through the mapping of a single
  plane — a table for a kind with one, a window otherwise — to a level, and
  only then takes a colour. So bands of 32-bit and float kinds composite,
  and a colour image of such a kind, which had no view as stored, opens as a
  linked composite of its colour planes over the range of their data, named
  as such.
- **SD4 — A fixed set of colours.** A layer's colour is one of red, green,
  blue, cyan, magenta, yellow and white. One layer defaults to white, two
  to green and magenta, three to red, green and blue. A free colour is not offered: the
  set is the one whose sums stay readable, and a result described as "band 8
  in cyan" can be reproduced from the words.
- **SD5 — Invalid samples and transparency.** A pixel where any shown layer
  has an invalid sample is drawn as an invalid sample is. A composite has no
  alpha: of an image with transparency it shows the colour planes, and is
  then not the image as stored.
- **SD6 — Layers share a grid.** The layers of a composite are planes of one
  source, or sources of one size. Bands on different grids are not
  composited.
- **SD7 — Limits by zscale.** A recipe's limits can come from zscale: valid
  full-resolution samples are taken at the centres of the cells of a regular
  grid with at most a stated number of cells — every pixel, where the image
  has no more than that — and sorted; a line is fitted to value against rank
  with iterative rejection of outliers; and the limits are that line through
  the median, with its slope divided by a contrast, read at the first and
  the last rank and cut to the sample's extremes. With too few samples left
  the limits are those extremes. The ends are those of the IRAF description;
  a widely used implementation reads the line one rank further in at each
  end, and its limits differ from these by one step of the slope. The contrast and the
  sample count are recorded with the limits, as percentiles are (ADR-0373
  §SD4), and the rule's steps are written in the package and in the help.
- **SD8 — The share that is clipped.** Beside a histogram the view states
  which share of the valid samples lies below the lower limit and above the
  upper one, from the histogram — exactly for a kind with one bin per code,
  and otherwise with the bin a limit falls in divided in proportion, and it
  says which. A value equal to a limit is not clipped.

### Milestones

- **M1** ✓ — the composite mapper, zscale and the clipped shares, in
  [`imaging/display`](../../public/science/imaging/display/doc.go). A layer's
  level is quantised as the colour mapper quantises it, and the channels are
  saturating sums of those levels. Measured informally on one machine
  (2026-11-21), a 512 px tile under a logarithmic stretch took 0.3, 0.5 and
  2.1 ms for one, three and eight 16-bit layers, and 0.7, 2.0 and 6.3 ms
  for 32-bit float ones.
- **M2** ✓ — the viewer shows layers; the app offers the composite, its link
  and its colours, and the same through its operations. Three things are
  narrower or other than the decisions above read. The layers of a view are
  planes of one image, and an image holds four planes: a composite shows at
  most four distinct bands — three where the bands declare an invalid value,
  the fourth plane being alpha, which declares none — and reaches eight
  layers only by showing a plane twice. Unlinked is the setting, so the
  statement shows whenever it is off and two or more layers are shown, also
  while their recipes still agree. And under one linked recipe, limits from
  the data are the joint range of the shown layers, while percentile and
  zscale limits are found from the plane of the layer in hand. Three bands
  of a stack that are no colour model open over the joint range of their
  data, linked, and the view says so; a colour image as stored keeps the
  nominal range of ADR-0373 §SD4.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `imaging/display` | a composite mapper beside the plane and colour mappers; zscale as a limits source; clipped shares from a histogram | their tests against a direct evaluation |
| Exported Go API: `widgets/imageview` | layers beside a single source; the histogram and limits of the layer in hand | `widgets/conformance`, the gallery demo, a scene |
| App operations of `apps/imagescope` | the composite set and read; zscale among the limits | its help book, its scenes |
| The recipe as a stored value | one more limits source | whatever stores a recipe: an older reader refuses the new name rather than misreading it |
| egui2 IDL, FFFI2 frame, renderers | none | — |

## Alternatives

- **A stretch per channel on the colour image.** Rejected as the shape: it
  changes what "the image as stored" shows without a place to say so. The
  composite is a different view with its own name.
- **Unlinked by default**, as tools built for separate channels do.
  Rejected: for bands that are one measurement in several wavelengths the
  shared stretch is the faithful one, and the unfaithful one should take a
  decision.
- **A colour image of a float kind through a table of its window**, keeping
  one path. Rejected: it would still be one recipe for three planes, and the
  composite covers it.
- **Free colours and a colour map per layer.** Rejected for now: sums of
  arbitrary colours hide which layer a hue came from.
- **An asinh composite that preserves colour ratios** across intensity, as
  survey images use. Not here: it is a mapping of three bands jointly, not
  of each, and deserves its formula checked against its source (Deferred).
- **Percentiles instead of zscale.** Kept beside it, not instead: on a field
  that is mostly background with a few bright sources, the percentile that
  suits one image does not suit the next.
- **Mapping in the host** (ADR-0373, O3). Unchanged: revisit on a measured
  miss.

## Consequences

### Positive

- Bands of unlike range and of any kind can be seen together, and the view
  says when their stretches differ.
- A float colour image has a view.
- Limits for a sky image need no hand.

### Negative

- A composite maps one plane per layer for every tile, and eight layers
  cost more than eight times one (M1). The image upload trial measured the
  mapping of one plane only
  ([trial §0](../trials/image-upload-cost/README.md)); a composite's cost
  on the hosts it measured is not known.
- There are now three ways of showing, and a person meets the choice.
- zscale samples: two images of the same field can get limits that differ
  in the last digits, and limits found from a sample are not a statistic of
  the image.

### Neutral

- A linked composite of red, green and blue and the colour image are the
  same pixels by two paths; a test holds them equal.
- White is a colour, so a single shown layer in white is the plane in grey.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the composite against a direct evaluation
  over every kind, with invalid samples and hidden layers; a linked red,
  green and blue composite equal to the colour mapping of the same planes;
  zscale against values computed by a reference implementation for a set of
  synthetic fields, kept as a table; clipped shares against a count over the
  samples. A scene that unlinks a composite and finds the statement of it on
  screen.
- **What would fail.** An unlinked composite with no statement of it; a
  layer's limits applied to another; a float band that is refused; an
  invalid sample drawn as dark; zscale limits that move when the image is
  read again.
- **Gap.** Interactive cost is not gated; the trial is rerun when the
  mapping path changes.

## Deferred

- **Joint mappings of several bands** — the colour-preserving asinh
  composite, a decorrelation stretch as a view.
- **Histogram equalisation and other data-dependent transfer functions**,
  which make the recipe depend on the image.
- **Compositing across grids.**
- **A colour map per layer and free colours.**
- **More than four distinct bands in one view**, which needs a view over
  several sources or an image of more planes; and more than eight layers.

## Status

Proposed — awaiting review by the repository owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md) §SD4, §SD5 — the recipe and the mapping.
- [ADR-0398](./0398-images-beyond-a-colour-model-more-sample-kinds-invalid-samples-stacks-a-placement-and-a-seam-for-readers.md) §SD1–§SD3 — sample kinds, invalid samples, bands of a stack.
- [Image upload-cost trial](../trials/image-upload-cost/README.md) — the measured cost of mapping one plane.
