---
type: adr
status: proposed
date: 2026-08-06
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0172: a Chart panel for play — one contract, four marks

## Context

Every numeric picture `play` draws today comes from a panel that was built for
one *kind* of number:

- **Series** ([ADR-0163](./0163-play-timeseries-workbench.md)) — a value against
  a *time* axis. Its claim is typed: the first temporal column is x, every
  numeric column is a lane. A result whose x is a service name or a bucket index
  is rejected outright.
- **Distribution** ([ADR-0161](./0161-play-distribution-panel.md)) — the
  `series`/`n`/`ps`/`qs` quantile contract.
- **Projection** — a 2-D embedding scatter, fed by the UMAP lane.
- **Timeline** ([ADR-0043](./0043-imzero2-timeline-widget.md)) — events and
  spans, not magnitudes.

What is missing is the *plain* chart: a category against a count, a number
against a number, a two-key grid against a third value. That is the most
ordinary thing a `GROUP BY` produces, and the only way to read one in `play`
today is the Table tab.

The substrate arrived with [ADR-0149](./0149-implot-core-port-painter-lane.md):
`widgets/implot` ports `Bars`, `Line`, `Scatter` and `Heatmap` (with the
rect/texture routing of §SD5), `SetupAxisTicks` for a categorical axis,
`SetupAxisScale` for a log axis, and plot-space `Clicked()` readback. Nothing
new is needed below the panel — this ADR is a *binding*, not a widget.

## Design space (QOC)

| Question | Options | Criterion | Chosen |
| --- | --- | --- | --- |
| How is the result claimed? | typed detection · named columns · a `mark` column | ADR-0122 §SD1 — naming settles same-typed ambiguity | named, bare (`x`/`y`/`z`/`series`) |
| How are several series expressed? | wide (a column per series) · long (a `series` key) · both | both idioms reach a SQL result; neither converts in a line | both |
| Where does the mark come from? | chips · a `mark` column · inferred only | v1 wants no new contract surface | chips, seeded by type |
| Bars with several series | grouped · stacked · both | stacking has no base-aware bar in the port | grouped; stacking deferred |
| Heatmap cell geometry | ordinal keys · true numeric grid | a `GROUP BY x, y` result is a matrix, not a field | ordinal, said out loud |

## Decision

### SD1 — One contract, two readings, discriminated by `z`

The claim is **named columns**, bare, per the ADR-0122 §SD1 doctrine the Kanban
(`lane`+`title`), Distribution (`series`) and hierarchy (`value`) contracts
already follow. `x` is required in both readings; the presence of a numeric `z`
switches which reading applies.

**Lanes** — no `z`:

| column | Arrow type | required | meaning |
| --- | --- | --- | --- |
| `x` | any | yes | the abscissa key (see §SD2) |
| *any other numeric column* | Int8‥64, UInt8‥64, Float16/32/64 | ≥1 | one lane; **the column name is the legend label** |
| `series` | any | no | splits the rows into groups; stringified |

**Grid** — `z` present:

| column | Arrow type | required | meaning |
| --- | --- | --- | --- |
| `x` | any | yes | cell key along the columns |
| `y` | any | yes | cell key along the rows |
| `z` | numeric | yes | the cell value |

The names `x`, `y`, `z` and `series` are claimed *by name, not by type*: `series`
is never a lane even when it is numeric, and neither is `x`. In the lanes
reading a column named `y` is an ordinary lane — which is what makes the
degenerate `SELECT a AS x, b AS y` draw without ceremony.

Rejections are loud and name what *this* result carries, never a silent empty
plot:

- `z` present but not numeric, or `y` absent → the grid reading is named and the
  fix is a cast or a rename.
- no numeric column besides `x` → the lanes reading is named.
- in the grid reading, a repeated `(x, y)` pair → **the whole result is
  rejected**, naming the row and the duplicated cell, with `GROUP BY x, y` plus
  an aggregate as the fix. Last-wins would fabricate a matrix the query did not
  ask for.

A NULL value is not a rejection: it becomes NaN, and the port already emits one
polyline per NaN-free run (`render.go`), skips NaN markers, and skips NaN bars.
So a hole in a lane breaks the line rather than being interpolated across —
[ADR-0163](./0163-play-timeseries-workbench.md) §SD2's rule, inherited. A hole in
the grid is drawn in the colormap's `BadColor`, transparent by default, so the
plot's own grid lines show through it — a cell nothing filled must not read as
the low end of the ramp, which is a measurement.

> **Fixed en route, 2026-08-06.** That last sentence was not true when it was
> written. `colormap.Config.At` — the scalar path both of implot's heatmap
> routes take — did not share `Map`'s substitution of a non-number, so a NaN
> reached `paletteLerp` as a NaN palette position, where `int(NaN)` is
> `math.MinInt64` and the palette index panicked. **Any** sparse grid crashed
> the host, which the dense 24 × 7 tour scene could not reach. `At` now
> substitutes `BadColor` for NaN and ±Inf; out-of-range values still clamp to
> the palette endpoints there rather than substituting, because `At` is also
> the gradient sampler a legend walks and a treemap cell's fill, and the
> substitution colours default to transparent. `paletteLerp`'s low guard is now
> spelled `!(t > 0)` so a NaN lands on the first stop instead of indexing out
> of range — the same branch, identical for every non-NaN input.

### SD2 — The abscissa's type picks the axis; row order is the category order

- `x` **numeric** → a continuous axis on the raw values.
- `x` **temporal** (`TIMESTAMP`, `DATE32`, `DATE64`) → a continuous axis in
  `ScaleTime` (Unix seconds, UTC labels). Free from the port; the alternative
  would be stringifying a timestamp into a category, which sorts wrong.
- `x` **anything else** → a **categorical** axis: the distinct values in
  **first-appearance order** take positions 0, 1, 2, …, labelled through
  `SetupAxisTicks`.

First-appearance order rather than a sort, because a string has no intrinsic
order and the query's `ORDER BY` is the only order it *does* have — sorting would
silently override the author. Past `chartMaxTickLabels` distinct values the
labels are dropped (they would overplot into an unreadable band) and the status
line says so.

Grid keys follow the same rule with one exception: **numeric and temporal keys
sort ascending**, because they *do* have an intrinsic order and a `GROUP BY x, y`
without an `ORDER BY` arrives shuffled. Either way the cells are **ordinal** —
equal width over the distinct keys, not placed at their numeric positions. A
heatmap built from a two-key `GROUP BY` is a matrix; drawing it as a field would
imply a spacing the result does not carry. The status line states the shape
(`24 × 7 cells over the distinct x and y`) so the reading is never inferred from
the picture alone.

### SD3 — Marks are chips, gated and seeded by the resolved types

| reading | chips offered | default |
| --- | --- | --- |
| lanes, `x` categorical | Bar · Line · Scatter | Bar |
| lanes, `x` continuous | Line · Bar · Scatter | Line |
| grid | Heatmap | Heatmap |

Plus a `log y` **checkbox** (`log colour` in the grid reading), offered only when
every drawn value is strictly positive — a log axis over a zero is a picture of
nothing, and an absent affordance is cheaper to understand than a plot that
silently drops points.

The marks are chips because they are a mutually-exclusive set; the log scale is
a checkbox because it is an independent boolean. It shipped as a fourth chip and
was reported as *not toggling* — it did toggle, but a pressed-button state is
too weak a signal to read a boolean off, so the control looked inert while
working. The affordance now carries its own state.

**The auto-fit is padded** by `chartFitMargin` (5% of each axis's data span),
applied through `IncludeX`/`IncludeY` so a double-click refit and a legend
toggle still take the ordinary path. The port fits tight, as upstream does,
which puts the extreme sample exactly *on* the plot border — a scatter marker
there is drawn half outside and the tallest bar merges into the frame, which is
how it was reported. Two carve-outs, both about not asserting something the data
does not: a bar chart keeps its **baseline** (padding below would float the bars
and read as a truncated scale, so only the free end moves), and a log axis pads
**multiplicatively** (subtracting a linear margin from a log minimum walks
toward zero, which has no position on that axis).

The grid reading also carries a `colorscale` legend bound to the *same*
`colormap.Config` the heatmap colorizes through, which is that widget's
documented pairing. It takes its height out of the plot rather than adding it
below, so both readings occupy one vertical budget and the key never lands under
the leaf's fold — cells with no readable magnitude are a picture of nothing
either.

Line and Scatter stay available on a categorical axis. With the ticks labelled,
a line across categories is exactly as meaningful as the row order it follows,
and "line chart of the twelve months" is a chart people legitimately want.

The mark is panel-local state. A `mark` column that lets the SQL pin it is
attractive for the agentic surface [ADR-0161](./0161-play-distribution-panel.md)'s
dialogue named as the strategic goal, and is deferred (§SD6) rather than
guessed at.

### SD4 — Bars are grouped; stacking is deferred

With S series on a bar chart, each x slot of width W gives every series a bar of
width 0.8·W/S, series s centred at x + (s + ½)·(0.8W/S) − 0.4W. W is 1 on a
categorical axis and the smallest positive gap between distinct x values on a
continuous one.

Stacking is **not** in v1. `implot`'s `Bars` draws from y=0 with no base
parameter, so a stack must go through the custom-item lane — and that lane's
closures are invoked during `End`, after legend visibility is known, while the
*bases* would have to be computed at declaration time, before it. Hiding a middle
series would leave a gap under the ones above it. Fixing that means either a
base-aware bar item or hidden-state feedback in the port; both are widget work,
and neither should gate a chart panel (AGENTS.md — descope rather than gate).

### SD5 — The panel binds the active result; a click publishes the row cursor

`chartPanel` is a `PanelI` on `chMain` alone, an observer of the active result
like Table, Kanban and Distribution — no private lane, nothing to `Close`. The
fold is cached on `(executed, schema pointer)`, the Distribution panel's key.

Its rows **are** result rows, so a click publishes the ordinary `signalSelection`
row cursor and Detail follows it — unlike Network/Sankey/Treemap, whose marks are
path prefixes or vertices and which therefore publish a value instead. The
nearest point is taken in axis-normalised plot space (each axis divided by its
visible range) so "nearest" means nearest *on screen* at any zoom; in the grid
reading it is the cell under the click. Past a tolerance nothing is picked: a
click on empty space must not drag the whole dock's cursor to a far-off row.

The traffic runs both ways. Whatever row the shared cursor names is echoed as a
cross marker on the point that carries it, sharing the series' legend entry and
palette slot rather than adding one — so a row selected in the Table is findable
in the plot, not only the reverse.

**The grid answers to hover per CELL.** The port expresses the built-in legend
hover as a stroke-weight multiplier, and a heatmap has no stroke to thicken — it
is one legend entry standing for the whole grid, so hovering it has nothing to
single out, and it read as a dead affordance. The cell is the unit a reader is
actually pointing at, so it is what answers: the cell under the pointer is
outlined and annotated with both keys and the value. A cell no row filled says
`no row` rather than reporting one, the same distinction its transparent fill
makes. The outline is an anonymous `Custom` item — no legend row, no palette
slot, and no contribution to auto-fit, so a hover cannot move the axes.

> **Driver gap closed en route.** No hover affordance in *any* imzero2 app was
> reachable from an ADR-0154 trace: the carrier client had `MoveMouse` but the
> trace vocabulary never exposed it, so a scene could click a painter-lane
> target by coordinate and never point at one. A `hover` verb now wires it
> through (`carrierclient/trace.go`), which is what makes this capturable — and
> is why the heatmap shipped with no hover affordance at all.

Registration: tab `chart`, DockID 25, body zone, `Lazy` (a plot is a heavy body),
`ShapeContract: true` (its rejection is worth the dock strip's `×` mark),
`Writes: [signalSelection]`.

### SD6 — Deferred, deliberately

Stacked bars (§SD4) · a `mark` column (§SD3) · error bars and a second value
channel · reader-chosen colormap palettes (v1 pins Viridis) · the Series tab's
envelope decimator, in place of which the caps of §SD1 truncate and *count* ·
faceting into `implot` Subplots · a `chart(...)` macro in the
[ADR-0161](./0161-play-distribution-panel.md) `descriptiveStatistics` mould.

## Surfaces — Tier 1

| Surface | Change |
| --- | --- |
| SQL result contract | new: `x` (+ numeric lanes, optional `series`), or `x`/`y`/`z` |
| Dock tab | new `chart` / DockID 25 / "Chart", body zone |
| Env | `BOXER_PLAY_FOCUS_CHART` (derived from the tab def; `doc/env-vars.md` regenerates) |
| Signals | writes `selection` (+ the dispatcher's `selection_node` / `selection_id`) |
| Go API | `NewChartDriver(ids)`, `PlayApp.chartDriver` — the sibling drivers' shape |

No existing surface changes; no column name in the new contract collides with a
claimed one (`series` is shared with ADR-0161 by *name* only — that contract also
requires `n`/`ps`/`qs`, so no result satisfies both by accident).

## Alternatives

- **Extend the Series tab to a non-temporal x.** Rejected: its typed claim is
  load-bearing (§SD1 there), and widening it to "the first column, whatever it
  is" reintroduces exactly the same-typed ambiguity the typed claim avoids.
- **A `chart_*` prefix** (the Timeline's `_tl_*` style). Rejected in dialogue:
  six characters of ceremony on the most ordinary query shape in the app, to
  prevent an "accidental" claim that — for a result which really does carry x and
  y — draws the right picture anyway.
- **Wide-only or long-only.** Rejected: a `GROUP BY k` with several aggregates is
  natively wide, a `GROUP BY k, g` natively long, and forcing either through a
  pivot is work the panel can simply not require.
- **Inferring the mark with no chips.** Rejected: bar-vs-line over the same
  columns is a reader's judgement about what the numbers *mean*, not a fact about
  their types.

## Consequences

### Positive

- The commonest `GROUP BY` result becomes a picture with one alias, and the same
  query drives Table and Chart without restructuring.
- Nothing new below the panel: every mark, the categorical ticks, the log scale
  and the click readback are existing, demo-covered `implot` surface.
- The contract is small enough to state in the reject message itself, which is
  how the other shape-contract panels teach theirs.

### Negative

- `x` and `y` are common column names, so results that never meant to be charted
  will claim the tab. Mitigated only by the tab being one of many and its mark
  being informative rather than modal.
- Caps truncate rather than decimate, so a very large result is drawn partially
  and *counted*, where the Series tab would have drawn an envelope of all of it.
- No stacked bars, which is the second thing a reader asks a bar chart for.
- The grid reading rejects the whole result on a duplicated cell — strict, and
  deliberately so, but it will surprise someone whose `GROUP BY` was incomplete.

### Neutral

- A temporal `x` makes Chart and Series both claim the same result. That is a
  feature (two readings of one shape) and costs nothing: the claims are
  independent and neither tab is modal.
- The mark and the log toggle are session state, not persisted with the layout.

## Migration — Tier 1

None. The panel is additive: a new tab, a new driver field, and four roster pins
(`play_tabs_test.go` ×2, `play_panes_menu_test.go`, `play_tab_marks_test.go`)
that enumerate the built-in tabs and must grow with it. No existing query,
layout or contract changes meaning. DockID 25 is unused, so a persisted dock
layout from an older build opens unchanged and gains the tab.

## Verification plan — Tier 1

Unit (pure, no Arrow, no rendering where possible):

1. `resolveChartColumns` over a schema table — lanes accepted, grid accepted,
   every reject message reached, `series`/`x` never becoming lanes.
2. Category interning: first-appearance order, positions dense from 0, tick
   labels dropped past the cap.
3. Grid keys: numeric ascending, string first-appearance, row 0 = the top =
   the last y key, holes NaN.
4. Duplicate-cell rejection names the row and the cell.
5. Grouped-bar geometry: S bars fit inside 0.8·W, centred on the slot, and the
   ordering is stable in series order.
6. Nearest-point selection under axis normalisation picks the on-screen nearest,
   not the raw-distance nearest, on an anisotropic range.
7. Fold caching: same `(executed, schema)` does not re-fold; either moving does.

Live, through the ADR-0154 headless carrier — no compositor, so it runs where
the tour does. Six scenes were added to `scripts/dev/play-screenshot-tour.sh`
(`08_chart_bars`, `_series`, `_heatmap`, `_numeric`, `_reject`, `_duplicate`),
nine captures in all, which is also what keeps the panel in the gallery as it
changes:

8. **Done 2026-08-06.** `08_chart_bars` — the wide idiom over a
   `LowCardinality(String)` key: two lanes labelled by their own column names,
   grouped bars inside each category slot, the categorical axis labelled in row
   order, and the status line reporting `2 series · 24 points · x: \`x\`
   (12 categories, in row order)`. Its trace then clicks the **Line** chip and
   re-captures, which exercises §SD3's chip state on the same claim.
9. **Done 2026-08-06.** `08_chart_series` — the long idiom: three groups off a
   `series` column, each its own legend entry, over a `DateTime64` x that takes
   `ScaleTime` and labels UTC. Detail followed the cursor into the row, showing
   `x`, `series` and `y`.
10. **Done 2026-08-06.** `08_chart_heatmap` — a 24 × 7 grid with numeric keys
    ascending, key index 0 at the BOTTOM, the colorscale legend reading
    `0 … 6000`, and the ordinal-cells caveat in the status line. Detail resolved
    a cell to its result row.
11. **Done 2026-08-06.** `08_chart_numeric` — the continuous axis the two above
    cannot show: bar slots from the smallest gap, a real hole on the axis where
    a band is missing, and two `NULL`s ending a lane rather than dropping it to
    zero, counted in the status line as `2 nulls (the line breaks; nothing is
    interpolated)`. Its trace then clicks **Scatter** and **log y**, each one
    capture apart, which is where §SD3's remaining two chips are evidenced —
    the log axis over lanes spanning two orders of magnitude being the case it
    exists for.
12. **Done 2026-08-06.** The two reject TIERS, deliberately as separate scenes
    because they behave differently. `08_chart_reject` is schema-level: numbers
    but no `x`, so the panel never claims, the pane draws the contract plus the
    result's own columns back, and the dock strip carries the **`Chart -`**
    shape mark. `08_chart_duplicate` is data-level: an incomplete `GROUP BY`
    repeating every cell, where the panel DOES claim on the schema and then
    refuses the whole result in-pane — naming the row, the cell and the
    `GROUP BY x, y` that fixes it — with **no** strip mark, because the shape
    was never the problem.
13. **Done 2026-08-06, after the crash above.** `08_chart_heatmap_sparse` — 40
    of 112 cells with no row, held as holes and counted in the status line.
    This is the shape the dense grid scene could not reach and that no unit
    test covered until `TestChartGridHolesAreColourable`, which is why the
    panic reached a user rather than the tour. A capture of it is now a
    regression as much as a feature.
14. **Done 2026-08-06, from three use reports.** The padded auto-fit is visible
    in `08_chart_numeric` and its two chip captures — every extreme sample now
    sits inside the frame, including the log axis's minimum at y = 1, and the
    bar baseline stays welded to the axis. The log control appears as a
    checkbox, unchecked then checked, in the same pair. `08_chart_heatmap_sparse`
    gained two hover captures: a filled cell reading
    `x = 12 · y = 5 · z = 12` and a hole reading `x = 13 · y = 4 · no row`.
15. Still open: the log control's *negative* case (absent when a non-positive
    value is present), a click landing outside the selection tolerance, and the
    caps (row truncation, series cap, the 40k-cell reject).

## Status

Proposed. M0 is the whole panel as described — there is no useful smaller cut,
the contract being what makes it useful at all. Implemented and driven
2026-08-06 ahead of review, per the verification items above; the decisions
remain open to revision until the ADR is accepted.

## References

### Related ADRs

- [ADR-0149](./0149-implot-core-port-painter-lane.md) — the implot port this binds.
- [ADR-0122](./0122-play-kanban-panel.md) §SD1 — the named-columns doctrine.
- [ADR-0163](./0163-play-timeseries-workbench.md) — the temporal sibling, and the
  no-fabrication rule inherited here.
- [ADR-0161](./0161-play-distribution-panel.md) — the panel shape (chips, fold
  cache, loud data-level reject) this one follows.
- [ADR-0097](./0097-play-reactive-query-graph.md) — the tab registry and channel
  dispatch.
