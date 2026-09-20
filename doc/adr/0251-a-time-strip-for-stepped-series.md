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

The first cut of this record was designed from those parts alone. A
[survey of what others ship and what they had to fix](../adr-background-work/time-scrubber-survey.md)
was made after it was built; it confirmed the outline — steps at their own
instants, playback that waits for both ends of a bracket, load state on the
strip, a band for the range and the body for the playhead — found defects in
the build, and found conventions the strip lacked. SD6 onward, and the
amendments to SD2, SD3 and SD5, come from it; the survey's §6 rows are cited
by number.

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

The scale is the largest *value*. A peak that runs past the top is drawn
clipped with a mark, and its number is the hover's to give: scaled to the
peaks, a series whose gusts run well above its means has its bars in the
bottom of the strip — the defect for which palette-range scaling is rejected
below, arriving through the peak.

The steps are the caller's every frame, so the widget can tell when they
change. When their instants differ from the previous frame's it moves the
playhead to the step nearest the instant it was showing: a rebuilt series
keeps the time and not the index (survey row 41).

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
reasoning: they want to look at where they put it. The nearest relative on
the same GUI library pauses for a drag and resumes; its clock is continuous,
so resuming loses nothing, where here a release lands on a step the reader
chose and resuming would leave it at once.

The readiness rule applies wherever playback starts, not only near a step, so
play pressed in the middle of a bracket whose far end is not there waits too.

**Dwell.** Playback holds the last step for `Dwell` seconds before a loop
wraps, and each end before a bounce turns. Without it the last step is fully
shown for one frame. The meteorological loop tools all have it and the one
figure a vendor documents is a bound of 2.5 s; the default here is 1 s.

**What playback needs next.** `Transport.Ahead` lists the steps playback
will enter, in order, through a loop's wrap and a bounce's turn, walking the
same rules as `Advance` — one enumerator, so a loader and the playhead cannot
disagree (row 3). SD9 is its consumer.

**A settled position.** `Events.Settled` says the position came to rest this
frame: a release, a click, a key, a button, and each whole step playback
reaches. A consumer whose work per position is a query reads that and needs
no debounce of its own; every path sets it, because a settled value that only
a drag updates goes stale (row 43).

**Waiting says what for.** The readout names the step awaited and for how
long, and gives the achieved rate when waiting has pulled it under the chosen
one. There is no ceiling in the widget: only the caller can declare a step
missing, and a missing step does not hold playback.

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

The band is the range's alone: a click there does not seek, so the double
click that clears the range has nothing to collide with (row 31), and a drag
shorter than a step neither makes a range nor clears one (row 32). A range
can be edited — an edge drags and stops at the other edge, the body slides
it whole, Escape puts back what was there — and the edge that was grabbed
stays the one being dragged. `In` and `Out` set an end at the playhead by
button and by key, which is the route that needs no drag (WCAG 2.5.7) and
the second, visible entry the nearest relative's readers asked for (row 33).
A range set around the playhead leaves it alone (row 26).

#### SD6 — load state by shape, labels by spacing

The bars' hue is the data's, so a step's state is told by shape: held is a
filled notch, idle a short tick, loading an outlined notch, missing a cross
under the baseline (WCAG 1.4.1; the scented-widgets guideline against
reusing colour). A load younger than a few hundred milliseconds is not drawn
as loading, so quick ones do not flicker.

Every label that can be read alone carries the zone, and its precision comes
from the series' smallest spacing — never from the value, which makes the
width flicker as the playhead moves (rows 11, 12, 57). Two adjacent steps
never print the same label.

Controls that do not apply are disabled and stay where they are; with fewer
than two steps the transport is disabled whole (rows 59, 61). A key at the
end of the label row says what the bar and the cap are.

#### SD7 — keys, and an exact entry

Left and Right are one step, Shift a longer stride, PageUp and PageDown the
same long stride (the ARIA slider pattern's large step), Ctrl the next and
previous day boundary, Alt the next and previous mark. Shift+Home and
Shift+End set the range's ends at the playhead, Delete clears it, Escape
cancels a drag. Up and Down stay with focus traversal. Each control's tooltip
says what it moves by and its key.

The readout's time is an edit field applied on Enter, snapping to the nearest
step. It parses every form it prints (row 58).

Looking costs nothing: hover, the snap preview and the drawing read what the
caller handed in and ask for nothing (row 48). While the playhead is dragged
the step a release would select is marked, and the readout gives its value —
snapping that is seen, not warped to on release.

#### SD8 — what a forecast needs from the strip

Left of the now line the strip is tinted; the readout gives the offset from
now and the length of the bracket under the playhead ("3 h step"). The rate
is in steps, so on a true axis the playhead speeds up where the cadence
coarsens; the words say so (row 22). A rate proportional to time is the
alternative and is deferred. Alternate days are shaded, which gives a linear
strip the cyclic reading a forecast's diurnal signal wants. The caller may
hand in marks — a run's start, the end of the analysis — drawn in the label
row and reachable by key.

#### SD9 — the flow layer fetches ahead

`flowoverlay.Layer` held the two steps around the display time and let the
rest go, so against a database every bracket began with a wait and the
waiting rule was most of what a reader saw. `Layer.SetAhead` takes the steps
`Transport.Ahead` lists; the layer keeps their windows beside the bracket's,
fetches them after the bracket in the order given, and lets go of what is in
neither. A request in flight that will answer what the display time has moved
on to is left to finish. With no list the layer is what it was. This takes up
the `PrefetchE` deferral of ADR-0250.

#### SD10 — many steps, and little room

Where steps outnumber pixel columns a column draws one bar to its largest
value and one cap to its largest peak, and the state lane shows the worst
state in it. The switch depends on the width and the step count and never on
position, and a mark keeps its meaning (row 60). `Options.ByIndex` spreads
steps evenly, for a series whose dense block is squeezed by its sparse one;
the axis type had the mode for steps without increasing times.

`Options.Compact` is one line: play and pause, the state lane with the
playhead and the range on it, and the time. It keeps the mode indicators
(row 63).

### Milestones

- **M1 — the widget.** ✓ Axis mapping, transport, strip, transport row, keys.
- **M2 — what feeds it.** ✓ `Layer.StepState`, `SummarizeE` in both test
  lanes, the purpose tag.
- **M3 — adoption.** ✓ The gallery demo and its scene; play's pane, its scene
  and its help page.
- **M4 — the transport after the survey.** ✓ Dwell, readiness at any start,
  `Ahead`, the settled position, range editing as pure operations, the instant
  kept across a change of steps.
- **M5 — the strip after the survey.** ✓ SD5's band, SD6, SD7, SD8, SD10.
- **M6 — look-ahead.** ✓ `Layer.SetAhead`; the demo and play's pane feed it.

### Deferred

- **The range as signals** (`vf_t_from`, `vf_t_to`), so a query can aggregate
  over the chosen period as it can over the Timeline's extent — when a query
  wants it.
- **Zoom and pan of the strip.** SD10's envelope serves several hundred
  steps; a series past that wants the waveform's view. What the survey asks of
  it when it comes: hard limits both ways, wheel deltas normalised, a route
  that needs no modifier, an edge marker for a playhead out of view, follow
  that yields to the reader and rests during a drag (rows 36–39, 64, 65).
- **A slider node in the accessibility tree.** The GUI library can give a
  painted widget a slider role, a value and a value text; the painter lane has
  no way to say so. Until the IDL has one, the transport row's named buttons,
  the editable time and the range's buttons are the keyboard and
  screen-reader surface.
- **Letter keys** (`I`/`O`, `[`/`]`), which the key vocabulary of ADR-0177
  does not carry.
- **A rate proportional to time**, if SD8's words do not suffice.
- **A following state** that moves with the wall clock. A forecast's clock
  moves a step an hour, and the nearest relative's record (rows 50, 51) is
  what the state costs to get right.
- **Switching single steps off**, as the meteorological loop tools do.
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

- ADR-0249 — the flow layer and its steps; ADR-0250 — the SQL source and play's pane.
- ADR-0208 — the waveform player, whose input pattern the strip follows.
- ADR-0043 — the timeline widget; ADR-0177 — focus-scoped keyboard capture.
- [A time strip for a stepped series — what exists, and what it had to fix](../adr-background-work/time-scrubber-survey.md)
  — the survey behind SD6–SD10 and the amendments.
