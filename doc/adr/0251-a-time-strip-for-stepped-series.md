---
type: adr
status: proposed
date: 2026-09-20
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0251: a time strip for stepped series

## Context

A flow layer (ADR-0249) shows one instant of a field that has many. The first
control over that instant, in the gallery demo and in play's Vector field pane
(ADR-0250), was a slider over the step index with a play checkbox beside it.
It moved the time and said nothing else, and three things it left unsaid cost
its reader something:

- **Which steps are worth visiting.** A forecast is tens of steps and the
  interesting ones — the front arriving, the gust peak — are found by visiting
  all of them. With a source that answers from a database every visit is a
  query.
- **What a move costs.** The layer holds the windows of the two steps around
  the display time and lets the rest go. Whether the step under the thumb is
  there, on its way or unavailable is known to the layer and was shown nowhere.
- **How time is spaced.** Steps are not evenly spaced (ADR-0249 SD1: hourly,
  then three-hourly). An index slider draws them evenly, so the same drag
  covers one hour here and three there.

Playback had a defect of its own: it advanced on the clock whether or not the
next step's window had arrived, so against a slow source the picture showed
one step of a blend alone and jumped when the other landed.

The tree already has the parts of a better control — a calendar tick layout
([the timeticks package](../../public/math/numerical/timeticks/)), an axis
painter, and in the waveform player (ADR-0208) the input pattern of a
painter-lane strip with a playhead — and no widget that puts them together for
a series of discrete steps. The timeline widget (ADR-0043) draws events on a
continuous, zoomable axis and has no playhead; the waveform's transport is
bound to an audio sink.

## Decision

We will add a widget, **timescrubber**, that draws a stepped series as a strip
on a true time axis with per-step state and an optional per-step value, owns
the playback transport, and knows nothing of what the steps are steps of. The
flow demo and play's Vector field pane adopt it; play feeds its bars from a
per-step summary of the field inside the current view.

### Subsidiary design decisions

#### SD1 — steps sit at their own instants

The axis is time, with calendar ticks and the coarser unit labelled where the
strip opens. A position stays what the flow layer takes — a fractional step
index — and is linear in time between two steps, so the strip and the layer
agree on what "a quarter of the way" means. Steps whose times do not strictly
increase have no time axis and are spread by index.

#### SD2 — the caller describes the steps every frame; the widget holds no data

A step is an instant, a state (idle, held, loading, missing), and optionally a
value and a peak. The caller rebuilds the slice each frame from whatever it
has. The widget therefore needs no invalidation rule, and a second consumer
with different data — or none: the gallery demo has no values — needs no
adapter.

Bars are scaled to the series, because the strip's question is *where* the
series is strong. What a value is in absolute terms is colour, through a
function the caller supplies, and the hover readout. play passes the flow
layer's palette over its speed range, so a colour on the strip is the colour
that step has on the map.

#### SD3 — the transport is a value type, and playback waits for data

`Transport` holds the position, the rate in steps per second, the mode (loop,
bounce, once) and the range, reads no clock and draws nothing; the widget
drives it with the frame's elapsed time, a test with numbers.

Playback enters the bracket between two steps only when both are ready, and
otherwise waits. It waits *just inside* the bracket, not on the step: a
position on a step asks the flow layer for that step alone, so waiting there
for the next one would wait for ever. Just inside, the layer sees the bracket
wanted and the picture is still the step's. Mid-bracket it does not stop for a
step that went away; the bracket was entered whole.

A person moving the playhead stops playback, on the waveform player's
reasoning: they want to look at where they put it.

#### SD4 — play's bars are the field's speed inside the view

`sqlfield.Source.SummarizeE` reduces every step inside some bounds to a mean
and a maximum of the magnitude in one statement, fixed per relation like the
window's (ADR-0250 §SD3). The mean is weighted by the cosine of latitude — an
unweighted mean over a lat/lon grid lets the crowded polar rows outvote the
rest. It reads every step where a window reads one, so nodes are decimated to
the factor a window of the same view has; the maximum is therefore a sample's.

play asks once per settled view. The source tags each statement's context
with its purpose, because play's executor supersedes a running query by a
stable identity and a summary must not replace a window, nor a window a
summary.

#### SD5 — a range is local to playback

A drag on the band along the strip's top sets the steps playback, and the
first/last controls, are limited to; a double click clears it. The press
origin decides whether a drag is the range or the playhead. The range is not
published as a signal (see *Deferred*).

### Milestones

- **M1 — the widget.** ✓ Axis mapping, transport, strip, transport row, keys.
- **M2 — what feeds it.** ✓ `Layer.StepState`, `SummarizeE` in both test
  lanes, the purpose tag.
- **M3 — adoption.** ✓ The gallery demo and its scene; play's pane, its scene
  and its help page.

### Deferred

- **The range as signals** (`vf_t_from`, `vf_t_to`), so a query can aggregate
  over the chosen period as it can over the Timeline's extent — when a query
  wants it.
- **Zoom and pan of the strip.** At several hundred steps the bars overlap
  into an envelope, which is readable; a series past that wants the
  waveform's view.
- **A cheaper summary.** One that reads a precomputed per-step table, under
  the trigger ADR-0250 records for precomputed levels.
- **Interval steps** drawn as spans and not as instants.

## Alternatives

- **Keep the slider and add buttons.** What shipped first. It answers none of
  the three questions in the Context.
- **Extend the timeline widget.** Its subject is events on a zoomable axis
  with lanes, brushing and bands; a playhead, step states and a transport
  would be a second widget inside it, and its consumers would carry them.
- **Extend the waveform player.** Its transport is an audio sink's and its
  data a peaks pyramid. The input pattern was taken from it; the types were
  not.
- **Steps evenly spaced, with uneven gaps marked.** Keeps a bar per step wide
  at any spacing. It keeps the slider's defect: equal drags are unequal times.
- **Bars scaled to the palette's range.** One scale for height and colour.
  With a mean well under the palette's top every bar is short and the series'
  shape — the thing asked about — is lost in the bottom fifth.
- **The summary over the whole field, once.** No query per view. A global
  mean of a global wind barely moves from step to step, so the cue is weakest
  where fields are largest.
- **The widget fetches its own values** through a callback. It would need
  cancellation, an identity for what it had asked, and an error surface —
  which the caller has already.

## Consequences

### Positive

- The cost of a move and the shape of the series are visible before the move.
- Playback against a slow source shows whole blends or waits, and says so.
- Any stepped series gets the control by describing its steps.

### Negative

- play sends one more statement per settled view, over every step. On a table
  ordered by time first it reads the view's share of every step.
- The strip and its transport row take height from the map.
- The maximum is a decimated sample's, and reads as "max" on the strip. The
  help page says so; the strip cannot.

### Neutral

- `flowoverlay.Layer` gains `StepState`, a read of what it already tracked.
- The pane's bare step buttons go; the scene anchors on the transport row's
  words.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- ADR-0249 — the flow layer and its steps; ADR-0250 (proposed) — the SQL source and play's pane.
- ADR-0208 — the waveform player, whose input pattern the strip follows.
- ADR-0043 — the timeline widget; ADR-0177 — focus-scoped keyboard capture.
