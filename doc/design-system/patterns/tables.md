---
type: explanation
audience: IDS app authors and contributors
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# IDS pattern: tables

Tables are the central data surface in ImZero2 apps — process lists in `imztop`, regex matches in `regex_explorer`, time-series readings in the Grafana-replacement panels, query results in scratch SQL panels, audit-log views in the runtime. Most other patterns (status legends, plots, time-range pickers) sit *around* a table, not in place of one. The IDS table pattern is therefore the most consequential per-app decision after foundations.

This doc covers when to use which egui table mechanism, the typography and alignment rules per column type, density behavior, interaction conventions, and the constraints that egui-extras imposes on table authors. It is not a tutorial — for that, see the `regex_explorer` or `imztop` source. It is an *explanation* of why IDS tables look and behave the way they do.

## Background

**egui mechanisms.** egui itself ships no general-purpose data-table widget. Two mechanisms are available:

- [`egui::Grid`](https://docs.rs/egui/latest/egui/struct.Grid.html) — simple row-by-column layout. No virtualisation, no header semantics, no sortable columns, no row-level selection. Suited to ≤ 20 rows × ≤ 5 columns of fixed content (key-value forms, small fact tables, layout primitives).
- [`egui_extras::TableBuilder`](https://docs.rs/egui_extras/latest/egui_extras/struct.TableBuilder.html) — the data-table widget. Header + body + striped backgrounds, virtualised rows, configurable column sizing (Auto / AtLeast / AtMost / Remainder), per-cell click handling, scrollbar. Designed for thousands-to-millions of rows when virtualised.

For everything beyond a fact-table, IDS uses `TableBuilder`. `Grid` is reserved for layout (key-value detail panels) and tiny tables.

**Foundational dependencies.** Table rendering pulls from every foundations sub-ADR:

- Typography ([ADR-0030](../../adr/0030-imzero2-design-system-typography.md)) — `Body.Mono` for code/data cells, `Body` for prose, `Body.Numeric` for numeric columns (Aile `tnum` on, IDS Mono tabular by construction).
- Color ([ADR-0031](../../adr/0031-imzero2-design-system-color.md)) — semantic palette for status cells; Crameri sequential for magnitude shading; Crameri qualitative (`batlowS`) for categorical columns; never raw `Color32::from_rgb`.
- Spacing / density / motion ([ADR-0032](../../adr/0032-imzero2-design-system-spacing-density-motion.md)) — `Padding.Default` for cell padding, `Gap.Items` for row stride, density preset drives both.

**Why data-intensive tables are hard.** Three forces tug against each other: information density (more rows, more columns, smaller text), legibility (enough whitespace to scan), and interaction (clickable cells, sortable headers, range selection). Swiss-minimalist aesthetic resolves the tension toward density-and-legibility by removing decoration — no row backgrounds beyond faint zebra-striping, no heavy borders, no decorative icons in cells. The grid is implicit in the alignment, not drawn in pixels.

## How it works

### Mechanism choice

| Use case | Mechanism |
|---|---|
| ≤ ~20 rows of fixed, non-virtualised content | `egui::Grid` |
| Key-value form (label + value pairs, edit fields) | `egui::Grid` |
| Anything with sortable headers, row selection, or > 100 rows | `egui_extras::TableBuilder` |
| > 1000 rows | `TableBuilder` with virtualised body (`body.rows(...)`) |
| Read-only "small status" tables (current process state, recent events) | `TableBuilder` non-virtualised |

Custom layout via `ui.horizontal(...) + ui.vertical(...)` chains is *not* an IDS table. If a use case doesn't fit either mechanism, the right answer is a Tier 3 escalation ([ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) §SD10), not a hand-rolled grid.

### Anatomy

An IDS table has three parts:

- **Header row.** Column labels in `Body` (proportional, Medium 500 weight), `text.secondary` color. Sortable column labels carry a subtle caret on hover and a directional caret while sorted. Headers are non-scrolling — sticky at the top of the body.
- **Body rows.** Cell content in the typography appropriate to the column type (see below). Zebra-striping (alternating row backgrounds) optional and density-dependent: enabled in Standard density, disabled in Tight (too much visual noise) and Roomy (whitespace already provides separation).
- **Footer.** Optional. Totals, counts, pagination indicator. Same typography as header; non-scrolling at the bottom of the body.

No outer border. The table is delineated by its content and the surrounding panel's `border.default`, not by drawing a frame around the table itself.

### Column types and per-column conventions

Columns fall into recognisable types; each has typography, alignment, and width-sizing conventions.

| Type | Examples | Typography | Alignment | Sizing |
|---|---|---|---|---|
| Numeric | counts, percentages, durations, byte sizes, rates | `Body.Mono.Numeric` | right | fixed (computed from max-value width) |
| Identifier | UUIDs, hashes, paths, hostnames, file names | `Body.Mono` | left | fixed for full-form; truncate-with-ellipsis if narrowed |
| Code / expression | regex strings, SQL fragments, log lines | `Body.Mono` | left | Remainder (must be last column — see Invariants) |
| Timestamp | ISO 8601 or relative ("3 min ago") | `Body.Mono.Numeric` for ISO, `Body` for relative | right for ISO, left for relative | fixed |
| Status / categorical | `running`, `error`, `pending` | `Caption.Mono` for technical, `Caption` for human | left | fixed |
| Descriptive text | names, descriptions, comments | `Body` (proportional Aile) | left | Auto or Remainder |
| Boolean / flag | yes/no, on/off, present/absent | icon glyph from Nerd Font ([ADR-0030 §SD12](../../adr/0030-imzero2-design-system-typography.md)) | center | fixed (icon width + `Padding.Inner` × 2) |

**Right-alignment of numerics** is the data-density convention — decimal points and order-of-magnitude line up across rows, making outliers visually obvious without reading the digits. **Mono numeric fonts** (IDS Mono, or Aile with `tnum`) keep column-widths stable as values change.

### Color and shading

- **Default cell text** — `text.primary`.
- **Secondary cell text** (caption-class content) — `text.secondary`.
- **Zebra striping** — `bg.faint` for odd rows; `bg.panel` for even (Standard density only).
- **Selected row** — `bg.surface` background with `border.default` 1-px top + bottom strokes; cell text remains `text.primary` for contrast.
- **Hovered row** — `bg.faint` with `Motion.Quick` (80 ms) cross-fade from non-hover state.
- **Status cells** — semantic palette per status meaning: `semantic.info.default` (informational), `semantic.success.default` (healthy), `semantic.warning.default` (caution), `semantic.error.default` (failed). Cell color is the *fg* color; cell background stays default.
- **Magnitude shading** (optional, for ordered numeric columns) — Crameri sequential (`batlow` default) interpolated by row value, applied as a faint cell background (alpha ≤ 0.30). Useful for outlier detection in long columns; off by default.
- **Diverging shading** (optional, for signed numeric columns) — Crameri diverging (`vik` default) for columns representing deviation from a baseline.

Never reach for raw `Color32::from_rgb` in cell code. Tier 1 lint L2 ([ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) §SD8) catches it.

### Density behavior

Per [ADR-0032 §SD2](../../adr/0032-imzero2-design-system-spacing-density-motion.md), density resolves spacing tokens at startup. For tables:

- **Cell vertical padding** — `Padding.Hair` (2/2/4 px Tight/Standard/Roomy). The text baseline drives row height; padding is the visual breathing room above/below.
- **Cell horizontal padding** — `Padding.Inner` (2/4/6 px).
- **Inter-cell horizontal gap** — `Gap.Inline` (4/6/8 px) for the visible space between adjacent column edges.
- **Header height** — `Body` line-height + `Padding.Tight` (4/6/8 px) above/below.

Zebra striping enabled in Standard only — see Anatomy. Both Tight and Roomy rely on intrinsic spacing rhythm for row separation.

Density is fleet-wide per app ([ADR-0029](../../adr/0029-imzero2-design-system-and-policy-as-code.md) §SD3 / [ADR-0032](../../adr/0032-imzero2-design-system-spacing-density-motion.md) §SD1). A single app's tables share one density; mixed-density screens are a Tier 2 V4 finding.

### Interaction

- **Sort.** Sortable columns: header click toggles asc → desc → unsorted. One column at a time (multi-sort out of scope for v1). Direction indicated by a caret glyph from Nerd Font (`\u{f0d8}` up, `\u{f0d7}` down). Unsorted columns show no caret.
- **Select.** Single-row selection: click cell or row. Multi-row selection (when supported): shift-click extends range; ctrl-click toggles individual rows. Selection state is app-owned; the table widget signals click events, not selection persistence.
- **Hover.** Row-hover cross-fade per Color section. Cell-hover (e.g., showing a tooltip for a truncated cell) uses `Motion.Quick` for the tooltip fade-in.
- **Focus.** Keyboard navigation (arrow keys) supported when the table consumes keyboard focus. Focused row shows `Stroke.Strong` (2 px) `semantic.accent.default` top + bottom borders.

### Performance

Three constraints from accumulated egui-extras experience — every IDS-conformant `TableBuilder` user must observe:

- **Skip during sizing pass.** The first frame of an `egui::Window` is a sizing pass; if `TableBuilder` runs during it, its column widths collapse to content widths and persist incorrectly. Pattern: `if ui.is_sizing_pass() { return; }` early in the panel body before building the table. See [`feedback_egui_extras_sizing_pass_bail`].
- **Remainder column must be last.** `egui_extras::TableBuilder` only honours `Column::remainder()` for the trailing column; non-trailing Remainder collapses to `AtLeast` and stops growing. The flexible column (typically code, descriptive text, or path) goes last in the column list. See [`feedback_egui_extras_remainder_must_be_last`].
- **`ClipContents(true)` on every column for responsive drag.** Column resize-drag is throttled to 8 px per frame unless `Column::clip(true)` is set. For data tables where the user expects 1:1 pointer-tracking on the resize handle, pass `ClipContents(true)` on every column. See [`feedback_egui_extras_clip_for_responsive_drag`].

For virtualised tables (> 1000 rows), use `body.rows(row_height, num_rows, |mut row| { ... })` rather than the iterative `body.row(...)` per-row API. The virtualised path renders only visible rows; row-height must be uniform (use a single computed height for the active density).

## Invariants

- The flexible (Remainder) column is the *last* column of the table, always.
- All cell colors come from IDS tokens (`text.primary`, `semantic.*`, `bg.faint`, Crameri palettes). No raw `Color32::from_rgb` in cell code.
- Numeric columns use a tabular-figure font (`Body.Mono.Numeric` or `Caption.Mono.Numeric`) and right alignment.
- Tables within one app share one density. Mixed-density across tables in the same app is a Tier 2 V4 finding.
- `TableBuilder` is skipped during `ui.is_sizing_pass()` — the first frame in an `egui::Window`.
- Every column passes `ClipContents(true)` when responsive resize-drag matters.
- Zebra striping is enabled only in Standard density; Tight and Roomy rely on intrinsic spacing.
- Status colors come from the semantic palette ([ADR-0031](../../adr/0031-imzero2-design-system-color.md) §SD2); data-magnitude shading comes from Crameri palettes ([ADR-0031](../../adr/0031-imzero2-design-system-color.md) §SD3). The two never collide on the same cell.

## Trade-offs

- **Virtualisation vs. simplicity.** Virtualised `body.rows(...)` requires uniform row height — apps that need variable-height rows (multi-line descriptions, expandable detail rows) lose virtualisation. The cutoff is empirical: > 1000 rows the perf win matters; < 100 rows variable height is fine; the middle band is judgement.
- **Sortable headers vs. simplicity.** Sortable columns add UI affordances (caret, click target, header-hover state) and per-column comparator code. Tables read by a human glancing at the data don't need sort; tables consumed as a working surface (regex matches, audit logs, process lists) do. Default: sortable when the natural ordering of the data is not the most useful order.
- **Magnitude shading vs. noise.** Sequential / diverging cell shading helps with outlier detection but adds visual weight. Shading every numeric column collapses the data-density advantage of right-aligned tabular figures. Default: shade *at most one* numeric column per table; pick the column where outliers most matter.
- **Density vs. legibility at the extreme.** Tight density at small DPI degrades cell padding below the legibility floor for body text. Mitigation: Tight tables should use `Body.Mono` (IDS Mono) for *all* cell text, not just numeric — the mono advance is narrower than Aile and the smaller text-render-floor of monospace tolerates Tight better than proportional does. This is a per-table override pattern, documented here, not a token change.
- **Zebra striping vs. Swiss minimalism.** Pure Swiss aesthetic argues against zebra-striping (visual noise without semantic content). Practical data-readability argues for it on tables ≥ 8 rows ≤ 30 rows where the eye drifts between columns. IDS lands on: enabled in Standard density only, where row separation is otherwise weakest.

## Anti-patterns

- Raw color literals for cell shading (caught by Tier 1 L2).
- Center-aligned numeric columns (defeats outlier detection).
- Putting the flexible column anywhere but last (egui-extras silently collapses it).
- Decorative use of Crameri palettes — shading every column, or using sequential palettes for categorical data.
- Sortable headers on tables with < 10 rows (UI overhead without payoff).
- Multi-line cells in virtualised tables (`body.rows(...)` requires uniform height).
- Hand-rolled "table" via `ui.horizontal(...)` chains for anything more than 3 rows.
- Mixed-density tables within one app (Tier 2 V4 finding).
- Disabling zebra striping in Standard density without a documented reason.

## Further reading

- [ADR-0029 — design system + policy-as-code](../../adr/0029-imzero2-design-system-and-policy-as-code.md) — parent framework; §SD11 documents the patterns layout.
- [ADR-0030 — typography](../../adr/0030-imzero2-design-system-typography.md) — `Body.Mono`, `Body.Numeric` token rationale.
- [ADR-0031 — color foundations](../../adr/0031-imzero2-design-system-color.md) — semantic palette + Crameri data-encoding.
- [ADR-0032 — spacing / density / motion](../../adr/0032-imzero2-design-system-spacing-density-motion.md) — `Padding.*`, `Gap.*`, density model.
- [`egui_extras::TableBuilder`](https://docs.rs/egui_extras/latest/egui_extras/struct.TableBuilder.html) — upstream API.
- [`egui::Grid`](https://docs.rs/egui/latest/egui/struct.Grid.html) — simpler grid widget.
- Memory: [`feedback_egui_extras_remainder_must_be_last`] — Remainder column ordering constraint.
- Memory: [`feedback_egui_extras_clip_for_responsive_drag`] — `ClipContents(true)` for responsive drag.
- Memory: [`feedback_egui_extras_sizing_pass_bail`] — skip TableBuilder during sizing pass.
