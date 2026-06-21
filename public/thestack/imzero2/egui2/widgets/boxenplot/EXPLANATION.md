---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# boxenplot — Explanation

`boxenplot` is the imzero2 widget that renders a letter-value plot
(Hofmann, Wickham & Kafadar 2017) — boxplot's tail-aware generalisation
suited to dashboards with thousands to millions of points per group.

## Composition

The widget sits on top of three components that own their own
EXPLANATION docs:

- **`boxer/public/analytics/stats/letterval`** — the LV math + the
  `QuantileOracle` interface this widget consumes. Owns the depth
  ladder and the outlier budget.
- **`boxer/public/analytics/stats/tdigest`** — the production
  streaming oracle. Any oracle works (exact sort, ClickHouse
  pushdown), but t-digest's tail-biased accuracy is the right
  match for LV's tail-heavy quantile pattern.
- **`egui2_definition_d_plot.go::plotBoxes`** — the egui_plot
  `BoxPlot` + `BoxElem` primitive added in M2 of the boxenplot
  programme. Whisker collapse (no whiskers) is wired here by
  passing `whisker_min == q1` and `whisker_max == q3`.

The widget itself is a thin shaping layer: it picks per-depth widths
and fill colours, decides how to render outliers, and feeds the
result into `plotBoxes` + `plotScatter` + `plotText`.

## Rendering model

For each `Render(argument, levels, extremes, perTailOverride)` call:

1. **Skip depth 1.** It is the degenerate "median = single value"
   sentinel; no box exists. The median value is, however, threaded
   into every BoxElem's `median` field so the median tick renders
   uniformly across the nested boxes.
2. **Map depths 2..maxDepth → BoxElems.** Each level becomes one
   element with `q1 = LowerValue`, `q3 = UpperValue`, `median = depth-1
   value`, `whisker_min = LowerValue`, `whisker_max = UpperValue`
   (whiskers collapsed). Width tapers with depth via
   `base · shrink^(depth-2)`. Fill samples the IDS sequential palette
   (default batlow) from `paletteTStart` (innermost = dark) to
   `paletteTEnd` (outermost = light).
3. **Resolve outlier mode.** If Auto, compare the deepest tail count
   (analytical or caller-supplied) against `OutlierAutoThreshold`:
   below → `Points`, at/above → `Count`.
4. **Render outliers.**
   - **Points**: each value in `extremes` becomes one scatter point
     at `(argument, value)`. Caller is responsible for sampling —
     typically a parallel top-K + bottom-K tracker maintained
     alongside the digest, since a quantile sketch alone cannot
     return individual extreme values.
   - **Count**: `+N` `plotText` labels at the lower and upper edges
     of the deepest box (N from `perTailOverride` if set, else the
     analytical `deepest.TailCount`). The label colour is
     `AnnotationColor` (`NeutralTextSecondary` by default).

## Why the analytical tail count is the default

`letterval.LVLevel.TailCount` is `⌊n · 2⁻ᵏ⌋` — the *expected* count
in each tail of LV depth k. It is by construction, not measurement:
the LV depth was chosen precisely so this tail has at least
`MinTailCount` observations.

For typical streaming use this is the right number to display. The
override (`perTailOverride ≥ 0`) is there for callers who count
extreme observations independently — e.g. they keep a top-K tracker
and want to display the true observed count rather than the
expected one. The two will agree to within `√n / 2^k` for a
well-behaved distribution.

## Why widths shrink with depth (default)

Hofmann's paper offers both constant-width and decreasing-width
variants. Default `shrink = 0.85` produces a soft taper (seaborn's
convention): the IQR box is widest, deeper boxes narrower. This
reflects the certainty asymmetry — the IQR estimates are based on
~n/4 observations, the deepest LV on ~MinTailCount, so reading
"inner = more reliable" is the visual cue.

`shrink = 1.0` switches to constant width (Hofmann's other variant);
useful when the boxenplot is overlaid on raw points and varying
widths would conflict with the points' visual rhythm.

## Cursor crosshair + status line

Live cursor inspection is wired through the same three-call pattern
the `ecdf` widget uses, so callers familiar with one can lift the
scaffold across to the other without re-learning the contract.

1. `r.At(plotID, argument, name, levels) Crosshair` — looks up the
   r15 plot-pointer hover register. `Valid` is true when the
   register names `plotID`, `HoverX` is finite, and
   `|HoverX - argument| ≤ snapWindow` (default 0.5, configurable
   via `SnapWindow`). For multi-distribution plots the caller
   iterates and keeps the last `Valid` Crosshair the loop produces
   — at most one distribution claims any given hover because the
   snap window is half the argument-axis spacing.
2. `r.PaintCrosshair(ch)` — emits a vertical `PlotVLine` at
   `ch.Argument` in the renderer's annotation colour at half
   alpha. The vline snaps to the matched distribution's centre
   (not the raw cursor X) so a hover between columns reads as
   "this column is selected" rather than a no-man's-land cursor.
   No-op when `ch.Valid` is false.
3. `boxenplot.WriteStatusLine(ch)` — emits a single
   `Small()`/`Weak()` `LabelAtoms` row carrying every datum required
   to interpret the hovered letter-value box, placed immediately
   below the surrounding `c.Plot(...).Send()`. The hovered ring is
   described by **the quantiles its edges represent**, not by the
   Hofmann/Wickham/Kafadar letter-value "depth" — the depth is an
   academic index for the quantile range, and a reader skimming a
   plot wants the directly-meaningful number ("this box spans the
   12.5th to 87.5th percentile"):

   - Inside a box (`ch.Depth ≥ 2`):
     ```
     <name> │ x=…, y=…  │  n=…, median=…  │  quantiles [lo%, hi%] = [v_lo, v_hi]  │  ≈… obs/tail beyond
     ```
   - Above the deepest box (`ch.Depth == 0` and `ch.PlotY > ch.MaxDepthHigh`):
     ```
     <name> │ x=…, y=…  │  n=…, median=…  │  above hi-th percentile (deepest box [v_lo, v_hi])  │  ≈… obs in this tail
     ```
   - Below the deepest box (`ch.Depth == 0` and `ch.PlotY < ch.MaxDepthLow`):
     ```
     <name> │ x=…, y=…  │  n=…, median=…  │  below lo-th percentile (deepest box [v_lo, v_hi])  │  ≈… obs in this tail
     ```

   Quantile percentages render via `%g` so a depth-3 box reads
   `[12.5%, 87.5%]` (= 75 % coverage) and a depth-2 reads
   `[25%, 75%]` (the IQR). Coverage (`hi% − lo%`) is intentionally
   not duplicated on the line — derivable by inspection and keeping
   the row scannable wins over the extra gloss. `n` is recovered
   from `LVLevel.TailCount` via the shallowest non-median LV
   (`n ≈ TailCount · 2ᵈ`); the recovery is exact when the original
   `n` was divisible by `2ᵈ` and otherwise off by less than `2ᵈ` —
   within human-readout tolerance.

   For panels with vertical room, `boxenplot.WriteStatusLineVerbose(ch)`
   is the explaining counterpart: the same facts spread over a
   fixed-height (`VerboseReadoutLineCount`) plain-language paragraph —
   "Hovered letter-value box spans the [25%, 75%] quantiles — value range
   […]. It holds the central 50% of the distribution; ≈N observations lie
   in each tail beyond it." — with a hover hint when `ch.Valid` is false.
   The terse `WriteStatusLine` is the compact register; the verbose one is
   what the `distsummary` inspector uses, mirroring `ecdf`'s verbose
   readout. (`ecdf` is symmetric the other way: `WriteStatusLine` is its
   verbose default and `WriteStatusLineTerse` the one-liner.)

   `ch.Depth` remains a `Crosshair` field for callers that want to
   key off the LV index programmatically (e.g. an external readout
   keyed to Hofmann's letter codes M/F/E/D/…); only the default
   `WriteStatusLine` format prefers quantile language.

The "smallest containing depth" rule (see `findContainingDepth`)
exploits LV's nesting invariant: depth 2 (IQR) is the innermost
ring; deeper depths widen monotonically. The smallest depth whose
`[LowerValue, UpperValue]` interval contains `HoverY` is the
visual ring the reader's eye picks out as "the cursor's box", so
that is what the status line names — even though every deeper ring
trivially contains the same point.

### Only egui_plot's auto-text label is suppressed; the highlight stays

`Render` passes `.SuppressElementText()` to `c.PlotBoxes`, which
wires an `element_formatter` closure that returns an empty `String`.
egui_plot's `add_rulers_and_text` (items/mod.rs:208) then draws a
zero-glyph text shape and otherwise behaves normally — so the
auto-sized "Max / Upper whisker / Q3 / median / Q1 / Lower whisker
/ Min" label (the one that clipped at narrow tooltips / windows)
is gone, but `on_hover` still runs and the BoxElem still re-paints
with `highlighted = true` (box_plot.rs:183) and pushes the axis
rulers. The reader keeps the visual "this is the hovered box"
affordance; the textual readout migrates to the bottom status line.

The coarser `.AllowHover(false)` toggle would have suppressed the
highlight + rulers too — `Plot::show_hover` filters its candidate
set by `entry.allow_hover()` (plot.rs:1566), so a box with that
flag off never reaches `on_hover` at all. `SuppressElementText`
threads the needle.

The previous design (before this rewrite) relied on the egui_plot
auto-text overlay and required a 12 px halo padding around the
plot inside `distsummary`'s hover popup to keep it from clipping
at the tooltip envelope; the halo is now redundant and
`distsummary` defaults to 4 px purely for visual breathing room.

## Trade-offs

- **The widget owns Render-side concerns only.** It does not own
  the Plot block, the axis ticks, or the legend. Caller wraps in
  `c.Plot(id)` and configures axes; the widget contributes one
  BoxPlot series (plus scatter/text) per Render call. Multiple
  Render calls with different `argument` values produce side-by-side
  groups inside one Plot.
- **Median is rendered N times (once per BoxElem).** All ticks land
  at the same y, so the visual result is identical to one tick.
  Cheaper than special-casing the median into a separate primitive
  and keeps the median visually present even if the user squints
  past the innermost box.
- **No widget state.** Each Render is a function of (config,
  levels, extremes). The `Crosshair` returned by `At` is a plain
  value the caller owns — adding it did not introduce per-renderer
  state, so the widget keeps its value-receiver pattern that
  matches `fieldview` / `errorview`.

## Further reading

- Hofmann, H., Wickham, H., & Kafadar, K. (2017). *Letter-value
  plots: Boxplots for large data*. JCGS 26(3), 469–477.
- Upstream LV math: [letterval EXPLANATION](https://pkg.go.dev/github.com/stergiotis/boxer/public/analytics/stats/letterval).
- Upstream sketch: [tdigest EXPLANATION](https://pkg.go.dev/github.com/stergiotis/boxer/public/analytics/stats/tdigest).
- IDS palette tokens: `src/go/public/keelson/designsystem/styletokens/data_encoding_api.go` (ADR-0031 §SD3).
- Plot primitive: `src/go/public/thestack/imzero2/egui2/definition/egui2_definition_d_plot.go::plotBoxes`.
