---
type: adr
status: proposed
date: 2026-09-19
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0247: one progress estimator — Holt smoothing for every rate and ETA, one job row to show them

**In one paragraph:** the Holt estimator that the CLI progress bar carried
(`hmi/progressbar`) moves into a pure package, `hmi/progressest`, with a
`Tracker` that drives it from a job's reported counters. Every rate and ETA a
progress readout shows comes from it: the keelson task estimator, `bgjob`, the
ECDF band warm-up, the audio track's peaks build and play's query progress.
`widgets/jobprogress` gains a speed figure, an `Amount` head, and an inline
layout, and `taskmonitor` and play draw their job rows through it.

## Context

The same question — how fast is this going, and when will it finish — was
answered in five ways:

- `hmi/progressbar.Estimator`: Holt's double exponential smoothing with a
  damped ETA (its EXPLANATION has the reasoning). Used by the CLI bar and by
  play's query progress.
- `keelson/runtime/task/estimator`: a 2 s sliding window over raw samples
  ([ADR-0038](./0038-keelson-background-task-primitive.md)), which the task
  handle, and through it `bgjob`, used. It is the estimator the Holt one
  replaced in the CLI bar, for the drawbacks recorded there: no trend, and a
  window that is either noisy or sluggish.
- The ECDF band warm-up and the audio track's peaks build
  ([ADR-0208](./0208-audio-waveform-player-widget.md)) each extrapolated a
  straight line from the start time — the average over the whole run, which
  reacts to nothing.
- Apps outside this repository that build on it did the same, or copied
  `bgjob`.

The two figures a user sees for one job could therefore disagree: the task
monitor's wire ETA (sliding window) beside a widget's own (straight line). The
Holt estimator could not simply be imported everywhere, because it lived in a
package that pulls in terminal handling and is blocked on freestanding wasm.

On the display side, `widgets/jobprogress` showed a bar, a percentage, an ETA
and Cancel, but no speed, and only as a stacked block. play and `taskmonitor`
each drew their own rows for want of a speed figure and an inline layout.

## Decision

1. **`hmi/progressest` holds the estimator and the spellings.** The Holt
   `Estimator`, the duration/ETA/byte formatters, and two new ones:
   `FormatRate` (IEC bytes, otherwise an SI magnitude and a unit) and
   `FormatRemaining` ("<1s left", "2m05s left", "~15m left"). No terminal, UI
   or bus dependency. `hmi/progressbar` keeps its names as aliases.
2. **`Tracker` is how a caller with counters uses it.** It anchors on the
   first observation, re-anchors when the counter goes backwards, folds an
   unchanged counter in only after `StallAfter` (1 s), and withholds rate and
   ETA until the second sample. A change of total does not re-anchor; a
   caller whose total change marks a new phase calls `Reset`. The rules and
   their reasons are in the package's EXPLANATION.
3. **Every estimator becomes a user of it.** `task/estimator.Inst` is a
   millisecond-clock face on a `Tracker`, so the wire figures of every keelson
   task (and every `bgjob` run) are Holt figures. The ECDF band job and the
   audio track sample a `Tracker`; `track.EstimateEtaMs` is removed and
   `BuildProgress` gains a `Rate`. play keeps its own tick-identity gate in
   front of the `Estimator`, because a tick carries a server clock that a
   plain counter does not.
4. **`jobprogress` is the job row.** `Input` gains `Rate` + `RateUnit`,
   `Amount` (replaces the percentage at the head of the status line),
   `Inline` (Cancel, bar and status drawn into the caller's open layout,
   for a toolbar whose height must not change with the job), and `BarWidth`.
   `StatusLine` is exported so a caller laying the text out itself spells it
   the same way. The widget still estimates nothing and stays keelson-free.
   `taskmonitor` renders its in-flight rows through it and shows the wire
   rate; play's lane and pane progress rows use the inline form.

## Alternatives

- **Keep the sliding window for tasks and Holt for displays.** Rejected: the
  two figures for one job disagree, and the window's drawbacks are the reason
  the CLI bar left it.
- **Estimate only in the widget, per frame.** Rejected as the sole path: the
  task wire carries an ETA for observers that never render, and the emission
  gate needs it producer-side. The `Tracker` serves both placements.
- **Make `jobprogress` own a `Tracker` (stateful widget).** Rejected: the
  widget's value is that it is a pure display of whatever the caller has; a
  caller that has an ETA already (the task wire, `bgjob`) would pay for a
  second estimate that could disagree with the first.

## Consequences

### Positive

- One algorithm and one set of spellings for rate and ETA across the CLI,
  the task wire, `bgjob`, and the imzero2 job rows.
- A job row shows speed wherever the producer measures one.

### Negative

- A task's ETA appears one sample later than under the window (Holt needs
  two intervals), and the displayed ETA is damped: during a gradual
  slowdown it lags the arithmetic estimate, as the CLI bar's already did.
- The ETA spelling changed where it read "ETA 1m20s" (play) or used the
  task humanizer's own format; long ETAs are now rounded ("~15m left").
- `track.EstimateEtaMs` and `estimator.NewWith` are gone.

### Neutral

- `hmi/progressbar` is unchanged for its callers; its estimator and
  formatters are aliases.

## Status

Proposed — 2026-09-19. Implemented in the working tree alongside this record.

## References

- [ADR-0038](./0038-keelson-background-task-primitive.md) — the task primitive and its estimator.
- [ADR-0208](./0208-audio-waveform-player-widget.md) — the peaks build's progress.
- `public/hmi/progressest/EXPLANATION.md` — the algorithm, the damping, and the `Tracker` rules.
- Harrison, C., Amento, B., Kuznetsov, S., & Bell, R. (2007). Rethinking the Progress Bar. *UIST '07*.
