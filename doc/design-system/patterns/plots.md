---
type: explanation
audience: IDS app authors and contributors
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# IDS pattern: plots

Plots — line charts, scatter plots, bar charts, histograms, heatmaps, sparklines — are the second-most consequential surface in ImZero2 apps after tables. `imztop` is mostly plots stacked on plots; the Grafana-replacement work is fundamentally a plot fleet ([`project_grafana_replacement`]); `regex_explorer` and the time-range picker both lean on plots for context; a typical dashboard panel hosts more pixels of plot than any other widget. The plot pattern is therefore the strongest application of IDS's color encoding rules, axis conventions, and Swiss-minimalist restraint.

This doc covers when to reach for which plot type, how IDS layers on top of `egui_plot`, the color-encoding decision tree, axis conventions (calendar-aware time ticks, Talbot-derived numeric ticks), density behavior, interaction, and the performance constraints that ImZero2's continuous-render loop imposes on plot authors. It is an explanation, not a tutorial — for canonical code, see `imztop`'s sparkline panels and the Grafana-replacement panel implementations.

## Background

**egui_plot capabilities.** `egui_plot` is the upstream substrate ([docs.rs/egui_plot](https://docs.rs/egui_plot)). It provides a `Plot` container with axes, zoom, pan, and a small primitive vocabulary: `Line`, `Points`, `BarChart`, `BoxPlot`, `HLine`, `VLine`, `Polygon`, `Text`, `Arrows`, `PlotImage`. There is no "heatmap" primitive — heatmaps are composed from `Points` with shape, radius, and color or from `Polygon` grids. There is no built-in stacked-area; stacking is the caller's responsibility (compose multiple `Line`s with cumulative Y values plus `Polygon` fills).

**Foundational dependencies.** Plots pull from every foundations sub-ADR:

- Color ([ADR-0031](../../adr/0031-imzero2-design-system-color.md)) — the data-encoding palette (Crameri `batlowS` qualitative, `batlow` sequential, `vik` diverging; viridis family as opt-in) is the plot color source. Semantic palette is used *only* for status overlays (HLine threshold, alert region) — never for series identity.
- Typography ([ADR-0030](../../adr/0030-imzero2-design-system-typography.md)) — `Caption.Mono.Numeric` for tick labels (tabular figures, fixed-width); `Caption` proportional for axis labels and legend entries; `Body` for plot titles.
- Spacing ([ADR-0032](../../adr/0032-imzero2-design-system-spacing-density-motion.md)) — `Padding.Outer` for plot margin to surrounding panel; `Gap.Items` for legend entries; `Stroke.Hair` for grid lines; `Stroke.Regular` for axes; `Stroke.Strong` for annotation lines.
- Motion ([ADR-0032 §SD5](../../adr/0032-imzero2-design-system-spacing-density-motion.md)) — pan / zoom transitions use `Motion.Quick`; reduced-motion resolves to instant.

**Axis tick generation.** Two upstream tick generators:

- **Calendar-aware time axis** — `boxer/public/math/numerical/timeticks` (per [`reference_boxer_timeticks`]). uPlot-derived ladder; produces visually-sensible time ticks across nanosecond-to-decade ranges. Use this for any X axis that represents time.
- **Talbot numeric axis** — `finddivisions` algorithm ([`project_finddivisions_talbot_weights`]). Produces visually-pleasing tick positions for numeric ranges. *Always populate `DefaultWeights`* — empty `TalbotOptions{}` degenerates scoring. Set `FastMode: true` for the interactive-pan case.

These are the two tick sources IDS uses. The default egui_plot tick logic is a fallback only.

**Why plots are hard.** Two real constraints tug against each other. **Information density** wants more points, more series, more annotations — the Grafana-replacement work targets ~100 k visible points per pane ([`project_grafana_replacement`]). **Continuous rendering** at 60–120 Hz ([`project_imzero2_continuous_rendering`]) limits how much we can paint per frame. The reconciliation is upstream M4-in-SQL pre-aggregation — apps down-sample in the data layer, not in the render path — so the plot itself never sees more than ~100 k points it cares about.

## How it works

### Plot type ↔ encoding intent ↔ palette

The first decision is what the plot is encoding, which dictates the palette:

| Plot type | Encoding intent | IDS palette |
|---|---|---|
| **Line plot** (time series, scalar vs continuous X) | series identity (categorical) | `batlowS` qualitative cycle |
| **Scatter plot** | two dimensions + optional third via color | qualitative if 3rd is categorical; `batlow` if 3rd is magnitude |
| **Bar chart** (categorical X) | category magnitude | `batlowS` qualitative or single semantic color |
| **Stacked bar** | composition within category | `batlowS` qualitative; same N colors fleet-wide |
| **Histogram** (binned distribution) | density of one variable | single semantic color, `info.default` default |
| **Heatmap** (binned 2D distribution) | magnitude over 2D grid | `batlow` sequential (or `vik` diverging if signed) |
| **Density plot** (KDE, contour) | continuous density | `batlow` sequential alpha-modulated |
| **Box plot** | distribution summary | `batlowS` qualitative or single `info.default` |
| **Sparkline** (inline mini-plot) | scalar trend over time, single series | single semantic color, `info.default` default |
| **Step plot** (discrete state over time) | state transitions | `batlowS` qualitative per state-class |
| **Annotations** (HLine, VLine, region) | reference values, thresholds, alert ranges | semantic palette (`warning` / `error` / `success`) |

The palette type is *intent-driven*, not aesthetic. Qualitative for categorical; sequential for ordered magnitude; diverging for signed deviation from a midpoint. Mixing these — using `batlow` for series identity, or `batlowS` for a heatmap — collapses the visual encoding's meaning. Tier 2 rubric V2 (color-encoding consistency) catches the misuse.

### Lines, scatters, bars — concrete usage

**Line plots** (the most common):

```rust
Plot::new("cpu_usage").show(ui, |plot_ui| {
    for (i, series) in series.iter().enumerate() {
        plot_ui.line(
            Line::new(PlotPoints::Borrowed(&series.points))
                .color(tokens::qualitative_cycle(i))
                .name(&series.name)
                .stroke(Stroke::new(tokens::STROKE_REGULAR, tokens::qualitative_cycle(i))),
        );
    }
});
```

`qualitative_cycle(idx)` reads from the `batlowS` LUT ([ADR-0031 §SD7](../../adr/0031-imzero2-design-system-color.md)); the i-th series gets the i-th color, deterministically. No random colors, no hand-picked hex values. `PlotPoints::Borrowed` avoids per-frame allocation when the data lives in app-owned storage.

**Secondary encoding** is mandatory per [ADR-0031 §SD6](../../adr/0031-imzero2-design-system-color.md) — color must not be the only series-distinguishing channel:

- Lines also vary by **style** — `LineStyle::Solid`, `LineStyle::Dashed`, `LineStyle::Dotted`. IDS convention: primary series solid; secondary / projected series dashed; comparison / baseline series dotted.
- Points also vary by **marker shape** — `MarkerShape::Circle`, `Square`, `Triangle`, `Diamond`. Cycle marker shape independently of color so CVD readers can still distinguish.

**Sparklines** are inline mini-plots inside table cells or status bar entries. They share the plot machinery but with most chrome stripped:

- No axes shown
- No grid
- No legend
- Single line, single color (`info.default` default)
- Small fixed dimensions: 60 × 16 px typical (Standard density)
- Optional endpoint marker for the current value

Sparklines communicate *trend over time*; their job is to give context to a numeric value in a table cell ("this number, recent history"). They are not a substitute for a full plot when the trend itself is the primary information.

### Axes

**Time axis.** For any X axis representing time, use `boxer/public/math/numerical/timeticks` for tick generation. The ticks adapt to zoom level (nanoseconds at the top end; decades at the bottom); labels format conditionally (`14:23:05` at second scale; `2026-05-14` at day scale; `2026-Q2` at quarter scale). Time zone is UTC by default — apps that display local time document the choice in their manifest.

**Numeric axis.** Talbot via `finddivisions`. Always populate `DefaultWeights`; set `FastMode: true` for interactive-pan plots where every frame computes new ticks.

**Tick labels.** `Caption.Mono.Numeric` for tabular figure alignment — numeric ticks at the same magnitude vertically align (digits stack). `text.secondary` color so they don't compete with the data. Density preset scales the size per [ADR-0030 §SD3](../../adr/0030-imzero2-design-system-typography.md).

**Axis labels** (the line `"CPU %"` or `"Bytes / second"` beside the axis). `Caption` proportional, `text.secondary`. Position: below the X axis (centered); rotated 90° to the left of the Y axis. **Units are mandatory** — Tier 2 rubric V5 flags missing units.

**Grid lines.** `Stroke.Hair` (1 px) in `border.faint` color. Density: visible at major tick positions only; minor ticks have no grid lines. Some plot types disable the grid entirely (sparklines, very dense scatter plots) — that is a per-plot choice.

**Log scale.** Opt-in for numeric axes spanning multiple orders of magnitude. Apps that use log scale annotate the axis label with `(log)` suffix so the reader is not surprised.

### Color encoding details

**Sequential palette use** (`batlow` and alternates):

- Heatmaps — cell color from value via `sequential(SequentialE::Batlow, t)` where `t ∈ [0, 1]`.
- Density plots — alpha + color from density.
- Magnitude-shaded tables (per [patterns/tables.md](./tables.md)) — same mechanism.

**Diverging palette use** (`vik` and alternates):

- Signed deviations from a baseline (anomaly score, correlation, change-from-yesterday).
- The plot's midpoint must correspond to `t = 0` in `diverging(DivergingE::Vik, t)`; non-symmetric data ranges still center the diverging midpoint at the baseline value, even if visual coverage is asymmetric.
- Always show a midpoint label in the legend ([patterns/status-and-legends.md](./status-and-legends.md) — diverging continuous legend anatomy).

**Qualitative palette use** (`batlowS`):

- Series identity in line / scatter / bar / stacked / step plots.
- Up to 10 colors via `qualitative_cycle(i)`. Beyond 10, repetition occurs — consider whether the plot has too many series to be readable. Down-sample series count if so.
- Per-app series-color registry recommended: if the same logical series (`cpu.user`) appears in three plots within one app, it should use the same color in all three. The registry is a `HashMap<SeriesName, u8>` mapping series-name to qualitative-cycle index; populated as series are first encountered in a panel.

**Semantic palette use** (info / success / warning / error / neutral / accent):

- *Only* for plot annotations and reference lines, never for series identity:
  - `HLine` at threshold → `Stroke.Regular` `semantic.warning.default`
  - `HLine` at hard limit → `Stroke.Strong` `semantic.error.default`
  - Highlighted "alert region" `Polygon` → `semantic.warning.subtle` fill, no stroke
  - "Selected range" `Polygon` → `semantic.accent.subtle` fill — overlapping the active brushed selection
- Series themselves are *never* `semantic.error.default` red just because the values look bad. Semantic colors signal *meaning we have assigned*, not data values.

### Legends

Plot legends follow [patterns/status-and-legends.md](./status-and-legends.md) — discrete legend anatomy for line / scatter / bar; continuous legend (gradient bar) anatomy for heatmaps / density plots.

Placement (recap):

| Plot dimension / series count | Legend placement |
|---|---|
| Small plot (< 200 px tall), few series | right of plot, vertical |
| Wide plot (> 600 px), few series | below plot, horizontal |
| Dense plot (≥ 8 series) | below, collapsible |
| Heatmap | right, vertical gradient bar |
| Multiple plots, shared series | shared legend at panel level |

`egui_plot`'s built-in `Plot::legend(Legend::default())` is acceptable for prototypes but IDS prefers external legends (rendered by app code in adjacent layout space) for tight control over typography, placement, and series-visibility toggles. The built-in legend ignores IDS typography tokens; the external legend uses `Caption` proportional consistently.

**Series-visibility toggle.** Clicking a legend entry toggles that series' visibility on the plot. The toggled-off entry shows a strike-through and reduced opacity in the legend; the plot redraws without that series. Reduced-motion respected — instant toggle, no fade.

### Interaction

- **Pan.** Click + drag inside plot area. `Motion.Quick` smoothing when reduced-motion is disabled.
- **Zoom.** Mouse wheel for proportional zoom around cursor; modifier + wheel for axis-specific zoom; box-select (drag with modifier) for region zoom.
- **Reset zoom.** Double-click inside the plot area; visible button in the panel header is also acceptable.
- **Hover.** Mouseover a data point shows a tooltip with the point's X, Y, and series name. Tooltip uses `Caption.Mono.Numeric` for the values, `Caption` for the name, `bg.surface` background with `Stroke.Hair` `border.default`.
- **Crosshair** (optional). A vertical line at the mouse X position with simultaneous tooltips per series. Useful for time-series with many series; off by default.
- **Reduced motion.** Pan and zoom transitions complete instantly; no smooth animation. Per [ADR-0032 §SD5](../../adr/0032-imzero2-design-system-spacing-density-motion.md).

### Annotations

- **`HLine`** — horizontal reference line. Use for thresholds (`Stroke.Regular` `semantic.warning.default`), hard limits (`Stroke.Strong` `semantic.error.default`), zero baselines (`Stroke.Hair` `border.default`).
- **`VLine`** — vertical reference line. Use for events (deploy times, alert timestamps), current-time markers (`Stroke.Hair` `semantic.accent.default`).
- **`Polygon`** — filled region. Alert zones, selected ranges, confidence bands. Always use the `subtle` emphasis of the chosen semantic role; default fills are too prominent.
- **`Text`** — in-plot label. Use sparingly; legends are usually better. When used, `Caption` size, `text.primary` color with `bg.surface` semi-transparent background pad.

### Multi-pane composition

Dashboards with multiple plots sharing a time range (the Grafana-replacement target) need axis synchronisation. `egui_plot::Plot::link_axis(group_name, link_x, link_y)` ties multiple plots together so pan / zoom on one propagates to all. IDS convention:

- Time-series dashboards: link X axis fleet-wide within the dashboard (`link_axis("dashboard_time", true, false)`).
- Sibling plots showing the same data axis: link Y as well.
- The time-range picker (forthcoming pattern doc) is the *primary* source of truth for X range; linked plots follow.

### Empty state

A plot with no data renders the plot frame (axes, grid) plus a centered "no data" message — see `patterns/empty-states.md` *(forthcoming)*. The reason should be specific: `"no data in this time range"`, `"loading..."`, `"data source unreachable"`. Status icon prefix per [patterns/status-and-legends.md](./status-and-legends.md). Avoid blank-plot-with-nothing — Tier 2 rubric V3 flags it.

### Streaming data

Plots that update in real-time (e.g., `imztop` CPU sparkline) face a UX hazard: data appends while the user is panning or zoomed in. Conventions:

- When zoom is at default (auto-fit), follow new data — the right edge advances.
- When user has zoomed or panned, *do not auto-follow* — preserve the user's view. Show a "live" indicator in the panel header.
- Click the "live" indicator to re-enter auto-follow mode.

This is essentially the Grafana / Prometheus convention; reinventing it would confuse operators familiar with that idiom.

### Density behavior

| Element | Tight | Standard | Roomy |
|---|---|---|---|
| Plot margin (panel ↔ plot frame) | `Padding.Tight` (6) | `Padding.Outer` (12) | `Padding.Loose` (16) |
| Axis tick label gap | `Padding.Hair` (2) | `Padding.Inner` (4) | `Padding.Tight` (6) |
| Legend swatch ↔ label | `Gap.Inline` (4/6/8) | per density | per density |
| Grid line stroke | `Stroke.Hair` (1) | constant | constant |
| Sparkline width × height | 48 × 12 | 60 × 16 | 80 × 20 |

Tick label sizes follow the density type-scale per [ADR-0030 §SD3](../../adr/0030-imzero2-design-system-typography.md) — Caption is 11 pt at Standard, 10 at Tight, 12 at Roomy. The `tnum` feature on Aile / tabular figures on IDS Mono is invariant; column-width alignment holds across densities.

### Performance

- **~100 k visible points per pane** is the design target ([`project_grafana_replacement`]); beyond that, down-sample upstream in SQL (M4-like aggregation) before the plot sees the data.
- **`PlotPoints::Borrowed(&[..])`** for series data owned by the app — avoids per-frame allocation. The alternative `PlotPoints::Owned(Vec<..>)` allocates on every paint.
- **Series count.** ~10 series visible is the soft cap (matches `batlowS`); ~20 is the hard cap before legend collapsing becomes mandatory. Beyond 20, the plot is communicating something else (a small-multiples view is probably the right abstraction).
- **Tick caching.** Talbot tick generation is fast per-frame but can be cached when zoom/pan is idle. Boxer timeticks similarly. Cache invalidation: any range change.
- **Heatmap cell count.** Cap at ~10 k cells (e.g., 100 × 100). Beyond that, render the heatmap to a texture once and blit; egui_plot supports image overlays via `PlotImage`.

## Invariants

- Color palette type matches encoding intent: qualitative for categorical series; sequential for ordered magnitude; diverging for signed deviation. Mixing these is a Tier 2 V2 violation.
- Every plot has an X-axis unit and (for non-categorical Y) a Y-axis unit visible somewhere — axis label, legend, or panel header. Missing units is a V5 finding.
- Time-series plots show the displayed time range explicitly — either in the legend, the panel header, or via a linked time-range picker.
- Series colors come from `qualitative_cycle(idx)`; raw color literals are banned (Tier 1 L2).
- Secondary encoding is mandatory beyond ~3 series: line style or marker shape varies independently of color.
- Annotation colors (HLine, VLine, regions) come from the *semantic* palette, never `batlowS`. Series colors come from `batlowS`, never the semantic palette. The two never collide in one plot.
- Per-app series-color registry: the same logical series (`cpu.user`) uses the same qualitative-cycle index in every plot within the app.
- Reduced-motion preference respected — pan / zoom complete instantly when set.
- Streaming plots that have been panned / zoomed by the user do not auto-follow new data.
- `PlotPoints::Borrowed` used wherever data lives in app storage; per-frame `Vec` allocation is a code smell.

## Trade-offs

- **`egui_plot` built-in legend vs. external IDS legend.** Built-in is one line of code and respects nothing about IDS typography; external is more code but consistent. Default: external for any plot that ships in IDS-conformant apps; built-in acceptable for internal debugging panels.
- **Sparkline vs. full plot.** Sparklines convey trend in minimal pixels; full plots convey trend plus magnitude plus scale. Default: sparkline when the value next to it carries the magnitude; full plot when the plot is the primary information.
- **Crosshair vs. per-point hover tooltips.** Crosshair gives simultaneous read across all series at one X; per-point tooltips give precise value at one (X, Y). Default: per-point tooltips by default; crosshair as an opt-in panel-level toggle for time-series dashboards.
- **Step plots vs. interpolated lines for discrete state data.** Interpolated lines lie when the data is a sequence of discrete state changes (process state, log level over time). Step plots are more honest but visually busier. Default: step plot for state data; interpolated line for sampled continuous data.
- **Per-app vs. fleet-wide series-color registry.** Per-app is simpler; fleet-wide makes cross-app comparison easier. Default: per-app for v1; revisit if cross-app dashboards become common.
- **Stacked area vs. multiple lines.** Stacked area is good for composition over time (memory by category, requests by status code); multiple lines are better for comparison. Default: lines unless the composition matters (i.e., the sum carries meaning, not just the individual series).
- **Auto-follow vs. preserve-view on streaming data.** Auto-follow is the "live" convention; preserve-view is the "investigating" convention. The user signals intent by panning / zooming — apps interpret that signal correctly. Default: preserve-view after any user-initiated viewport change; "live" indicator + click to resume.

## Anti-patterns

- Qualitative palette (`batlowS`) for a heatmap (`batlow` is required).
- Sequential palette (`batlow`) for series identity (use `batlowS`).
- Diverging palette (`vik`) without a clear midpoint (every value is "deviation from what?").
- Random per-series colors (use `qualitative_cycle(idx)` for determinism + CVD safety).
- Hardcoded `Color32::from_rgb(...)` for series — Tier 1 L2 catches.
- 3D effects, drop shadows, perspective on bars (decorative — Swiss minimalism rejects).
- Pie charts (avoid — bar charts communicate the same composition more legibly).
- Plot legend rendered inside the plot area (occlusion).
- Plot without axis units (Tier 2 V5).
- Time-series plot without an explicit time-range label.
- Sparkline with a legend (sparklines have no legend by definition; they are inline context).
- Plot with > 20 series visible (small-multiples is the right pattern instead).
- Rendering > 100 k raw points without upstream down-sampling.
- Auto-following data while the user has panned (preserves nothing, frustrates investigation).
- Using `semantic.error.default` red for a "bad" series — semantic colors signal meaning we assigned, not data badness.
- Polygon fills at `default` emphasis (too prominent; use `subtle` for annotation regions).

## Further reading

- [ADR-0029 — design system + policy-as-code](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — parent framework; Tier 2 rubric V2 (color-encoding), V5 (legend completeness), V8 (animation feel) are the load-bearing graders for plots.
- [ADR-0030 — typography](../../adr/0030-imzero2-design-system-typography.md) — `Caption.Mono.Numeric` for tick labels; `Caption` for axis labels and legend.
- [ADR-0031 — color foundations](../../adr/0031-imzero2-design-system-color.md) — Crameri / viridis data-encoding §SD3; `qualitative_cycle` / `sequential` / `diverging` API §SD7; semantic palette §SD2.
- [ADR-0032 — spacing / density / motion](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — `Padding.Outer` for plot margin; `Stroke.Hair`/`Regular`/`Strong` for grid / axes / annotations; `Motion.Quick` for interactions.
- [patterns/tables.md](./tables.md) — magnitude shading uses the same Crameri sequential / diverging machinery as heatmaps here.
- [patterns/status-and-legends.md](./status-and-legends.md) — discrete and continuous legend anatomy; status-icon catalogue used in plot annotations.
- patterns/time-range-picker.md *(forthcoming)* — primary source of truth for time-series X range; linked plots follow.
- patterns/empty-states.md *(forthcoming)* — "no data" overlay for empty plots.
- [`egui_plot`](https://docs.rs/egui_plot) — upstream substrate; `Plot`, `Line`, `Points`, `BarChart`, `BoxPlot`, `HLine`, `VLine`, `Polygon`, `PlotPoints::Borrowed`.
- `boxer/public/math/numerical/timeticks` ([`reference_boxer_timeticks`]) — calendar-aware time axis tick generator.
- `boxer/public/math/numerical/finddivisions` ([`project_finddivisions_talbot_weights`]) — Talbot numeric axis tick generator; *always populate `DefaultWeights`*.
- Crameri, F. (2018). *Scientific colour maps* (Version 8.0.1) [Zenodo](https://doi.org/10.5281/zenodo.1243862) — source for `batlow`, `vik`, `batlowS` ([ADR-0031](../../adr/0031-imzero2-design-system-color.md) §SD3).
- van der Walt & Smith (2015). *viridis family.* [matplotlib colormap rationale](https://bids.github.io/colormap/).
