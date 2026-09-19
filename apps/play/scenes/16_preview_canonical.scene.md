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
    BOXER_PLAY_FOCUS_PREVIEW: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# preview canonical

Preview — the canonical form the nanopass pipeline rewrites the buffer into, before any handle resolution

```sql
SELECT t, countIf(altitude > 30000) AS cruising, countIf(altitude <= 30000) AS lower, count() AS total
FROM default.planes_mercator_sample100
WHERE t != ''
GROUP BY t
HAVING total > 5
ORDER BY total DESC
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"16_preview_canonical"}
```
