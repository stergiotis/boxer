---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Progress ETA and rate estimation

## Background

This estimator started inside the CLI progress bar (`hmi/progressbar`), where
it replaced a sliding-window linear rate estimator (a ring buffer of samples,
Δcount/Δtime between oldest and newest). That approach has two drawbacks:

1. **No trend awareness:** it cannot anticipate acceleration or deceleration.
   During TCP slow-start or cache warm-up phases, the ETA lags reality.
2. **Window-size tradeoff:** a small window reacts fast but is noisy; a large
   window is smooth but sluggish.

The keelson task estimator kept a sliding window of its own, and several
job producers extrapolated a straight line from their start time. ADR-0247
moved the estimator out of the terminal package so every progress readout
uses this one; the CLI bar re-exports it under its old names.

## How it works

### Algorithm: Holt's Double Exponential Smoothing (DES)

Holt's DES maintains two exponentially smoothed state variables:

- **Level (S)** — the smoothed processing rate (items/sec).
- **Trend (B)** — the smoothed rate-of-change of the rate (acceleration).

On each observation x_t (instantaneous rate at render time):

    S_t = α · x_t + (1 − α) · (S_{t−1} + B_{t−1})
    B_t = β · (S_t − S_{t−1}) + (1 − β) · B_{t−1}

The level equation already incorporates the trend via the `(S + B)` term, so
using S_t directly as the rate estimate captures acceleration without explicit
forecasting.

### Parameters

| Parameter | Value | Rationale |
|-----------|-------|-----------|
| α (level) | 0.3   | Matches tqdm/Rich defaults; responsive without excess noise |
| β (trend) | 0.1   | Conservative trend tracking; avoids overshooting on transient spikes |

### Sampling

The CLI bar updates the estimator at each render tick (~250 ms), not on every
`Tick()` / `Add()` call. This decouples estimation frequency from processing
throughput and makes α/β behavior predictable regardless of workload.

A caller that holds a job's counters instead of a sampling loop drives it
through `Tracker`, which is called whenever the caller looks — per frame on
a render thread, per report on a producer — and decides what each look
means:

- The first observation anchors; a counter that goes **backwards** is a new
  run and re-anchors, so the old run's level does not bias the new one.
- An **unchanged** counter is not folded in until `StallAfter` (1 s by
  default) has passed. A frame loop re-reads the same counter many times
  between reports; folding each read would drag the rate to zero. After
  `StallAfter`, the stall is real and is folded in as a zero-rate sample.
- A **changed total** does not re-anchor: some producers refine their total
  while the rate holds. A caller whose total change means a new phase calls
  `Reset`.
- Rate and ETA both wait for the **second** sample, so they appear together.

Callers whose samples carry their own identity (a tick with a server-side
clock) gate the estimator themselves rather than use `Tracker`.

### Display Dampening

Raw ETA values are passed through a monotonic-clamping filter before display:

- **Decreases** are shown immediately (good news).
- **Small increases** (within 10% of displayed value) are suppressed —
  the displayed ETA holds steady.
- **Large increases** (>10% above displayed) break through to the display.

This eliminates the "3 min… 7 min… 2 min" oscillation common in naive
implementations, at the cost of occasionally showing an ETA that is slightly
optimistic during gradual slowdowns.

### ETA Label Precision

Displayed ETA precision is reduced as the estimate grows, following findings
from Harrison et al. (UIST 2007) that coarser granularity feels faster:

| Range         | Precision         | Example   |
|---------------|-------------------|-----------|
| < 1 min       | Exact seconds     | 42s       |
| 1–10 min      | Seconds           | 3m42s     |
| 10–60 min     | Nearest minute    | ~15m      |
| > 1 hour      | Nearest 5 minutes | ~1h15m    |

## Invariants

- Smoothing state is mutated only when a caller samples — a render tick, or a
  `Tracker` observation whose counter moved or stalled — and never for
  samples closer than 50 ms, so `α` / `β` behaviour does not scale with how
  often a producer reports.
- Monotonic-clamping never increases the displayed ETA by less than the 10%
  threshold; decreases are always immediate.

## Trade-offs

- Dampening prefers *perceived* stability over arithmetic accuracy; during a
  gradual slowdown the display lags the true estimate.
- Coarser precision at longer horizons favours perceived speed (Harrison et
  al.) over numeric fidelity.

## Further reading

- Harrison, C., Amento, B., Kuznetsov, S., & Bell, R. (2007).
  Rethinking the Progress Bar. *UIST '07*.
- LaViola, J. (2003). Double exponential smoothing: an alternative to
  Kalman filter-based predictive tracking. *EGVE '03*.
- tqdm: smoothing parameter defaults (α = 0.3).
