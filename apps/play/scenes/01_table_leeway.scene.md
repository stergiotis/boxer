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
  requires: ["clickhouse", "table:anchor.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# table leeway

Table — a leeway-encoded result: backtick handles resolve to physical column names, typed cells, the row grid

```sql
SELECT `id:id`, `symbol:value`, `text:text`, `geoPoint:pointLat`, `geoPoint:pointLng`, `timeRange:beginIncl`
FROM anchor.facts
ORDER BY `id:id`
LIMIT 200
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"01_table_leeway"}
```
