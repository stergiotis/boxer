---
type: explanation
audience: end-user
status: draft
title: What the numbers are
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# What the numbers are

Every number image scope states is computed from the samples the file
holds, not from the pixels on screen. This page says how, and what each one
assumes.

## Samples and values

A sample is what the file stores: an integer from 0 to 255 or from 0 to
65535. PNG and JPEG carry no physical unit and no scale, so here a sample's
value is the sample. The samples of a 16-bit PNG are kept as 16 bits; they
are never passed through 8.

Pixel (column, row) covers the square from (column, row) to (column + 1,
row + 1), and row 0 is the top. A region of columns 10–19 and rows 0–4 is
ten columns by five rows.

Two things a reader does are recorded and shown in the status row, because
they change what a sample means:

- **JPEG is lossy.** Its samples are what the decoder reconstructs from
  quantised frequency coefficients, and a colour JPEG's red, green and blue
  are a conversion from stored luma and chroma. Statistics of a JPEG describe
  that reconstruction.
- **A palette image is expanded.** The planes hold the palette's colours,
  not the indices the file stores.

Transparency is a plane of its own, kept straight: the colour samples under
a transparent pixel are what the file stores and are not multiplied by
alpha. Statistics treat every plane separately and do not weight by alpha.

## The display recipe

The recipe maps a value to a colour in three steps. The value is placed
between the two limits, giving a number from 0 to 1, with values outside the
limits taken as 0 or 1. The transfer function bends that number — *linear*
leaves it, *log*, *sqrt* and *asinh* give more of the colour map to low
values, *gamma* raises it to a power. The result picks a colour from the
colour map, or for a colour image sets the level of red, green and blue
under one shared recipe.

None of it changes a sample. A stretch decides what is easy to see; it is
not a correction, and two images under different recipes are not comparable
by eye.

The colour maps are a plain grey ramp and the design system's sequential and
diverging scales, which are perceptually uniform. A diverging scale has a
centre: it suits values that depart from a reference in two directions, and
on other data its centre colour marks nothing.

## Reduced levels

Shown smaller than it is, the image is drawn from a reduced level: each
drawn cell is the mean of the block of samples it covers, the mean of the
samples themselves and not a mean of means. The level is the finest one that
still has a cell for every screen pixel, and the line under the image names
it.

A mean hides what it averages. A single saturated sample in a block of
sixteen is drawn as a slightly brighter cell, and *Flag clipped* at a reduced
level marks a cell whose mean is outside the limits, so clipped samples
inside a block can go unmarked. At **1:1** and above, what is drawn is the
samples.

## The readout

The line under the image gives the column and row under the pointer and the
stored sample of every plane there, read from the planes at full resolution
whatever level is drawn.

## The histogram

The histogram under a single plane counts every sample of that plane — the
whole plane, not the part in view. For 8- and 16-bit planes it has one bin
per possible value, so it loses nothing. The strip draws it with a
logarithmic height, so that a handful of samples is visible beside millions;
bar heights are therefore not proportional to counts.

## Percentiles

A percentile is nearest rank: the *p*th percentile is the smallest sample
value with at least *p* percent of the samples at or below it. It is always a
value that occurs in the data, and the median of an even number of samples
is the lower of the two middle ones. Other tools interpolate between ranks
or pick the upper middle value, and can differ by one sample value.

## Region statistics

For a marked region the window gives, per plane: the count of samples, the
smallest and largest, the arithmetic mean, the standard deviation, the
median and the 1st and 99th percentiles. They are computed from the region's
samples at full resolution.

The standard deviation divides by the number of samples, not by one less:
it describes the spread of these samples and is not an estimate for a larger
population the region might be drawn from. Neighbouring pixels of an image
are rarely independent, so the usual step from a standard deviation to the
uncertainty of a mean does not apply to a region.

The planes are measured separately. Nothing combines them into a luminance
or weights them by transparency.

## What is not done

No colour profile is applied, no gamma is removed before averaging, and no
unit is attached to a value. Means at reduced levels and the mean of a
region are means of stored samples, which for an sRGB-encoded image are not
means of light.
