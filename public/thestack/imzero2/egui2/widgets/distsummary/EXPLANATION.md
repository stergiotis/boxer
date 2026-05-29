---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# distsummary — Explanation

`distsummary` is the imzero2 widget that surfaces a single statistical
distribution at two levels of detail in the same cell of UI:

- **Level 1 (in-flow anchor)** — a compact monospace line showing the
  classical Tukey 5-number summary (min, Q1, median, Q3, max) plus n,
  with a Phosphor `chart-line` glyph hinting at expandability.
  Paired with the standard
  [`inspector.AnchorToggle`](../inspector/) glyph from ADR-0046's
  shared-affordance vocabulary, so the "drill into me" affordance is
  visible at rest (not hover-only — screenshots can capture it).
  Cheap, stateless from the caller's view, width-friendly enough to
  drop into a table column or a key-value row.
- **Level 2 (inspector window)** — a draggable [`c.Window`] opened by
  clicking the anchor toggle and closed by clicking it again or the
  window's native title-bar X. The body is a two-tab surface:
    - **ECDF tab (default)** — empirical CDF plus a finite-sample
      exact simultaneous confidence band (Berk-Jones default; DKW /
      equal-precision / higher-criticism selectable via
      `.Ecdf(...)`), drawn by [`widgets/ecdf`](../ecdf/) over a grid
      built from the digest by
      [`widgets/ecdfdigest`](../ecdfdigest/).
    - **Boxenplot tab** — the scientifically correct letter-value
      plot (Hofmann/Wickham/Kafadar 2017) drawn by
      [`widgets/boxenplot`](../boxenplot/) over the LV ladder
      `letterval.RecommendedLevels(digest)` returns.

  The provenance chip ([`inspector.ProvenanceChip`](../inspector/),
  ADR-0046) renders above the tab bar when the caller bound a
  `Provenance` so the inspector self-documents which subject /
  source-app produced the distribution. A bezier tether
  ([`inspector.AnchorTether`](../inspector/)) connects the anchor
  toggle to the open window so multi-pane dashboards make the
  "this anchor watches that window" relationship geometrically
  obvious.

## Why two levels

A bare ECDF or boxenplot is dense — typically 200–500 px tall and
demanding of horizontal real estate for axes. Dashboards routinely
need to show *many* distributions side by side (one per service, one
per cluster, one per percentile bucket). The level-1 line is small
enough that hundreds of distributions fit on one screen; the level-2
inspector stays out of the way until the operator clicks the anchor
toggle on the row they care about.

The two levels and the two tabs all derive from the **same**
underlying `*tdigest.TDigest` (per ADR-0046's shared source-of-truth
rule — see *Sketch ownership* below). Switching tabs swaps the view,
not the sketch — what you see in the level-1 label, the ECDF, and
the boxenplot are three projections of one statistical state, so
they cannot disagree.

## Why two tabs

ECDF and boxenplot answer different questions about the same data,
and operators reach for different ones depending on what they're
looking at:

- **ECDF + simultaneous band** is the default because it is the
  most information-dense honest summary — every quantile is read
  directly off the curve, and the confidence band shows finite-
  sample uncertainty in `F` at every `x` simultaneously. Right
  view for "is this distribution shifted vs. that one", "where is
  the heavy tail", "is the median estimate trustworthy".
- **Boxenplot** is the right view when the question is about tail
  structure at multiple depths and you want the answer compressed
  into a single column of boxes. Letter-value depths reveal
  outlier structure that a single ECDF curve makes the reader
  count by eye.

Both render from the same digest with no redundant accumulation; the
inactive tab costs nothing per frame.

## Sketch ownership (ADR-0046)

`distsummary` is a value inspector in the ADR-0046 sense and follows
that ADR's shared-source-of-truth rule (see
`doc/adr/0046-imzero2-value-inspector-infrastructure.md` §Updates
2026-05-25):

- The caller owns the `*tdigest.TDigest` and pushes observations
  into it. The widget never `Push`es, never `Clone()`s, never
  allocates a parallel sketch per tab.
- Level 1 (`computeFiveNumberSummary`) reads `Count` / `Min` / `Max`
  / `Quantiles([0.25, 0.5, 0.75])` once per frame.
- ECDF tab (`renderEcdfBody`) calls
  `ecdfdigest.BuildDigestGrid(digest, gridN)` — `gridN`=128 by
  default — to build the `(xs, fnAt)` grid. The simultaneous-band
  inversion is an O(n²) Moscovich-Nadler solve far too slow for the
  render thread at large n, so it never runs inline: a non-blocking
  `ecdf.Renderer.BandReady(n)` probe picks the path each frame. Warm
  → `RenderGrid` draws curve + band straight from the upstream
  `ecdfbands` cache (keyed by `(n, α, method)`,
  `boxer/public/analytics/stats/ecdfbands/invert.go:19`, so the solve
  runs once per parameter combo and is reused on every subsequent
  frame). Cold → `RenderGridCurveOnly` draws the curve immediately
  while `ecdf.Renderer.EnsureBandJob(scope, tasks, n)` warms the band
  on a background keelson job (ADR-0038) and a `widgets/jobprogress`
  readout shows progress + ETA below the plot; a later frame finds
  the cache warm and draws the band.
- Closing the inspector (title-bar X) or retracting it (anchor
  handle) — both land on `instanceState.pinned == false` — calls
  `ecdf.CancelBandJob(scope)`, aborting an in-flight band solve
  within one eval so it never outlives its window. The warm-up is
  keyed by the per-call `scope`, so cancelling one inspector never
  disturbs another; a solve that already finished stays in the
  `ecdfbands` cache, so a reopen redraws the band instantly.
- Boxenplot tab (`renderBoxenplotBody`) calls
  `letterval.RecommendedLevels(digest)` (bounded by
  `RecommendedDepth(n)`, typically ~7 levels) and forwards the
  resulting ladder to `boxenplot.Renderer.Render`.
- Only the active tab body runs per frame; `renderLevel2Body`
  dispatches on `instanceState.tab` and the inactive branch is
  skipped entirely.

If the ECDF cannot render (digest is `nil`, `Count() == 0`, or the
support has collapsed to a single value), `renderEcdfBody` returns
`false` and the dispatcher falls back to the boxenplot body for
that single frame. The user's tab choice is preserved in
`instanceState.tab` so the ECDF returns automatically once the
digest broadens again.

## Composition

This widget owns no math and no rendering primitives. It is glue:

- **`boxer/public/analytics/stats/tdigest`** — streaming quantile
  oracle. The widget reads `Count`, `Min`, `Max`, `Quantiles`, and
  (via `ecdfdigest`) `CDF` from the same caller-owned digest.
- **`boxer/public/analytics/stats/letterval`** —
  `RecommendedLevels(oracle)` populates the LV ladder used by the
  boxenplot tab.
- **`boxer/public/analytics/stats/ecdfbands`** (transitive via
  `ecdfdigest`) — the simultaneous-band families (Berk-Jones,
  DKW-Massart, equal-precision Stepanova-Wang, higher-criticism
  Donoho-Jin) plus the cached Moscovich-Nadler inversion.
- **`widgets/ecdf.Renderer`** — held by value inside the distsummary
  `Renderer`; defaults are taken from `ecdf.New()`, and callers can
  swap a fully-configured `ecdf.Renderer` via `.Ecdf(r)` to override
  band family, alpha, or stroke styling.
- **`widgets/ecdfdigest`** — the one-call bridge from
  `*tdigest.TDigest` to the explicit `(xs, fnAt, n)` grid the
  `ecdf.Renderer` consumes via `RenderGrid`. Keeps the `ecdf`
  widget free of any tdigest import.
- **`widgets/boxenplot.Renderer`** — held by value inside the
  distsummary `Renderer`; defaults from `boxenplot.New`. Callers
  swap via `.Boxenplot(bp)` to override palette, outlier mode, box
  width, etc.
- **`widgets/inspector`** — `AnchorToggle` (level-1 disclosure
  affordance), `AnchorTether` (bezier connector from toggle to
  window), `ProvenanceChip` (`↳ subject · Xs ago · schema · from
  app` header row), and the `Provenance` struct itself.
- **`keelson/runtime/icons.IconChartLine`** — Phosphor glyph used as
  the expandability affordance in level 1 (ADR-0044 iconography).
- **`c.Window`** — egui's draggable window block with `OpenBound`
  wired through an R10 databinding so closing via the native
  title-bar X flips the same `instanceState.pinned` flag the
  anchor toggle drives (per ADR-0026 amendment
  `feedback_egui_native_affordances`).
- **`c.SelectableLabel`** — egui's tab-row primitive; one per tab,
  each carrying a stable `AbsoluteWidgetId` derived from the call's
  scope so two `.Render(...)` invocations on the same `Renderer`
  drive independent tab state.

## Interaction model

Click-to-pin is *the* interaction. Hover was the original design
(see ADR-0046 history) but did not survive contact with multi-pane
dashboards — operators wanted to compare two distributions side by
side without keeping the cursor over either anchor. The pinned-
window pattern gives them that and pairs naturally with the bezier
tether.

Alternatives considered and rejected:

- **Inline expand** (swap the level-1 line for the plot at the same
  site) breaks the row's height invariant — the surrounding
  vertical flow would ripple-resize when the plot opens. Acceptable
  in a dedicated detail pane; wrong for table cells. A separate
  widget can be built on the same `boxenplot.Renderer` /
  `ecdf.Renderer` if needed.
- **Inline-pinned popup** (egui's `popup_below_widget` etc.) ties
  the popup geometry to the anchor's parent layout, which fights
  the multi-pane comparison use case. The free-floating `c.Window`
  + bezier tether reads as "this anchor opens that window"
  visually even when the window is dragged to a different pane.
- **Hover-only** (no click-to-pin) — the original design. Killed by
  the cursor-must-stay-here constraint above.

## Per-instance state

`distsummary` carries per-instance state in a package-level
`sync.Map` keyed by `idPrefix#<hex>` (the per-call scope), holding
one `instanceState{pinned, tab}` per logical instance. The widget
itself remains value-receiver / fluent-builder so the call shape
stays consistent with other inspectors (fsmview is pointer-
receiver for its own reasons; that's the exception, not the rule).

The `instanceState.tab` zero value is `tabECDF`, so a freshly opened
window honours the documented "ECDF is default" contract without an
explicit initialiser.

The state map is never garbage-collected — acceptable for typical
app shapes (dozens of unique `distsummary` surfaces); apps that
dynamically mount / unmount short-lived instances with one-shot
`idPrefix`es leak `O(mounts)` memory. Document but don't engineer
for it yet.

## Id stack contract

`Render` takes a `c.WidgetIdCreatorI` and consumes **exactly one**
prepared id via `idGen.Derive()` at the top of the body. The
derived value is also the per-call-site scope disambiguator (see
`callScope`), so two `.Render(...)` invocations from the same
`Renderer` (sccmap's size + color, imztop's cores + history) see
distinct scopes — independent toggle ids, window ids, tab states,
and bezier rect-capture seqs.

What the widget deliberately does **not** do:

- It does not call `ids.PrepareStr` recursively on top of the
  caller's already-`Prepared` state. Doing so would re-enter the
  `WidgetIdStack` state machine (`Initial → Prepared` while
  currently `Prepared`) and panic. The level-2 plot ids, toggle
  id, window id, tab-selector ids, and tether seqs are all
  `c.MakeAbsoluteIdStr(scope + "-<suffix>")` — absolute ids
  derived from the call scope, not the prepared id — because the
  r15 hover register match requires a stable bit pattern
  independent of surrounding `WidgetIdStack` context
  (`feedback_plot_id_for_hover_match`).
- It does not invoke any FFFI2 *fetcher* outside the documented
  set (the boxenplot / ecdf hover-register reads via `.At(...)` /
  `.AtGrid(...)`, all driven by the `StateManager`'s post-Sync
  cache, never inline pipe reads).

The `n == 0` branch explicitly calls `idGen.Derive()` (one hop) and
returns, so the "one Prepare in, one Derive out" invariant holds in
every branch.

## What this widget deliberately does not do

- **No data ingestion.** Caller owns the `*tdigest.TDigest`. The
  widget never `Push`es into it. (Per ADR-0046 §Updates 2026-05-25
  shared-sketch rule.)
- **No private sketch / cache / accumulator.** Both ECDF and
  boxenplot read from the same caller-owned digest. `instanceState`
  holds `(pinned, tab)` only — no statistical state.
- **No re-styled boxenplot or ECDF.** Both renderers' defaults
  (Batlow palette + Auto outlier mode for boxenplot; Berk-Jones +
  α=0.05 + AccentDefault soft fill for ECDF) propagate unchanged.
  Override via `.Boxenplot(bp)` / `.Ecdf(r)`.
- **No grid-resolution magic.** ECDF's grid resolution defaults to
  `defaultEcdfGridN` (128); override via `.GridN(n)`. Values below
  2 are clamped back to the default so a typo at the call site
  cannot produce a degenerate two-point grid.

## Composition example

```go
r := distsummary.New("latency-cluster").Provenance(inspector.Provenance{
    Subject:   "app.gateway.event.latency-bucket",
    SourceApp: "gateway",
    SampledAt: time.Now(),
})
for _, row := range latencyClusters {
    for range c.Horizontal().KeepIter() {
        c.Label(row.Name).Send()
        r.Render(ids.PrepareSeq(row.RowID), row.Digest, row.Extremes)
    }
}
```

One `Renderer` per visual style; the `Render` call is the per-row
cost. Allocations inside `Render` are bounded by:

- Level 1: one `Quantiles` call (single buffer flush), one label
  string build.
- ECDF tab (when active): one `gridN`-sized `(xs, fnAt)` allocation
  from `ecdfdigest.BuildDigestGrid`; subsequent `ecdfbands` work
  is cached per `(n, α, method)`.
- Boxenplot tab (when active): one LV ladder slice (~7 levels
  typical) from `letterval.RecommendedLevels`.

Inactive tab body costs zero.
