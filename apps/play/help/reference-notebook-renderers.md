---
type: reference
audience: end-user
status: draft
title: Notebook renderers
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Notebook renderers

A sealed notebook (ADR-0371) is a markdown report that holds its own data. A
presentation cell in it is a `sql` fence with a renderer as its second word,
and every renderer but `table` is one of play's result panes under its pane
id. This page lists them with what each reads. How a pane behaves beyond its
contract is in its section of the **Features** page.

A cell only picks columns of a stored record and names them, so the names
are how a record is fitted to a renderer:

```sql chart
SELECT t AS x, region AS series, p99 FROM readings
```

## What each renderer reads

"Member" is a query in the cell's own `WITH` list. Play finds such a relation
as a CTE of the buffer; a notebook cell names it itself, because a cell reads
records and nothing else.

| Renderer | Reads | Also reads |
| --- | --- | --- |
| `table` | any columns, as the cell names them | a gloss in an alias, `p99 AS "p99@gloss/duration;unit=ms"` |
| `chart` | `x`, and every number left as a lane; or `x`, `y` and `z` as a heatmap | `series` to split the rows into groups |
| `series` | the first time column as the axis and every numeric column as a lane, whatever they are called | members `scores` and `spans` |
| `timeline` | `_tl_time` | `_tl_time_end`, `_tl_lane`, `_tl_label`; member `bands` |
| `dist` | `series`, `n`, `ps` and `qs`, one row per distribution | |
| `graphview` | member `edges` with `source` and `target` | members `vertices` and `graph_opts` |
| `network` | member `edges` with `source` and `target` | member `vertices` |
| `sankey` | member `flows` with `source`, `target` and `value` | member `nodes` |
| `files` | `path` | `size`, `mtime`, `is_dir`; every other column is a column of the listing |
| `icicle` | a `stack` array and a `value`; or `id`, `parent` and `value`, one row per node | |
| `treemap` | as `icicle` | `color`, a number or a category |
| `world` | a text column of ISO country codes or country names | a numeric column to fill by |
| `kanban` | `lane` and `title` | `subtitle`, `dot_<label>` tallies; member `lanes` |
| `bowtie` | `top_event`, `side` and `path`, one row per barrier of a path | `step`, `barrier`, `status`, `evidence` |
| `riskmatrix` | `id`, `p_lo`, `p_hi`, `impact_lo` and `impact_hi` | `name`, the four `inherent_*` columns |
| `tolerance` | `service`, `tolerance_hours`, `scenario` and `recovery_hours` | `tolerance` and `recovery` under a `gloss/duration` in place of the `_hours` pair |

A renderer whose required input is missing or of another shape draws nothing
and says what it wanted, in the cell. An optional member that does not fit
is left out, with a line above the picture saying so.

A member with no `FROM` is a row of settings:

```sql graphview
WITH edges AS (SELECT source, target FROM calls),
     graph_opts AS (SELECT 'hierarchical' AS layout)
SELECT * FROM edges
```

## What differs from the pane in play

- **Nothing is selected.** A click that would write a signal in play writes
  nothing, and a pane that reads a signal reads none.
- **Controls last as long as the view.** A chart switched to bars is not
  written into the notebook.
- **The time column must be a `DateTime64` or a `Date` in the record.**
  ClickHouse writes a plain `DateTime` to Arrow as an unsigned integer, and a
  pane then sees a number.
- **The caps are the pane's.** A pane that reads 100,000 rows in play reads as
  many of a record.

## Panes that are not renderers

Table, Detail, Schema, Cards and Chat draw from their play window, and Map,
Vector field and Projection need a source a stored record is not. `table` is
the notebook's own table, not play's Table pane.

## A cell for each

The sample notebook `renderers.md` in
`public/semistructured/markdown/mdnotebook/samples` has one cell per renderer
over the smallest record that satisfies it, and every record there makes its
own rows. The widget gallery's **notebook samples** demo draws it. Two more
of its cells, as they stand in the sample:

```sql timeline
SELECT begins AS _tl_time, ends AS _tl_time_end, region AS _tl_lane FROM slow_hours
```

```sql riskmatrix
SELECT id, name, p_lo, p_hi,
       impact_lo AS "impact_lo@gloss/money;unit=EUR",
       impact_hi AS "impact_hi@gloss/money;unit=EUR"
FROM risks
```
