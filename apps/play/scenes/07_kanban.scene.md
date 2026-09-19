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
    BOXER_PLAY_FOCUS_KANBAN: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# kanban

Kanban — the result as a board (ADR-0122): the lane/title columns by name, an optional lanes node fixing the column order

```sql
WITH lanes AS (
  SELECT arrayJoin(['A320', 'B738', 'A20N']) AS lane
)
SELECT t AS lane, r AS title, any(`desc`) AS subtitle
FROM default.planes_mercator_sample100
WHERE t IN ('A320', 'B738', 'A20N') AND r != ''
GROUP BY t, r
ORDER BY lane, title
LIMIT 20 BY lane
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"07_kanban"}
```
