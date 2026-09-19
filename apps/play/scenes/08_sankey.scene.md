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
    BOXER_PLAY_FOCUS_SANKEY: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# sankey

Sankey — the result as a flow-quantity diagram (ADR-0159): a required flows channel carrying source/target/value, plus an optional nodes channel whose stage column selects the alluvial reading

```sql
WITH
  ops AS (
    SELECT ownOp
    FROM default.planes_mercator_sample100
    WHERE ownOp != '' AND t != '' AND altitude > 0
    GROUP BY ownOp
    ORDER BY uniq(icao) DESC
    LIMIT 4
  ),
  models AS (
    SELECT t
    FROM default.planes_mercator_sample100
    WHERE t != '' AND altitude > 0 AND ownOp IN (SELECT ownOp FROM ops)
    GROUP BY t
    ORDER BY count() DESC
    LIMIT 8
  ),
  legs AS (
    SELECT ownOp AS op,
           t     AS model,
           multiIf(altitude < 10000, 'below 10k',
                   altitude < 25000, '10-25k',
                   altitude < 35000, '25-35k',
                   'above 35k')     AS band
    FROM default.planes_mercator_sample100
    WHERE ownOp IN (SELECT ownOp FROM ops)
      AND t IN (SELECT t FROM models)
      AND altitude > 0
  ),
  flows AS (
    SELECT concat('0:', op) AS source, concat('1:', model) AS target, count() AS value
    FROM legs
    GROUP BY source, target
    UNION ALL
    SELECT concat('1:', model) AS source, concat('2:', band) AS target, count() AS value
    FROM legs
    GROUP BY source, target
  ),
  nodes AS (
    SELECT DISTINCT concat('0:', op) AS id, op AS label, 0 AS stage FROM legs
    UNION ALL
    SELECT DISTINCT concat('1:', model) AS id, model AS label, 1 AS stage FROM legs
    UNION ALL
    SELECT DISTINCT concat('2:', band) AS id, band AS label, 2 AS stage FROM legs
  )
SELECT * FROM flows ORDER BY value DESC
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":3000}
{"do":"capture","text":"08_sankey"}
```
