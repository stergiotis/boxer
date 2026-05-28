---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Progress Bar ETA Estimation

## Background

The initial implementation used a sliding-window linear rate estimator (ring
buffer of 20 samples, computing Δcount/Δtime between oldest and newest). This
approach has two drawbacks:

1. **No trend awareness:** it cannot anticipate acceleration or deceleration.
   During TCP slow-start or cache warm-up phases, the ETA lags reality.
2. **Window-size tradeoff:** a small window reacts fast but is noisy; a large
   window is smooth but sluggish.

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

The estimator is updated at each render tick (~250 ms), not on every `Tick()` /
`Add()` call. This decouples estimation frequency from processing throughput
and makes α/β behavior predictable regardless of workload.

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

- Smoothing state is only mutated at render-tick boundaries, so `α` / `β`
  behaviour is independent of upstream throughput.
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
