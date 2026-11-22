---
type: adr
status: proposed
date: 2026-11-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0399: Regions — shapes in a stated frame, one inclusion rule, statistics and profiles from the samples

## Context

[ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md)
measures one kind of region, a rectangle of whole pixels, and defers
"regions beyond a rectangle" and "line profiles". A person marking a star, a
field, a cell or a transect needs a point, a line, an ellipse and a polygon;
a program working beside them needs the same regions as values it can name,
store and hand back. Since
[ADR-0398](./0398-images-beyond-a-colour-model-more-sample-kinds-invalid-samples-stacks-a-placement-and-a-seam-for-readers.md)
an image has a placement and may be one selection of a stack, so a region has
two more things to say: which coordinates it is in, and which positions of a
stack it belongs to.

The tools that carry regions record what goes wrong when these are left
open.

- **A frame left to a default.** A DS9 region file may omit its coordinate
  system and then means one of several, may mix units inside one shape, and
  loses information when written to the other region formats
  ([DS9 regions](https://ds9.si.edu/doc/ref/region.html)).
- **A shape that silently becomes another.** GeoJSON has no curve, so an
  ellipse written to it is a polygon when read back
  ([RFC 7946](https://www.rfc-editor.org/rfc/rfc7946) §3.1).
- **An inclusion rule nobody wrote down.** ImageJ stated where area and line
  coordinates lie relative to pixels only in 1.52u, and the meaning of several
  of its measurements changed across versions
  ([release notes](https://imagej.net/ij/notes.html)). Whether a pixel on a
  boundary is in a region decides every count and every sum, and a small
  aperture measured by pixel centres differs from one measured by covered
  area by several percent
  ([photutils, aperture methods](https://photutils.readthedocs.io/en/stable/user_guide/aperture.html)).
- **A profile that is an interpolation.** A line profile sampled at an
  arbitrary spacing between pixel centres reports values the detector did
  not record.

## Decision

We will add regions to the image engine as plain values: a parametric shape,
the frame its coordinates are in, an anchor that says which positions of a
stack it belongs to, and an identity apart from any style. One written rule
decides which pixels a shape covers; a second, by covered area, is offered
under its own name. Statistics and profiles are computed from full-resolution
samples under the definitions the engine already has, and every result says
which rule and which definitions produced it.

### Subsidiary design decisions

- **SD1 — Five shapes, each kept as what it is.** A point; a line segment; a
  rectangle, with a rotation; an ellipse, with a rotation; a polygon of one
  ring, whose inside is by the even-odd rule. A circle is an ellipse. A shape is stored by its parameters and is
  never replaced by an approximation of itself: an ellipse read back is an
  ellipse.
- **SD2 — One frame per region, stated.** A region's coordinates are in
  exactly one frame: the continuous pixel coordinates of ADR-0373 §SD3, or a
  placement's frame, named as the placement names it. There is no default
  and no mixing inside a shape. A region in a placement's frame is brought to
  an image's pixels through that image's placement — exactly where the
  shape's kind is closed under the map (an ellipse stays one under an
  affine; a rectangle stays one where the map keeps its right angles and is
  a polygon otherwise), and otherwise as a polygon whose edges stay within a
  stated fraction of a pixel of the true outline. On the sky a line and a
  polygon's edge are great-circle arcs, and a rectangle or an ellipse is laid
  out in the plane tangent at its own centre, east and north; under a
  gnomonic placement everything but the ellipse then maps exactly. The
  result of a measurement says which happened. A region is not brought to an
  image whose placement names another frame: frames match by kind and by a
  code, or for a physical frame by units, that both state — an empty code or
  unit is unknown and matches nothing. A physical frame has no origin, so a
  match by units says the scales agree and not that two images share a
  place.
- **SD3 — The inclusion rule.** An area shape covers the pixels whose centre
  lies inside it. A centre exactly on the outline is decided along its row:
  the shape meets the row's centre line in intervals, and each interval
  holds its left end and not its right end; an edge counts for a row only
  when one of its ends is at or above the line and the other strictly below.
  So a centre on a left or a top edge is inside, one on a right or a bottom
  edge is not, and a vertex or a tangent point alone on the line covers
  nothing: two shapes sharing an edge cover every pixel along it once, and a
  rectangle on whole coordinates covers what the rectangle of ADR-0373
  covered. Ties are decided by exact arithmetic, not by rounding. A point covers the pixel that
  contains it. A line covers the pixels it passes through. This is the rule
  for counts, ranges, percentiles and everything drawn.
- **SD4 — Coverage by area, under its own name.** For a sum or a mean over a
  small shape the engine also computes, exactly, the fraction of each pixel's
  area that an area shape covers — for an ellipse through the intersection
  of a circle with a polygon, with no supersampling — and from it an
  area-weighted sum, mean and covered area. Percentiles and extremes have no
  weighted form here and are always by SD3. A result names the rule each of
  its figures used.
- **SD5 — The anchor.** A region belongs to all positions of a stack, or to
  the positions it names on the selector axes it names. It is measured on
  each plane separately, as a rectangle is now. An anchor names axes by name,
  so it survives a stack being re-read.
- **SD6 — Identity and meaning apart from style.** A region has an id, a
  label, a free-text note, and an origin — who or what made it. Colour and
  stroke are not part of it. A region and a set of regions serialise to a
  versioned JSON document that holds everything above and nothing else; that
  document is the interchange form.
- **SD7 — Statistics.** A region's statistics per plane are those of
  ADR-0373: the count of valid and invalid samples, range, mean, standard
  deviation with divisor n, nearest-rank percentiles, over the covered
  full-resolution samples. They are computed from the samples, never from a
  reduced level.
- **SD8 — The profile.** A line's profile is the pixels it passes through,
  in order, each once: for each, the pixel, the length of the line inside it,
  the distance along the line to the middle of that length, and the sample
  of every plane. Nothing is interpolated and no spacing is chosen. A part
  of the line that runs along a pixel edge belongs to the pixel below or
  right of it, and a line through a pixel corner passes between the two
  diagonal pixels. Where the image has a physical or reference-system
  placement with one unit for both axes, distance is also given in that
  unit, as a length in the frame's coordinates — which for a system in
  degrees is not a distance on the ground.
- **SD9 — A method id.** Every result carries the identifier of the rules
  that produced it. A change to SD3, SD4, SD7 or SD8 is a new identifier.

### Milestones

- **M1** ✓ — the shapes, the frame, the anchor and the JSON form; coverage by
  centre and by area; statistics and the profile over an image in memory and
  over a tiled store, in
  [`imaging/regions`](../../public/science/imaging/regions/doc.go). A
  measurement does not read the anchor: whether a region applies at a
  position is asked separately, by whoever holds the stack.
- **M2** ✓ — the viewer reports the gestures and the app draws, makes, lists,
  measures and profiles regions, by hand and through its operations. The
  rectangle of ADR-0373 stays what it was, a transient mark, and becomes a
  region when asked. A region's origin is written by the app from who made
  the call, never taken from the caller. A region is switched between the
  pixel frame and the frame of the image it is on exactly, or the switch is
  refused with the reason. Measurements go stale when the region, the image
  or the selection changes, and say so. Regions last as long as the window
  unless their document is saved.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Exported Go API: `public/science/imaging/regions` | added | its tests; the package-properties table; the image engine how-to |
| Exported Go API: `imaging/stats` | counting over the row spans of a shape, beside counting over a rectangle | its tests against a direct evaluation |
| Region document (JSON) | added, versioned | a round-trip test and a golden per shape |
| Exported Go API: `widgets/imageview`, app operations of `apps/imagescope` | with M2 | `widgets/conformance`, the app's help book |

## Alternatives

- **Area coverage as the only rule.** Rejected: a count, a percentile and a
  drawn outline need a yes or no per pixel, and the existing rectangle is by
  whole pixels.
- **Centre coverage as the only rule.** Rejected: it is several percent off
  for the small apertures that are the common case on a star or a spot, and
  the remedy elsewhere is supersampling at a factor the result then depends
  on.
- **A polygon for everything.** Rejected: it is what GeoJSON forces, and it
  discards the centre and radius a person typed and a program wants back.
- **DS9, CRTF or GeoJSON as the stored form.** Rejected as the stored form:
  each loses a shape, a frame or an anchor this model has. Reading and
  writing them is import and export, which reports what it dropped
  (Deferred).
- **A profile at a fixed step with interpolation.** Rejected as the
  definition: the step and the kernel are choices no result records by
  default. A smoothed or resampled profile is an operation on the profile's
  table.
- **A region as a mask raster.** Not the general form: a mask has no
  parameters to edit and no frame to move between images. A mask a
  measurement produces is a plane and belongs with label images (Deferred).

## Consequences

### Positive

- A region made by a person and one made by a program are the same value,
  and a measurement can be repeated from its document.
- Two tools reading one result know which pixels were counted.
- A world region applies to every image that shares the frame.

### Negative

- Two coverage rules exist, and a reader has to look at which one a figure
  used.
- The exact area of an ellipse over a pixel is more code than supersampling,
  held by tests against closed forms and against a fine supersampling.
- A region in a frame cannot be brought to an image in another: the engine
  resolves no reference-system codes (ADR-0398 §SD4).

### Neutral

- A rotated rectangle brought through an affine with shear is a polygon in
  the result, and stays a rectangle in the region.
- The profile of a line is unevenly spaced by construction.

## Verification plan — Tier 1

- **Lane.** Default `go test`: coverage by centre against a direct test of
  every pixel centre, including shapes on whole and half coordinates and
  shapes sharing an edge; coverage by area against closed forms (a circle's
  total is πr², a polygon's is its shoelace area) and against a 64 × 64
  supersampling; statistics against a direct evaluation over the covered
  samples for every sample kind, with invalid samples; the profile's lengths
  summing to the line's length; the document round trip.
- **What would fail.** A pixel counted twice or not at all along a shared
  edge; an ellipse that comes back as a polygon; a region applied to an image
  of another frame; a profile value that is not a stored sample; an invalid
  sample counted.
- **Gap.** The polygon approximation under a non-affine placement is held to
  its stated tolerance at its vertices only.

## Deferred

- **Reading and writing DS9, CRTF and GeoJSON regions.**
- **Polygons with holes, multi-part shapes, annuli as shapes.** An annulus
  is two ellipses to a measurement that needs one.
- **Mask and label regions.**
- **A profile with a width**, averaged across the line.
- **Moving a region between the frames of two images.**

## Status

Proposed — awaiting review by the repository owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0373](./0373-an-image-viewer-samples-apart-from-presentation-mapped-in-go-shipped-as-tiles.md) §SD3, §SD8, §SD9 — the pixel convention, numbers from samples, results as tables.
- [ADR-0398](./0398-images-beyond-a-colour-model-more-sample-kinds-invalid-samples-stacks-a-placement-and-a-seam-for-readers.md) §SD2–§SD4 — invalid samples, the stack's axes, the placement.
