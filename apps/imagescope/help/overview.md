---
type: explanation
audience: end-user
status: draft
title: An image as samples
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# An image as samples

Image scope opens one PNG or JPEG file and keeps it as what the file holds:
one to four planes of samples at their own bit depth — 8 or 16 bits — with a
colour model saying what the planes are. What the window draws is those
samples through a display recipe: two limits, a transfer function between
them and, for a single plane, a colour map. Changing the recipe changes the
picture and never the samples, so every number the window states — the
readout under the pointer, the histogram, a region's statistics — is the same
under every recipe. How each is computed, and what it assumes, is on the
**What the numbers are** page; this page is the window.

The viewer reads PNG and JPEG and nothing else. It applies no colour profile:
samples are shown as sRGB, and a file that declares another encoding gets a
note in the status row saying the declaration was not applied. An image
larger than 64 megapixels is refused.

An agent drives the window through its operations catalog; that, with the
phases of the window's machine, is on the **Operating image scope as an
agent** page.

## Opening an image

**Open…** raises the system file picker. The app does not see the
filesystem: it is handed the one file picked, reads it once and gives the
handle back. Nothing is kept after the window closes — neither the image nor
the view of it.

The **Sample** buttons, on the empty window and under *Advanced*, open a
synthetic image made from its own coordinates: a 16-bit field with a bright
blob and a saturated column, an 8-bit colour image that fades to transparent,
a small 8-bit image to zoom into, a float image spanning six decades with a
block of invalid samples, and a 16-bit image whose samples stand for values
through a scale and an offset. They are for trying the controls; nothing
about them is measured. The last two are the only way to reach float samples
and value transforms here, since PNG and JPEG carry neither.

The status row leads with the window's phase chip — *empty*, *opening*,
*viewing*, *measuring*, *measured* — and carries what the last open came to.
Under it, in warning badges, is what to know about the samples before
reading numbers off them: that a JPEG's samples are what the decoder
reconstructs, that a palette was expanded, that an animation is shown as its
default image.

## Looking

Drag to pan and use the wheel to zoom. **Fit** shows the whole image and
**1:1** one sample per screen pixel. Magnified, each sample is a square
unless *Smooth* is on. Shown smaller than it is, the image is drawn from
means of blocks of samples, and the line under the image says so: *drawn from
means of 4 × 4*. That line also names the pixel under the pointer and its
stored values, one per plane.

**Show** chooses between the image *as stored* — a colour image as colour,
over a checkerboard where it is transparent — and one plane of it by itself.
A single plane has a colour bar beside it and its histogram beneath it.

## Limits

The two limits decide which values span the colour map; values outside them
are drawn at its ends.

- **data** puts them at the smallest and largest value of the plane shown.
  One extreme sample is enough to set them.
- **nominal** puts them at the range the sample kind can hold.
- **1–99 %** and **0.1–99.9 %** put them at two percentiles of the plane,
  counted over the whole plane, so a few extreme samples cannot pull them
  wide.
- The two handles on the histogram can be dragged; limits set that way are
  *manual*.

Under **Advanced** are the transfer function (linear, log, sqrt, asinh,
gamma), the colour map, *Invert*, *Flag clipped* — which draws samples
outside the limits in two marker colours — and *Smooth*.

## Measuring a region

Turn on **Select region** and drag on the image: the drag marks a rectangle
of pixels instead of panning. The panel under the image then shows the
region's statistics, one row per plane: the number of samples, the smallest
and largest, the mean, the standard deviation, the median and the 1st and
99th percentiles. They are computed from the samples of the region at full
resolution, whatever level the view is drawn from. **Clear region** removes
the mark.

## Send to play

**Send to play** publishes the stored samples of the marked region — of the
whole image when none is marked — as a dataset named `pixels`, one row per
pixel with its column, its row and one column per plane, and opens play on
it with a query that restates the statistics table. At most about four
million pixels are sent; a larger region is refused, and the remedy is a
smaller one. The button reports what opened, or why nothing did.
