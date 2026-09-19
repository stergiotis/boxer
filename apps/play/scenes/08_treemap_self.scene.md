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

# treemap self

Treemap — a container with a quantity of its own (ADR-0166 §SD3): every table under a mebibyte is rolled up into its database, which then gets a cell of its own inside its own box

```sql
WITH p AS (
    SELECT database AS db, table AS tbl, sum(bytes_on_disk) AS bytes
    FROM system.parts
    WHERE active
    GROUP BY db, tbl
)
SELECT concat('db:', db)                        AS id,
       ''                                       AS parent,
       db                                       AS label,
       toFloat64(sumIf(bytes, bytes < 419430400)) AS value,
       'bytes'                                    AS unit
FROM p
GROUP BY db
UNION ALL
SELECT concat('tbl:', db, '.', tbl) AS id,
       concat('db:', db)            AS parent,
       tbl                          AS label,
       toFloat64(bytes)             AS value,
       'bytes'                      AS unit
FROM p
WHERE bytes >= 419430400
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2500}
{"do":"click","name":"full"}
{"do":"capture","text":"08_treemap_self","settleMs":600}
```
