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

A sample is what the file stores, in one of six kinds: an unsigned integer
of 8 or 16 bits (0 to 255, 0 to 65535), a signed integer of 16 or 32 bits,
or a float of 32 or 64 bits. A sample is kept in the kind it was stored in:
the samples of a 16-bit PNG are kept as 16 bits and never passed through 8.
PNG and JPEG hold the unsigned kinds only; files of the formats a host
registers can hold the others, and so do the *elevation*, *counts*, *float*
and *double* sample images. A plane
may carry a value transform — a scale and an offset — saying what quantity
a sample stands for; the limits, the colour bar, the histogram and every
statistic are then in values, and the readout gives the value with the
stored sample beside it. PNG and JPEG carry no scale, so a file's value is
its sample; a format that stores a scale and an offset with its samples has
one, and so has the *scaled* sample image.

A sample can be invalid: it marks that nothing was measured there. In a
float plane NaN is invalid. A plane of any kind may also declare one stored
value invalid — an elevation model's border, a detector's dead pixels — and
the status row then says which value it is; the *elevation* and *counts*
sample images declare one. An invalid sample is drawn as the checkerboard,
named *invalid* in the readout, counted in the statistics' *invalid* column
and left out of every other figure, the histogram and the limits included.
In a colour image a pixel is drawn invalid when any of its colour planes
is.

Pixel (column, row) covers the square from (column, row) to (column + 1,
row + 1), and row 0 is the top. A region of columns 10–19 and rows 0–4 is
ten columns by five rows.

What a reader does is recorded and shown in the status row, because it
changes what a sample means. A file that stores its bottom row first has
its rows flipped; an array stored in another axis order has its samples
reordered; a reader's own notes say what it read and left unapplied. Two
more:

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
values, *gamma* raises it to a power. The transfer functions that take a
parameter have a slider for it under *Advanced*: log's covers about as many
decades as its parameter has digits, three by default, and asinh's about
one by default, reaching further down as it is made smaller. An image that
spans more decades than the parameter covers shows its low values as the
bottom of the colour map. The result picks a colour from the
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

Invalid samples are left out of a cell's mean, and a cell with none valid
is drawn invalid. A mean of integer samples is rounded to a whole sample; if
it would round onto the value the plane declares invalid, it is drawn one
step away from it, so a reduced level can be one step off the mean of its
samples. No number is read from a reduced level.

## A selection of a stack

A stack is planes of one size and kind along named axes. A selector axis —
a time, a depth — shows one of its positions; a band axis shows one band,
or three as red, green and blue. The image shown is taken from the planes
at the selected positions and copied into the window's tiles, so it is
measured, drawn and sent exactly as an image opened by itself would be.
The selection itself combines no planes; a mean over time or a ratio of
bands is an operation, whose result is an image of its own.

Three bands are drawn under one recipe, the same limits for each: the
colour image says how the bands compare in stored values, which for bands
measured on different scales may not be what is wanted. A stack whose bands
are no colour model opens three bands over the range of their data
together, linear, rather than over the whole range the sample kind can
hold, under which reflectance-like 16-bit data is nearly black; the status
line says so. A recipe per band is what a composite is for.

## A composite of layers

A composite is a third way of showing an image, beside the image as
stored and one plane. It is a list of up to eight layers. Each layer is one
band of the stack — or one plane of an image that is no stack's — under a
recipe of its own, drawn in a colour. For every pixel, each shown layer's
value is placed between its limits and bent by its transfer function
exactly as a single plane's is, giving a level from 0 to 255; the level is
added to the red, green and blue of the layer's colour, and each of red,
green and blue is cut at 255. Hidden layers add nothing. A pixel where any
shown layer's sample is invalid is drawn invalid, as the checkerboard. A
composite has no transparency: of an image with alpha it shows the colour
planes, and is then not the image as stored.

Three layers in red, green and blue under one recipe are, pixel for pixel,
the colour image of the same three planes. So a colour image of 32-bit or
float samples, which has no other colour view, opens as that composite:
linked, over the range of its three planes' data together, and named as a
composite in the status row. Three bands of such a stack open the same way.

For a stack, the bands the layers name are taken as the planes of one image
— each band once — so the layers can name at most four different bands,
three when the stack declares an invalid value. A band may be the plane of
several layers, in different colours or under different recipes.

### Linked and unlinked

A composite starts linked: one recipe for every layer, and a change of the
limits, the transfer function or the inversion changes them all. Data
limits of a linked composite are the range of its shown layers' planes
together; percentile and zscale limits are found from the layer in hand's
plane and applied to all.

Unlinked, each layer has its own recipe, and the recipe controls, the
histogram and its handles act on the layer in hand. Stretching layers
separately changes the colour balance: a band twice as bright as another
can be drawn the same, and a colour on screen no longer says how the bands
compare. Two unlinked views of the same data, or the same view of two
images, are therefore not comparable by colour, and while a composite is
unlinked the window says so beside the image, `get_view` says so, and the
view resource carries it. Unlinking is a setting, the **Linked** box; no
gesture unlinks a composite on the way.

### The seven colours

A layer is red, green, blue, cyan, magenta, yellow or white: each full in
some of red, green and blue and empty in the others, so the sums stay
readable and a picture can be described in words — "band 8 in cyan" — and
made again. One layer starts white, which shows its plane in grey; two
start green and magenta, which most people tell apart; three start red,
green and blue. A layer added later takes the first colour no layer has.
Two layers that share a channel add in it and saturate at 255.

## Limits by zscale

zscale finds limits around the bulk of a plane's values from a sample of
them, the way astronomy's display tools do; a few bright sources cannot
pull the limits wide, where the 99th percentile of a field of faint
background with some bright stars may sit on a star. Its steps:

1. **The sample.** For a sample count *n* (1000 unless given), the step *d*
   is the smallest whole number for which ⌈width/*d*⌉ · ⌈height/*d*⌉ is at
   most *n*. The image is cut into that many columns and rows of cells of
   near-equal size, and the pixel at the centre of each cell is read, at
   full resolution. An image of at most *n* pixels gives every pixel. An
   invalid sample, or an infinite value, is skipped and not replaced; *m*
   values remain.
2. **Sorted.** The values are sorted ascending. Their median is the middle
   value, or the mean of the two middle ones.
3. **A line, with rejection.** A straight line is fitted by least squares
   to the sorted values against their rank 0 … *m*−1, over the values not
   rejected. A value lying more than 2.5 standard deviations of the
   residuals (of the values kept) from the line is rejected, and with it
   the values next to it in rank, ⌊*m*/100⌋ in all counting itself, at
   least itself. A rejected value stays rejected.
4. **Repeated** at most five times, while the last round rejected something
   and at least half the values, and at least five, remain.
5. **The ends.** The line's slope is divided by the contrast (0.25 unless
   given), which steepens it. The limits are that line through the median
   at the centre rank, read at the first and the last rank, each cut to the
   smallest and largest sampled value. When too few values remain, or fewer
   than five were sampled, the limits are the smallest and largest sampled
   value.

The ends are those of the original description of the method. A widely
used implementation reads the line one rank further in at each end, so its
limits differ from these by one step of the slope divided by the contrast.
The contrast and the sample count are recorded in the recipe, as the
percentiles are. zscale reads a sample, not every pixel, so the limits are
not a statistic of the image: the same image always gives the same limits,
but two images of one field can get limits that differ in the last
digits. For an image read into tiles, the sample is read from the tiles it
falls in, off the frame, and the limits in force stay until it lands.

## What the limits clip

Beside the histogram the window states which share of the plane's valid
samples lies below the lower limit and which above the upper one — of the
single plane shown, or of a composite's layer in hand. A value equal to a
limit is not clipped: it is drawn at the end of the colour map. For 8- and
16-bit planes, whose histogram has one bin per value, the shares are counts.
For 32-bit integer and float planes they are read off the histogram's 4096
bins, the bin a limit falls in split in proportion as if its samples were
spread evenly over it, and the window writes *about* before them; each is
then off by at most that bin's share. Nothing is stated until the histogram
is counted.

## The readout

The line under the image gives the column and row under the pointer and the
stored sample of every plane there, read from the planes at full resolution
whatever level is drawn. A plane is named by its label where it has one —
the band an image was taken from — and otherwise by its colour.

For an image whose file places its pixels, the line also gives where the
pointer is in that frame, at its exact position within the pixel, not the
pixel's centre. The frame is what the file states, and no more is done with
it:

- **A coordinate reference system** — named by a code such as EPSG:2056 — gives
  coordinates as that system defines them, metres for a projected one,
  written to a tenth of a pixel and labelled with the code. They are not
  converted: two images in different systems cannot be compared by
  position here.
- **The sky** — a gnomonic (TAN) projection — gives right
  ascension and declination in decimal degrees, not hours, minutes and
  seconds. Another projection, a distortion polynomial or a second system
  the file declares is not applied; the status row lists it, and where the
  affine alone would misplace pixels the image is left unplaced.
- **A physical scale** gives lengths from an origin that means nothing.

A raster that states its values at pixel centres was moved half a pixel by
its reader so that pixel (column,
row) still covers the square from (column, row); the placement's source
says so.

## The histogram

The histogram under a single plane counts every sample of that plane — the
whole plane, not the part in view. For 8- and 16-bit planes, signed or not,
it has one bin per possible value, so it loses nothing. A plane of 32-bit
integers or of floats has too many possible values for that, and its
histogram divides the range of its samples into 4096 equal bins. The strip
draws it with a
logarithmic height, so that a handful of samples is visible beside millions;
bar heights are therefore not proportional to counts.

## Percentiles

A percentile is nearest rank: the *p*th percentile is the smallest sample
value with at least *p* percent of the samples at or below it. It is always a
value that occurs in the data, and the median of an even number of samples
is the lower of the two middle ones. For a plane of floats or of 32-bit
integers the histogram divides the range into 4096 equal bins and a
percentile is located within one bin: it is right to a bin's width, which
over a range of six decades is coarse at the low end. Other tools interpolate between ranks
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

## Which pixels a region covers

A region's figures count the pixels it covers by one written rule. A point
covers the pixel that contains it. A line covers the pixels it passes
through. A rectangle, an ellipse or a polygon covers the pixels whose
centre — (column + ½, row + ½) — lies inside it; a polygon that crosses
itself is inside where a ray from the centre crosses its outline an odd
number of times. A centre exactly on the outline is decided along its row:
it is inside on a left or a top edge and outside on a right or a bottom one,
so two regions that share an edge count each pixel along it once, and a
rectangle on whole coordinates covers exactly the pixels of the marked
rectangle of the same corners. These decisions are made exactly, not by
rounding. Every count, range, percentile and outline drawn follows this
rule.

The area figures, when asked for, weigh every pixel the shape touches by
the fraction of the pixel's area inside the shape, computed exactly: a sum,
a mean (the sum over the covered area) and the covered area in pixels. On a
small shape — a star's aperture, a spot — they can differ from the figures
by centre by several percent, which is why both are offered and each is
named. Percentiles and extremes have no area form and are always by centre.
Invalid samples are left out of both and counted apart.

A region in the frame of a placement is brought to the image's pixels
through that placement: exactly where the shape keeps its kind, as a
polygon of its corners where a rectangle does not stay one, and as a
polygon within a stated fraction of a pixel where an ellipse on the sky
does not stay an ellipse. A result says which happened. A region in a frame
whose code or units the image does not share is not brought to it at all:
the frame codes are compared, never converted. Every result carries the
identifier of these rules, `regions/1`; a change to them is a new one.

## A line's profile

A line's profile is the pixels the line passes through, in order from its
start, each once. For each it gives the length of the line inside the
pixel, the distance along the line to the middle of that length, and every
plane's stored sample there. Nothing is interpolated and no spacing is
chosen, so the points are unevenly spaced, and the chart draws them as
points, not joined. A part of the line that runs along a pixel edge
belongs to the pixel below or to the right of it, and a line through a
pixel's corner passes between the two diagonal pixels. Where the image's
placement has one unit for both axes, the distance is also given in that
unit, as a length in the frame's coordinates; for a frame in degrees that
is not a distance on the ground.

## Operations: values, invalid samples, the border

An operation reads values — each stored sample through its plane's value
transform — and writes a plane of floats: 32-bit where every input is
8-bit, 16-bit or 32-bit float, 64-bit otherwise. Nothing wraps or
saturates: a difference of two unsigned images is negative where it should
be.

An output sample that depends on an invalid input sample is invalid, and so
is any result that is not a finite number — a division by zero, the
logarithm of a negative value. *axis-stat* is the exception by its
declaration: it leaves invalid samples out of each pixel's statistic, and a
pixel with none valid is invalid, except under *count*. In an expression,
`isvalid(x)` and `where(c, a, b)` see invalid samples; every other operation
passes them on.

An operator that reads neighbours takes a border with no default, because
it changes numbers: *invalid* makes every output sample whose neighbourhood
reaches past the image's edge invalid; *nearest* lets the nearest edge
sample stand in for each sample outside. The border chosen is in the record.

A standardisation finds the mean and the standard deviation (divisor n) of
each plane's valid samples in two passes before any output, with sums in a
fixed order, so the same inputs give the same quantities; the quantities are
in the record. A median filter's window is full and odd-sized whenever its
output is valid — a window holding an invalid sample gives an invalid one —
so its median is the middle sample. *axis-stat*'s median of an even number
of samples is the lower of the two middle ones.

Nothing is computed at a reduced level. The result is computed once, whole,
and its reduced levels are made from its own samples, so what is drawn at
any zoom is the reduction of what was computed. Its record names the
operator and version, the parameters as resolved, each input by the digest
of its samples with its value transforms, its declared invalid value and
its stack positions, what first passes found, and the result's digest; run
again from the record on the same inputs, the same operator gives the same
digest.

## What is not done

No colour profile is applied, no gamma is removed before averaging, and no
unit is attached to a value. Means at reduced levels and the mean of a
region are means of stored samples, which for an sRGB-encoded image are not
means of light.
