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
    BOXER_PLAY_FOCUS_TABLE: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# subquery toggle

Subquery toggle — the editor's account of the query the caret is in: the tinted extent, its underlined environment, and the Run subquery button the toggle adds

```sql
WITH busy AS (
  SELECT t, count() AS n FROM default.planes_mercator_sample100 WHERE t != '' GROUP BY t
)
SELECT t, n FROM busy WHERE n > (SELECT avg(n) FROM busy) ORDER BY n DESC
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"click","name":"Subquery","role":"check_box"}
{"do":"capture","text":"26_subquery_toggle","settleMs":600}
```
