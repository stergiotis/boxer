---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# letterval — Explanation

`letterval` computes the letter-value (LV) summary that underlies a
boxenplot. It is the pure-math layer between any quantile oracle —
[`tdigest.TDigest`](../tdigest/), an exact sort-backed table, a
ClickHouse pushdown helper — and a rendering layer such as the
`boxenplot` widget in pebble2impl.

## Background

Tukey's boxplot summarises a distribution with five numbers (min, Q1,
median, Q3, max) and flags everything past `Q3 + 1.5·IQR` as an
"outlier". For `n` in the thousands this fence flags too many points:
a Gaussian with `n = 10⁶` produces ~7000 mechanically-flagged outliers,
which is both unhelpful (they aren't anomalous) and unreadable
(overplotting).

Hofmann, Wickham & Kafadar (*JCGS*, 2017, "Letter-value plots: Boxplots
for large data") generalised the boxplot by stacking *nested* boxes:
the kth letter value (LV_k) is the pair `(q_low_k, q_high_k) = (2^-k,
1 − 2^-k)`. Depth 1 is the median (degenerate, single value); depth 2
the IQR; depth 3 the eighths; and so on. Plotting these as concentric
boxes (typically shaded by depth) yields a multi-resolution view that
scales gracefully and reduces the over-flagged-outliers problem to a
single threshold past the deepest LV.

## How it works

### Depth indexing

The package uses Hofmann's depth convention:

| depth | letter | lower q   | upper q     | per-tail count   |
|-------|--------|-----------|-------------|------------------|
| 1     | M      | 0.5       | 0.5         | n / 2            |
| 2     | F      | 0.25      | 0.75        | n / 4            |
| 3     | E      | 0.125     | 0.875       | n / 8            |
| k     | …      | 2^-k      | 1 − 2^-k    | n · 2^-k         |

Depth 1 is degenerate (`LowerValue == UpperValue == median`); the
rendering layer treats it as the centre line, not a box.

### Recommended depth

`RecommendedDepth(n)` implements Hofmann's rule of thumb:

    k_max = ⌊ log₂( n / MinTailCount ) ⌋

with `MinTailCount = 8` (each tail at the deepest level retains ≥ 8
observations, so the LV value at that depth is still estimable, not
pure noise). It is clamped to `[1, MaxDepth=16]` so we always render
at least the median and never request quantiles past `2⁻¹⁶ ≈ 1.5e-5`
where no realistic quantile sketch maintains useful tail precision.

### Oracle decoupling

The package depends only on a three-method `QuantileOracle` interface:
`Quantile(q)`, `CDF(x)`, `Count()`. Three concrete oracles are
expected:

1. **`tdigest.TDigest`** — production streaming case. Tail-biased
   error, perfect fit for LV's tail-heavy quantile pattern.
2. **Sort-backed exact oracle** — testing reference and the natural
   choice when the dataset fits in memory.
3. **ClickHouse pushdown oracle** (future) — wraps a single
   `quantilesTDigest(q1, q2, …)(col) GROUP BY g` round-trip per
   render, returning per-group values. Avoids shipping raw data when
   the source already holds the digest.

Keeping the oracle interface narrow means a rendering layer that
imports `letterval` does not transitively import any sketch
implementation.

### Outlier budget

`BudgetFor(levels)` returns the per-tail and total expected observation
count beyond the deepest rendered LV (`= deepest.TailCount`). The
rendering layer uses this to switch between:

- **(a) discrete-point mode** — when the budget is small enough that
  individual extremes can be drawn without overplotting (Tukey's
  classical convention).
- **(b) count-annotation mode** — when the budget exceeds a threshold,
  collapse the tail into a single "+N more" annotation.

The toggle and threshold are widget-layer concerns; `letterval`
exposes the budget so the widget can decide.

## Invariants

- `len(Levels(o, k)) == k` whenever `o.Count() > 0` and `k > 0`.
- `LowerValue ≤ UpperValue` for every level (oracle's monotonicity
  required).
- Levels are emitted in increasing depth (`1, 2, …, maxDepth`).
- Adjacent levels nest: `LowerValue` is non-increasing in depth,
  `UpperValue` is non-decreasing in depth.
- For depth 1, `LowerValue == UpperValue`, `LowerQ == UpperQ == 0.5`.
- `TailCount[k] == ⌊ n · 2^-k ⌋`, halving with each step.
- `RecommendedDepth(0) == 0`; `RecommendedDepth(n) ≥ 1` for `n ≥ 1`;
  monotone non-decreasing in `n`.

## Trade-offs

- **Depth 1 is degenerate by design.** A separate "median" type would
  give the type system a small extra invariant but force every consumer
  to special-case both the type and the depth=1 sentinel; one type
  with a documented depth-1 convention reads more naturally.
- **No outlier sampling here.** Returning the actual extreme values
  needs raw data (the bottom-K + top-K), which a streaming sketch
  does not retain. That responsibility lives with the widget layer,
  which can pair the oracle with a separate top-K tracker.
- **`TailCount` is expected, not observed.** Computed analytically as
  `n · 2^-k`. For a perfectly-uniform distribution this is exact; for
  finite samples of any other distribution it is an expectation. Using
  `oracle.CDF(deepest.LowerValue) · n` would give a sketch-measured
  count but is structurally redundant (it just inverts the
  Quantile-then-CDF round-trip back to ~ `2^-k`).

## Further reading

- Hofmann, H., Wickham, H., & Kafadar, K. (2017). *Letter-value plots:
  Boxplots for large data*. Journal of Computational and Graphical
  Statistics, 26(3), 469–477.
- Sibling: [`tdigest`](../tdigest/EXPLANATION.md) — the streaming
  oracle this package was designed around.
- Reference: <https://pkg.go.dev/github.com/stergiotis/boxer/public/analytics/stats/letterval>
