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
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# treemap ratio

Treemap — a numeric colour on a DECLARED scale (ADR-0166 §SD2): the ramp pinned to the ratio's own 0–100 by color_min/color_max, its ticks suffixed with color_unit, and the status line saying which range is on

```sql
WITH p AS (
    SELECT database,
           table,
           sum(data_compressed_bytes)   AS comp,
           sum(data_uncompressed_bytes) AS raw
    FROM system.parts
    WHERE active AND data_uncompressed_bytes > 0
    GROUP BY database, table
  )
SELECT concat(database, '/', table) AS id,
       database                     AS parent,
       table                        AS label,
       toFloat64(raw)               AS value,
       'B'                          AS unit,
       round(100 * comp / raw, 1)   AS color,
       toFloat64(0)                 AS color_min,
       toFloat64(100)               AS color_max,
       '%'                          AS color_unit
FROM p
UNION ALL
SELECT database                                  AS id,
       ''                                        AS parent,
       database                                  AS label,
       toFloat64(0)                              AS value,
       'B'                                       AS unit,
       round(100 * sum(comp) / sum(raw), 1)      AS color,
       toFloat64(0)                              AS color_min,
       toFloat64(100)                            AS color_max,
       '%'                                       AS color_unit
FROM p
GROUP BY database
ORDER BY id
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2500}
{"do":"capture","text":"08_treemap_ratio","settleMs":600}
```
