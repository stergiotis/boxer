---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-02; not yet read by
> a second person.

> **Provenance.** Design-space survey, compiled 2026-09-02, ahead of any
> decision: nothing in here is settled, and an ADR — not this page — is where
> any of it would become one. Every claim about this repository was verified
> against the working tree on the compile date by reading the code; none was
> measured. Where a claim about egui / egui_table behaviour could not be
> verified from the bindings, it is marked *unverified*. There is no external
> literature in this page.

# Displaying co-arrays and ragged arrays in play — a design-space survey

## 1 Question and scope

play's Table pane renders every Arrow list cell of a leeway-shaped result as
`[len=N]`. For the two collection shapes leeway data arrives in —
**co-arrays** (several arrays positionally aligned, element *k* of each
belongs to instance *k*) and **ragged arrays** (one flat value stream
partitioned by a lengths lane) — the length is the least informative thing a
cell could say. This page maps how such cells could be made readable in
(a) the tabular view and (b) the detail view, what each option costs, and
which existing seams it would ride on.

Scope is **leeway-shaped results only**: results whose physical column
names parse, so that `CardDriver.ColumnClasses` gives the exact structure.
Nothing here infers structure from data or from aliases; a projected result
(an aliased `LW_*` expression, a `groupArray`) stays on the plain grid and
the ad-hoc detail path, unchanged, and is a separate enquiry if it becomes
one. Also out of scope: changing what a query returns, and the Schema pane.

## 2 The two shapes, as leeway lays them out

The vocabulary is the query algebra's
([leeway-query-algebra](../explanation/leeway-query-algebra.md) § Terminology):

- **co** — two lanes are co when they are indexed by the same axis. In a
  tagged section, every value column and every membership column is a
  co-lane of the section's instance axis: a DB row's value array has one
  element per attribute, and element *k* of the membership lane is the
  tag of element *k* of the value lane. A co-section group extends the
  alignment across sections that share a cardinality.
- **ragged** — a collection-valued attribute stored as one flat value lane
  plus a lengths lane (`card` / `len` / `cusum*`, role class *support*),
  with the invariant `arraySum(card) = length(vals)`. On leeway reads a run
  is never empty, so order-based picks are always defined.

Two consequences follow for display, and both are specific to leeway:

1. **The items of a co-array cell have names.** The section's membership
   lane gives every position a tag, so a readable rendering of a
   tagged-section value cell is `tag=value` pairs, not bare values. The
   driver already renders memberships (`membership.Renderer`, via
   `MembershipSinkI`); the per-DB-row grid simply does not ask for them.
2. **Raggedness is exact.** The support columns say where each attribute's
   run begins and ends; no boundary has to be guessed. A cell can say
   "3 attributes · 7 values" rather than one length.

Everything below reads structure from `streamreadaccess.ColumnClass`
(section, role class, canonical type) and from
`IntermediateColumnContext.CoSectionGroup` — the same per-schema cache the
Detail and Schema panes use — so every surface agrees on what a column is.

## 3 What renders today

| surface | rendering |
| --- | --- |
| Table, per-DB-row grid (`renderMasterTable`) | list cell → `[len=N]` (`gloss.FormatArrowElem`); support and membership columns hidden unless revealed by the options bar |
| Table, per-attribute grid (`renderAttrTable`) | one row per attribute; a nested array's items exploded one per row; items glossed on the inner array; co-alignment is *visible* because sibling lanes stack in step within a section, never across sections |
| Detail, leeway card (`Table2CardEmitter`) | one row per attribute; membership chips in primary / secondary columns; the values cell packs `name=value · …`; nested-array items comma-joined; a co-section group merged into one wide section labelled `co · <key> · <inner>`; block faces per value (ADR-0186) |

So the *ragged* reading already exists in both views — the per-attribute
grid and the card give one row per attribute with the run's items — and the
*co* reading exists within a section in both. What is missing:

- In the per-DB-row grid, neither reading exists: every collection is a
  count, and the grid is the default view.
- In the card, alignment *across the rows of one section* is lost: its own
  doc comment records that the packed values cell "sacrifices intra-section
  column alignment for cross-section uniformity", so in a wide section or a
  merged co-section group the eye cannot read down a lane.

Two documented constraints bound any change. The per-attribute grid's
width cache is sampled from the packed `[len=N]` text, so it under-sizes
exploded columns (`attrColWidths` comment) — a new packed rendering moves
that seed. And the per-DB-row grid has a re-fit frame on which cells go
out untruncated so a column can grow to its content (`selectableCell`'s
`truncate` flag): whatever a cell renders must be bounded, or the longest
array on the page sets the column width.

Deferrals and precedents already on record:

- [ADR-0186](../adr/0186-play-gloss-catalog.md) §SD8 defers "a paint variant
  of the inline face (arrays as sparklines)". `gloss.KindOfArrow` classes a
  list as `ValueKindOther`; a gloss that wants items is applied by the host
  to the inner array, which only the per-attribute grid does today.
- [ADR-0097](../adr/0097-play-reactive-query-graph.md) Update 2026-07-13
  deliberately did not build section-grouped column *bands* in the
  per-DB-row grid, because a visibility filter in physical order met the
  ask then. Alignment is a different ask, so that reasoning does not
  transfer unexamined.
- The Experiments tab's `unicode` sink already renders "one box-drawn table
  per section — column headers are the section's value names, one row per
  attribute; ragged sections show as ragged tables". It is the in-tree
  precedent for the per-section grid §5 discusses for the card.
- `componentview` (ADR-0075 / ADR-0146) renders *recognised* components of a
  leeway row, one foldable panel per component, and routes the rest to a
  generic fallback. It is the typed complement to the card, not a rendering
  of arbitrary sections. Since ADR-0075's 2026-09-02 Update the Detail pane
  draws it above the card for a facts row carrying any of play's registered
  kinds; it does not change how the card or the grids show a collection.
- `fieldview` renders typed key-value pairs as a collapsible outline whose
  Array containers hold their children beneath them — the widget-level
  precedent for stacking a collection's items (§5 P), lifted from the
  logviewer's detail pane.

## 4 Design space — the per-DB-row grid

The grid keeps one row per DB row and a one-line monospace cell. The
per-attribute grid is the existing answer to "show me every item"; the
question for this grid is what a cell can say without leaving the row.

### 4.1 One collection cell

| option | shows | cost / risk | seam |
| --- | --- | --- | --- |
| **A. length only** (today) | `[len=N]` | none | `gloss.FormatArrowElem` |
| **B. structured count** | `3 attrs` for a value lane, `3 attrs · 7 values` when the section carries a nested array, from the support columns | bounded; says *what* the count counts, which `[len=N]` does not; no items | `glossCell`, reading `ColumnClass` |
| **C. named preview, capped** | `3 · speed=1.2, alt=4.5, …` — the first *k* items as `tag=value` through the membership renderer and the item's gloss inline face, then an ellipsis with the remainder | bounded, so the re-fit frame is bounded; needs the membership lane read per cell (the attr sink reads it raw per attribute already); the attr grid's width seed moves | as B, plus `membership.Renderer` |
| **D. full inline join, truncated** | everything that fits | unbounded on the re-fit frame: the column grows to the longest array on the page | as C |
| **E. paint face** | a sparkline for numeric items, a dot-strip for booleans, a proportion bar for low-cardinality tags | a paint inside the cell (`PaintPolyline` / `PaintRectsFilled` exist; `play_cost_chart.go` is the in-play precedent for painting into a laid-out rect); fixed width; unreadable for text; is the SD8 deferral | a **collection face** on the gloss side |
| **F. hover peek** | the section (or co-section group) as a small table on hover: rows are attributes, columns are the lanes, memberships as the first column | `HoverUi.Render` serializes the tip body every frame for every cell it wraps — the bindings splice both deferred blocks unconditionally — so gate it: record the hovered cell from the button response this frame, render the tip only for that cell next frame | `c.HoverUi` inside `selectableCell` |
| **G. expand in place** | items stacked inside the cell, the row grows | needs per-row heights in egui_table; the card gets them from egui_extras via `NewTableRowHeight`, and whether `EtRowHeight` gives egui_table the same is *unverified*; also blurs "one grid row per DB row", which the per-attribute grid already trades away deliberately | none clean |

B is the floor and costs almost nothing; C is the first option that shows
data, and its `tag=value` form is what makes a leeway cell readable rather
than merely populated. E and F are the ceiling and compose with C. D and G
fight the grid's own layout model, and the per-attribute grid already
covers what they would show.

### 4.2 Several co-lanes

A section with two or more value columns puts its lanes side by side as
columns while the positions sit inside cells, so the reading "for
attribute *k*, what are lat, lng and h3?" runs against the grid's geometry.

| option | what the reader gets | cost / risk |
| --- | --- | --- |
| **H. slot-aligned previews** | C, with every item padded to a fixed slot width within the section, so position *k* sits at the same x offset in every sibling cell and the tag is shown once (in the first lane) rather than repeated | a per-section slot width per page; monospace already; degrades for text items of very unequal width |
| **I. section band** | a header band over the section's (or co-group's) columns carrying the name and the page's attribute-count range (`n = 2..7`) | the bindings expose sticky headers but no multi-level header (*unverified* whether the crate does); a first cut is a per-section header tint plus hover text, which is what ADR-0097 declined and would need to be revisited on the alignment ground |
| **J. section peek** | F, on any cell of the section | as F |
| **K. explode the selected row** | the selected DB row's attribute rows inserted into the grid beneath it — the per-attribute grid's rows for one entity, in the per-DB-row grid | needs no per-row heights (the row *count* changes, not a row's height); the attr sink already produces the rows for one entity; selection, ids and the width tiers all have to accept a grid whose row count is not the result's row count |
| **L. zipped virtual column** | one synthesized column rendering `(1.2, 4.5), (1.3, 4.6), …` per section | a virtual column is a new concept for the grid: sort, width identity, selection and the gloss catalog all key on Arrow fields; large blast radius |
| **M. position cursor** | hovering item *k* in one lane highlights item *k* in the siblings | per-item hit regions inside a cell; the most expensive option here |

H and J together give the co reading without touching the grid's model.
K is the interesting middle: it brings the per-attribute grid's exact
reading to the default view for the one row the reader is looking at,
and is the option most worth a prototype. L and M are not recommended.

## 5 Design space — the leeway card

The card already gives the ragged reading (one row per attribute, items
comma-joined) and shows memberships per row. The gap is alignment across
rows, and long items.

| option | what the reader gets | cost / risk |
| --- | --- | --- |
| **N. per-section grid mode** | a section rendered as its own small table — columns are the section's value names, rows are attributes, a co-section group as one wide table with a rule between the sections' column groups; the Experiments tab's `unicode` sink in widgets | the emitter buffers `table2UnifiedRow`s with a flat `valuePairs` list; the section's value-name list is already carried (`colNames`), so this is a second row renderer, not a new model; a nested egui_extras table inside a cell is *unverified*, and the card's own comment records that a self-scrolling table inside an outer scroll area crops rows; a per-section toggle beside the existing collapse would keep the packed default |
| **O. aligned pairs** | the packed cell laid on a `Grid` with one column per value name, so a name sits in the same column across the section's rows | cheaper than N; alignment holds only while the row is wide enough for all pairs on one line |
| **P. item stacking** | a nested array's items one per line under the pair name instead of comma-joined | the row-height heuristic (`rowHeight`) already accounts block heights; a multi-line inline face would need the same |
| **Q. block face** | numeric arrays as a sparkline or histogram block; `SetCellBlock` already renders block faces per value | a collection block face on the gloss side; `implot` has an inert sparkline mode |
| **R. co-group rule** | in the merged co-section row, a visible rule between the two sections' value groups and the co-group key once, in the header, rather than in every row's section cell | small; a labelling change inside the existing renderer |

N is the alignment fix and O its cheaper approximation. P only matters
for long items. R is a polish that makes the merged co-section legible as
two sections sharing one axis rather than one wide section.

## 6 Cross-cutting concerns

- **One collection face.** C, E, P and Q all want the same thing: a face
  that knows it is looking at a collection of items with a kind and,
  for leeway, a name per item. ADR-0186's faces are per value; the host
  applies them to items on the inner array in one place. A collection face
  composed from the item face plus a layout (named join, stack, sparkline)
  would give both views the same rendering and retire the SD8 deferral. It
  belongs on the gloss side, as an ADR-0186 Update or its own ADR.
- **Column width identity.** `;view=row` / `;view=attr` (ADR-0151 §SD1 tags)
  isolate the two grids' widths. B or C change the row grid's intrinsic
  widths for list columns; stored overrides survive (identity-keyed), the
  estimator's seed moves, and the attr grid's under-sizing note (§3) goes
  away once the seed is a preview rather than `[len=N]`.
- **Raw cells.** The ADR-0186 toggle bypasses glosses. Whether B / C count
  as "a gloss" decides whether raw shows `[len=N]` or the preview. Keeping
  `[len=N]` under the toggle keeps a machine-readable escape.
- **Sort.** Sorting a list column permutes rows on raw values today; a
  preview does not change that, a virtual column (L) would have to.
- **Hover cost.** F / J must be gated to the hovered cell; an ungated
  `HoverUi` per cell serializes every tip every frame.
- **Id stack.** A tip body or a nested table inside a cell is a multi-child
  scope and needs `c.IdScope(...)`; a mismatched stack compiles and panics
  at render (AGENTS.md § egui2 / imzero2).
- **Selection.** Every cell is a frameless button whose click selects the
  row. A tip target keeps that; K changes what a row *is*, so the
  row-click → `selection` contract (ADR-0097 SD8) has to map an inserted
  attribute row back to its DB row, as the per-attribute grid does.

## 7 One possible sequencing — not a decision

Ordered by value per unit of new mechanism, each step useful on its own:

1. **B, then C.** Structured count, then the named capped preview, in the
   per-DB-row grid. Touches `glossCell`; the memberships come from the
   same raw reads the attr sink does. No new concept.
2. **R**, the co-group rule in the card. A labelling change.
3. **H + J.** Slot-aligned previews and the section peek. Co-arrays become
   readable as co-arrays in the default grid.
4. **N** (or O first), the per-section grid mode in the card.
5. **K**, explode-the-selected-row, as a prototype before deciding.
6. **Collection face** on the gloss side (E, P, Q), which can run in
   parallel with 3–5.

Steps 1–2 are small enough to ship without an ADR. Steps 3–5 change how
the two views are laid out and each warrants an ADR Update on ADR-0097 (the
grid) or a new ADR (the card, which is a widget in `leewaywidgets`, not
play).

## 8 To verify before an ADR

- Whether egui_table (the per-DB-row grid) supports per-row heights through
  `EtRowHeight`, or only the default height passed to `EndETable`. Decides G.
- Whether an egui_extras table renders correctly inside a `HoverUi` tip and
  inside an egui_extras cell (J, N).
- What the membership renderer's text costs per cell when read for every
  visible list cell (C), against the attr sink's per-attribute reads.
- Whether the width estimator's re-fit frame stays bounded with C on a page
  of wide sections, or whether *k* has to shrink with the lane count.
