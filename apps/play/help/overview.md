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
(default `http://localhost:8123/`, overridable with `--url`) and pulls results
back as Arrow, so wide leeway-encoded tables arrive without a row-by-row decode.

You never write a `FORMAT` clause: the app rewrites the query to end with
`FORMAT ArrowStream` before sending it. One consequence — DDL such as
`TRUNCATE`/`CREATE` does not round-trip through the playground, because the
appended `FORMAT` clause is invalid on those statements.

## Tabs

The window is a dock of tabs you can rearrange and split:

- **Editor** — the SQL buffer plus a row of parameter editors (see below). The
  buffer is persisted across sessions.
- **Preview** — the editor's SQL re-rendered in its canonical, syntax-highlighted
  form. A parse aid, not a second query.
- **History** — previously run queries.
- **Table** — the result grid. Select a row here to drive the Detail tab.
- **Projection** — a derived/projection view over the result columns.
- **Timeline** — plots time-shaped results on a horizontal time axis, when the
  result matches the timeline column contract (see the example-queries page).
- **Detail** — the per-row card for the row selected in Table.

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

## Parameters

Top-level `SET param_<name> = <value>` statements are lifted out of the body and
shipped to ClickHouse on the request URL, so a `{name:Type}` placeholder in the
query is substituted server-side:

```sql
SET param_event = 'DDOS';
SELECT * FROM anchor.facts
WHERE has(`tv:symbol:value:val:s:m:0:24:0::data`, {event:String})
```

## Where the data comes from

The table you query depends on your deployment — a boxer deployment typically
exposes `spinnaker.facts`. For local exploration there is a self-contained demo
table, `anchor.facts`. The **Example queries** page covers how to load it and a
verified query set that walks each tab above.
