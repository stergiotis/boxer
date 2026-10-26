---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-10-26 as background to
> a decision not yet proposed: how `widgets/recordtable` — the Arrow record
> grid play's Table pane, tally, imztop and watchbill draw with — gains row
> grouping, and with what semantics. Nothing here is a decision; §12 maps the
> findings onto the shapes available in this tree and §13 lists the forks a
> design dialogue has to settle. §2 says where the tree stood on the compile
> date and will go stale as it moves; the ADRs it cites are authoritative.
>
> **Provenance — clean-room.** This page rests on product and API
> documentation, user guides, help-centre pages, specifications and
> standards, published papers, vendor and author blog posts, changelogs and
> release notes, forum threads, and the *prose* of public issue and
> pull-request conversation pages. **No source file, repository code view,
> diff, commit page or package content of any surveyed project was read**,
> and where a permitted page carried code the behaviour is described and the
> code is not. The rule was applied to permissively-licensed projects exactly
> as to copyleft ones — the bar
> [ADR-0119 §SD6](../adr/0119-imzero2-pipelineview-widget.md) set:
> method-level extraction, everything re-derived. Two boundaries are drawn
> explicitly. egui and egui_table are this tree's rendering substrate, not an
> inspiration source, and their *documentation* was read as a dependency's
> documentation is; their source was not consulted for this page. The tree's
> own code was read freely and is what §2 describes.
>
> The reading was done on the compile date by five delegated passes — AG
> Grid; the other JavaScript and headless grids; native and immediate-mode
> toolkits; spreadsheets, BI tools and query engines; usability research and
> accessibility — each held to the rule above and each required to tag every
> claim as read on a fetched page, seen only in a search snippet, recalled,
> or inferred. A claim appears below as fact only where a pass read it on a
> page; snippet-only and recalled claims are marked or left out, and
> inference is marked *(inference)*. **A number or a quoted rule below is a
> pass's reading of a page only where it sits inside quotation marks.** §14
> lists the pages consulted and the incidents under the protocol.

# Row grouping for an Arrow record grid — the design space, and what it meets in this tree

## 1 Question and scope

`widgets/recordtable` draws an Arrow record as an egui_table grid: a
row-number gutter, sortable headers whose sort is a display permutation over
the record, cells whose whole rect selects the row, column widths the user
drags and keeps, and a replay cache for the cells of a page nothing has
touched. It is flat: every display position is one record row. The question
is what it takes to let a reader *group* those rows — by one column or
several, with a header per group carrying the key, a count and summaries,
collapsible — without giving up what the flat grid already guarantees: the
record untouched, row identity the record's own index, the sort a
permutation, selection a cursor into the record, every other pane's idea of
"row 7" unchanged.

"Grouping" is used here for the *view* operation: the grid partitions rows it
already holds. It is distinct from the query having grouped (a `GROUP BY`,
a `ROLLUP`, a `WITH TOTALS`), which §10 treats on its own because the
tools that mix the two are the ones whose users report confusion. Three
sub-questions:

1. **Where grouping lives and what a group row is** — a layer between the
   record and the grid, or a tree the grid walks; a distinct row kind in one
   flat stream, or a nested container — and what each costs under per-frame
   rebuild and row virtualisation (§4).
2. **Which semantics the established grids converged on**, and which they
   revised after shipping: how groups are declared, identified, expanded,
   aggregated, sorted, filtered, selected, numbered and drawn (§§5–9).
3. **Where the boundary with SQL sits**: which aggregates a grid may compute
   over the rows it holds and which belong to the engine, and how a result
   that *arrived* grouped can be told from one the grid groups (§10).

Out of scope: pivoting (columns from values — every surveyed grid treats it
as a different product, and AG Grid removes leaf access under it), server-side
and lazily-loaded groups (none of the surveyed libraries managed them without
a second API surface; the record here is held in full), editing of grouped
rows, and column grouping (hierarchical headers). Multi-row selection is
named where a grouping decision constrains it, not designed.

## 2 What the tree has, and where it stops (2026-10-26)

### 2.1 `widgets/recordtable`

The grid is an immediate-mode widget under ADR-0267's contract
([ADR-0267](../adr/0267-imzero2-go-widget-api-contract-immediate-and-semi-retained.md)):
the host keeps a `State` and a `SortState` across frames and calls
`RenderGrid(Input)` per frame. What matters for grouping:

- **The display sequence is a permutation.** `Input.Order` maps display
  position → record row; nil is record order. `SortState.OrderFor` computes
  it stably, so ties keep the query's order and the sort refines rather than
  replaces it. `PageStart`/`PageEnd` window display positions. Every cell
  is id-scoped by its *record row*, so a cell keeps its widget id across a
  sort and a page turn, and `Selected` is a record row highlighted wherever
  it is drawn.
- **Sort is a view, with a way back.** Clicking cycles ascending →
  descending → unsorted; the third state exists because a query result
  arrives in a deliberate `ORDER BY`. Nulls sort last ascending and first
  descending. Comparison is typed for the common Arrow types and falls back
  to the rendered text.
- **Columns are declared, cells are computed.** A `Column` names an Arrow
  column or is computed (`Arrow < 0`, a `Cell` func), has a caption, sub
  lines, hover, a width seed and a width-identity type, and opts out of
  sorting. Width identity is `(name, type, view)` under
  [ADR-0151](../adr/0151-table-column-width-overrides.md)
  §SD1, so a column rendered two ways carries two stored widths.
- **The cells cache.** With `Input.CellsKey` the grid replays the previous
  frame's cell bytes while its key — page, window, selection, sort, column
  layout, and whatever the host folds in — is unchanged and no cell has a
  response. Anything a cell reads that the key does not name is a stale
  cell. A grouping layer's every input has to join that key.
- **`Chrome` is the cell layer alone** — inset, header alignment, full-rect
  selectable cells — and is what play's per-attribute grid (several grid
  rows per record row) is built on directly, bypassing `RenderGrid`. That
  grid is the tree's one precedent for a display sequence that is not a
  permutation of record rows, and it lives in `apps/play`, not in the
  package.

### 2.2 The egui_table binding

`c.EndETable` (the binding declared in `egui2_definition_d_table2.go`)
offers what a flattened, virtualised grouped grid needs and one thing it
does not:

- **Per-row heights.** A row-height node per row feeds a prefix sum, so
  rows may differ in height and the scroll extent stays right.
- **Full-width row blocks.** `Rows(row)` is a deferred block spanning the
  whole row before its cells run
  ([ADR-0176 §SD5](../adr/0176-native-tree-widget.md)) — the seam a row
  background, a row-wide click sense, or a group caption that reads as one
  band needs. The binding guards against egui_table calling it once per
  region.
- **Visible-range read-back and scroll-to-row.** `VisibleRange` reports the
  rows egui_table will draw (one frame late), which is what gates cell
  emission today; `ScrollToRow` reveals a row.
- **Sticky header rows and sticky leading columns** — but **no sticky body
  rows**: nothing pins a group header while its rows scroll beneath it.
- **One selected row** (`SelectedRow`), one row per display position.

### 2.3 `widgets/tree`: the tree's own flatten-and-virtualise precedent

ADR-0176 built the native tree over the same etable: a columnar hierarchy
(`Labels`, `Parents`) is flattened by a pure function into
`[]Row{Node, Depth, HasChildren, Expanded, IsLastChild}` and rendered one
etable row per flattened row, gated on the visible range. Its `State` holds
expansion, selection and a keyboard cursor; the 2026-08-09 update added a
`Keys` identity column so entries survive a rebuild, and the expansion map
is **three-valued** — it stores only nodes whose state differs from a
default, so "the reader closed this" and "this node is new" are told apart.
Keyboard navigation (ADR-0177 M3) is a capturing Frame around the table with
Left/Right collapsing and expanding, Enter/Space activating, Home/End and
Page keys. Nothing of this is grouping, but every mechanism a grouped grid
needs for its *group rows* already exists here once, and §12 leans on it.

### 2.4 play's Table pane

- **Paging is over display positions**, not record rows: the pager's range
  is applied to the sorted sequence, and an agent's `set_table_options`
  with a `row` maps a record row to its display position through the sort.
- **Every table gesture is an op.** A header click goes through
  `set_table_options` as the person's gesture where play's launcher routes
  it, so an agent can drive and observe the pane; `get_table` reports the
  columns, the sort, the page. Grouping state would have to be reported and
  settable the same way or the pane's two operators diverge.
- **The options bar** already holds per-result view state (granularity,
  revealed column classes, raw cells, identity columns) and is where a
  group picker would sit.
- **Results arrive as Arrow.** The client appends `FORMAT ArrowStream`, so
  a ClickHouse `WITH TOTALS` block — delivered out of band in the formats
  that carry it — never reaches the grid, and a `WITH ROLLUP` arrives as
  ordinary rows (§10).
- The SQL AST already models `WITH TOTALS` (`WithTotals` on the select
  DTO), so play can *know* a query asked for totals it will not see.

### 2.5 The other hosts

tally (a listing beside a treemap, with a height ceiling and no row
numbers), imztop (a process table polled every frame, sort routed through
a view gesture, `NoUnsorted`) and watchbill (a job list with computed
columns) all call `RenderGrid`. A grouping layer that lives in the package
reaches them unchanged; one that lives in play does not. imztop's polled
record is the case that rules out any per-frame recomputation that is not
keyed on the record.

### 2.6 play's leeway mode: a grouped display that was built by hand

A leeway-shaped result ([streamreadaccess EXPLANATION](../../public/semistructured/leeway/streamreadaccess/EXPLANATION.md))
is an entity per Arrow row with two kinds of content: *plain* backbone
columns (an id, a timestamp, routing, lifecycle) that map one to one onto
the row, and *tagged sections* whose every column is a `List<X>` — for
entity *i*, the list's offsets at *i* are that entity's attributes in the
section, a cardinality column partitions a non-scalar value into its items,
and membership columns (ref, verbatim, parametrized, and the mixed and
high-cardinality variants) say which tags each attribute carries. The Table
pane's options bar reshapes such a result through
`streamreadaccess.ClassifyArrowColumns`, which buckets every physical
column as value, support or membership and names its section (ADR-0097's
2026-07-13 update). Three of its controls are column visibility. The fourth,
**row granularity**, is the use case this survey has to meet:

- **Per DB row** is the flat grid: a tagged column's cell packs its items
  (`[len=N]`), and a row click selects the record row.
- **Per attribute** explodes each entity down its own rows: a section with
  *N* attributes occupies *N* stacked rows, a non-scalar value fans its items
  further, the `#` gutter and the backbone columns show once on the
  entity's top row and are blank below, a membership or support value shows
  once on its attribute's first row. Sections **overlay** rather than stack:
  each starts at the entity's first row, so cells "line up only *within* a
  section (same attribute), never across sections", and the entity's height
  is the *largest* section's run. A click on any of its rows selects the
  source entity, so the exploded rows cross-filter the app as record rows
  do. It is built on `Chrome` directly (`apps/play/play_table_attr.go`,
  a sink driven by the leeway `Driver`, then a bespoke etable loop), not on
  `RenderGrid`; it has no collapse, no count, no aggregate, no sort and no
  paging by record row — `set_table_options` refuses them in this mode —
  and its width identities carry a `;view=attr` tag so a column fitted to
  one granularity is not reused by the other.

Read against §§3–9, the per-attribute view is a grouped display with the
entity as the group and the attributes as leaves, minus everything a group
row gives (a band, a chevron, a count, a summary) and with one structural
choice the surveyed grids do not make: the leaves of one group are not a
list but a union of per-section runs sharing row offsets. The Detail pane's
record card (`widgets/leewaywidgets`) draws the same entity the other way —
one table with a *section header row* before each section's rows, labelled
`tagged · metric`, `plain · entity-id`, `co · <key> · <inner>`, carrying a
per-section summary (`N rows · B · %`), a separator between records — which
is the tree's own precedent for a group header with aggregates over this
structure, and the vocabulary a section-level group label should reuse.

Three facts about the structure matter for a group key:

- A **tagged value column is a list per entity**, so grouping the flat grid
  by one is §5.6's multi-value case made unavoidable: by combination (the
  set of an entity's values is the key) or by element (the entity appears
  under every value). The per-attribute explode *is* the by-element form.
- A **membership is the natural group key** for a leeway result — all
  attributes tagged the same across entities — and it is not an Arrow
  scalar of the record row: it is a value the `Driver` emits per attribute,
  with an identity (a ref id or a verbatim string) distinct from its
  parameters. A key function over record rows cannot reach it.
- A **temporal backbone column may arrive as a bare `uint32`** (a width-32
  `DateTime`), indistinguishable from a cardinality column by Arrow type;
  `ColumnClass.Temporal()` and `CanonicalType` are what a date-bucketing key
  has to read, not the Arrow type.

## 3 Two things are called grouping, and a third is nearby

The spreadsheet and BI pass found the vocabulary split that the grid passes
assume without naming:

- **Outline grouping** (Excel Outline and Subtotal, Google Sheets Group):
  the rows stay flat and complete; collapsible *spans* are laid over a range
  the user sorted first, and the summary rows are real rows holding
  formulas. Excel's Subtotal page says the grand total is "derived from
  detail data, not from the values in the subtotals", and that the list
  must be sorted by the key first. The outline merely hides; copying a
  collapsed range copies the hidden rows too, which is the long-standing
  trap §9.6 records.
- **Key grouping** (Numbers categories, Airtable, Notion, Linear, Jira,
  Asana, pandas, every JS grid): rows are *partitioned* by a column's
  values; group headers are synthetic rows carrying a key, a count and
  optional aggregates; collapse hides the leaves and keeps the header.
  Numbers is the clearest consumer model: a summary row *above* each group
  carrying the name, a function chosen once "automatically added to all
  groups at the same level", "The summary row remains visible even when
  the group is collapsed", groups sortable by their summary values, and a
  way to turn categories off and see the flat table again.
- **Pivot / matrix** (Excel PivotTable, Power BI matrix, Tableau totals,
  FINOS Perspective): the engine aggregated; the view only arranges keys
  and totals (compact / outline / tabular, subtotal top or bottom, repeat
  labels, expand and collapse). Both Tableau ("Total All Using", a two-pass
  total) and Power BI ("Custom totals") *name* the alternative semantics —
  aggregate the displayed rows — as an explicit, non-default choice.

The grid question is key grouping. Outline grouping is what a result that
arrived with rollup rows would want (§10), and the two must not share a data
structure *(inference)*: one has synthetic rows and a key, the other has
real rows and a span.

## 4 Where grouping lives, and what a group row is

### 4.1 A layer above the source, never the source

Every mature toolkit keeps the source untouched and groups in a layer:
WPF's collection view is "a layer on top of a binding source collection"
with sort, filter and group queries, and one source may have several views;
KDE's categorised proxy sorts a flat model by category and derives headers
from runs; GTK 4.12's `GtkSectionModel` answers "which (start, end) range is
position *p* in" over a flat list, headers come from a factory and are not
rows; AppKit's `NSTableView` lets the delegate flag a row as a group row in
an otherwise flat data source. SlickGrid's DataView makes the grid "think
that the 'group' rows are just regular data items" and warns that "the row
indexes the grid refers to are, in fact, the indexes into the output of the
DataView". Glide's row grouping (alpha) is the purest statement: the host
declares where headers sit in an already-sorted array, collapse is index
arithmetic, and a `mapper` translates a visible row to
`{path, isGroupHeader, originalIndex}`.

The tree's `Order` permutation is already this layer for sorting. Grouping
extends it from "position → row" to "position → (kind, row | group)"
*(inference)*.

### 4.2 Flat stream with a row kind, or a tree of containers

The vendors who shipped both chose. Telerik's RadGridView had `Nested`
group rendering — group rows nested into one another, each with its own
container — and added `Flat`, where "all rows are on the same level" and
the header is a distinct `GroupHeaderRow`; the docs now say "it is highly
recommended to use the new Flat mode as it leads to better performance"
and that most new features and fixes "only target this mode". Qt never
shipped a grouping proxy, and the forum threads record why community ones
failed: a proxy that invents group nodes has rows with no counterpart in
the source, so mapping back is undefined and selection, editing, arrow keys
and update propagation break. GTK separates the two concepts outright:
*sections* (non-collapsible runs, header from a factory, sticky-capable)
versus *tree rows* (collapsible, state on a row object, whose `position`
"changes as rows expand and collapse").

The JS libraries split the same way: TanStack, MUI, Kendo and Infragistics
materialise a tree of group rows with `subRows`; SlickGrid, Glide,
Syncfusion captions and DevExtreme emit a flat interleaved stream. For an
immediate-mode grid that already holds a permutation, the flat stream is
the natural fit, and the toolkit pass's reading of Dear ImGui confirms it
from the other side: the maintainer's answer to a tree inside a clipped
table is "linearize the tree data", give rows explicit heights, rebuild the
visible list on expansion change "deferring the rebuild to the next frame",
and read open state without drawing (`TreeNodeGetOpen` and storage ids
exist for that; a demo of 18,000 roots went from 26 ms to 2.5 ms).
egui_table's `prepare(visible range)` and `row_top_offset` assume exactly
this shape.

### 4.3 The immediate-mode constraints, stated

From the ImGui, egui and WPF readings *(inference where marked)*:

- The visible-row list must be computable **before any row is drawn**:
  clipping needs the count and heights first. So expansion state lives in
  the grid's own state, readable without rendering a node — never in
  widget-local memory. WPF's documented bug is the same lesson from the
  retained side: an `IsExpanded` bound to a recycled container "is used
  for the new item" and "the old item loses the correct `IsExpanded`
  value".
- Mark group-affecting changes dirty; do not recompute per frame. ImGui's
  sort specs carry a dirty flag; WPF's live grouping is opt-in.
- Heterogeneous row heights cost a prefix sum per rebuild; uniform heights
  keep the fast path (Kendo's virtual scrolling "requires that all Grid
  rows have an equal, predefined height").
- Single-precision totals lose accuracy "once the total height passes about
  16.7 million pixels".

## 5 Declaring groups

### 5.1 An ordered list of keys

Every library converged on an ordered list of column ids whose order is
the nesting order: TanStack `grouping: string[]`, MUI `rowGroupingModel`,
Kendo `GroupDescriptor[]`, Tabulator `groupBy` (field, function or array),
WPF `GroupDescriptions`, Outlook's "Then by". AG Grid is the exception —
per-column `rowGroup: true` plus `rowGroupIndex` to order them — and its
grid-state object stores the list form anyway; the pass reads that as two
sources of truth for one ordering. DevExtreme's per-column `groupIndex` is
the same shape with the same cost. Caps are common and small: Airtable
three levels, Numbers five, Excel's outline eight, Infragistics ten
grouped columns as a hard maximum.

### 5.2 Key versus display value

The key is identity; the label is presentation; most libraries let them
differ (`getGroupingValue`, `groupingValueGetter`, `calculateGroupValue`,
WPF's `Converter`, Infragistics' `groupingComparer` for ranges). AG Grid
learned the distinction the hard way: tree-data paths are identifiers, so
duplicate display names dropped rows until key and display were decoupled;
and v31 had to make group values typed because "Group values will no longer
be typeless". MUI restricts keys to string, number, Date, null or undefined.

### 5.3 Interval bucketing is a key function

Dates to day / week / month / year (Excel pivots, Sheets, Numbers' "By"
pop-up, Notion's Relative / Day / Week / Month / Year), numbers to a step
with an optional start and end (Excel, Sheets, Notion ranges), strings to
the first letter (Notion, WPF's example, DevExpress `Alphabetical`), and
dates relative to today (Explorer, Outlook, DevExpress `DateRange`:
"Today, Tomorrow, Last Week, Older") all appear as a transform *on the
key*, never on the aggregate. DevExpress's `GroupInterval` enum and WPF's
converter are the two explicit API shapes; AG Grid got there in v34.2 with
`groupHierarchy` after closing a feature request for a per-column grouping
function. Numeric ranges are custom even at DevExpress.

### 5.4 Null, empty and unexpected keys

SQL engines and Arrow put all null keys in one group (ClickHouse
"NULL==NULL"; Arrow "null is treated as a distinct key value"); pandas
drops them by default. Row tools *name* the group — "(Blanks)" last in AG
Grid, "No Value" in Asana, "Unassigned" in Jira — and users ask to move it
to the top because missing values are the exceptions worth seeing first
(an Asana feature request). MUI instead leaves null-keyed rows ungrouped,
listed before the groups. AG Grid's unbalanced mode (null-keyed rows as
siblings of groups) breaks sorting and its server model. Airtable records
whose key is unexpected "land at the bottom" outside any group and users
could not tell why — the UX pass's example of what an unlabelled case
costs.

### 5.5 Empty groups: nest, not cross

Polaris' table algebra distinguishes *nest* ("only creates set entries for
which there exist records") from *cross* (every combination). Group-by is
nest. Products that show empty groups do it through an explicit toggle
(Linear, Notion, Airtable's "show or hide groups with no records"), and
Airtable's works only for field types with an enumerable option list;
SlickGrid's `predefinedValues` and Tabulator's `groupValues` are the API
forms. A grid over an arbitrary SQL result has no domain to enumerate and
cannot offer it *(inference)*.

### 5.6 Multi-value keys

Airtable groups by the exact *combination* of a multi-select because a
record "can only exist in one place" in a view; Notion *duplicates* the row
into every tag's group; WPF's converter "may return a collection, which
indicates the items can appear in more than one group". The SQL equivalent
is `ARRAY JOIN` before `GROUP BY`. Duplication inflates counts; the BI pass
reads both as legitimate with the caveat labelled.

## 6 Identity and expansion

### 6.1 A group is its key path

The sharpest lesson in the JS pass. Libraries that key expansion by the
value path survive re-sort and refresh: SlickGrid's `groupingKey` or
varargs of grouping values, MUI's `getGroupRowIdFromPath`, Kendo's
`GroupKey {field, value, parentGroupKeys}`, Infragistics' hierarchy of
`{fieldName, value}`, DevExtreme's `expandRow(key)`. Those that did not had
to repair it: TanStack's `expanded` state is wiped "whenever the grouped
row model recomputes" unless `autoResetExpanded: false`; Kendo documents
that after a `groupChange` "some of the group keys may no longer be valid";
Syncfusion addresses DOM caption elements. AG Grid retired
`rememberGroupStateWhenNewData` in v31 in favour of row ids, and without
them replacing data means "Row Groups re-created, all open groups closed".
AppKit demands that an outline item's "pointer must stay the same, and it
must maintain `isEqual(_:)` sameness" for collapsed state to persist.

### 6.2 Default policy and persistence are two settings

Outlook's custom view offers "All collapsed / All expanded / As last
viewed" and "can save the expand/collapse state of your groups but doesn't
do this by default"; DevExpress starts collapsed unless
`AutoExpandAllGroups`; AppKit has `autosaveExpandedItems`; AG Grid has
`groupDefaultExpanded` (a depth, −1 for all) plus `isGroupOpenByDefault`,
and `resetRowGroupExpansion()` which "discards all overrides and
re-evaluates each group against the configured defaults"; MUI has
`defaultGroupingExpansionDepth` plus `isGroupExpandedByDefault`. The
tree's own `tree.State` already encodes the pair as a default plus a map of
differences (§2.3).

### 6.3 What collapsed-by-default costs

Outlook 2010's collapsed "Today" group made inboxes look empty and support
staff were told to check the collapse state first; the fix was a visible
triangle. NN/g's accordion findings apply: hidden content "can be
overlooked", auto-collapsing siblings blocks comparison, offer Expand All
and Collapse All, keep each section's state until the user changes it. Jira
loses expansion on browser Back on lists of hundreds of issues and users
call it a productivity loss. Excel exposes per-level collapse as its 1…8
outline buttons.

### 6.4 Expanding does not scroll

"When expanding a group the vertical scroll does not change, which can
result in the child rows not being visible" — AG Grid leaves the reveal to
the app through `ensureIndexVisible`. The tree widget's pending-reveal and
`ScrollToRow` are the local mechanism.

## 7 Aggregation

### 7.1 A fold with a composable accumulator

AG Grid's aggregation runs bottom-up and "For higher-level groups,
`params.values` contains the aggregated results from the child groups";
sums compose, averages do not, so a custom function returns a wrapper
carrying what the parent needs — the documented avg-of-avg trap, and the
community threads where a formatter broke because the aggregate was an
object rather than a number. react-table v7 gave aggregators both inputs,
`(leafValues, aggregatedValues)`; TanStack hands `getLeafRows` and
`getChildRows` thunks so a mean may re-aggregate from leaves; SlickGrid has
`aggregateChildGroups`. The BI pass's reading of Excel ("derived from
detail data"), Tableau (an AVG total is the average of all underlying
records, which "differs from the average of the shown averages") and Power
BI (the default re-evaluates the measure; "Custom totals" average the
displayed rows) is the same rule from the engine side: a parent level's
average needs sum and count, or the leaves.

### 7.2 What decomposes and what does not

Count, sum, min, max, any / all: every tool rolls these up locally. Mean:
only with count carried. Count distinct, median and percentiles: not
decomposable from per-group numbers — Excel's Distinct Count needs the Data
Model so totals re-evaluate; Tableau's two-pass percentile total is an
"average percentile"; AG Grid's docs say median must walk the leaves with
`getAggregatedChildren`, "which is expensive". Arrow's `hash_tdigest` and
`hash_approximate_median` and ClickHouse's `-State` / `-Merge` combinators
are the mergeable-sketch answer, which belongs to the query (§10).

### 7.3 Scope: filtered leaves, stated once

AG Grid: "the aggregated values reflect only the rows which have passed
the filter", flipped by `suppressAggFilteredOnly`; MUI: "By default,
aggregation only uses filtered rows", `aggregationRowsScope: 'all'` as the
escape; DevExtreme and Infragistics follow the filter; Telerik footers "are
calculated over the entire data source and respect any applied filter
expression". Syncfusion defaulted to *page-wise* totals and needed
`disablePageWiseAggregates`. The grids that left this implicit grew a flag
later. For a grid whose display sequence is a permutation the natural scope
is "the leaves the sequence contains" *(inference)*.

### 7.4 Placement: header, footer, both

Numbers puts the summary above the group and keeps it visible when
collapsed. Few's rule: footers are conventional, but "When the summary
values are more important to your message or to your readers than the
details ... it often makes sense to place them near the group headers",
and both is "a small price to pay". DevExpress can align summaries under
their columns in the group row (`AlignGroupSummaryInGroupRow`) or show a
group footer; Telerik draws column aggregates in a row under the header in
Flat mode with an alignment choice; MUI's `getAggregationPosition` returns
footer, inline or none per group node; Tabulator's calcs are rows at the
top or bottom of each group; AG Grid moved footers to
`groupTotalRow: 'top' | 'bottom'` and a pinned `grandTotalRow`, and blanks
the group row's aggregates when a footer exists unless
`groupSuppressBlankHeader` — the UX pass reads header values "jumping" to
the footer on expand as the documented annoyance. A full-width caption row
has no cells, so AG Grid's `groupRows` display cannot show per-column
aggregates at all and a maintainer told users to render them themselves.

### 7.5 Grouping and aggregation are separable

TanStack v9 split them ("Aggregation is independent from column grouping")
so a grand total works without groups and groups work without aggregates;
MUI's footer-only aggregation is the same idea; AG Grid aggregates at the
root even with no grouping. MUI had to add `groupId` to the aggregation
function after users could not inject server-computed per-group totals —
pass group context (key path, depth, column) to a custom aggregator.

## 8 Sorting, filtering, selection and numbering under groups

### 8.1 Sorting

Two questions, often conflated: how groups order among themselves, and how
leaves order within a group.

- **Groups by key, ascending, by default**, togglable from the group chip
  (Telerik: "By default, when the data is grouped, the groups are sorted
  Ascending"; DevExpress: the grouped column's sort direction). AG Grid's
  default is arrival order — "first come, first served" — and its
  `initialGroupOrderComparator` "executes before filtering and
  aggregation" so it cannot see aggregates, which the pass reads as a trap
  born of an implicit pipeline order. pandas sorts keys by default,
  `sort=False` keeps first appearance; Polars `maintain_order`.
- **Groups by an aggregate** is first-class in DevExtreme
  (`sortByGroupSummaryInfo`), DevExpress (`GroupSummarySortInfo`, bound to
  a declared summary item), Numbers (sort groups by summary values),
  Perspective (the sorted column's aggregate at each depth), Infragistics
  (a member-count strategy); a recipe in MUI and SlickGrid; a comparator
  hack in AG Grid that "unlinks" the group column from its source columns.
  MUI's single grouping column cannot sort levels differently — "level 1
  by aggregate, level 2 alphabetically" is inexpressible — and the issue
  was closed as expected behaviour: keep per-level sort specs.
- **Leaves sort within groups.** Finder: "grouping is performed in a fixed
  order of groups which may not match the selected sort order within each
  group"; a header click sorts inside each group. AG Grid's default
  *reorders groups* on a leaf-column sort and `groupMaintainOrder` is the
  opt-out; the UX pass reads Finder's behaviour as the less disorienting
  one, with "sort groups by aggregate" as the explicit alternative. JavaFX
  names the choice as `TreeSortMode.ALL_DESCENDANTS` versus
  `ONLY_FIRST_LEVEL`.
- A sort on the grouped column itself is a no-op on leaves (constant within
  a group) and so naturally means "reverse the groups" *(inference)*.

### 8.2 Filtering

Filter before group: TanStack's row-model chain is core → filtered →
grouped → sorted → expanded → paginated, so aggregates see filtered leaves
and empty groups vanish; AG Grid: "if all of a groups children are filtered
out, the group is also hidden". Two documented complications: AG Grid's
single-child hiding is judged *before* filtering ("Only groups with one
child before filtering are removed"), and its `groupAggFiltering` lets a
filter test a group's aggregate and then pulls the whole subtree, which
tree data does by default and grouping does not. Whether a count badge is
over filtered or total leaves "is nowhere stated explicitly" — the UX pass
asks that it be a labelled decision.

### 8.3 Selection

Parent toggles descendants, partial → indeterminate (WinUI's tree view,
Carbon's tri-state header checkbox, MUI's `rowSelectionPropagation` over
*filtered* descendants with auto-deselect of hidden rows, AG Grid's
`groupSelects: 'self' | 'descendants' | 'filteredDescendants'`); Ant leaves
parent and children unlinked by default. In descendant modes AG Grid keeps
group nodes out of `getSelectedRows`, or callers double-count. Outlook's
automation reports `Selection.Count` = 0 when a collapsed group header is
selected — the mismatch to avoid. For a single-selection grid, the native
pass reads AppKit (group rows are not selectable) and the Qt failures as
the rule: a group row is either not a selection or is a whole-group action,
and the selection stays keyed on the source row.

### 8.4 Row numbers

Report tools make it a scope choice — SSRS `RowNumber(scope)` restarts per
group, Stimulsoft `Line` restarts and `LineThrough` continues — and group
rows carry no number. Excel's outline keeps sheet row numbers, so a
collapsed range shows as a gap in the gutter. egui_table calls a row-number
column a "gutter" and makes it sticky; the native pass reads that under
grouping the gutter shows the source row number for leaves and nothing, or
the count, for a header *(inference)*. Infragistics counts group rows
toward the page size and excludes collapsed rows from paging; Kendo pages
before grouping so a group may continue on the next page, and DevExtreme
renders "continuation messages when a group spans pages".

## 9 Drawing a grouped grid

### 9.1 Two forms of group row

**A synthesised group column** (AG Grid's default `singleColumn`, MUI's
grouping column, Kendo's `singleColumn` / `multipleColumns` display modes):
chevron, indent, key and count in one cell, aggregates aligned under their
own columns, the grouped data column hidden by a separate switch. **A
full-width caption row** (AG Grid `groupRows`, DevExtreme, Syncfusion,
SlickGrid's metadata provider, Kendo's default, every row tool): the key
and count span the row, and aggregates go to a footer row, inline in
parentheses (DevExtreme), or column-aligned in the same row (DevExpress,
Telerik Flat). AG Grid's `custom` display exists only for two needs — a
column left of the group column, and per-column control of several group
columns — and is "not advised". Excel's three pivot layouts and pandas'
sparsified MultiIndex (repeated outer labels blanked) are the same choice
in print: compact (one indented column, blank repeats) versus tabular
("Repeat item labels"); the BI pass reads repeat-labels as a toggle over
one layout, not two layouts. TanStack names the three cell states a
renderer has to know — *grouped* (the key cell), *aggregated*,
*placeholder* (a value already shown by the parent, drawn blank).

### 9.2 The header row's contents and conventions

Every product shows the key and a leaf count; Linear's header toggles
between the issue count and a summed estimate on click; Airtable adds a
per-group summary bar. Chevron first, then the selection control, then the
key (Carbon: "The expandable icon always appears first and to the left of
the selection icon"); chevron right when collapsed and down when expanded
(WinUI; Material 1's up/down is the outlier). Indent per level, but not
heavy — Atlassian: "Avoid heavily indenting table cells, as this makes
them less scannable"; Ant's default is a small fixed step per level. Whole-row
click toggles only when the row has no other interactive element
(Atlassian's default is chevron-only). Few: delineate groups with white
space or one subdued rule, not full grids; keep the column structure
identical in every group ("Don't vary the structure of the table from
group to group"); put multi-level keys on one header row left to right.

### 9.3 Sticky group rows

The most-requested feature in AG Grid's v28 ("users will always know which
group's rows they're scrolling through"), extended across models in 29.3;
Wijmo stacks pinned headers "from parent to child" and keeps them
clickable; Kendo pins "a sticky copy" that "scrolls away with the group";
UWP's sticky group headers are a *panel* property, on by default, Top
placement only; AppKit's `floatsGroupRows` defaults on; DevExpress
`AllowFixedGroups`. Costs recorded: AG Grid's `groupHideOpenParents`
disables stickiness, and sticky rows reorder the DOM so the accessibility
page suggests turning them off; Kendo needs uniform row heights. The
native pass reads the mechanism as a draw-time overlay keyed on the run
that straddles the viewport top, belonging to the virtualising layer —
which in this tree is the Rust side, where no such overlay exists (§2.2).
UWP's SemanticZoom (a zoomed-out list of group headers) is the touch-era
substitute for collapse-all.

### 9.4 Row heights and virtualisation

AG Grid's `getRowHeight` may branch on `node.group`; heights are fixed
once computed; auto-height disables column virtualisation and is lazy "so
the scrollbar can jump". WPF needed `IsVirtualizingWhenGrouping` in 4.5
because grouping defeated UI virtualisation. Glide's per-row path lookup
was the hot spot at around twenty groups until a flat index per group was
stored; its alpha had an off-by-one that lost one row per group.
SlickGrid defers totals until a group is first expanded
(`lazyTotalsCalculation`) and can skip them for collapsed groups.

### 9.5 Keyboard and the accessibility tree

The APG treegrid pattern: Right Arrow on a collapsed row expands it, on an
expanded row moves into the row; Left Arrow on an expanded row collapses
it; Enter on the first cell of a row with `aria-expanded` "opens or closes
the child rows"; Up/Down, Home/End, Ctrl+Home/End, Page Up/Down; the tree
pattern adds asterisk ("Expands all siblings that are at the same level as
the current node") and recommends type-ahead "for trees with more than 7
root nodes". GTK's expander binds `+` / `*` / `-` / `/` and Shift+Left /
Right; Excel uses Alt+Shift+= and Alt+Shift+-; Kendo's TreeList and AG
Grid toggle on Enter. ARIA 1.2 is strict about *where* the properties go:
`aria-expanded`, `aria-level`, `aria-posinset`, `aria-setsize` are valid
only on rows of a `treegrid` and "authors MUST NOT apply [them] to a row
that descends from a table or grid"; a row without children must not carry
`aria-expanded`; virtualised rows need `aria-rowcount` / `aria-rowindex`,
whose inconsistency "could have devastating effects on assistive technology
behavior"; `rowgroup` is structural only, with no keyboard interaction.
Roselli's warning stands: grid semantics are invisible to keyboard-only
users, who get no cue that arrow keys are needed. For a non-DOM grid this
reads as: group rows expose level, expanded state, position-in-set and
child count to the accessibility tree; leaves never carry expanded; the
flat grid carries none of it *(inference)*.

### 9.6 Documented confusions, as a checklist

Finder renamed "Arrange By" to "Use Groups" because "many users picked
Arrange By when they only wanted to sort", and the fixed group order versus
within-group sort "is disorientating until you realise what you've done";
make it obvious grouping is on and how to turn it off. Outlook's collapsed
default looked like missing mail. Excel copies hidden detail rows and SUM
ignores the collapse; copy and export must say whether they take visible
rows or all. Jira duplicates items under "Unassigned" when hierarchy and
grouping disagree, and a filter "breaks the ability to display the issues
in their hierarchy". AG Grid's `groupRows` cannot show aggregates;
`showOpenedGroup` and `groupHideOpenParents` are orthogonal and routinely
confused; single-child hiding ignores filters.

## 10 Grid-side versus query-side

### 10.1 The boundary the BI tools drew

Pivot and matrix visuals never aggregate client-side: Power BI's total row
"shows the result of evaluating the field across the entire filter
context"; Tableau's automatic totals come from the "disaggregated data".
Row tools (Airtable, Notion, Linear, Numbers) aggregate over the records
they hold, with the decomposable set. The documented rule *(inference)*:
decomposable aggregates and interval bucketing over an already-fetched
record are view-side; distinct counts, true averages over rows the grid
has not fetched (a `LIMIT`ed result), percentiles, and anything over the
unfetched population are query-side. ClickHouse's window functions
(`PARTITION BY` "defines how to break a resultset into groups" without
collapsing rows) are the query-side way to attach a group aggregate to
every row — a rewrite a tool can suggest when it cannot compute locally.

### 10.2 Recognising a result that arrived grouped

Standard SQL marks rollup rows by null keys plus a `GROUPING()` bitmask
("Returns a bit mask indicating which GROUP BY expressions are not included
in the current grouping set"; rightmost argument least significant in
PostgreSQL). ClickHouse differs in three ways that matter here: `WITH
ROLLUP` fills the already-aggregated key columns with **type defaults**
("set to 0 or empty line"), not nulls, unless `group_by_use_nulls = 1`;
its rollup rows are *appended after* the detail rows, finest first, grand
total last, so arrival order is not a layout; and `WITH TOTALS` is one
extra row "delivered as a separate block" in the formats that carry it —
which `ArrowStream` does not (§2.4) — whose contents may include rows a
`HAVING` later excluded (`totals_mode`, default `before_having`).
ClickHouse's `GROUPING()` "returns a bitmask", 1 for a subtotal row. Of
the three signals only a selected `GROUPING()` column survives arbitrary
user SQL unchanged, because a user can alias it; default-valued keys
collide with real zeros and empties and are unsafe to infer from
*(inference)*. A tool that wants rollup rows drawn as group headers or
footers has to re-place them by key prefix *(inference)*.

### 10.3 Keep the two states apart

Jira: "Your grouping and sorting will only be visible to you". Numbers:
turn categories off and the flat table is back. The BI pass reads both as
the rule that view grouping is per-view state distinct from the SQL the
user wrote: changing the grouping never rewrites the query, and the user's
`ORDER BY` stays recoverable — which is the same reason `SortState` keeps
its third, unsorted state.

## 11 How the APIs moved after shipping

The migration guides are the most direct record of what the authors would
have done first:

- **Booleans into enums.** AG Grid collapsed `groupUseEntireRow` /
  `groupMultiAutoColumn` into `groupDisplayType`; `groupSelectsChildren` /
  `groupSelectsFiltered` into `rowSelection.groupSelects`;
  `groupRemoveSingleChildren` / `groupRemoveLowestSingleChildren` into
  `groupHideParentOfSingleChild`; two suppress flags into one
  `suppressGroupChangesColumnVisibility`; `groupIncludeFooter` /
  `groupIncludeTotalFooter` into `groupTotalRow` / `grandTotalRow` — over
  three major versions.
- **Identity over remembered state.** `rememberGroupStateWhenNewData` was
  removed for `getRowId`; TanStack grew `autoResetExpanded: false`; Kendo
  deprecated its group-binding directive after a bug that left 50 of 1,000
  rows on clearing groups.
- **Split what was fused.** TanStack v9 separated aggregation from
  grouping; MUI added `groupId` to the aggregator; AG Grid gave tree data
  its own module and typed its group values.
- **Named semantics.** Tableau's "Total All Using" and Power BI's "Custom
  totals" name the over-displayed-rows alternative rather than silently
  picking it.
- **Caps and laziness.** Infragistics' ten grouped columns; SlickGrid's
  lazy totals; Glide's flat group index; AG Grid's warning that expand-all
  over unloaded groups "could potentially be a huge number of groups
  expanded".

## 12 The design space, mapped onto this tree

### 12.1 The shapes

**O1 — A grouping layer inside `recordtable`, group rows as a row kind.**
`Input` takes a host-owned `GroupState` beside `SortState`; the widget
turns `(Order, groups, expansion)` into a flat line sequence —
`{kind, record row | group}` — cached on the record and a state
generation, and draws a group line as a full-width row block (chevron,
indent, label, count) with aggregate cells aligned under their columns.
Leaves keep their record-row identity, ids, selection and the sort; paging
windows the line sequence; the cells key folds the generation in. This is
Telerik Flat, SlickGrid's DataView, AppKit's group row and Glide's mapper
in one, on the tree's own `tree.Flatten` precedent.

**O2 — A second widget composed on `Chrome`.** `widgets/groupedtable` with
its own `Input`, as play's per-attribute grid is built. Keeps `RenderGrid`
untouched, but duplicates the width protocol, the headers, the fit frame,
the cells cache and the sort, and every host that wants groups changes
its call.

**O3 — The host computes groups and hands the grid a line sequence.** Glide's
alpha shape: `Order` plus a list of header positions, the grid draws
headers where told. Minimal widget change; every host re-implements
partition, keys, expansion and aggregates, and play, tally and watchbill
would diverge.

**O4 — Query-side only.** play rewrites the SQL with `GROUP BY ... WITH
ROLLUP` (or `GROUPING SETS`) and the grid recognises rollup rows by a
`GROUPING()` column it injects. Exact aggregates of every kind over the
whole population; but it re-runs the query, cannot work on a result that
cannot be re-run, loses the leaf rows unless it also injects a window
query, and the other hosts have no SQL.

### 12.2 Against the criteria

| | O1 layer in `recordtable` | O2 second widget | O3 host-computed lines | O4 query-side |
|---|---|---|---|---|
| C1 every `RenderGrid` host gets it | ++ | − | −− | −− |
| C2 keeps record-row identity, sort, selection contract | ++ | + | + | − |
| C3 aggregate correctness across levels | + (decomposable set; leaves available at every level) | + | − (per host) | ++ |
| C4 frame cost, cells cache intact | + (one generation in the key) | − (new cache) | + | ++ (flat grid) |
| C5 works on a held result that cannot be re-run | ++ | ++ | ++ | −− |
| C6 drivable and reportable as play ops | + | + | + | + |
| C7 code moved or duplicated | + | −− | − | − |

O1 is the pass's recommendation. O4 is not an alternative to it but a
later complement: §10's recognition of query-side group rows is a play
feature over the same line sequence, triggered when a user's SQL already
rolls up *(inference)*.

### 12.3 Seams the shape needs, independent of which is chosen

- **A line sequence between `Order` and the etable.** Today display
  position = order index. Grouping inserts group lines; `displayRows`,
  `recordRow`, `selection` and `cellsKey` have to read lines instead.
- **A per-row block for group lines.** `Rows(row)` exists; its use with
  cells in the same row (aggregates) is what §9.1's aligned form needs and
  the tree widget does not exercise.
- **A key path and a three-valued expansion map** — `tree.State`'s shape,
  keyed by the group's value path rather than a node index.
- **Group state in the gesture route.** play's `set_table_options` and
  `get_table` carry the group spec and expansion so an agent and a person
  see one pane.
- **Width identity for the group row's label.** The label spans columns;
  it must not acquire a stored width of its own (ADR-0151).

### 12.4 A proposal for the dialogue to take or leave

Build O1 in `widgets/recordtable` with the semantics the passes converged
on — an ordered list of levels each naming an Arrow column and an optional
key function; groups identified by their key path; expansion three-valued
with a depth default; leaves sorted within groups by the header sort,
groups ordered by key with a per-level override by an aggregate; the null
group one labelled group at an edge; aggregates the decomposable set
computed from leaves at every level, placed in the group row aligned under
their columns; group rows unselectable and toggled by a whole-row click;
the gutter numbering leaves by record row and group rows not at all;
paging over lines. Defer sticky group rows (no substrate), interval
bucketing beyond the key-function seam, query-side recognition, per-row
heights and keyboard navigation, each with its trigger recorded.

### 12.5 The leeway use case, under O1

The per-attribute view (§2.6) is the strongest test of the shape, because
its leaves are not record rows. Two ways to meet it:

**Explode, then group.** The leeway `Driver` already walks entity → section
→ attribute → item; instead of writing `[]attrGridRow` of strings for a
bespoke renderer, the walk builds an **exploded Arrow record** — one row
per attribute (or item), with the source row number, the section name, the
attribute index and the membership identity as columns beside the value
and backbone columns — and hands it to `RenderGrid` like any other record.
Then today's per-attribute view is `GroupBy: [source row]` with the entity
as a collapsible group line carrying its attribute count; grouping by
section, by membership, or by membership then entity falls out of the same
layer with no leeway code in the widget; aggregates per tag (count, sum and
mean of a numeric value column) come free; sort, paging and the gesture
ops work because a line is a row of a record again; a leaf click maps to
the source entity through a column the host reads. What changes on screen:
sections **stack** instead of overlaying — one attribute per row, the other
sections' columns blank on it — so an entity is as tall as the *sum* of its
section runs rather than the largest, and the "cells line up within a
section" alignment is replaced by one-thing-per-row. The record card already
stacks sections under headers, so the Detail and Table panes would agree.

**Tree data over the record.** Keep the overlaid layout and give `Line` a
third kind — a sub-row of a record row, `{Row, Sub}` — with the record row
itself as an expandable parent. This is AG Grid's tree-data product beside
its row grouping, with the differences AG Grid documents: the parent is a
real row (selectable, numbered), counts include the parent, the key is the
path. It keeps the leeway walk in play and the widget learns a line kind
only play uses; grouping by membership is still out of reach, because the
leaf is not a row with columns.

The passes' reading favours the first: every library that gave hierarchy
its own row model (AG Grid tree data, Handsontable NestedRows, MUI tree
data) ended with a second set of semantics for identity, counts, filtering
and selection, and the overlaid layout is the one convention no surveyed
product shares. The cost is a visible change to a shipped view and a
rewrite of `play_table_attr.go` into a record builder.

**Scoped at the design dialogue of 2026-10-26.** Only the one-row-per-
attribute case is in scope for the leeway host, and its grouping is by a
plain-section attribute first (a backbone column of the exploded record —
the entity, a timestamp, a routing value) and then by the membership
*hierarchically*: a canonical path such as the JSON Pointer `jsonshred`
writes (`/metrics/cpu`; an array step is `_` with its index in the
attribute's parameters; `~0`, `~1`, `~2`, `~n` escape a tilde, a slash, a
literal `_` and a number kept as text) opens into one nested group per
proper prefix, and the leaf's own name is its last segment. Grouping the
flat per-DB-row grid by a tagged column is out of scope: the pane refuses
and points at the granularity toggle.

That second level is **tree data by path**, not fixed-level key grouping,
and the passes recorded what that costs where it was built (AG Grid's
`getDataPath`, MUI's `getTreeDataPath`, Handsontable's nested rows):

- The path is the identity. Rows under the same prefix are siblings;
  intermediate prefixes with no row of their own are *filler* groups, which
  AG Grid creates on demand, counts in child counts, and whose state "can
  reset" when a path changes.
- Depth varies per row, so a leaf and a subgroup are siblings at the same
  node — the unbalanced case AG Grid otherwise forbids — and their relative
  order has to be decided (an issue asked for them interleaved by key; AG
  Grid's default keeps them apart).
- Counts are descendant-inclusive in tree data and leaf-only in grouping;
  one of the two has to be named.
- Array steps: `_` with a parameter per element means `/tags/_` is one path
  for every element. Opening it per index (`/tags/0`, `/tags/1`) reads as
  the document; keeping `_` as one group with the index on the leaf reads
  as the schema. Both are a segmenter's choice, not the layout's.

Two consequences for the shape either way:

- **A group line may be a record.** When the level-0 key *is* the source
  row, the group line stands for a record row and a click on it should
  select, not only toggle — Finder's "clicking a header selects the group".
  The level has to say so, and the chevron keeps the toggle.
- **The group label should carry the structure's own vocabulary.** A
  section-level group reads `tagged · metric` as the record card does; a
  membership-level group reads the tag's identity with its parameters as
  the label's detail.

## 13 Open forks for a design dialogue

1. **Default expansion.** Expanded to all levels (Numbers, DevExtreme,
   Infragistics, Tabulator) or collapsed with a count (MUI, AG Grid,
   DevExpress). The count badge makes collapsed safe; a LIMITed analyst
   result favours expanded.
2. **Group row form.** Full-width caption with aligned aggregate cells
   (DevExpress, Telerik Flat) or a synthesised group column (AG Grid, MUI).
   The former keeps `Columns` unchanged and uses the row block that exists;
   the latter gives the label a column and a width.
3. **Aggregates: in the package or from the host.** A built-in decomposable
   set keyed by `Column`, a host-computed `Cell` per group, or both.
4. **Group order on a leaf sort.** Finder (groups fixed, leaves sort within)
   or AG Grid (groups reorder). The passes favour Finder with an explicit
   "order groups by aggregate".
5. **Paging.** Over lines (group rows count, collapsed leaves do not —
   Infragistics) or over leaves (groups may straddle pages — Kendo,
   DevExtreme's continuation rows).
6. **The null group's edge.** Last (AG Grid, Asana) with a first option, or
   first.
7. **Hiding the grouped column.** The host's business (it owns `Columns`)
   or a widget option. The libraries keep it a separate switch either way.
8. **Expansion persistence.** Session-only in the host's `State`, or stored
   like widths under ADR-0151's persist document.
9. **Query-side recognition.** Whether play's first cut reads a
   `GROUPING()` column at all, or only records the trigger.
10. **Selection of a group row.** Unselectable (AppKit) now; a whole-group
    action later if multi-select arrives.

11. **The per-attribute view: explode then group, or tree data over the
    record.** Settled 2026-10-26: explode then group; sections stack.
12. **A group line that is a record.** Whether a level may declare its key a
    selection, so the band click selects and the chevron toggles.
13. **Grouping a flat leeway grid by a tagged column.** Settled 2026-10-26:
    out of scope; the pane points at the granularity toggle.
14. **Under a path node, leaves before subgroups, after them, or
    interleaved by segment key.** A JSON object lists its members together;
    file managers put folders first.
15. **Array steps in a path: one group per index, or one `_` group with the
    index on the leaf.**
16. **The count on a path group: leaves beneath it, or every descendant
    including filler groups.**

## 14 Sources and incidents

Each pass's full URL list is reproduced from its report; the pages below
were fetched and read. Pages that returned 404, 403, 429 or failed DNS are
listed under incidents, as are the pages each pass declined to open under
the clean-room rule.

### AG Grid

Current docs: grouping, grouping-display-types, grouping-single-group-column,
grouping-multiple-group-columns, grouping-group-rows,
grouping-custom-group-columns, grouping-opening-groups, grouping-footers,
grouping-sorting, grouping-data, grouping-group-panel, grouping-row-selection,
aggregation, aggregation-filtering, aggregation-custom-functions,
aggregation-columns, row-selection-multi-row, tree-data, tree-data-paths,
tree-data-self-referential, tree-data-filtering, tree-data-selection,
tree-data-group-column, tree-data-opening-groups, pivoting, pivoting-totals,
pivoting-column-groups, pivoting-result-columns, server-side-model-grouping,
server-side-model-selection, server-side-model-datasource,
server-side-model-api-reference, server-side-model-configuration,
server-side-model-tree-data, row-ids, row-object, data-update-transactions,
data-update-row-data, grid-state, grid-options, column-properties,
keyboard-navigation, accessibility, row-height, client-side-model, errors/6,
upgrading-to-ag-grid-27 / -31 / -32-2 / -33 / -34 (all under
`https://www.ag-grid.com/javascript-data-grid/`). Archives: 26.0.0 and
26.1.0 grouping; 27.3.0 grouping, aggregation, aggregation-custom-functions,
grouping-sorting, tree-data, server-side-model-grouping, row-ids; 31.3.2
grouping, grouping-opening-groups, grouping-sorting, grouping-filtering,
grouping-group-order, grouping-unbalanced-groups, aggregation, tree-data,
server-side-model-grouping, server-side-model-selection, row-ids. Blog and
release notes: dev.to "What's new in AG Grid 28"; ag-grid.com blog for 29.3,
30, 34.2, 35.2. Issue conversation pages (prose only): ag-grid/ag-grid
#1599, #2419, #6722, #7072, #7357, #7446, #10227.

### Other grids

TanStack: v8 guide/grouping, api/features/grouping, guide/expanding,
api/core/row, guide/sorting, guide/migrating, guide/row-models; latest
guide/grouping, guide/aggregation, guide/expanding. react-table v7
useGroupBy (netlify mirror). MUI X: row-grouping (current and v5),
aggregation, server-side-data/row-grouping, tree-data,
recipes-row-grouping; mui/mui-x #12684, #6995 (prose). Handsontable:
row-parent-child, column-summary, forum thread 297. Glide: docs root,
sitemap, llms-full.txt; glide-data-grid #958, PR #924, PR #999 (prose).
SlickGrid: slickgrid-universal gitbook grouping page and wiki pages;
6pac/SlickGrid and mleibman/SlickGrid DataView wikis; v2.1 release notes
(groups.google.com); DefinitelyTyped rendered documentation for DataView,
Group, GroupingOptions. Tabulator 6.3: group, column-calcs,
components#group, events, callbacks#group, sort. DevExtreme: Grouping
guide, Group Summary, Custom Aggregate Function, columns API,
remoteOperations, Selection, Scrolling. Kendo Angular: grouping,
aggregates, expanded-state, group-sorting, templates, manual-grouping,
GroupableSettings, GroupResult, scroll-modes/virtual; Kendo React
sticky-group-headers; feedback.telerik.com 1360823. Syncfusion EJ2:
grouping, lazy-load-grouping, group-and-caption-aggregate, aggregates,
groupSettings API. Infragistics Ignite UI Angular: grid/groupby,
grid/summaries. Perspective: docs.rs perspective-client 5.1.0
ViewConfigUpdate, Aggregate, SortDir; perspectiveR article;
panel.holoviz.org Perspective pane.

### Native toolkits, immediate mode, ARIA

WPF: data-binding-overview; how-to-group-sort-and-filter-data-in-the-datagrid-control;
how-to-group-items-in-a-listview-that-implements-a-gridview;
PropertyGroupDescription, CollectionViewGroup, GroupStyle,
VirtualizingPanel.IsVirtualizingWhenGrouping, ICollectionViewLiveShaping;
optimizing-performance-controls; WPF 4.5 what's-new. UWP: data-binding-in-depth;
CollectionViewSource.IsSourceGrouped; ItemsStackPanel
AreStickyGroupHeadersEnabled and GroupHeaderPlacement; semantic-zoom.
Explorer / Outlook: Wikipedia File Explorer; helpdeskgeek grouping;
learn.microsoft.com Q&A 4277462; support.microsoft.com group-or-ungroup
messages; robert365.com ?p=609 and ?p=529. DevExpress WinForms: data-grid
grouping (3500); GridColumn.GroupInterval; sort group rows by summary
values (3076). Telerik WPF RadGridView: grouping basics, groupingmodes,
aggregates, group-footers, programmatic-grouping. Qt / KDE:
QSortFilterProxyModel, QAbstractProxyModel; forum.qt.io posts 13847 and
128558; QGroupingProxyModel (qadvanceditemviews); KCategorizedView (legacy
PyKDE API page). Apple: AppKit NSTableView.floatsGroupRows,
NSTableViewDelegate isGroupRow, NSOutlineView, NSOutlineViewDelegate
isGroupItem; UIKit UITableView and its Style enum (the .json documentation
endpoints). GTK 4: SectionModel, ListHeader, ListView, TreeListModel,
TreeListRow, TreeExpander. JavaFX 21: TreeTableView, TreeSortMode,
TreeItem. Dear ImGui: issues #3740, #6990, #3823; release v1.80; the
Tables wiki pointer page. egui: docs.rs egui_extras TableBuilder,
TableBody, TableRow; egui_table crate root, README, Table, TableDelegate,
PrefetchInfo. ARIA: APG treegrid pattern and treegrid-1 example, grid
pattern, grid-and-table-properties practice; MDN rowgroup role.

### Spreadsheets, BI, query engines

Excel: outline-group-data, insert-subtotals, design-the-layout-and-format-of-a-pivottable,
repeat-item-labels, group-or-ungroup-data-in-a-pivottable; Power Query
group-by. Google Sheets: answers 9060449, 1272900, 7572895. Numbers: guides
tan7e3e9a388 (mac and iOS), tan7cfc99b73, tanb8d90f744, tan3ec8281cf.
Tableau: calculations_totals_grandtotal_turnon, qs_hierarchies. Power BI:
matrix visual and its format settings. Airtable: grouping-records;
community threads 12930, 15834, 19705. Notion: views-filters-and-sorts;
release 2021-10-19; thomasjfrank.com grouping guide. Linear:
display-options; changelog 2021-02-12. Jira: organize-the-list-view;
manage-and-update-work-in-the-list-view (teaser); JRACLOUD-99126. Asana:
forum threads 698744 and 1048302. pandas: user_guide/groupby,
user_guide/advanced. Polars: DataFrame.group_by. ClickHouse: select/group-by,
grouping_function, window-functions. PostgreSQL: queries-table-expressions,
functions-aggregate, sql-select. Apache Arrow: cpp/compute.

### Design systems and research

W3C: APG treegrid and treeview patterns; WAI-ARIA 1.2 (downloaded and
text-searched). Adrian Roselli, "ARIA Grid As an Anti-Pattern". Carbon
data-table usage; Atlassian table-tree usage; GitLab Pajamas table; Ant
Design table; Material 1 data tables; GOV.UK table; WinUI tree-view. NN/g:
data-tables, accordions-complex-content, accordions-on-desktop. Few, *Show
Me the Numbers* ch. 8 (PDF, converted with pdftotext); Stolte, Tang,
Hanrahan, *Polaris* (PDF); the Tables tools page (Table Lens); HiTailor
(arXiv 2208.05821). Product pages: AG Grid grouping-group-rows,
grouping-single-group-column, grouping-data, grouping-sorting,
grouping-row-selection, grouping-opening-groups, grouping-footers,
row-numbers; MUI row-grouping; Wijmo sticky-group-headers; Kendo React
sticky-group-headers, TreeList keyboard-navigation; Kendo jQuery grouping
overview and aggregates; Syncfusion grouping; SSRS RowNumber function;
eclecticlight.co Finder groups; macmost.com Finder groups.

### Incidents

- **Unreachable or rate-limited:** perspective.finos.org and its docs
  (DNS, six attempts); observablehq.com documentation (HTTP 429, four
  attempts); Observable Plot's group page (429 three times) — those
  sections rest on snippets and recall and say so. Salesforce
  lightning-tree-grid (403); Lightning Design System tree-grid, Apple HIG
  outline views, m2.material.io data tables, a11ysupport.io treegrid
  (empty renders); mrexcel (403); elevenforum (403); QtCentre threads (TLS
  certificate expired); several 404s on renamed vendor pages, replaced by
  the pages listed above.
- **AG Grid's current API tables render descriptions client-side**, so the
  fetcher saw names and types only; option semantics were taken from the
  26.0.0, 27.3.0 and 31.3.2 archives, whose wording is older.
- **Declined under the clean-room rule** (search results that were source
  or package content, not opened): github.com blob and raw views of
  rerun's `expanded_rows.rs`, Amarok's `QtGroupingProxy.cpp`, an
  `imgui_tables.cpp` mirror and `imgui_demo.cpp`, JavaFX `TreeTableView.java`,
  `gtksectionmodel.c`, an oxiui-table `height.rs`; jsdocs.io rendered type
  declarations for ag-grid; unpkg's `slick.dataview.js`; Glide's `API.md`
  blob; a react-table typings gist; tessl.io and deepwiki.com copies of
  Perspective's docs; npm README tabs; the Files and diff tabs of every
  pull request cited; a `narwhals` package file, the docs.rs source view of
  Polars' `group_by/mod.rs`, a `@observablehq/plot` `group.d.ts`;
  Web Platform Tests fixtures, a Chromium code review, Mozilla searchfox
  pages; a Hegemonart `table.md` on jsdelivr; the `@atlaskit/table-tree`
  npm page; GitHub discussions carbon#11914 and nvaccess/nvda #13892 /
  #3023 (their prose would have been permitted; only snippets were used).
- No pass reported a clean-room breach.
