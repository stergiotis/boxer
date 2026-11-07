---
type: explanation
audience: end-user
status: draft
title: Terrain line of sight
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Terrain line of sight

Terrain scope answers one question about two points on a map: can an
observer at the first see a target at the second over the terrain between
them, and how much does that answer move once the positions and heights are
allowed to be a little wrong. The terrain is swissALTI3D, read as 2 m tiles
from a directory on the machine, so the app covers Switzerland and nowhere
else. The method, its assumptions and what every number means are on the
**Line of sight under uncertainty** page; this page is the window.

An agent drives the window through its operations catalog, and another app
opens it on two points through its launch config; both, with the phases of
the sweep machine, are on the **Operating terrain scope as an agent** page.
Nothing the window computes is published for SQL. The parameters are not
persisted; a plainly opened window starts on the defaults.

## Picking the points

The Map tab is a slippy map over Switzerland. The first click places the
observer, the second the target, and the sweep starts on its own. The status
row leads with the sweep's phase chip — *idle*, *one point*, *two points*,
*sweeping*, *result*, *failed* or *cancelled*; a click opens its graph and
history — beside the selection sentence: *Selection: 0/2 — click the
observer point on the map*, *Selection: 1/2 — click the target point on the
map*, *Selection: 2/2 — drag the sliders to re-run live*, and *Computing
line-of-sight sweep ensemble…* while a sweep runs. After the second click the map zooms in
on the pair. **Reset** clears both points and any result; a sweep still in
flight is discarded, since its result would describe the old points.

With a result, the map draws the fan: one line from the observer to each
ray's end, coloured by the fraction of the ensemble in which that ray was
clear, with the centre ray drawn heavier. The observer and target markers
carry their coordinates.

## The controls

The row above the tabs holds the three parameters a first sweep needs, each
with its tooltip:

- **observer** — Observer eye height above the terrain (metres) — the near
  end of every sight line (e.g. 1.7 for a standing person).
- **sweep ±** — Half-range of the bearing fan (degrees): rays span ±this
  either side of the observer→target line.
- **samples** — Number of Monte-Carlo ensemble members drawn from the input
  distributions (0 = nominal sweep only).

The **Advanced** fold holds the rest: Target height, the angular spacing of
the fan, and the 1σ uncertainty of each input. The defaults are a standing
observer, a ground-level target and metre-scale position noise. Inside it:

- **target** — Target height above the terrain (metres) — the height that
  must be visible at the far end.
- **step** — Angular spacing between adjacent rays in the fan (degrees).
  Fewer degrees = denser fan = more rays.
- **σ obs pos** / **σ tgt pos** — 1σ Gaussian uncertainty of the observer's
  (target's) horizontal position (metres). 0 pins the observer (target).
- **σ obs ht** / **σ tgt ht** — 1σ Gaussian uncertainty of the observer
  (target) height (metres). 0 pins the height.

Every parameter is live: a change relaunches the sweep. Changes made while
one runs are coalesced — at most one sweep is in flight, and the next starts
from the latest values once it ends.

## The sweep task

Each sweep is a keelson task of kind `terrainscope.sweep`, titled *line-of-
sight sweep*, so it has a row on the bus and can be cancelled from there as
well as from the window. While it runs, the status row shows the standard
progress row with a **Cancel**. Progress is indeterminate: the ensemble is
one call into the elevation library that reports nothing per sample, so the
row carries a phase note — *opening the elevation tiles*, then *nominal
sweep and N ensemble members* — and no fraction or ETA. A per-sample report
from the library is a recorded deferral; until it exists the bar cannot
fill.

Launching a sweep drops the previous result at once, because it described
other parameters, and the plot tabs say the sweep is running until the new
one lands. Cancelling therefore leaves no result: the status row shows a
*sweep cancelled* badge beside a **Retry** button, and the plot tabs say *No
sweep to show — change a parameter or press Retry in the status row above.*
Run the sweep again with the same parameters. Changing any parameter also
relaunches it.

When a result lands, a chip in the status row reports the wall-clock time of
the last ensemble simulation (nominal sweep + all Monte-Carlo members) and
the sample count.

## Tiles, and when the sweep fails

The tiles are read directly from the filesystem (ADR-0099, phase 1). The
directory is `SWISSTOPO_TILES_DIR`, which defaults to `~/data/swisstopo`,
and it must hold swissALTI3D 2 m cloud-optimised GeoTIFF tiles. The tile
index is scanned once per process, on the first sweep, and shared by every
window. A headless elevation service that would replace the direct read
with a bus capability is the ADR's deferred phase 4.

A sweep that fails shows a toned badge, *sweep failed:* followed by the
reason, beside **Retry**, and the plot tabs point back to the status row.
The app produces two reasons of its own. *elevation tiles not available* —
the directory is missing, empty or unreadable; a line under the status row
then also names the variable and the directory it resolved to. *line-of-
sight sweep ensemble* followed by the library's error — a profile the
sampler could not read, which is an error and not a gap filled in. The
error stays until a parameter change or **Retry** relaunches; a failed sweep
never shows as computing.

## The tabs

The three tabs open in one dock leaf; drag one out to see two side by side.

- **Map** — the picked points and the fan.
- **Sweep** — the metric rows and the profile plot: one terrain profile per
  ray, the envelope band behind each, the centre sight line, and the
  obstruction points. Hover any metric for what it means.
- **Distributions** — the empirical CDF of each randomised input's realised
  draws. With every σ at 0 it says so and draws nothing.

Before there is a result each plot tab says what is missing: the observer
click, the target click, that the sweep is computing, that it failed, or
that it was cancelled.
