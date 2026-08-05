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

`play` gained its first hierarchy panel with
[ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md): the Icicle tab, which
reads a result as depth rows and keeps the order of a path. The other
space-filling form — nested rectangles whose *area* is the value — has had a
widget in the tree for far longer (`widgets/treemap`: squarified layout, drill
navigation with breadcrumbs, an animated zoom, a pluggable `ColoringI`) and has
never had a `play` panel.

One was proposed and dropped. The
[pprof-as-data](../adr-background-work/pprof-profiles-as-data.md) ladder's M4
was a treemap result panel; it was cut on 2026-08-04 (`aae43bac`) once the
icicle shipped, because for *stack profiles* the icicle answers the same
question on the same input and keeps the path order a treemap discards. That
kill was explicit about its own scope:

> This is a judgment about *profiles*, not about treemaps. […] a hierarchy whose
> reading really is "what is big" with no meaningful path order […] is a live
> reason to revisit. Nothing here blocks that; it would just want its own
> motivation rather than this ladder's.

The motivation is an upcoming capability-management surface. Its inventory is a
hierarchy in which the containment is real and the sibling order is not: what
matters is which subtree is large, and the path from the root is a grouping
rather than a sequence. That is the reading the survey held the door open for,
and it is the reading the icicle is worst at — depth rows spend the whole x
axis on a total whose ordering carries no information.

Two things make the panel cheap and one makes it not:

- The widget exists, so no new render surface is needed. This is the "thin
  adapter" the survey costed at 400–700 lines.
- Icicle's column contract is already generic. `resolveIcicleColumns` and its
  two builders are questions about column names that produce a flat
  `{Labels, Parents, Self}` tree; nothing in them is about depth rows.
- The widget carries a defect that ADR-0160 §SD1 named and routed *around*
  rather than fixing: `layout.Node.TotalSize()` sums a node's children and
  **ignores the node's own `Size`**. Every contract this panel would accept can
  put a value on an interior node, so the defect is now load-bearing.

## Design space (QOC)

**Question.** What is the smallest *generic* treemap panel over the existing
widget, given the contract the Icicle tab already defines?

| Criterion | Why it matters |
| --- | --- |
| C1 — reuse of the shipped widget | drill navigation, breadcrumbs, the zoom tween and the coloring seam are the bulk of an interactive treemap |
| C2 — one hierarchy contract, not two | a query that draws as an icicle should draw as a treemap; two resolvers drift on the first added column |
| C3 — value conservation | a picture scaled against a total must not quietly lose part of it |
| C4 — a second measure has somewhere to go | area is one channel; the consumer sizes by count and reads a class or a severity |
| C5 — cost per cell | cells are egui `Frame`s with `SenseClick`, not batched rects — cell count is the budget, not node count |

## Decision

### SD1 — One hierarchy contract, hoisted out of the Icicle panel

`play_hierarchy.go` owns the contract both panels resolve against, generalised
from ADR-0160 §SD9 without changing what it accepts:

- **Folded** — `stack` (a list) + `value`: one row per root-to-leaf path,
  interned into a trie with the interior nodes synthesised.
- **Nodes** — `id` + `parent` + `value`, optional `label`: one row per node, and
  the only one of the two in which an interior node can carry a value of its
  own.

Plus `unit` (labels a quantity) and the `color` of SD2. A list-typed `stack`
still wins when a schema satisfies both; the reject messages take the form's
name as a parameter, so the Icicle tab still says "flame view" and the Treemap
tab says "treemap".

Duplicating the resolver instead is rejected on C2: the `color` column of SD2
would have been the first divergence, on the first commit.

### SD2 — `color` is optional and dual-typed

A numeric `color` drives a continuous colormap (the `imztop` Proc Map idiom:
area is RSS, tint is CPU load). A string `color` drives the qualitative cycle
of [ADR-0156](./0156-qualitative-palette-dark-surface.md), assigned in first-seen
order and wrapping past the palette's seven hues — with the wrap *counted* in
the status line, since two categories sharing a hue is a claim the picture
cannot otherwise retract.

Absent the column, colour falls back to depth, which is the widget's own default
and encodes structure rather than a second measure.

Interior nodes have no `color` of their own in folded mode — they are
synthesised, and nothing in the result describes them. Left there, that has a
cost the first captures made obvious and the design did not: **at the default
nesting the picture was nearly colourless**, because SD5's frontier shows
containers and the colour lived on the leaves below them. The encoding was only
visible under `full`, which is not where a reader starts.

So an undescribed node **inherits** its colour from its descendants. A node's
own colour always wins; inheritance fills silence and never overwrites what the
query said. The two arms aggregate differently because the types do:

- **Numeric** — the value-weighted mean of the children's effective colours,
  weighted by the total each child occupies, since area is what the reader is
  comparing. An unweighted mean would let a sliver outvote the subtree beside
  it. Descendants with no colour are excluded from both sums rather than
  counted as zero.
- **Categorical** — inherited only when the described descendants **agree**. A
  mean of nominal categories does not exist, and the modal one would claim a
  category for a container that has several. A mixed container stays on the
  depth ramp, which makes neutral mean "look inside" — a reading, where
  "mostly `fs`" would be a claim the query never made.

Both are counted in the status line (`N coloured from below`, `N mixed`), which
is what makes the rule self-diagnosing: a picture that is still grey says how
much of it is genuinely heterogeneous rather than leaving the reader to guess
whether the encoding is working.

Inheritance cannot widen the colormap's range or add a category — a weighted
mean lies inside the range and an inherited key already exists — so the range
survey still reads the result's own colours alone.

**A legend, for the data mode only.** Numeric gets a `colorscale` gradient bar
with a computed tick axis; categorical gets a row of swatches in the cycle order
the hues were assigned, capped at twelve with the remainder counted. The depth
mode gets none: it encodes structure rather than identity, and a key mapping its
colours to depth numbers would be chrome explaining an axis nobody reads off.

The bar renders the **same `Colormap` instance** the cells are coloured from,
which is what `treemap.ContinuousColoringFromMap` exists for. A legend built
from the same two numbers rather than the same object would drift the moment
either side changed how it samples the palette, and a legend that disagrees with
the picture it explains is worse than no legend.

The legend sits above the canvas with the other readouts, for the reason
ADR-0160 §SD9 gives: a pane too short for the picture must not push the thing
that explains it out of sight.

A colour-free contract is rejected on C4: for the driving consumer, "how many"
and "of what kind" are two questions, and area answers only the first.

### SD3 — A node's own value counts, and is given a rectangle

Two halves, because the first alone is worse than neither.

**In `TotalSize()`** — an interior node returns `Size + Σ children` rather than
`Σ children`. The leaf fallback is untouched: a childless node with
`Size <= 0` still reports 1, which is what keeps an unweighted tree from
laying out as a division by zero.

**In `ComputeLayout` / `ComputeLayoutAt`** — when an interior node's own `Size`
is positive, it is squarified *as one more area* alongside its children and the
resulting rectangle is recorded for the node itself, readable as
`Layout.SelfRectOf(node)`. The widget paints it as a non-drillable cell carrying
the node's own name.

The second half is not decoration. `squarify` normalises the areas it is given
to fill the box exactly, so a parent whose own value merely inflated its total
would have that value silently redistributed into its children: correct sizing
against its siblings, and an over-stated picture the moment you drill in. This
is not hypothetical — `imztop` hit it, and `imztop_procmap_tree.go` hand-rolls a
synthetic self-leaf for exactly this reason, with a comment saying a heavyweight
parent with light children would otherwise read as tiny.

Fixing it in the panel instead — a synthetic `(self)` child, which is what
`imztop` does — was the cheaper option and is rejected on C3 and cohesion: it
puts an invariant of the layout in two consumers, invents `*Node`s that were
never rows, and leaves the next consumer to discover the same thing a third
time.

### SD4 — The widget gains `SetRoot`, and the panel does not reconstruct

`New` fixes the root at construction and there is no way to replace it, so a
host whose tree changes must build a new `*Treemap`. `SetRoot(root)` replaces
the tree and resets the breadcrumb to the root — the honest reset, since a drill
path is a list of `*Node` pointers into the tree that was replaced.

Reconstructing per result would also work and is rejected narrowly: `New` runs
`metrics.init`, whose text measurements settle a frame late, so a result swap
would cost a frame of mis-sized labels for no gain.

Re-resolving the drill path by *label* across a re-run — so re-executing the
same query keeps you where you were — is deferred (SD6); it is a real
convenience and it needs a rule for a path that no longer exists.

### SD5 — The panel binds the active result; navigation and selection are local

`apps/play/play_treemap_panel.go` is the **Treemap** dock tab, a `PanelI` over
`chMain` — the active result, as Table, World, Kanban and Icicle take it, rather
than a private CTE lane. One input needs no lane, and any query naming the
columns draws without being restructured.

The drill path is panel-local state and is not published: it is a position in a
view, not a fact about the result. A clicked cell publishes its label as
`selection_key`, matching Network, Sankey and Icicle and for the same reason —
in folded mode a cell is a path *prefix*, so it spans many rows and no single
row cursor is honest about it.

`maxNestingDepth` stays at the widget's default of 1 (the frontier's children
plus one preview level). That is what makes C5 tractable: the cells emitted per
frame are bounded by the frontier's fanout rather than by the tree's size, so
the node cap can stay generous while the frame cost stays flat. "Show
everything" is offered as a control, not as the default.

### SD6 — Deferred, deliberately

Recorded so they do not gate this cut:

- **Retiring `imztop`'s hand-rolled self-leaf.** SD3 makes it redundant, and
  removing it is a change to a shipped picture that wants its own capture to
  compare against.
- **Drill-path preservation across a re-run** (SD4).
- **A clickable legend.** Neither arm filters: a swatch does not isolate its
  category and the bar does not brush a range. Both are the obvious next
  gesture, and both want a decision about whether filtering is a view state or
  a published signal — which is the question SD5 answered for navigation and
  would have to answer again here.
- **A treemap on the implot custom-item lane.** Batched rects and pointer-
  anchored zoom would lift the C5 ceiling, at the cost of re-rolling drill
  navigation as a layout re-root. The Frame-based widget is what exists; this
  is what it would be replaced by, not extended into.
- **Ordering controls.** There are none to defer: `squarify` sorts by area
  internally and restores the caller's order only on the way out, so a cell's
  placement is a function of its value and nothing else. An ordering control
  would be a layout change, not a panel one.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `widgets/treemap/layout` (exported Go API under `public/`) | changed — `TotalSize()` counts a node's own `Size`; added `Layout.SelfRectOf` | see Migration: a no-op for every current caller |
| `widgets/treemap` (exported Go API under `public/`) | added — `SetRoot`; the self rect is painted as a cell | additive; a tree with no interior `Size` renders unchanged |
| `play` dock tabs | added — the **Treemap** tab: frozen `DockID` 24, a `ShapeContract` mark, `selection_key` in `Writes`, and the derived `BOXER_PLAY_FOCUS_TREEMAP` knob | tab-registry counts in `play_tabs_test.go`, `play_tab_marks_test.go`, `play_panes_menu_test.go`; `doc/env-vars.md` regenerates |
| `sqlapplet` result-panel roster | added — `treemap` in **both** `resultTabIDs` and `orderedResultTabIDs` | classified in neither list is what the tab-policy gate fails on (`1502e0e1`) |
| `play` internals | `resolveIcicleColumns` and the two builders move to `play_hierarchy.go` and gain a `color` column | package-private; the Icicle tab's accepted shapes are unchanged |
| egui2 IDL | **unchanged** | no new paint opcode |

`DockID` 24 rather than 23: [ADR-0163](./0163-play-timeseries-workbench.md)
proposes 23 for its Series tab and is still under review, so this takes the next
free id rather than editing a live proposal.

## Alternatives

- **A treemap on the implot lane.** Rejected on C1 for this cut and recorded in
  SD6 as the replacement it would be.
- **A synthetic `(self)` child in the panel.** Rejected in SD3 — cheaper, and it
  scatters a layout invariant across consumers.
- **Changing only `TotalSize()`.** Rejected in SD3 as worse than doing nothing:
  right against siblings, wrong on drill-in, and silent about both.
- **Duplicating the icicle resolver.** Rejected in SD1 on C2.
- **A row cursor rather than `selection_key`.** Rejected in SD5: a cell is a
  prefix, not a row.

## Consequences

### Positive

- A second reading of the same query. Any result that draws as an icicle draws
  as a treemap, with no restructuring, because SD1 makes it one contract.
- The layout defect ADR-0160 §SD1 routed around is fixed at its root, so the
  next consumer of `treemap/layout` inherits the fix instead of the workaround.
- `imztop`'s hand-rolled self-leaf becomes redundant (retirement deferred).
- Colour and area are independent channels, which is what the driving consumer
  needs and what the icicle has no room for.

### Negative

- `TotalSize()` is exported and its meaning changes. It is a no-op for every
  caller in this tree (Migration), but an out-of-tree caller that sets `Size` on
  an interior node would see its picture change without a compile error.
- The panel's cost model is fanout-bounded rather than culled: a node with
  thousands of direct children emits thousands of `Frame`s. The node cap and the
  minimum cell size bound it, and neither is as principled as the icicle's
  visible-range cull.
- The qualitative arm of SD2 wraps past seven categories. It is counted, not
  prevented, and a query with fifty categories gets a legible picture that is
  lying about seven of them.
- Two hierarchy panels is two tabs every applet negotiates per frame — the exact
  cost `aae43bac` cited when it dropped M4. It is paid here against a consumer,
  which is what that kill asked for.

### Neutral

- No IDL change and no new paint opcode; the widget already draws through calls
  that exist.
- The drill path is widget state, so the panel holds no navigation state of its
  own beyond the pin.

## Migration — Tier 1

**`TotalSize()` is a no-op for every current caller.** All five consumers set
`Size` on leaves only — `imztop` Proc Map (which routes a parent's own weight
through an explicit self-leaf child) and Topology, `sccmap`, `scctree`,
`leewaywidgets/topology_sink` — so `Size + Σ children` and `Σ children` agree on
every tree in this tree, and the existing `layout` tests pass unchanged. The
same holds for the self rect: it is emitted only when an interior `Size` is
positive, which no current caller produces.

Nothing else migrates: `SetRoot` and `SelfRectOf` are additive, and the panel is
new.

## Verification plan — Tier 1

- **`layout`** — `TotalSize` over an interior node with its own size, a leaf
  keeping its fallback, and a golden tree asserting the pre-change value for a
  tree whose interior sizes are zero (the Migration claim, as a test).
  `SelfRectOf`: absent when the size is zero or the node is a leaf; present,
  non-overlapping with every child rect, and area-proportional when it is not;
  and the children's rects still tile the box exactly.
- **The widget** — `SetRoot` resets the breadcrumb and leaves a stale drill path
  unreachable; the self cell is not drillable and does not appear for a tree
  without interior sizes.
- **The shared contract** — the icicle panel's existing resolver and builder
  tests keep every assertion they had, with only their three mode constants
  renamed, which is the evidence that hoisting changed nothing; plus new cases
  for `color` in both arms, the first-answer-wins conflict counter, the
  qualitative wrap counter, and a `color` column of an unusable type ignored
  rather than rejecting the draw.
- **The panel** — both contracts build the same tree the icicle builds; total
  conservation across the flat-to-pointer conversion including interior self
  values; the drop, truncate and cap counters; and the reject messages naming
  the treemap rather than the flame view.
- **Visual** — two tour scenes, both captured 2026-08-05.

  `08_treemap` takes the root view, the fully nested view and a drill-in, over
  the same population and the same contract as `08_icicle` so the two forms can
  be compared directly. It confirms the drill navigation and its breadcrumb, the
  pointer readout (`996 positions (24.2%) · 1 child(ren)`), off-path siblings
  keeping their space, and the colour ramp across the leaves.

  `08_treemap_self` exists for SD3 alone, because the first scene cannot reach
  it: every path there is exactly three deep, so no interior node carries a
  value and no self rect is emitted. It rolls the small tables of
  `system.parts` up into their database, and the capture shows `default`'s box
  holding `planes_mercator — 3.3G bytes` beside `default — 429.0M bytes`. That
  second cell **is** the invariant: without it the 429 MB would have been
  redistributed into the child, which would have read as 3.43 GiB.

  The colour inheritance of SD2 is what the first `08_treemap` capture forced:
  that pane was uniformly grey, and the same view now reads `43 coloured from
  below` with the fleets separated by altitude at the default nesting. That
  scene also carries the numeric legend.

  `08_treemap_category` is the categorical arm of both: the same fleets coloured
  by an altitude band, so the swatch key draws and the agree-or-neutral rule is
  visible — `3 categories · 30 coloured from below · 11 mixed`, with an operator
  flying one band taking its hue and one flying several staying neutral.

  The legend's size was a capture finding too. The widget spends 55% of its
  height on the gradient and the rest on labels, so the first attempt at 34 px
  clipped the tick labels mid-glyph and collided the last two; 360×48 with five
  desired ticks is what fits SI-suffixed labels.

  Two things the captures taught, neither of them predicted:

  - **A cell has no accessible name**, being an egui `Frame` with a label
    inside, so no locator resolves one and the drill click is the anchor
    ladder's last rung — a coordinate ([ADR-0127](./0127-imzero2-interaction-record-replay.md)
    §SD4). It has to land on the container's header strip: leaf-click sensing
    is on, so a click inside a child pins that leaf instead.
  - **The nesting control's second option is `full`, not `all`**, because a
    forest's synthetic container is named `all` and sits in the breadcrumb one
    row below it. Two meanings of one word in one pane, and a locator that
    could not tell them apart either.
- **IDS** — `designlint` over the changed packages, expected clean.

## Status

Proposed — 2026-08-05. Awaiting human review.

Built the same day: the `layout` change and its tests, the widget's `SetRoot` /
`SetColoring` / `SetMaxNestingDepth` / `WithSelfCellLabel` and the self cell, the
hoisted contract with the Icicle panel moved onto it, and the panel with its
plumbing, tests and snippets. SD3's second half was found while implementing
rather than while designing — the first draft of this ADR changed `TotalSize()`
alone, which is worse than changing nothing (see SD3) — so that decision records
what the code does rather than what was proposed.

Both tour scenes captured the same day, including the one that exists only to
show a self cell. SD2's inheritance was added after those captures showed the
default view was colourless, and re-captured. Nothing in the verification plan
is outstanding.

## References

### Method sources (clean room — papers and public documentation only)

| Source | Extracted |
| --- | --- |
| Shneiderman, *Tree visualization with tree-maps: 2-d space-filling approach*, ACM ToG 11(1), 1992 | the form: containment as nesting, quantity as area |
| Bruls, Huizing, van Wijk, *Squarified Treemaps*, Data Visualization 2000 | the layout already inlined in `treemap/layout`; that it normalises areas to the box is what SD3's second half is about |
| Stasko, Catrambone, Guzdial, McDonald, *An evaluation of space-filling information visualizations for depicting hierarchical structures*, IJHCS 53(5), 2000 | the form-choice evidence behind Context: nested areas beat depth rows when the task is magnitude rather than structure |

### Related ADRs

- [ADR-0160](./0160-imzero2-icicle-flamegraph-widget.md) — the Icicle panel: the
  column contract SD1 hoists, and the `TotalSize` mismatch its §SD1 named.
- [ADR-0156](./0156-qualitative-palette-dark-surface.md) — the qualitative cycle
  SD2's string arm assigns from.
- [ADR-0097](./0097-play-reactive-query-graph.md) — the channel contract and the
  `selection_key` signal SD5 publishes.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — the result-panel roster
  the new tab must be classified in.
- [ADR-0043](./0043-imzero2-timeline-widget.md) — where the receiver-owned
  composite-widget pattern `treemap` established is written down.
