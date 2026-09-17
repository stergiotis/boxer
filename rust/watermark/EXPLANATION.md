---
type: explanation
audience: watermark crate maintainer
status: draft
---

> **Status: draft — pre-human-review.** Channel limits require qualification on the intended capture path.

# Tiled luminance identification

This scheme carries a cooperative image/session identifier, not authenticated
provenance. Anyone with the encoder can replace the identifier. Error correction
and CRC check transmission damage; neither prevents a different valid message
from being decoded. ADR-0241 records the recovery-over-appearance decision.

## Containment is not recovery

The eight-byte payload and CRC-16 occupy 80 information bits. Seven extended
Golay [24,12,8] words carry those bits plus four zero padding bits. The 168 coded
bits occupy data cells in a 14×13 grid; fourteen other cells provide fixed
luminance references. Each 16×16 cell has an inner 8×8 region and surrounding ring.

The tile repeats every 232×216 pixels. A 464×432 axis-aligned, unscaled crop
contains at least one complete tile at every phase. One aligned tile can also
be decoded from a smaller image. The larger bound is about arbitrary placement,
not the minimum dimensions of every decodable image.

Containment provides no protection against destroyed contrast, occluded reference
cells, rescaling or other channel changes. The worst placement has one complete
tile; combining several tiles is an improvement, not part of that bound.

## Constructing contrast

A blind additive delta competes with source content. Inner and ring means cancel
a locally linear gradient, but not text or texture. Clipping also removes an
excursion directed beyond the luminance range.

The recovery-first encoder measures the ring mean, moves it inside the interval
that leaves headroom for either symbol, and targets an inner mean one delta above
or below it. A convex move toward black or white attains each target while keeping
samples in range. The adjustment preserves some texture but may visibly change
fine detail. Delta measures the resulting mean contrast, not maximum distortion.

Reference inner blocks are held at fixed levels. Their positions remain clustered
in two rows for layout compatibility. These blocks can be conspicuous, and damage
to those rows can remove the evidence needed to locate/calibrate a tile.

The color path applies a move toward black or white to RGB channels to attain the
modified luminance. Extraction and reconstruction share gamma-domain Rec.709
weights. This preserves color where possible, not exact chroma under large changes.
Non-opaque images must be composited first: a screenshot observes the composition,
not the hidden RGB values in a transparent PNG.

## Reading evidence

A coarse covariance search over the known tile period, followed by pixel-scale
refinement, estimates grid phase. It does not detect scale. Covariance can also
match image content; it is not a detection probability.

Reference levels fit a monotonic measured-to-nominal calibration. Tiles whose fit
fails are excluded rather than combined on a different, uncalibrated scale. The
remaining inner-minus-ring contrasts are averaged before hard bit decisions.
Calibration cannot undo arbitrary nonlinear transformations of textured pixels:
transforming a mean and averaging transformed samples are different operations.

Golay corrects up to three flipped bits per word and detects exactly four. Larger
patterns may map into a different codeword. Explicitly uncorrectable words and
nonzero padding are rejected before CRC-16/CCITT-FALSE validation. No universal
false-detection probability follows from the checksum width without a model of
the received data and the number of decoding attempts.

## What tests establish

Fast tests separate cell-contrast construction, known-origin decoding, location,
PNG/color round trips and invalid input. Generated fixtures include saturation,
text-like glyphs, sharp edges and pixel-scale texture as well as smooth gradients.
A separate ffmpeg lane exercises the declared codecs and crop offsets, and a
small moving sequence. Its dependencies are required when the lane is selected.

The pathological codec acceptance arm uses stronger contrast than the default;
the default's high-frequency checkerboard failure remains a characterization
case. Combined texture/gamma/noise tests likewise distinguish mild-transform
acceptance from stronger-transform characterization. In particular, gamma 0.8
on an eight-pixel checkerboard can fail even at contrast 16. Establishing clean
mean contrast does not remove that nonlinear channel limitation.

These are reproducible cases, not a promise about every image or codec setting.
In particular, a full-frame success must not be reported as single-tile success.
The CLI reports both, along with luminance distortion. Scaling, rotation,
perspective, severe occlusion and the live imzero2 capture path are not qualified
by these tests.
