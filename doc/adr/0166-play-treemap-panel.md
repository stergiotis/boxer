---
type: adr
status: proposed
date: 2026-08-05
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0166: a Treemap panel for play — one hierarchy contract, and a node's own value

## Context

`play`'s first hierarchy panel was the Icicle tab
([ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md)): depth rows, keeping the
order of a path. The other space-filling form — nested rectangles whose *area* is
the value — has had a widget far longer (`widgets/treemap`: squarified layout,
drill navigation, an animated zoom, a pluggable `ColoringI`) and never had a
panel.

One was proposed and dropped — the
[pprof-as-data](../adr-background-work/pprof-profiles-as-data.md) ladder's M4,
cut on 2026-08-04 (`aae43bac`) because for *stack profiles* the icicle answers
the same question and keeps the path order a treemap discards. That kill scoped
itself to profiles and left the door open for "a hierarchy whose reading really
is *what is big* with no meaningful path order", wanting its own motivation.

That motivation is a capability-management surface: an inventory where
containment is real and sibling order is not — the reading the icicle is worst
at, since depth rows spend the x axis on an ordering carrying no information.

Two things make the panel cheap: the widget exists (the "thin adapter" the survey
costed at 400–700 lines), and icicle's contract is already generic — its resolver
and builders are questions about column names producing a flat
`{Labels, Parents, Self}` tree. One makes it not: the widget carries a defect
ADR-0160 §SD1 named and routed *around* rather than fixing —
`layout.Node.TotalSize()` sums a node's children and **ignores its own `Size`**.
Every contract this panel accepts can put a value on an interior node, so that
defect is now load-bearing.

## Design space (QOC)

**Question.** What is the smallest *generic* treemap panel over the existing
widget, given the contract the Icicle tab already defines?

| Criterion | Why it matters |
| --- | --- |
| C1 — reuse of the shipped widget | drill navigation, breadcrumbs, the zoom tween and the coloring seam are the bulk of an interactive treemap |
| C2 — one hierarchy contract, not two | a query that draws as an icicle should draw as a treemap; two resolvers drift on the first added column |
| C3 — value conservation | a picture scaled against a total must not quietly lose part of it |
| C4 — a second measure has somewhere to go | area is one channel; the consumer sizes by count and reads a class |
| C5 — cost per cell | cells are egui `Frame`s with `SenseClick`, not batched rects — cell count is the budget, not node count |

## Decision

### SD1 — One hierarchy contract, hoisted out of the Icicle panel

`play_hierarchy.go` owns the contract both panels resolve against, generalised
from ADR-0160 §SD9 without changing what it accepts:

- **Folded** — `stack` (a list) + `value`: one row per root-to-leaf path,
  interned into a trie with the interior nodes synthesised.
- **Nodes** — `id` + `parent` + `value`, optional `label`: one row per node, and
  the only one of the two in which an interior node can carry its own value.

Plus `unit` and the `color` of SD2. A list-typed `stack` still wins when a schema
satisfies both; reject messages take the form's name as a parameter, so the
Icicle tab still says "flame view".

Duplicating the resolver is rejected on C2: `color` would have been the first
divergence, on the first commit.

### SD2 — `color` is optional, dual-typed, inherited, and has a legend

A numeric `color` drives a continuous colormap (the `imztop` Proc Map idiom: area
is RSS, tint is CPU load) over a range the result is **surveyed** for unless the
query declares one; a string one drives the qualitative cycle of
[ADR-0156](./0156-qualitative-palette-dark-surface.md), assigned first-seen and
wrapping past its seven hues — with the wrap *counted*, since two categories
sharing a hue is a claim the picture cannot otherwise retract. Absent the column,
colour falls back to depth.

**Inheritance.** Interior nodes have no colour of their own in folded mode: they
are synthesised, and nothing describes them. Left there the default nesting is
nearly colourless, since SD5's frontier shows containers and the colour lives on
the leaves below — so an undescribed node inherits from its descendants. Its
*own* colour always wins, inheritance filling silence rather than overwriting
what the query said. The arms aggregate differently because the types do:

- **Numeric** — the value-weighted mean of the children's effective colours,
  weighted by the total each occupies, since area is what the reader compares. An
  unweighted mean would let a sliver outvote the subtree beside it.
- **Categorical** — only when the described descendants **agree**. A mean of
  nominal categories does not exist, and the modal one would claim a category for
  a container that has several; neutral instead means "look inside".

Both are counted (`N coloured from below`, `N mixed`), making the rule
self-diagnosing: a picture still grey says how much is genuinely heterogeneous.
Inheritance can neither widen the range nor add a category, so the range survey
still reads the result's own colours alone.

**The numeric range may be declared, and the channel may carry a unit.**
`color_min`, `color_max` and `color_unit` are optional, read by the same
first-answer rule as `unit`, and inert beside a categorical `color` — a nominal
set has no endpoints. Surveying is the right default for a measure whose range is
a property of *this result*; it is wrong for one whose range is a property of the
**measure**, a ratio being the obvious case. A coverage map spanning 12–68% would
otherwise paint 68% at the top of the ramp, reading as fully covered, and no two
runs of the query could be compared by colour. Only the query knows which kind it
has. Out-of-range values clamp to the palette ends rather than stretching it
(`colormap.Config.At`), which is what makes the pinned scale hold.

A pair that is non-finite or not strictly ordered is **rejected** — falling back
to the survey and saying so in the status line — rather than repaired the way a
degenerate *surveyed* range is widened: widening would invent an endpoint the
query did not ask for, and drawing quietly on the survey would put the picture on
a scale other than the one its author wrote down. The status line likewise marks
a declared scale as such, since a ramp pinned to 0–100 and one stretched over
12–68 draw the same cells and look identical.

`color_unit` is separate from `unit` because area and tint are different
measures: the coverage map's value is statements and its colour a percentage. It
is bounded shorter than `unit` — it suffixes every legend *tick*, where a long
one costs the whole bar rather than one line — and is spaced off the number only
when it begins with a letter, the convention that separates `9.0G bytes` from
`72.5%`.

A panel-side "scale: data / declared" control was rejected: the correct setting
is a fact about the query, so a reader would have to know to flip it, and it
would reset per pane rather than travel with the applet.

**Legend, for the data mode only.** Numeric gets a `colorscale` gradient bar,
categorical a row of swatches in cycle order capped at twelve; depth gets none,
encoding structure rather than identity. The bar renders the **same `Colormap`
instance** the cells use — what `treemap.ContinuousColoringFromMap` exists for —
since one built from the same numbers rather than the same object drifts as soon
as either side changes how it samples the palette. It sits above the canvas per
ADR-0160 §SD9: a pane too short for the picture must not push what explains it
out of sight.

A colour-free contract is rejected on C4: "how many" and "of what kind" are two
questions, and area answers only the first.

### SD3 — A node's own value counts, and is given a rectangle

Two halves, because the first alone is worse than neither.

**In `TotalSize()`** — an interior node returns `Size + Σ children`. The leaf
fallback is untouched: a childless node with `Size <= 0` still reports 1, which
keeps an unweighted tree from laying out as a division by zero.

**In `ComputeLayout` / `ComputeLayoutAt`** — a positive interior `Size` is
squarified *as one more area* alongside the children and recorded as
`Layout.SelfRectOf(node)`, which the widget paints as a non-drillable cell
carrying the node's own name.

The second half is not decoration. `squarify` normalises the areas it is given to
fill the box exactly, so a parent whose own value merely inflated its total would
have it redistributed into its children: correct against its siblings,
over-stated the moment you drill in, silent about both. Not hypothetical —
`imztop_procmap_tree.go` hand-rolls a synthetic self-leaf for exactly this
reason.

### SD4 — The widget gains `SetRoot`, and the panel does not reconstruct

`New` fixes the root at construction. `SetRoot` replaces the tree and resets the
breadcrumb — the honest reset, a drill path being `*Node` pointers into the tree
being replaced. Reconstructing is rejected narrowly: `New` re-runs
`metrics.init`, whose measurements settle a frame late, so a swap would cost a
frame of mis-sized labels for no gain.

### SD5 — The panel binds the active result; navigation and selection are local

`apps/play/play_treemap_panel.go` is the **Treemap** dock tab, a `PanelI` over
`chMain` — the active result, as Table, World, Kanban and Icicle take it, rather
than a private CTE lane. One input needs no lane, and any query naming the
columns draws without being restructured.

The drill path is panel-local and unpublished: it is a position in a view, not a
fact about the result. A clicked cell publishes its label as `selection_key`,
matching Network, Sankey and Icicle — in folded mode a cell is a path *prefix*,
so it spans many rows and no row cursor is honest about it.

`maxNestingDepth` stays at the widget's default of 1, which is what makes C5
tractable: cells per frame are bounded by the frontier's fanout rather than tree
size. "Show everything" is a control, not the default.

### SD6 — Deferred, deliberately

- **Retiring `imztop`'s hand-rolled self-leaf.** SD3 makes it redundant; removing
  it changes a shipped picture and wants its own capture to compare against.
- **Drill-path preservation across a re-run** (SD4) — needs a rule for a path
  that no longer exists.
- **A clickable legend.** Neither arm filters; both want a decision on whether
  filtering is view state or a published signal — SD5's question, asked again.
- **A treemap on the implot custom-item lane.** Batched rects and anchored zoom
  would lift the C5 ceiling, at the cost of re-rolling drill navigation as a
  layout re-root. What the widget would be *replaced by*, not extended into.
- **Ordering controls.** None to defer: `squarify` sorts by area internally and
  restores the caller's order only on the way out, so placement is a function of
  value alone. An ordering control would be a layout change.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `widgets/treemap/layout` (exported, under `public/`) | changed — `TotalSize()` counts a node's own `Size`; added `Layout.SelfRectOf` | see Migration: a no-op for every current caller |
| `widgets/treemap` (exported, under `public/`) | added — `SetRoot`, `SetColoring`, `SetMaxNestingDepth`, `WithSelfCellLabel`; the self rect is painted as a cell | additive; a tree with no interior `Size` renders unchanged |
| `play` dock tabs | added — the **Treemap** tab: frozen `DockID` 24, a `ShapeContract` mark, `selection_key` in `Writes`, the derived `BOXER_PLAY_FOCUS_TREEMAP` knob | tab-registry counts in `play_tabs_test.go`, `play_tab_marks_test.go`, `play_panes_menu_test.go`; `doc/env-vars.md` regenerates |
| `sqlapplet` result-panel roster | added — `treemap` in **both** `resultTabIDs` and `orderedResultTabIDs` | classified in neither list is what the tab-policy gate fails on (`1502e0e1`) |
| `play` internals | the icicle resolver and its two builders move to `play_hierarchy.go` and gain `color`, plus the optional `color_min` / `color_max` / `color_unit` | package-private; the Icicle tab's accepted shapes are unchanged, and it does not read the colour channel at all |
| applet column contract (`sqlapplet` books) | added — three optional column names a query may emit | additive: a query emitting none is surveyed exactly as before |
| egui2 IDL | **unchanged** | no new paint opcode |

`DockID` 24 rather than 23: [ADR-0163](./0163-play-timeseries-workbench.md)
proposes 23 for its Series tab and is still under review, so this takes the next
free id rather than editing a live proposal.

## Alternatives

Each decision carries its own kill clause; these are the rejected options and
where the reasoning sits.

- **A synthetic `(self)` child in the panel** (what `imztop` does) — cheaper, and
  rejected on C3 and cohesion: it puts a layout invariant in two consumers,
  invents `*Node`s that were never rows, and leaves the next consumer to
  rediscover it (SD3).
- **Changing only `TotalSize()`** — worse than doing nothing: right against
  siblings, wrong on drill-in, silent about both (SD3).
- **A treemap on the implot lane** — rejected on C1, recorded in SD6 as the
  replacement it would be rather than an extension.
- **Duplicating the icicle resolver** (SD1, on C2); **a row cursor rather than
  `selection_key`** (SD5 — a cell is a prefix, not a row); **modal-category
  inheritance** (SD2 — it would claim a category the query never stated).
- **A panel control for the colour range**, and **inferring a percentage from a
  column named `pct`** — both rejected in SD2: the first puts a fact about the
  query in the reader's hands, the second is magic that breaks on the first
  ratio spelled differently.

## Consequences

### Positive

- A second reading of the same query: anything that draws as an icicle draws as a
  treemap unrestructured, because SD1 makes it one contract — with colour and
  area as independent channels, which the icicle has no room for.
- The layout defect ADR-0160 §SD1 routed around is fixed at its root, so the next
  consumer inherits the fix rather than the workaround.

### Negative

- `TotalSize()` is exported and its meaning changes. A no-op for every caller in
  this tree (Migration), but an out-of-tree caller that sets `Size` on an
  interior node would see its picture change without a compile error.
- The cost model is fanout-bounded rather than culled: a node with thousands of
  direct children emits thousands of `Frame`s, bounded only by the node cap and
  the minimum cell size — neither as principled as the icicle's visible-range
  cull. The qualitative arm likewise wraps past seven categories, counted rather
  than prevented.
- Two hierarchy panels is two tabs every applet negotiates per frame, the exact
  cost `aae43bac` cited when it dropped M4 — paid here against a consumer, which
  is what that kill asked for.
- **A hierarchy that is really a DAG is flattened by the query, silently.** In
  node mode `parent` is one column, so a multi-parent node is reduced *before*
  the panel sees it, and having never seen the others it cannot count the loss
  the way it counts every other. In folded mode the node arrives once per path
  and becomes that many cells, its value counted per path so the total overstates
  the distinct one. Both are the query's to state. Not hypothetical: the driving
  corpus is a DAG, and flattening drops real edges.

### Neutral

No IDL change and no new paint opcode; the drill path is widget state, so the
panel holds no navigation state beyond the pin.

## Migration — Tier 1

**`TotalSize()` is a no-op for every current caller.** All five set `Size` on
leaves only — `imztop` Proc Map (which routes a parent's own weight through an
explicit self-leaf child) and Topology, `sccmap`, `scctree`,
`leewaywidgets/topology_sink` — so both readings agree on every tree here and the
existing `layout` tests pass unchanged. The self rect likewise is emitted only
for a positive interior `Size`, which no current caller produces. Nothing else
migrates: the additions are additive and the panel is new.

## Verification plan — Tier 1

- **`layout`** — `TotalSize` over an interior node with its own size and a leaf
  keeping its fallback; a zero-interior-size tree asserting the *pre-change*
  value, which is the Migration claim as a test. `SelfRectOf`: absent for a leaf
  or a zero size; otherwise non-overlapping with every child rect,
  area-proportional, and leaving the children tiling the rest exactly.
- **The widget** — `SetRoot` resets the breadcrumb and leaves a stale drill path
  unreachable; the self cell is inert and absent without interior sizes.
- **The shared contract** — the icicle panel's resolver and builder tests keep
  every assertion with only their three mode constants renamed, which is the
  evidence hoisting changed nothing; plus `color` in both arms, the
  first-answer-wins and wrap counters, and an unusable `color` type ignored
  rather than rejecting the draw. Plus the declared scale: overriding the
  survey, absent leaving the survey untouched, an unordered pair rejected and
  reported, inert beside a categorical `color`, and the unit's spacing rule.
- **The panel** — both contracts build the tree the icicle builds; total
  conservation across the flat-to-pointer conversion including interior self
  values; the drop, truncate and cap counters; inheritance (own-wins, weighting,
  transitivity, mixed-stays-neutral); the legend sharing the cells' colormap.
- **Visual** — four tour scenes. `08_treemap` (root, fully
  nested, drill-in) over `08_icicle`'s population and contract, so the two forms
  compare directly; it carries the numeric legend. `08_treemap_category` colours
  the same fleets by band, drawing the swatch key and the agree-or-neutral rule.
  `08_treemap_self` exists for SD3 alone, every path in the first scene being
  exactly three deep and emitting no self rect: it shows `default`'s box holding
  `planes_mercator — 3.3G bytes` beside `default — 429.0M bytes`, which without
  the self rect would have read as 3.43 GiB. The first three were captured
  2026-08-05.

  `08_treemap_ratio` (2026-08-17) is the declared scale: `system.parts`
  compression ratio, area in bytes and tint in per cent, reading
  `colour 0%–100% (declared scale)` over ticks `0% 25% 50% 75% 100%`. Its
  picture sits low on the ramp and largely dark, which is the evidence rather
  than a flaw — every table here compresses well, and a *surveyed* range would
  have spread those few points across the whole palette and drawn something
  colourful that meant nothing.

  A cell has **no accessible name** (an egui `Frame` with a label inside), so a
  drill click is the anchor ladder's last rung — a coordinate
  ([ADR-0127](./0127-imzero2-interaction-record-replay.md) §SD4) on the
  container's header strip. The scenes carry the rest of that reasoning.
- **IDS** — `designlint` over the changed packages, expected clean.

## Status

Proposed — 2026-08-05. Awaiting human review. Built and captured the same day;
nothing in the verification plan is outstanding. Two decisions record what the
code does rather than what was first proposed: SD3's second half, and SD2's
inheritance and legend — all three found by a capture rather than by design.

SD2's declared scale (`color_min` / `color_max` / `color_unit`) was added
2026-08-17, driven by a consumer: ADR-0169's coverage map, whose colour is a
ratio and whose first cut therefore bucketed it into six string brackets — the
panel's continuous arm existed all along, and what was missing was a way to say
that a percentage's ends are 0 and 100 rather than whatever the repository
happens to span. That applet now declares the scale and the brackets are gone.

Since 2026-08-15 the picture fills its leaf rather than following a fixed
aspect — the rule shared with the Sankey, Icicle and Network panes
(`apps/play/play_pane_box.go`), whose reasoning is in ADR-0159's 2026-08-15
Update. Two parts of it are this pane's own. It budgets the leaf for the
breadcrumb bar the widget draws *above* the container it is handed, since
filling a pane means covering the widget's own chrome as well. And it turns the
widget's summary line off — `treemap.WithStatusLine`, added for this and
defaulting to the old behaviour — because `pointerLine` above the picture
already reads a cell, in the result's unit where the widget's says bytes; that
line has been below the fold since the panel was built, and filling the pane
would otherwise have been what finally revealed it.

## References

### Method sources (clean room — papers and public documentation only)

| Source | Extracted |
| --- | --- |
| Shneiderman, *Tree visualization with tree-maps: 2-d space-filling approach*, ACM ToG 11(1), 1992 | the form: containment as nesting, quantity as area |
| Bruls, Huizing, van Wijk, *Squarified Treemaps*, Data Visualization 2000 | the layout already inlined in `treemap/layout`; that it normalises areas to the box is what SD3's second half is about |
| Stasko, Catrambone, Guzdial, McDonald, *An evaluation of space-filling information visualizations for depicting hierarchical structures*, IJHCS 53(5), 2000 | the form-choice evidence behind Context: nested areas beat depth rows when the task is magnitude rather than structure |

### Related ADRs

- [ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md) — the contract SD1
  hoists, and the `TotalSize` mismatch its §SD1 named.
- [ADR-0156](./0156-qualitative-palette-dark-surface.md) — SD2's qualitative cycle.
- [ADR-0097](./0097-play-reactive-query-graph.md) — the channel contract and the
  `selection_key` SD5 publishes.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — the result-panel roster.
- [ADR-0043](./0043-imzero2-timeline-widget.md) — the receiver-owned composite
  pattern `treemap` established.
