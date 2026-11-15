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
reads `get_view`.

For scenes and headless runs, `BOXER_IMAGESCOPE_SAMPLE` opens a new window on
a sample (`field`, `colour`, `small`, `float` or `scaled`) and `BOXER_IMAGESCOPE_REGION` marks a
region on it: column, row, width, height. A value that names no sample or
does not parse leaves the window as it would otherwise be and says so in the
status row.

## The resources

- `image` — the image open: name, size, colour model, sample kind, plane
  names, and where its samples came from, including whether the source was
  lossy. Written by `open_sample`, and by the person's **Open…**.
- `view` — the plane shown, the display recipe and the marked region.
  Written by `set_plane`, `set_recipe` and `set_region`.

## The phases

`image_state` gives the window's phase and the edges open from it;
`image_machine` the whole graph.

- *empty* — no image. Every operation but `open_sample` and `get_view` is
  refused, naming those two ways on.
- *opening* — a file is being chosen, read or decoded, or a sample made.
- *viewing* — an image is shown and no region is measured.
- *measuring* — a region is marked and its statistics are being computed.
- *measured* — the marked region's statistics are computed.

An open that fails or is cancelled returns to the phase it interrupted;
`get_view`'s `Error` or `Note` says which.

## Reading

- `get_view` — the phase, the `image` and `view` resources, the
  magnification, the level drawn (`0` is the samples; `k` means each drawn
  cell is the mean of a 2^k × 2^k block) and what the last open, measurement
  and handover came to.
- `read_pixels` — a block of samples, every plane, as values in row-major
  order, through the plane's value transform; an invalid sample is 0 and
  counted in `Invalid`. At most 1024 pixels: a larger block is cut to whole rows that fit
  and `Truncated` is set. Both sizes zero reads one pixel. The values come
  from the samples, whatever the recipe shows.
- `measure` — statistics of every plane over a region, with the method
  stated in `Method`. The zero region means the marked one, and is refused
  when none is marked. A region that reaches outside the image is refused
  naming the image's size. The numbers are computed from the samples at full
  resolution; see **What the numbers are** for what the standard deviation
  and the percentiles are.

Pixel (column, row) covers the square from (column, row) to (column + 1,
row + 1); row 0 is the top. A region is `Col`, `Row`, `Width`, `Height`.

## Changing the view

Every command sets a value; none toggles, so a repeat leaves the same view.

- `open_sample` — open `field`, `colour`, `small`, `float` or `scaled` in
  place of what is shown. It settles when the image is shown. The view returns to the image
  as stored under its default recipe and the region is cleared.
- `set_plane` — `as stored`, or a plane name from `get_view`. The recipe
  returns to that view's default, keeping the colour map, so call
  `set_recipe` after it, not before.
- `set_recipe` — the whole recipe. Read `get_view`, change fields of
  `View.Recipe` and send it back. `Limits` is `data`, `nominal`,
  `percentile` (with `PercentLo` and `PercentHi`; a single plane only) or
  `manual` (with `Lo` and `Hi`). Percentile limits need the plane's
  histogram, which is counted off the frame: until it is, `get_view`'s
  `LimitsPending` is true and the limits in force stay. Percentiles that fall
  on one value are refused once the histogram is there.
  A name that is no transfer function or colour map is refused naming the
  ones that are.
- `set_region` — mark a region; the zero region clears the mark. The window
  then computes its statistics, and `get_view`'s `Measured` says when
  `measure` returns them without recomputing.
- `set_zoom` — `fit` or `actual`.

## Sending the pixels to play

`send_to_play` publishes the stored samples of the marked region, or of the
whole image when none is marked, as the dataset `pixels` — columns `col`,
`row` and one per plane — and opens the playground on it. It writes outside
the app, so the host asks the person to confirm it every time. More than
about four million pixels are refused; mark a smaller region. The outcome is
in `get_view`'s `Handover`.
