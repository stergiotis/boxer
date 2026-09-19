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
    BOXER_PLAY_FOCUS_TREEMAP: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# treemap

Treemap — the same population the Icicle scene draws, read as nested areas (ADR-0166): area is the position count, colour is the typical altitude, and the drill navigation replaces the depth axis

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
           altitude AS alt
    FROM default.planes_mercator_sample100
    WHERE ownOp IN (SELECT ownOp FROM ops)
      AND t != '' AND desc != '' AND altitude > 0
  )
SELECT [op, maker, model] AS stack,
       count()            AS value,
       round(avg(alt))    AS color,
       'positions'        AS unit
FROM legs
GROUP BY stack
ORDER BY value DESC
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2500}
{"do":"capture","text":"08_treemap","settleMs":600}
{"do":"click","name":"4 deep"}
{"do":"capture","text":"08_treemap_deep","settleMs":600}
{"do":"click","name":"full"}
{"do":"capture","text":"08_treemap_all","settleMs":600}
{"do":"click","name":"drill"}
{"do":"click","x":200,"y":719}
{"do":"capture","text":"08_treemap_drill","settleMs":600}
```
