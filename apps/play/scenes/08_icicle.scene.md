---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1888x1100"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_ICICLE: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# icicle

Icicle — the result as an icicle plot (ADR-0160): one row per depth, width as value, over the folded stack/value contract; the same population the Sankey draws, read as a containment hierarchy rather than a flow

```sql
WITH
  ops AS (
    SELECT ownOp
    FROM default.planes_mercator_sample100
    WHERE ownOp != '' AND t != '' AND desc != '' AND altitude > 0
    GROUP BY ownOp
    ORDER BY count() DESC
    LIMIT 8
  ),
  legs AS (
    SELECT ownOp AS op,
           splitByChar(' ', desc)[1] AS maker,
           t AS model,
           concat(toString(intDiv(altitude, 10000) * 10), 'k ft') AS band
    FROM default.planes_mercator_sample100
    WHERE ownOp IN (SELECT ownOp FROM ops)
      AND t != '' AND desc != '' AND altitude > 0
  )
SELECT [op, maker, model, band] AS stack, count() AS value, 'positions' AS unit
FROM legs
GROUP BY stack
ORDER BY value DESC
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2500}
{"do":"capture","text":"08_icicle","settleMs":600}
{"do":"click","name":"flame"}
{"do":"capture","text":"08_icicle_flame","settleMs":600}
```
