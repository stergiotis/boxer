---
type: reference
audience: end-user
status: draft
title: Snippets
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Snippets

A small library of ready-to-run queries against `anchor.facts`, the
self-contained demo table. Click **Insert** above any block to splice it into
the editor at the cursor; place the caret where you want it first (an empty
editor takes the snippet whole). Do not add a `FORMAT` clause — the app appends
one. The verified, explained versions live on the **Example queries** page.

## Whole entities

`SELECT *` returns the full leeway row, so the Detail tab renders the leeway
card. Run it, then click a row in Table.

```sql
SELECT * FROM anchor.facts LIMIT 50
```

## One scenario

Narrow to a single scenario to vary which tagged sections are populated.

```sql
SELECT * FROM anchor.facts
WHERE hasAny(`tv:symbol:value:val:s:m:0:24:0::data`,
             ['DDOS', 'PORT_SCAN', 'SQL_INJECTION'])
```

## Query graph (CTEs become nodes)

Each top-level CTE splits into its own node of the reactive query-graph
(ADR-0097): `recent` and `by_kind` below become nodes — `by_kind` reads
`recent`, and the final `SELECT` reads `by_kind`. Today the chain fuses back into
a single query (identical to running it inline) and the panels observe the final
(sink) node; a forthcoming graph view surfaces the structure.

```sql
WITH
  recent AS (
    SELECT * FROM anchor.facts LIMIT 50
  ),
  by_kind AS (
    SELECT
      `tv:symbol:value:val:s:m:0:24:0::data`[1] AS event_type,
      count()                                   AS n
    FROM recent
    GROUP BY event_type
  )
SELECT event_type, n
FROM by_kind
ORDER BY n DESC
```

## One entity by id

The ids 10005, 10010, 10015, 10020, 500003 carry the sparse `geoArea` section.

```sql
SELECT * FROM anchor.facts WHERE `id:id:u64:2k:0:0:` = 10005
```

## Timeline contract

Map the `timeRange` section onto the canonical slot columns the Timeline tab
reads; timestamps must be `DateTime64`.

```sql
SELECT
  `tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`[1] AS _tl_time,
  `tv:timeRange:endExcl:val:z64:2k:0:0:0::data`[1]   AS _tl_time_end,
  `tv:symbol:value:val:s:m:0:24:0::data`[1]          AS _tl_lane
FROM anchor.facts
WHERE length(`tv:timeRange:beginIncl:val:z64:2k:0:0:0::data`) > 0
ORDER BY _tl_time
```

## Timeline regions (background bands)

Unlike the others, this block belongs in the Timeline tab's **Background bands**
editor, not the main editor — Insert here would put it in the wrong box. Bands
return the `_tl_band_*` slots: a `from`/`to` `DateTime64` pair, a
`_tl_band_color` that must be an IDS token name (`neutral.default`,
`accent.default`, `warning.default`, `error.default`, …), and an optional
`_tl_band_label`. The `_time_data_min` / `_time_data_max` tokens are replaced
with the main result's time extent, so a band can be sized relative to whatever
the query returned. This one shades the middle 50% of the visible window —
adjust the `0.25` / `0.75` fractions to move or resize the region.

```sql
WITH _time_data_min AS lo,
     _time_data_max AS hi
SELECT
  addMilliseconds(lo, toInt64(0.25 * dateDiff('millisecond', lo, hi))) AS _tl_band_from,
  addMilliseconds(lo, toInt64(0.75 * dateDiff('millisecond', lo, hi))) AS _tl_band_to,
  'accent.default'                                                     AS _tl_band_color,
  'mid 50% of window'                                                  AS _tl_band_label
```

## Ad-hoc columns

Aliased columns are not leeway-shaped, so Detail falls back to prefix grouping
and Table shows a plain grid.

```sql
SELECT
  `id:id:u64:2k:0:0:`                       AS id,
  `id:naturalKey:y:g:0:0:`                  AS natural_key,
  `tv:symbol:value:val:s:m:0:24:0::data`[1] AS event_type
FROM anchor.facts
ORDER BY id
```

## Parameter prelude

A top-level `SET param_*` statement rides the request URL; the `{name:Type}`
placeholder is substituted by ClickHouse.

```sql
SET param_event = 'DDOS';
SELECT * FROM anchor.facts
WHERE has(`tv:symbol:value:val:s:m:0:24:0::data`, {event:String})
```

## Trivial states

```sql
SELECT * FROM anchor.facts LIMIT 1
```

```sql
SELECT 1 AS hello, now() AS ts
```
