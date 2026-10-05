---
type: explanation
audience: end-user
status: draft
title: The SQL Playground
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# The SQL Playground

`play` is a graphical ClickHouse SQL playground. You type a query, run it, and
inspect the result through several linked views. It speaks ClickHouse over HTTP
(default `http://localhost:8123/`, overridable with `--clickHouseUrl`) and pulls results
back as Arrow, so wide leeway-encoded tables arrive without a row-by-row decode.

You never write a `FORMAT` clause: the app rewrites the query to end with
`FORMAT ArrowStream` before sending it. One consequence — DDL such as
`TRUNCATE`/`CREATE` does not round-trip through the playground, because the
appended `FORMAT` clause is invalid on those statements.

## Tabs

The window is a dock of tabs you can rearrange and split (each, and every other
feature, is covered in depth on the **Features** page). They fall into three
groups by what they read.

**The editor**, top left:

- **Editor** — the SQL buffer plus a row of parameter editors (see below). The
  buffer is persisted across sessions.
- **History** — previously run queries.

**The tool panes**, beside the editor — each reads the buffer, or something
derived from it, so you keep them open while typing:

- **Docs** — the server's own documentation for whatever the caret is on.
- **Preview** — the editor's SQL re-rendered in its canonical, syntax-highlighted
  form. A parse aid, not a second query. Its "as sent to server" view shows the
  statement that actually ships.
- **Flow** — clause-level dataflow inside one statement: where the columns a
  `SELECT` returns come from.
- **Passes** — the pre-execute rewrite sequence the buffer runs through, in
  order, with any that were skipped marked.
- **Diagnostics** — parse advice and the full error texts (the other tabs only
  point here). When boxer's grammar can't parse the buffer, an `EXPLAIN AST`
  probe against the server distinguishes a boxer grammar gap from broken SQL.
- **Snippets** — a library of ready-to-run fragments with Insert/Replace buttons.
- **Model** — explain the buffer or fix its last error through a language model
  the host provides; answers land as text you insert. A question to a query
  goes to the chat app driving the window.
- **Vocabulary** — the functions this buffer can call, grouped by where each one
  runs: installed on the endpoint, expanded by play before the statement ships,
  or computed in play and never sent. Server functions are marked present or
  missing against what the endpoint actually carries.
- **Completion** — what may stand at the caret: component kinds, fields,
  introspection tables, the endpoint's tables and columns.
- **Glosses** — the catalog of value renderings (a temperature with its unit, a
  masked value, a URL as a link, markdown as prose, …), the buffer's rendering
  rules, and how each column of the current result resolved.
- **Experiments** — one leeway batch driven through a chosen rendering sink.

**The result panes**, below — these draw the query's result. Each has a
contract: column names it reads from the result, or CTEs it reads by name, and
it says what it wanted when the result does not fit (see *How a pane reads the
query* on the Features page):

- **Table** — the result grid. Select a row here to drive the Detail tab.
- **Detail** — beside the others: the per-row card for the selected row,
  whichever pane selected it.
- **Projection** — a leeway-shaped result's entities as a neighbour graph, clustered, laid out as a graph, with why the clusters are what they are.
- **Timeline** — events on a time axis, from `_tl_time` and friends.
- **Map** — an in-database-rendered geo raster over a pannable map, for tables
  with mercator columns (queries on its own, independent of the editor).
- **Vector field** — a wind or current drifting as particles over a map, from a
  `vector_field` CTE.
- **World** — a schematic world choropleth when the result names countries
  (ISO codes or names) alongside a numeric column.
- **Kanban** — the result as cards in lanes, from `lane` and `title` columns.
- **Chat** — a message transcript, from `ts`, `sender` and `body`.
- **Cards** — a paged grid of cards, from `card_*` columns.
- **Network** — a node-link graph, ranked top-down or left-right, from a CTE
  named `edges` (and optionally `vertices`).
- **Graphview** — the same `edges` graph laid out live: a force simulation, a
  tree or rings, that you drag, zoom and grow by query.
- **Sankey** — flow quantities, from a CTE named `flows`.
- **Distribution** — distribution summaries side by side, from
  `descriptiveStatistics(…)`.
- **Icicle** and **Treemap** — a hierarchy as a flamegraph or as nested areas,
  from a `stack` array or `id` / `parent` rows, plus a `value`.
- **Series** — numbers against a time axis: the first time column and every
  numeric one.
- **Chart** — bars, lines, points or a heatmap, from `x`, `y`, `z`, `series`.
- **Files** — the result as a tree of files, from a `path` column.
- **Graph** — the reactive query-graph: the buffer's CTEs as nodes; observe an
  intermediate node to point the result tabs at it, or bind one tab to it. Also
  hosts the **signals** editor (see below).
- **Schema** — a structural inspector over the result's schema.

## The Detail card and leeway data

The Detail tab picks one of two rendering paths automatically, from the result's
column names:

- **Leeway card** — when the columns are leeway-encoded (names like `id:id:…`
  and `tv:<section>:…`), the card groups them into the entity's plain section,
  its tagged sections, and the membership chips on each attribute. A `SELECT *`
  from a leeway table takes this path.
- **Ad-hoc grouping** — for ordinary SQL results (aliased or aggregated
  columns), columns are grouped by name prefix into pinned / relations / data /
  meta sections.

On both paths a column can carry a **gloss** — a named rendering such as
`` `t@gloss/temperature;unit=C` `` or `` `notes@text/markdown` `` — declared by
alias, by the `gloss(…)` macro, or by a `-- play: gloss` rule; the Table shows
its one-line face and Detail its block face where there is one. The Features
page covers the catalog.

## Parameters and signals

Top-level `SET param_<name> = <value>` statements are lifted out of the body and
shipped to ClickHouse on the request URL, so a `{name:Type}` placeholder in the
query is substituted server-side:

```sql
SET param_event = 'DDOS';
SELECT * FROM anchor.facts
WHERE has(`tv:symbol:value:val:s:124::I:0::data`, {event:String})
```

A placeholder *without* a `SET` line is a live **signal** instead: panels
publish values under shared names as you interact (the selected row, the Map
viewport, the Timeline extent), any query can reference them, and the Graph
tab's **signals** section is where you inspect them or set one by hand. The
**Features** page covers the constant-vs-signal model, the **Live** re-run
toggle, and what happens when a placeholder is left unfilled.

## Where the data comes from

The table you query depends on your deployment — a boxer deployment typically
exposes `boxer.facts`. For local exploration there is a self-contained demo
table, `anchor.facts`. The **Example queries** page covers how to load it and a
verified query set that walks each tab above.
