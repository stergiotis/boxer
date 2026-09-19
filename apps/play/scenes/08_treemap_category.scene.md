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

# treemap category

Treemap — a CATEGORICAL colour column (ADR-0166 §SD2): the qualitative key below the control row, and the inheritance rule that colours a container only when its descendants agree

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
       multiIf(avg(alt) < 10000, 'low',
               avg(alt) < 25000, 'mid',
                                 'high') AS color,
       'positions'        AS unit
FROM legs
GROUP BY stack
ORDER BY value DESC
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2500}
{"do":"capture","text":"08_treemap_category","settleMs":600}
{"do":"click","name":"full"}
{"do":"capture","text":"08_treemap_category_full","settleMs":600}
```
