---
type: adr
status: proposed
date: 2026-10-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0336: recordtable groups rows as a line sequence over the sort permutation — group rows are a row kind, identified by their key path

## Context

`widgets/recordtable` draws an Arrow record as a flat egui_table grid: a
row-number gutter, a header-click sort held as a display permutation over
the record ([SortState]), single-row selection keyed on the record row,
column widths kept under [ADR-0151](./0151-table-column-width-overrides.md),
and a replay cache for the cells of a page nothing touched. Four hosts draw
with it — play's Table pane, tally, imztop, watchbill. The question this
ADR answers is how a reader groups those rows by one column or several,
with a header per group carrying the key, a count and summaries, and
collapses what they do not want to see — without the grid giving up what
the flat form guarantees: the record untouched, a row's identity its record
index, the sort a permutation, selection a cursor the other panes agree
with.

The design space was surveyed first under a clean-room procedure
([data-grid-grouping-design-space-survey](../adr-background-work/data-grid-grouping-design-space-survey.md),
2026-10-26: AG Grid, the other JavaScript grids, native and immediate-mode
toolkits, spreadsheets and BI tools and query engines, usability and
accessibility guidance). What it established, and this decision rests on:

- **Grouping lives in a layer above the source, and the vendors who shipped
  both a tree of containers and a flat row stream now recommend the flat
  stream.** Telerik added a Flat mode and maintains only it; Qt never
  shipped a grouping proxy because one that invents rows breaks every
  consumer that assumes a 1:1 index mapping; SlickGrid, Glide, AppKit and
  GTK's sections are flat by construction. Dear ImGui's recorded answer to
  a tree in a clipped table is "linearize the tree data", explicit heights,
  state readable without drawing. This tree's own `widgets/tree`
  ([ADR-0176](./0176-native-tree-widget.md)) is that shape already.
- **A group is identified by its key path, never by a display index.**
  Every library that keyed expansion otherwise had to repair it (TanStack's
  `autoResetExpanded`, Kendo's stale keys, AG Grid retiring
  `rememberGroupStateWhenNewData` for row ids).
- **Grouping is declared as an ordered list of levels; the key is separable
  from the label; interval bucketing is a key function.** AG Grid's
  per-column flags plus an index were the exception and its state object
  stores the list anyway.
- **Aggregates roll up correctly only as a fold with a composable
  accumulator, over a stated scope.** The avg-of-avg trap is documented by
  AG Grid, Excel, Tableau and Power BI; the grids that left the filtered /
  unfiltered scope implicit grew a flag later. Count distinct and
  percentiles do not decompose and belong to the query.
- **Leaves sort within groups; groups order by key, with an explicit
  per-level override by an aggregate.** Finder's fixed group order under a
  within-group sort is the less disorienting default; MUI's single grouping
  column could not express per-level orders.
- **The group row carries the key, a leaf count and aggregates aligned
  under their columns, is toggled by a whole-row click when it has no
  other control, and is not a selection.** Numbers keeps the summary row
  visible when collapsed; AG Grid's full-width caption cannot show
  aggregates at all; AppKit's group rows are not selectable.
- **The substrate has per-row heights, full-width row blocks, sticky header
  rows and sticky leading columns, and no sticky body rows.** Sticky group
  headers, the most-requested feature in AG Grid's history, have no seam
  here today.
- **play's results arrive as `ArrowStream`**, so ClickHouse `WITH TOTALS`
  never reaches the grid and `WITH ROLLUP` rows arrive as ordinary rows
  with type-default keys. Recognising query-side groups is a separate
  feature with a separate trigger.
- **play's per-attribute leeway view is a grouped display built by hand**
  (survey §2.6): one entity over *N* stacked rows, the gutter and backbone
  once on the top row, no collapse, count, sort or paging, built on
  `Chrome` beside `RenderGrid`. Its leaves are not record rows, and the
  group key a leeway reader wants most — a membership — is not an Arrow
  scalar of the row. Explode first, then group (survey §12.5), makes that
  view and the new ones one layer.

Two decisions were taken at the design dialogue of 2026-10-26 and bound
what follows: for the leeway host only the one-row-per-attribute case is
in scope, grouped by a plain-section attribute and then by the membership
path hierarchically; and grouping the flat per-DB-row grid by a tagged
column is out of scope. A membership path is a key of *variable depth*, so
the level model below has a path kind beside the column kind.

## Design space (QOC)

**Question.** Where does grouping live, and what is a group row?

**Options.**

- **O1** — A grouping layer inside `recordtable`: a host-owned
  `GroupState` beside `SortState`, a cached line sequence
  `{kind, record row | group}` between the permutation and the etable,
  group rows drawn as a row kind.
- **O2** — A second widget composed on `Chrome`, as play's per-attribute
  grid is, with its own input and state.
- **O3** — The host computes groups and hands the grid header positions
  over its `Order` (Glide's shape); the grid only draws them.
- **O4** — Query-side only: play rewrites the SQL with `ROLLUP` /
  `GROUPING SETS` and the grid recognises the rows by an injected
  `GROUPING()` column.

**Criteria.**

- **C1** — Every `RenderGrid` host gets grouping without a new call.
- **C2** — Record-row identity, the sort permutation and the selection
  contract survive unchanged.
- **C3** — Aggregates are correct across levels for the set offered.
- **C4** — Frame cost: the cells cache stays effective, nothing is
  recomputed per frame that is not keyed.
- **C5** — Works on a held result that cannot be re-run, and on a polled
  record (imztop).
- **C6** — Driveable and reportable as play ops the same way the sort is.
- **C7** — No duplication of the width protocol, headers, fit frame and
  cache.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | −− | −− |
| C2 | ++ | +  | +  | −  |
| C3 | +  | +  | −  | ++ |
| C4 | +  | −  | +  | ++ |
| C5 | ++ | ++ | ++ | −− |
| C6 | +  | +  | +  | +  |
| C7 | +  | −− | −  | −  |

O4 is not a rival to O1 but a later complement over the same line
sequence (§SD10).

## Decision

We will add row grouping to `widgets/recordtable` as a view layer: a
host-owned `GroupState` holding an ordered list of levels and a key-path
expansion map; a pure, cached *layout* that turns the sort permutation into
a sequence of lines, each a record row or a group; and a renderer that draws
a group line as a full-width row block with its aggregates aligned under
their columns. Leaves keep their record-row identity, ids, selection and
sort. `Input.Groups == nil` is today's grid, byte for byte.

### SD1 — The group spec is an ordered list of levels, keyed on Arrow columns

```go
type GroupLevel struct {
    Arrow    int                        // the Arrow column the key comes from
    Key      func(row int64) Key        // nil: the column's value; a bucket otherwise
    Segments func(row int64) []Key      // a path level: one nested group per proper prefix
    Order    GroupOrder                 // how sibling groups at this level order
    KeyIsRow bool                       // the key is a record row number (§SD6)
}

type Key struct {
    Value string // identity, compared bytewise
    Label string // what the group row shows; empty means Value
    Null  bool   // the one null group of this level
}

type GroupOrder struct {
    Desc       bool // reverses the order
    Appearance bool // first appearance in the incoming permutation
    ByAgg      bool // by the aggregate of Input.Columns[AggColumn], whose Agg must be set
    AggColumn  int
    NullsFirst bool // the null group leads rather than trails; Desc does not move it
}
// The zero value orders by Key.Value, typed where the key is the column's.
// A Value may hold any text; the group's Path spells the joiner U+001E as
// U+001F, and groups are found by parent, level and key while partitioning.
```

The order of `Levels` is the nesting order. A level is one of two kinds. A
**column level** yields one key per row. A **path level** (`Segments`
set) yields a row's segments and opens one nested group per *proper*
prefix: the last segment is the leaf's own name and makes no group, a
prefix no row ends at is a group all the same (a filler, in AG Grid's
word), and depth varies from row to row, so a leaf and a subgroup may be
siblings under one node. A row whose segments are empty, or whose
`Segments` is nil for the row, is the level's null group. Levels after a
path level nest under its deepest group. The segmenter is the host's: the
widget never parses a path. A level's `Key` is the only
seam for interval bucketing (a date to its month, a number to a step, a
string to its initial): the package ships none in the first cut and the
seam is the deferral (§SD11). Keys compare on `Value`; `Label` is
presentation, so two rows with the same `Value` and different labels are
one group with the first label, which the host can see and should not
produce. A level whose `Arrow` names a column the record lacks is an
error drawn in the grid's place (ADR-0267 W17).

### SD2 — A group is its key path, and expansion is a three-valued map over paths

A group's identity is the sequence of `Key.Value` from the root to its
level, joined with a separator that cannot occur in a value (the path is a
byte string, never shown). `GroupState` keeps a map by path holding
only the groups whose state **differs** from the default: a group is open
unless its depth is at or below the depth `CollapseAll` or
`CollapseBelow(depth)` set, and the map says otherwise. This is
`tree.State`'s shape (ADR-0176, the 2026-08-09 update) by path instead of
node index, and for the same reason — "the reader closed this" and "this
group is new" have to be told apart when the record changes. The package
default is all open, because a collapsed-by-default grid with no counts is
the documented way to make a result look empty, and an analyst's LIMITed
result wants reading before folding. `IsOpen`, `SetOpen`, `Toggle`,
`ExpandAll`, `CollapseAll`, `CollapseBelow` and `ResetExpansion` are
methods on `GroupState`; none scrolls. A `Reveal(row)` opens the ancestors
of a record row and names its line as `Layout.Revealed` once, for the
renderer to scroll to — what a selection made in another pane needs.

Expansion is session state in the host's `GroupState`. Persisting it
across sessions is deferred (§SD11); the key path is what a persisted
form would store.

### SD3 — The layout is pure, cached, and is where the tests live

```go
type Line struct {
    Row   int64 // the record row of a leaf, -1 on a group line
    Group int32 // the group itself on a group line; a leaf's parent (0, the root)
}

type Group struct {
    Path   string // the level and key of every ancestor and its own, joined; opaque
    Level  int32  // -1 on the root
    Depth  int32  // 0 for a root-level group; a path level adds a depth per prefix
    Key    Key
    Parent int32  // -1 on the root
    Leaves int64  // leaf rows beneath it, whatever its expansion
    Open   bool
    Line   int64  // where it is drawn, -1 while an ancestor is closed
}

func (inst *GroupState) LayoutFor(rec arrow.RecordBatch, order []int64, n int64, cols []Column) (*Layout, error)
```

`Groups[0]` is the root: every leaf's ancestor, never drawn, whose
aggregates are the grand totals.

`LayoutFor` partitions the first `n` rows of `order` (nil for record
order) by the levels' keys, filing rows in arrival order under a trie of
groups, so the
incoming permutation — the header sort, or the query's own order — is the
order of leaves *within* every group: a leaf sort never reorders groups,
which is Finder's behaviour and the survey's recommendation. Sibling groups
then order by `GroupOrder`; under a path node, subgroups and leaves are
one sibling set ordered by segment key — a JSON object lists its members
together — and `Appearance` keeps the incoming order for both. Two leaves
under a column level keep the permutation's order whatever the level's
order says, including `Desc`. The null group sits at its edge against
anything, group or leaf, which is what keeps the comparator consistent
when a path node mixes the two. A path
group's `Leaves` counts the rows beneath it, filler groups excluded, the
same count a column group carries. The result is `Lines []Line` (group lines
followed by their leaves while open, the null group at the edge its level
asks for), `Groups []Group`, and the aggregates (§SD4). It is recomputed
only when the record pointer, the permutation's identity (the slice and
its length — a permutation changed in place is not seen, by contract), the
levels, the columns' aggregate declarations, or the expansion generation
changes; expansion alone re-flattens without re-partitioning. The slices are the
state's own, valid until the next call, like `SortState.OrderFor`. A
`Layout` answers `PosOf(row int64) int64` (the line a record row is drawn
on, −1 while folded away) and `GroupAt(line int64)`, which is what paging
by record row and play's `row` option need.

The partition, the orders, the null edge, the three-valued expansion and
the aggregates are asserted without a renderer.

### SD4 — Aggregates are a fold with a composable accumulator, over the leaves the layout holds

```go
type AggE uint8 // AggNone, AggCount, AggSum, AggMin, AggMax, AggMean

type Column struct {
    // …existing fields…
    Agg     AggE                              // a built-in, computed from the leaves at every level
    AggCell func(g Group) Cell                // the host's own; wins over Agg
}
```

Built-ins accumulate an `AggValue` — the leaf count, the numeric count,
sum, min and max — per group bottom-up: a parent merges its children's
accumulators, so sum, count, min and max compose and mean is `sum / num`
of the merged pair, never an average of averages. `Layout.Aggregate(group,
col)` and `Layout.Total(col)` read them. The scope is the leaves the layout holds — the rows the
permutation names — stated once here so that a host's filter, which
shortens `Order`, shortens the aggregates with it. Non-numeric columns
offer `AggCount` only. Count distinct, median and percentiles are not
offered: they do not decompose, and the survey's engines all re-evaluate
them at the source; a host that wants one computes it (`AggCell`) or asks
the query. A grand total over the whole record is the root group's
accumulator and is exposed on `Layout` for a host that draws a footer; the
grid draws none in the first cut.

### SD5 — A group line is a full-width row block with aligned aggregate cells

A group line is one etable row. Its `Rows(row)` block
(ADR-0176 §SD5) paints the band and senses the whole-row click that
toggles it, and carries, inset by the level's depth: the disclosure
chevron (right when closed, down when open), the key's label, and the leaf
count. The band's text starts after the leading (sticky) columns and is
clipped at the first column that carries an aggregate. The host replays a
row block in the scrolled region alone (ADR-0176 §SD6), so nothing drawn
under the gutter would show, and the gutter keeps the body's colour on a
group line; the band's fill is a translucent neutral tint, so it reads in
either theme over the body. The cells of aggregated columns are emitted
into their own columns on the same row through `Chrome.Inset` with
`Weak` off and `Selected` never on.
Columns without an aggregate emit no cell on a group line, so the band
shows through. This is DevExpress's aligned-summary form and keeps
`Columns`, their widths and their identities exactly what the flat grid
has; a group line adds no width identity of its own (ADR-0151 §SD1).
egui_table's striping is off while grouped — it is painted per cell, over
the band — and a leaf line's row block paints the stripe instead, as
`widgets/tree` does. The row blocks are not part of the cells replay, so
they are emitted on every frame and their clicks read before the replay
decision.

Group lines take the grid's row height. Per-row heights exist in the
binding and are deferred (§SD11), because uniform rows keep egui_table's
fast path and the cells cache's key simple.

### SD6 — Leaves keep their identity; group lines are not a selection

A leaf line's cells are scoped by its record row, as today, so a cell keeps
its id across a sort, a page turn and a fold; a group line's are scoped by
its key path, so a click made on the previous frame lands on the same
group after a sort or a new record. `Input.Selected` is a record row and
is highlighted on the line it is drawn on; a selected row folded away is
simply not drawn, and stays selected. A click on a group line's band or
chevron is reported as `Result.GroupToggled` (the group's index in the
layout), never as `Result.Clicked`; the grid applies nothing itself — the
host passes the frame to `GroupState.Apply`, as it does the sort, or
routes the gesture first (play's ops). The gutter shows the record row
number on leaves and nothing on a group line.

One level may declare `GroupLevel.KeyIsRow`: its key is a record row
number (the source row of an exploded record, §SD12), so a group line at
that level stands for a record. The chevron still toggles; a click on the
band reports `Result.GroupClicked` with the key's row, and the host selects
it — Finder's "clicking a header selects the group" — and hands it back as
`Input.SelectedKeyRow`, which paints that band as selected. The gutter
then shows that row number on the group line.

### SD7 — Sorting: leaves within groups, groups by their level's order

The header click keeps its contract: `Result.SortClicked` names the Arrow
column, the host applies it to `SortState`, and the new permutation
reaches `LayoutFor`, which sorts leaves within groups. A click on the
header of a column that is a group level is the one exception: its values
are constant within a group, so the grid routes it as a toggle of that
level's `GroupOrder.Desc` (`Result.GroupOrderClicked`, the level), and the
header's glyph shows the level's direction. Ordering a level by an
aggregate (`ByAgg`) is set by the host — play's options bar — not by a
header gesture.

### SD8 — Paging windows the lines

`PageStart` / `PageEnd` window the line sequence: a group line counts, a
folded leaf does not. A host that pages by record row maps through
`Layout.PosOf`. A group may straddle a page; the grid draws no continuation
row.

### SD9 — Every grouping input joins the cells key

The cells cache replays bytes while its key holds. `GroupState` carries a
generation that moves on any change to levels, orders or expansion, and
the grid folds that generation, the columns' aggregate declarations, the
line count, the selected key row and the page window into `cellsKey`. A
host's `CellsKey` still names what the grid cannot see, an `AggCell`
closure's inputs among it. A reader that copies a layout keys the copy on
`LayoutGen`, the generation the layout was computed under, since a change
asked before a frame's draw moves `Gen` first.

### SD10 — The widget groups what it holds; it never reads SQL

Recognising a result that *arrived* grouped — a `GROUPING()` column, a
`ROLLUP`'s default-valued keys, totals the client could fetch in another
format — is play's feature over the same line sequence, not the widget's,
and is deferred with its trigger: a user's query that already rolls up and
asks for its rows drawn as group rows. play's first cut records that the
AST knows `WITH TOTALS` was asked for and that `ArrowStream` dropped it.

### SD11 — What is deferred, and what triggers it

- **Sticky group rows.** No seam: egui_table pins header rows and leading
  columns, not body rows. Trigger: a host whose groups routinely exceed a
  screen. The cheap substitute to try first is a one-line breadcrumb of the
  top visible line's ancestors above the grid, from `VisibleRange`.
- **Interval bucketing helpers** (date to day / week / month / year,
  numeric step, string initial, relative-to-today). The `Key` seam is in
  SD1; the helpers land with the first host that groups a timestamp.
- **Keyboard navigation** (Left / Right collapse and expand, Enter toggles,
  asterisk expands siblings) through a capturing Frame as `tree` does
  under [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md).
  Trigger: the first keyboard scene for the Table pane.
- **Per-row heights** for group lines. Trigger: a host that wants a taller
  band or a two-line header.
- **Persisted expansion** under ADR-0151's persist document, keyed by
  path. Trigger: a host whose groups outlive a session.
- **Multi-value keys** (one row in several groups): query-side
  `ARRAY JOIN` is the answer until a host asks.
- **A grand-total footer row**, and totals placement at the bottom of a
  group. The accumulator exists (SD4); the rows do not.
- **Hiding a grouped column** is the host's business — it owns `Columns` —
  and play's options bar decides it there.
- **Multi-row selection and "select the group"**: a group line is
  unselectable until a selection model that can hold more than one row
  exists.

### SD12 — play's per-attribute view is an exploded record, grouped by a plain attribute and then by the membership path

The leeway `Driver` walk that fills `attrExplodeSink` builds an **exploded
Arrow record** instead of grid rows: one row per attribute — per item for a
non-scalar value — carrying the source row number, the section name (a
tagged section's own; a plain section its item type's; a co-section
group its first section's, as the driver names it), the attribute index,
the membership's low-cardinality path and its high-cardinality parameters
as columns beside the value, support and membership columns the options
bar reveals. A backbone attribute is a row of its own, like any other, and
the other columns are blank on it. play hands it to `RenderGrid` with two
levels:

1. **A plain-section attribute** — a backbone column of the exploded
   record: the entity by default, with `KeyIsRow` so the group line is the
   record and a band click selects it; a timestamp or routing column when
   the reader picks one.
2. **The membership path**, a path level whose segmenter reads the
   `jsonshred` grammar: a JSON Pointer split on `/`, the `~0` `~1` `~2`
   `~n` escapes undone, and each `_` step replaced by the attribute's next
   parameter, so `/metrics/cpu` opens `metrics` over the leaf `cpu` and
   `/tags/_` with parameter `1` opens `tags` over the leaf `1`. A ref
   membership resolves its identity through play's handle and gloss
   resolution before segmenting.

With the first level alone this reproduces the per-attribute view as a
collapsible entity per group line carrying its attribute count; with both,
an entity's document reads as its tree, and the aggregates of §SD4 apply
to the value columns at every node. A leaf click maps to the source row
through the column. Sort, paging and `set_table_options` work in this
granularity because a line is a row of a record again; the refusal the op
carries today goes. Grouping the flat per-DB-row grid by a tagged column
is refused with a pointer at the granularity toggle.

What changes on screen: sections **stack** rather than overlay — one
attribute per row, the other sections' columns blank on it — so an entity
is as tall as the sum of its sections' runs, not the largest; the Detail
pane's record card already stacks, so the two panes agree. The bespoke
renderer and its id arithmetic retire; the `;view=attr` width tag stays, on
the exploded record's columns.

A temporal backbone column may arrive as a bare `uint32`; a date-bucketing
`Key` for play reads `ColumnClass.CanonicalType`, not the Arrow type, and
is one of §SD11's helpers when it lands.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `recordtable.Input` | gains `Groups *GroupState`, `SelectedKeyRow`; nil is the flat grid | nothing: every host compiles unchanged |
| `recordtable.Column` | gains `Agg`, `AggCell` | `WidthColumns` unchanged; a group line has no width identity |
| `recordtable.Result` | gains `GroupToggled`, `GroupClicked`, `GroupOrderClicked` | hosts that route gestures (play, imztop) read them; others call `GroupState.Apply` |
| `recordtable.State` | the cells key folds the group generation and layout identity | the cache's key test |
| play `set_table_options` / `get_table` | gain `group_by` (levels with order), `expand` / `collapse` (a group's printed path — its keys joined by ` / `, a null key as `(null)` — or `all`), and report the levels, the line count and the page's group lines | play's ops schema and its help page |
| `apps/play` options bar | a group picker over the result's columns, the per-level order, the null edge | `tableDisplayOpts` |
| `apps/play` per-attribute view | the explode sink builds an Arrow record; the bespoke etable loop retires (§SD12) | the `;view=attr` width tag on the exploded record's columns, the sort refusal in `set_table_options` (the fold and the page read now work), scene `02_table_per_attribute` |

No IDL or Rust change: the row block, the visible-range read-back and
scroll-to-row already exist.

## Alternatives

- **A second widget on `Chrome` (O2).** Rejected: it duplicates the width
  protocol, the headers, the fit frame and the cells cache, and no host
  gets grouping without a new call.
- **Host-computed header positions over `Order` (O3).** Rejected: four
  hosts would each re-implement partition, key paths, expansion and
  aggregates, and diverge.
- **Query-side only (O4).** Rejected as the mechanism: it re-runs the
  query, fails on a result that cannot be re-run, loses the leaves under
  `ROLLUP`, and three hosts have no SQL. Kept as a later complement (SD10).
- **A synthesised group column** (AG Grid's default, MUI). Rejected for the
  first cut: it gives the label a column, a width identity and a place in
  `Columns` that every host would have to accommodate; the full-width band
  with aligned aggregates uses the row block that exists and leaves
  `Columns` alone. Revisit if the band's label clipping (SD5) proves worse
  than a column.
- **Groups reorder on a leaf sort** (AG Grid's default). Rejected: the
  survey's reading of Finder and MUI is that it disorients; an explicit
  per-level order by aggregate covers the need.
- **Expansion keyed by line index.** Rejected: it is the documented
  foot-gun in every library that tried it.
- **Offer count distinct, median, percentiles.** Rejected: not decomposable;
  the engines re-evaluate them and so should the query.
- **Collapsed by default.** Rejected as the package default: the Outlook
  lesson; a host may set `DefaultDepth: 0`.
- **Array steps as one `_` group with the index on the leaf.** Rejected for
  the first cut: opening `/tags/_` per index reads as the document the
  reader loaded; the schema reading is a different segmenter on the same
  level and costs nothing to add.
- **Tree data over the record for the per-attribute view** — a third line
  kind, a sub-row of a record row, keeping the overlaid layout. Rejected:
  every surveyed library that gave hierarchy its own row model ended with a
  second set of identity, count, filter and selection semantics; the
  overlaid layout is shared by no surveyed product; and grouping by
  membership stays out of reach because the leaf is not a row with columns.
  The cost accepted instead is a visible change to a shipped view (§SD12).

## Consequences

### Positive

- Four hosts gain grouping by setting one field; the flat grid is unchanged
  byte for byte when they do not.
- The record, the sort and selection keep their contracts; the other panes'
  idea of a row is untouched.
- The layout is pure and tested without a renderer; the renderer adds one
  row kind.
- Aggregates are correct across levels for what is offered, and what is not
  offered is named with its reason.

### Negative

- Group lines without sticky headers lose the reader's place in a long
  group; the breadcrumb substitute is untested.
- A full-width label clipped at the first aggregated column can collide
  with an aggregate on an early column; the host orders its columns with
  that in mind.
- The cells cache's key grows; a host that changes expansion every frame
  defeats it, as it would with any per-frame change.
- A polled record (imztop) re-partitions on every new record pointer;
  grouping a table that refreshes every second costs a sort per refresh.
- The per-attribute view changes shape: sections stack, so an entity with
  several sections is taller than today, and a reader used to the overlay
  sees one attribute per row instead.

### Neutral

- Expansion is session-only until a host asks for more.
- Query-side group recognition stays a play feature over the same lines.

## Migration — Tier 1

- **Breaks.** Nothing: `Input.Groups` is nil by default and the three new
  `Column` / `Result` fields are zero-valued.
- **Path.** A host that wants grouping adds a `GroupState` field, sets
  `Input.Groups`, and reads `Result.GroupToggled` only if it routes
  gestures.
- **Regeneration.** None; no IDL change.
- **Old shape.** Kept: the flat grid is the zero value.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the layout (partition, orders, null edge,
  three-valued expansion, accumulators, `PosOf`); the widget's discard-channel
  render test and conformance (ADR-0267 W19 / W20); a headless scene over
  the gallery's `recordtable` demo
  (`widgets/recordtable/scenes/recordtable-grouping.scene.md`,
  [ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md))
  that folds by chevron and by band, selects a leaf through the group
  lines, selects a `KeyIsRow` band without folding it, and reveals a
  buried row, asserting the line count and the readout after each; with
  M3, a play scene (`apps/play/scenes/02_table_grouped.scene.md`) that
  groups a `numbers()` result two levels deep from the picker, folds by
  chevron and by the buttons, reverses a level and asserts the pager's
  line count after each; with M3b,
  a second play scene over `anchor.facts` in the per-attribute granularity
  that groups by entity then by path, folds a prefix, and asserts the leaf
  count on the entity's line. Layout tests cover a path level: filler
  groups, a leaf beside a subgroup, the `_` step, the empty path.
- **What would fail.** A leaf sort that reorders groups; an expansion lost
  across a new record with the same keys; a mean that is an average of
  averages; a cells replay after an expansion change.
- **Gap.** The band's clipping against an aggregated column is visual and
  judged in the scene's capture, not asserted. An aggregate wider than the
  leaves under it is truncated on any line the fit frame did not see:
  egui_table fits a column to the cells on screen, and the grid fits once
  per column set, or on `Input.Refit`, as the flat grid does.

### Milestones

- **M0 — Layout core.** ✓ `GroupState`, `Key`, `GroupOrder`, `LayoutFor`,
  accumulators; tests; no rendering. Shipped 2026-10-26 in
  `widgets/recordtable/group.go`; the shapes in §SD2–§SD4 are as shipped.
- **M1 — Render.** ✓ Group lines in `RenderGrid`, the row block, aligned
  aggregates, the gutter rule, `GroupToggled`, the cells key. Shipped
  2026-10-26 in `widgets/recordtable/group_render.go`; headless only, the
  band's look is M2's capture to judge.
- **M2 — Demo and scene.** ✓ The gallery's `recordtable` demo
  (`demo/apps/widgets/egui2_hl_recordtable_demo.go`): an exploded record
  of readings grouped by a section column and then by a JSON Pointer
  path, or by the vessel's source row (`KeyIsRow`) and then the path, with
  the host's expand, collapse, reveal and aggregate controls; its scene,
  above. Shipped 2026-10-26; the capture showed the band's text clipped
  under the gutter and a fill that did not read, both fixed in §SD5's
  terms, and the reveal scroll, which M1 had left out.
- **M3 — play.** ✓ The picker on the pager row (`play_table_group.go`),
  `set_table_options` with `group_by`, `expand` and `collapse`,
  `get_table` with the levels, the line count and each group line's
  path, counts and fold state; the band, chevron and grouped-header
  gestures routed through `set_table_options`; a selection made in
  another pane revealed, with the page turned to its line; the pager
  counting lines. The spec is held by column name, so it survives a
  rerun and waits through a result without its columns. Shipped
  2026-10-26 with scene `apps/play/scenes/02_table_grouped.scene.md`.
  Aggregates and the null edge have no control in the picker yet; the
  op takes `nulls_first`.
- **M3b — play's per-attribute view** ✓ as an exploded record grouped by
  the entity and then by the membership path (§SD12), with a segmenter
  that reads the `jsonshred` grammar. Shipped 2026-10-26 in
  `apps/play/play_table_attr.go`: the sink builds the record — source row,
  section, attribute index, path, parameters, then the visible columns'
  cells as text, kept while the page, the columns and the glosses hold —
  and the bespoke etable loop retired; scene
  `apps/play/scenes/02_table_per_attribute.scene.md`. What §SD12 names and
  this cut leaves: a plain attribute other than the entity as the first
  level, the sort (the header sorts nothing; `set_table_options` still
  refuses it there), a ref membership's name through the registries (a ref
  is spelled `#` and its number, as the record card spells it), and the
  parameters' spelling, read as a list of indices whatever joins them until
  a writer fixes it.
- **M4 — Deferrals as triggered** (SD11), each a dated entry here.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [data-grid-grouping-design-space-survey](../adr-background-work/data-grid-grouping-design-space-survey.md) — the clean-room survey this decision rests on.
- [ADR-0151](./0151-table-column-width-overrides.md) — column-width identity.
- [ADR-0176](./0176-native-tree-widget.md) — the flatten-and-virtualise precedent, the row block, the three-valued expansion map.
- [ADR-0177](./0177-imzero2-focus-scoped-keyboard-capture.md) — keyboard capture, for the deferred navigation.
- [ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md) — scenes.
- [ADR-0267](./0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md) — the widget contract.
- [recordtable-grids](../howto/recordtable-grids.md) — the host wiring the grid expects.
- [ADR-0097](./0097-play-reactive-query-graph.md) — the 2026-07-13 update that introduced the Table pane's leeway display modes.
- [streamreadaccess EXPLANATION](../../public/semistructured/leeway/streamreadaccess/EXPLANATION.md) — the leeway data model and its Arrow layout.
- [jsonshred](../../public/semistructured/leeway/jsonshred/jsonshred.go) — the path grammar the segmenter reads.

[SortState]: ../../public/thestack/imzero2/egui2/widgets/recordtable/sort.go
