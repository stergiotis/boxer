---
type: explanation
audience: end-user
status: draft
title: An image as samples
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# An image as samples

Image scope opens one PNG, JPEG, TIFF, FITS, NPY or NPZ file and keeps it
as what the file holds: planes of samples in their own kind — 8 or 16 bits,
signed integers of 16 or 32 bits, floats of 32 or 64 bits — with a colour
model saying what the planes are. A TIFF, FITS or NPY file can hold more
planes than one image: a stack, along axes such as time or band, of which
the window shows one selection. What the window draws is those
samples through a display recipe: two limits, a transfer function between
them and, for a single plane, a colour map. Changing the recipe changes the
picture and never the samples, so every number the window states — the
readout under the pointer, the histogram, a region's statistics — is the same
under every recipe. How each is computed, and what it assumes, is on the
**What the numbers are** page; this page is the window.

The viewer reads those six formats and nothing else. It applies no colour
profile: samples are shown as sRGB, and a file that declares another
encoding gets a note in the status row saying the declaration was not
applied.

What opens is bounded twice. The file picker hands the window the whole
file, and refuses one larger than its read limit — 64 MiB unless the host
raises it — before the window sees a byte: that limit is the bound on what
opens here. Within it, a plane larger than 64 megapixels and a stack whose
samples take more than 1 GiB are refused from the file's header. A stack
stays in memory while it is open.

An agent drives the window through its operations catalog; that, with the
phases of the window's machine, is on the **Operating image scope as an
agent** page.

## Opening an image

**Open…** raises the system file picker. The app does not see the
filesystem: it is handed the one file picked, reads it once, whole, and
gives the handle back. A PNG is shown as it is read, from the top down; the
histogram, the data limits and measurements are offered once the whole file
is in, and the limits then move to the image's data range unless you set
them meanwhile. If the file turns out to be damaged, what was shown before
comes back. A TIFF, FITS, NPY or NPZ file is read whole and then shown.
What a reader does not read — a compression, a sample format, a projection
it cannot evaluate, a malformed header — is refused, and the status row
quotes the reader's own sentence, which names it. Nothing is kept after the window closes — neither the image nor
the view of it.

The **Sample** buttons, on the empty window and under *Advanced*, open a
synthetic image made from its own coordinates: a 16-bit field with a bright
blob and a saturated column, an 8-bit colour image that fades to transparent,
a small 8-bit image to zoom into, a float image spanning six decades with a
block of invalid samples, a 16-bit image whose samples stand for values
through a scale and an offset, a signed 16-bit elevation model with a
border declared invalid, a 64-bit float image whose ripples a 32-bit float
could not hold, and a signed 32-bit count image with a corner declared
invalid. They are for trying the controls; nothing about them is measured.
The sample *bands* is a stack: 16-bit samples along a time axis of three
acquisitions and a band axis of four spectral bands, B2, B3, B4 and B8,
with 0 declared invalid along a strip that moves with the time, placed in
EPSG:32632 at 10 m a pixel.

The status row leads with the window's phase chip — *empty*, *opening*,
*viewing*, *selecting*, *measuring*, *measured*, and *measuring-regions*,
*profiling* or *running* while that work is under way — and carries what the last
open came to. Under it, in warning badges, is what to know about the samples
before reading numbers off them: that a JPEG's samples are what the decoder
reconstructs, that a palette was expanded, that an animation is shown as its
default image, that rows were flipped, which stored value marks no
measurement, and the notes the file's reader left. For an image whose file
says where its pixels are, a line names the frame — a coordinate reference
system by its code, the sky, or a physical scale — the size of a pixel in
it and the statement of the file it came from, and a badge lists each thing
the file declared that is not applied.

## A stack

A stack opens on position 0 of every axis, and on the image as stored when
its bands are a colour model — a TIFF's grey or RGB — otherwise on its
first band. A row per selector axis — a time, a depth, a page, a FITS
cube's third axis — has a stepper, **‹** and **›**, around a slider, and
says which position is shown: its label, its coordinate with the axis's
unit, and which of how many it is. The **Bands** row chooses *as stored*,
*one band* — shown by itself through a colour map — or *three bands*, shown
as red, green and blue under one shared recipe, with a picker for each.
Three bands of signed 32-bit or float samples are refused, with the reason
on the status row: their colour mapping is not drawn; show one at a time.

A change of position or bands takes the new selection in the background —
the phase reads *selecting* meanwhile — and then shows it where the old one
was: the view, the marked region and, while the view stays a single plane
or stays a colour image, the plane and the recipe are kept. Limits taken
from the data are taken again from the new plane, and the status row says
so. Every number — the readout, the histogram, a region's statistics, what
**Send to play** sends — is of the selection shown.

## Looking

Drag to pan and use the wheel to zoom. **Fit** shows the whole image and
**1:1** one sample per screen pixel. Magnified, each sample is a square
unless *Smooth* is on. Shown smaller than it is, the image is drawn from
means of blocks of samples, and the line under the image says so: *drawn from
means of 4 × 4*. Those reduced levels are built in the background when an
image opens, with a progress row in the status row; cancelling it changes
nothing on screen, since each tile is then reduced as it is needed. That line also names the pixel under the pointer and its
stored values, one per plane, and for an image with a placement where the
pointer is in its frame: easting and northing in the reference system's own
units, labelled with its code and not converted to any other, or on the sky
right ascension and declination in decimal degrees.

**Show** chooses between the image *as stored* — a colour image as colour,
over a checkerboard where it is transparent or invalid — and one plane of it
by itself. A single plane has a colour bar beside it and its histogram
beneath it. **composite** — on the **Show** row for an image, on the
**Bands** row for a stack — shows several planes or bands together, each a
layer with a colour and, unlinked, limits of its own, the colours added.
The **Layers** row then gives each layer's number (the layer in hand, which
the limits and the histogram act on), its band, its colour, a *shown* box
and **×** to remove it, **+ layer** to add one, and **Linked**: on, every
layer shares one recipe; off, each keeps its own, and the window says
beside the image that the colours are not comparable with another view's.
A legend under the image names each shown layer's colour, band and limits.
A colour image of floats or of 32-bit integers, which has no other colour
view, opens as the linked composite of its red, green and blue, and the
status row says so.

## Limits

The two limits decide which values span the colour map; values outside them
are drawn at its ends.

- **data** puts them at the smallest and largest value of the plane shown.
  One extreme sample is enough to set them.
- **nominal** puts them at the range the sample kind can hold; a float
  plane has none.
- **zscale** finds them around the bulk of the values from a sample of
  about a thousand of them, the way astronomy's display tools do; a few
  bright sources do not pull them wide.
- **1–99 %** and **0.1–99.9 %** put them at two percentiles of the plane,
  counted over the whole plane, so a few extreme samples cannot pull them
  wide.
- Beside the histogram the window says which share of the plane's samples
  the limits clip below and above — *about*, for a float plane, whose
  histogram has bins.
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

## Regions

A region is a shape on the image that stays: a point, a line, a
rectangle, an ellipse or a polygon, kept by its numbers — a centre, a size,
radii, an angle, vertices — with a label, a note, and who made it. The
**Tool** row above the image chooses what a gesture makes. *point*: click.
*line*, *rectangle*, *ellipse*: drag, a rectangle and an ellipse across the
box that holds them. *polygon*: click each vertex, then the first again or
Enter; Escape drops it. *select*: click a region to select it, drag the
selected one to move it, Delete removes it; elsewhere a drag pans as usual.
A rotation is a number, set in the fields.

The **Regions** section beside the image has the fields of the region
selected — label, note, its numbers, **Apply**, **Remove** — and two rows of
buttons. **Frame** states the region in pixel coordinates, where it applies
to every image of the window, or in the frame of the image's placement,
where it applies to every image placed in that frame; switching converts
the numbers exactly, or is refused with the reason, as for a rectangle that
would become a polygon. **Anchor** ties the region to the stack position
shown, or to every position. A region that does not apply to the image
shown — another frame, another position — is listed and not drawn, and the
table says why.

The rectangle **Select region** marks is not one of these: it stays the
quick selection whose statistics are under the image. **Keep as region**
makes a region of it.

**Measure** computes the selected region's figures, **Measure all** every
region drawn, each as a task: per plane the samples covered, the mean, the
standard deviation and the median, with *Area figures* adding the figures
weighted by how much of each pixel the shape covers. They are of the image
shown and its selection, which they name, and are marked *stale* when the
region, the image or the selection changes. **Profile**, on a line, draws
its samples along the line, one point per pixel it crosses, and lists them
under a fold. **Send figures to play** and **Send profile to play** publish
them as datasets and open play on them. **Save regions…** and **Load
regions…** write and read the regions document through the file picker.

A region drawn in the accent colour with a solid line is yours; one drawn
dashed with a ◇ before its label was made by an agent, or read from a
document that said so.

## Operations

The **Operations** section applies an operator to images of the window:
an expression over named inputs, a statistic across them (*axis-stat*), a
Gaussian smoothing, a median filter, or a standardisation. Its fields are
built from the operator's declaration: a row per input — its name, its
image and, for a stack, its band and positions — and a field per parameter.
A parameter that changes the numbers at the image's edge, the border, has
no default and must be chosen; the expression field says where a formula
stops parsing as it is typed. The plan above **Run** states what would be
made, or why it cannot run. A run is a task: its result is shown as it
fills, joins the **Images** row as r1, r2, …, and carries a record of what
was done — the operator, the parameters, each input by its digest and its
positions, and what a first pass found — which **Save record…** writes to
a file. A cancelled or failed run leaves nothing. Results are held in
memory, at most 1 GiB of them; **Drop** frees one.

Nothing of this outlives the window: regions, results and records are gone
when it closes, unless they were saved.

## Send to play

**Send to play** publishes the stored samples of the marked region — of the
whole image when none is marked — as a dataset named `pixels`, one row per
pixel with its column, its row and one column per plane, and opens play on
it with a query that restates the statistics table. At most about four
million pixels are sent; a larger region is refused, and the remedy is a
smaller one. The button reports what opened, or why nothing did.
