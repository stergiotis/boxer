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
