---
type: reference
audience: end-user
status: draft
title: Operating terrain scope as an agent
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Operating terrain scope as an agent

Terrain scope holds no model grant of its own. An agent reaches a window
through its operations catalog under a task grant the person approved
(ADR-0269, ADR-0318), places the two points, reads the sweep's fan and
changes a parameter; the person sees every change in the window and in the
command log, and the same handlers run when they click. `keelson('operations')`
lists the operations with their arguments; this page says how to use them
together and what each refuses.

## Opening a window on two points

Another app opens terrain scope on a pair of points through its launch
config, kind `terrainscopeLaunch` (`apps/terrainscope/launchcfg`): the
observer and the target as WGS84 degrees, and optionally the observer
height, the target height and the sample count, where zero leaves the
window's default. The sweep starts on the first frame, as it does after
the second click on the map. A config whose point is outside WGS84 or whose
parameter is outside its range fails the open with the reason rather than
opening a plain window.

For scenes and headless runs, `BOXER_TERRAINSCOPE_POINTS` does the same
from the environment: four decimal numbers, observer latitude and longitude
then target latitude and longitude, separated by commas. A value that is
not four numbers leaves the window idle and says so in the status row. A
launch config wins over the variable.

## The resources

- `points` — the observer and the target, WGS84 degrees, and how many of
  them are placed (0, 1 or 2). Written by `set_points` and `clear_points`.
- `parameters` — the nine sweep parameters by name, with their ranges:
  `observer_height` and `target_height` (metres above terrain, 0 to 100),
  `sweep_half_deg` (0 to 30), `sweep_step_deg` (0.1 to 5), `sigma_obs_pos`
  and `sigma_tgt_pos` (1σ metres, 0 to 50), `sigma_obs_height` and
  `sigma_tgt_height` (1σ metres, 0 to 30), `samples` (0 to 64, an integer).
  Written by `set_parameter`. While the person drags a slider the resource
  reads as being edited, and an agent's write lands as a conflict: the
  person wins a tie.
- `sweep_state` — the phase of the sweep machine, below.

## The phases

The status row's chip is a state machine the window mirrors every frame
(keelson-app T1): **idle**, no point placed; **one point**, the observer
placed; **two points**, both placed and the sweep about to launch;
**sweeping**; then one of **result**, **failed** and **cancelled**. A
parameter change or `retry` moves a settled phase back to sweeping; `clear_points`
moves any phase to idle. `sweep_state` returns the current phase, the edges
open from it with the gesture that causes each, and the latest steps;
`sweep_machine` returns every state and edge. Failure is a phase of its
own here rather than a return to the phase it interrupted, because a failed
sweep leaves no result behind to return to: the previous result is dropped
when a sweep launches.

## get_view

A query over a snapshot of the window: the phase, the `points`, the
`parameters`, whether a sweep is running, the error of a failed one, the
note of a cancelled one, and the result's size (rays, samples, the
simulation's wall-clock) without the rays themselves. It also names the
tile directory the sweep reads and, once a sweep has tried to open it, why
it could not.

## get_result

A query returning the last sweep's fan: the observer and target in LV95
metres, the range, the sample count, how many rays are nominally visible,
the mean visible fraction, the centre ray's fraction, and one row per ray
with its bearing offset, its nominal verdict, its visible fraction and its
first obstruction. `Limit` bounds the rows, 64 by default and at most 256,
from the most negative bearing; `Truncated` says the fan has more. `Method`
states what the fractions are and the σ that were drawn, as the method page
does: straight sight lines, no curvature or refraction, terrain the only
obstruction, independent Gaussians from a fixed seed. A fraction is a
statement about these inputs and this terrain, not a calibrated
probability.

The refusals name the remedy: *there is no result yet; set_points launches
a sweep*; *the sweep is running; read get_view until its phase is result*;
*the last sweep failed:* with the reason, and *retry relaunches it*.

## set_points

A command placing both points at once and sweeping between them; it never
toggles, so the same points again sweep again. It is asynchronous
(ADR-0358): the call stays running while the sweep does and settles with
the phase the sweep ended in — `result` with the ray and sample counts,
`failed` with the error, `cancelled`, or `idle` when the points were
cleared meanwhile. The previous result is dropped at once, so `get_result`
refuses until the new sweep lands. A latitude outside -90 to 90 or a
longitude outside -180 to 180 is refused naming the range; whether the
terrain covers the points is the sweep's to say, and a point outside
Switzerland fails it with *elevation tiles not available* or the library's
own error.

The person's gesture is the second click on the Map tab.

## set_parameter

A command setting one named parameter to a value within its range; the
names and ranges are the `parameters` resource's. A value outside the range
is refused naming it (*sweep_step_deg is between 0.1 and 5*); an unknown
name is refused naming the names. `samples` is rounded to an integer. With
two points placed the sweep relaunches with the new value; its landing is
read through `get_view`. The person's gesture is the slider.

## clear_points

A command clearing both points and any result; a sweep in flight is
discarded, since its result would describe the old points. The person's
gesture is the **Reset** button.

## retry

A command running the failed or cancelled sweep again with the same points
and parameters. It is available only in those two phases; otherwise the
catalog says *nothing to retry*, and `set_points` or `set_parameter` is the
way to launch one. The person's gesture is the **Retry** button.

## cancel

A command cancelling the running sweep. It is the person's decision:
cancelling a job is consequential, so the host asks them to confirm it
every time an agent asks, and the person's own click on the progress row's
**Cancel** goes through the same handler without a prompt. It is available
only while a sweep runs. The result the sweep would have replaced stays
dropped, so after a cancel the window shows the *sweep cancelled* badge and
**Retry**, and `get_result` refuses until a sweep lands.

## What is not an operation

The map's camera, the dock layout and the plot tabs are the person's view
and have no operation. Nothing the window computes is published as a
dataset; a sweep's fan is read through `get_result`, bounded. The tile
directory is `SWISSTOPO_TILES_DIR`, configuration of the process rather
than of a window, and no operation changes it.
