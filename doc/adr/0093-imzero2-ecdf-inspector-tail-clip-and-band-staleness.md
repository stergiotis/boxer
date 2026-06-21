---
type: adr
status: proposed
date: 2026-06-21
---

# ADR-0093: ECDF inspector — adaptive tail cutoff and confidence-band staleness

## Context

The ECDF tab of the `distsummary` inspector (`widgets/distsummary`, rendering
`widgets/ecdf` over a t-digest via `widgets/ecdfdigest`) had three rough edges
on a real heavy-tailed distribution (file-size metrics in the `sccmap` demo,
subject `scc-dist`):

1. **The long tail crushed the body.** The x-axis ran `[Min, Max]` and the grid
   was uniform over that range, so for a heavy right tail the entire informative
   body was squeezed into the leftmost few percent and ~95% of the grid points
   fell in the flat tail.
2. **The cursor readout was bare notation** (`x = … F_n(x) = … band […] nearest
   X_(i) = …`) and, on the digest/grid path, mislabelled a grid evaluation point
   as an order statistic `X_(i)`.
3. **Confidence-band staleness was invisible.** The band's critical value is
   data-independent and cached by `n`, and the band *shape* is recomputed every
   frame, so no *wrong* band is ever silently drawn. But a capped-`n` band is
   wider than the true `n` warrants, and on a growing digest the exact O(n²)
   solve's target `n` moved every frame, cancelling and restarting the solve
   forever so the exact band never appeared.

This ADR records the two policy decisions taken: how to trim the tail, and how to
make band staleness both visible and self-settling. The plain-language verbose
readout is the vehicle for (2) and for the staleness visibility in (3).

## Design space (QOC)

**Question.** How should the ECDF x-view trim an uninformative tail?

**Options.**

- **O1 — Tukey 1.5×IQR fences** as the x-limits. The canonical boxplot-whisker
  rule.
- **O2 — Skew-adjusted boxplot** (Hubert & Vandervieren 2008): asymmetric fences
  via the *medcouple* robust skewness.
- **O3 — Fixed quantile clip** to e.g. `[p0.1, p99.9]`, always on.
- **O4 — Adaptive quantile clip, IQR-triggered, per-side** (chosen): the cutoff
  is a quantile, but a side is clipped only when its tail is long relative to the
  IQR — `(max−Q3)/IQR` or `(Q1−min)/IQR` past ~3 (Tukey's "far out" multiple).

**Criteria.** C1 natural fit to an ECDF (y-axis already cumulative probability);
C2 computable from a t-digest (the data source); C3 doesn't trim a well-behaved
distribution; C4 predictable "fraction shown".

|    | O1 (1.5×IQR fence) | O2 (medcouple) | O3 (fixed quantile) | O4 (adaptive quantile) |
|----|--------------------|----------------|---------------------|------------------------|
| C1 | −  (cutoff x ≠ a clean fraction) | − | ++ (reads off the y-axis) | ++ |
| C2 | +  | −− (needs the full sample / pairwise; no impl in-repo) | ++ | ++ |
| C3 | −  (over-flags skewed/heavy tails — the repo already rejects 1.5×IQR, `letterval.go`) | + | −  (trims a clean dist's true max) | ++ |
| C4 | −  | −  | ++ | ++ (+ honors the `(max−min)/IQR` heaviness intuition) |

## Decision

### Tail cutoff — adaptive, IQR-triggered, per-side quantile (O4)

`distsummary.tailClipBounds` computes the x-view per-side: clip the upper side to
`Quantile(upperP)` (default p99.9) only when `(max−Q3)/IQR` exceeds `triggerIQR`
(default 3.0), symmetrically for the lower side with p0.1; otherwise show the
full support on that side. The grid is rebuilt over the clipped window
(`ecdfdigest.BuildDigestGridRange`) so resolution lands where it is visible. The
band's calibration is unaffected — it always uses the true count. A hidden-tail
annotation below the plot names the visible window and how much of which tail was
hidden. Configurable via `Renderer.TailClip` / `TailTrigger` / `NoTailClip`;
default-on is safe because the trigger leaves well-behaved distributions
full-range.

Rejected: **O1** (the repo already rejects 1.5×IQR as over-flagging skewed data —
`analytics/stats/letterval`), **O2** (the medcouple is not computable from a
t-digest and is unimplemented; it also addresses skew, not tail-heaviness),
**O3** (needlessly hides a clean distribution's true extent). O4 uses the IQR
only as the *trigger* — not as the cutoff — so it is not subject to the 1.5×IQR
objection.

### Band staleness — visible, and settle live bands

**Visible.** The cursor crosshair carries band provenance (`ecdf.Crosshair`:
`BandKind`, `Method`, `BandN`, `SampleN`, `FromGrid`); the verbose readout
(`ecdf.WriteStatusLine`) and an always-visible band-state line name the band
(exact family + calibration `n`, or the conservative DKW preview) and flag it
conservative when `BandN < SampleN`.

**Settle.** `distsummary.bucketExactN` rounds the exact-band `n` *down* to a
coarse geometric ladder (default step 1.25) before it is used for cache lookup,
the warm-up job, and rendering, so a drifting sample size reuses a cached solve
instead of restarting it. Rounding down keeps `BandN ≤ trueN`, so the band is a
conservative over-cover, never under-coverage — and the gap is the visible
staleness, not a silent error. Windowed / recomputed / slow-growth digests
settle; pathological fast-unbounded growth still can't out-run any fixed bucket
and cleanly stays on the always-correct DKW preview, which the readout explains.

Rejected for "settle": a **hysteresis / let-the-running-solve-finish** scheme
that self-tunes the bucket to the solve duration — more correct but a larger
change to the `ecdf` band-job semantics and per-instance state; deferred. Bucket
granularity is configurable (`Renderer.ExactBandBucket`); the readout makes any
resulting conservatism honest.

## Consequences

- Heavy-tailed inspectors (sccmap `scc-dist`, imztop, fps) render a usable body
  by default; the trim is annotated, not silent.
- The readout is plain-language and honest about grid-vs-sample and about which
  band is in force; it occupies a fixed `ecdf.ReadoutLineCount` height so
  hover on/off never reflows the host window.
- A live or repeatedly-recomputed exact band settles; its calibration `n` (and
  any conservatism) is visible.
- Both crosshair widgets offer two readout registers: a terse single line and an
  explaining fixed-height paragraph. `ecdf` defaults verbose (`WriteStatusLine`)
  with `WriteStatusLineTerse` the one-liner; `boxenplot` defaults terse
  (`WriteStatusLine`) with `WriteStatusLineVerbose` the paragraph. The
  `distsummary` inspector uses the verbose register on both tabs.
- Pure helpers (`tailClipBounds`, `bucketExactN`, `formatTailClipNote`,
  `formatBandStateLine`, `ecdf.formatReadout` / `formatStatusLineTerse`,
  `boxenplot.formatStatusLine` / `formatVerbose`) are unit-tested without egui.

## Implementation

First cut committed to main `520663c2` 2026-06-21: `ADR-0093` markers in
`widgets/ecdf` (Crosshair provenance, `formatReadout`/`WriteStatusLine`),
`widgets/ecdfdigest` (`BuildDigestGridRange`), `widgets/distsummary`
(`tailClipBounds`, `bucketExactN`, `renderEcdfBody`), and
`analytics/stats/ecdfbands` (`BandMethodE.String`). Follow-up: terse/verbose
readout registers added to both `ecdf` and `boxenplot`, inspector boxenplot tab
switched to the verbose register.
