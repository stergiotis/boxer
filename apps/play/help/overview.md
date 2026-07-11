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
feature, is covered in depth on the **Features** page):

- **Editor** — the SQL buffer plus a row of parameter editors (see below). The
  buffer is persisted across sessions.
- **Preview** — the editor's SQL re-rendered in its canonical, syntax-highlighted
  form. A parse aid, not a second query.
- **History** — previously run queries.
- **Table** — the result grid. Select a row here to drive the Detail tab.
- **Projection** — a derived/projection view over the result columns.
- **Timeline** — plots time-shaped results on a horizontal time axis, when the
  result matches the timeline column contract (see the example-queries page).
- **Snippets** — a library of ready-to-run fragments with Insert/Replace buttons.
- **Detail** — the per-row card for the row selected in Table.
- **Map** — an in-database-rendered geo raster over a pannable map, for tables
  with mercator columns (queries on its own, independent of the editor).
- **World** — a schematic world choropleth when the result names countries
  (ISO codes or names) alongside a numeric column.
- **Graph** — the reactive query-graph: the buffer's CTEs as nodes; observe an
  intermediate node to point the result tabs at it. Also hosts the **signals**
  editor (see below).
- **Schema** — a structural inspector over the result's schema.
- **Diagnostics** — parse advice and the full error texts (the other tabs only
  point here). When boxer's grammar can't parse the buffer, an `EXPLAIN AST`
  probe against the server distinguishes a boxer grammar gap from broken SQL.

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

## Parameters and signals

Top-level `SET param_<name> = <value>` statements are lifted out of the body and
shipped to ClickHouse on the request URL, so a `{name:Type}` placeholder in the
query is substituted server-side:

```sql
SET param_event = 'DDOS';
SELECT * FROM anchor.facts
WHERE has(`tv:symbol:value:val:s:m:0:24:0::data`, {event:String})
```

A placeholder *without* a `SET` line is a live **signal** instead: panels
publish values under shared names as you interact (the selected row, the Map
viewport, the Timeline extent), any query can reference them, and the Graph
tab's **signals** section is where you inspect them or set one by hand. The
**Features** page covers the constant-vs-signal model, the **Live** re-run
toggle, and what happens when a placeholder is left unfilled.

## Where the data comes from

The table you query depends on your deployment — a boxer deployment typically
exposes `spinnaker.facts`. For local exploration there is a self-contained demo
table, `anchor.facts`. The **Example queries** page covers how to load it and a
verified query set that walks each tab above.
