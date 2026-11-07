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

Table — a leeway-encoded result: backtick handles resolve to physical column names, typed cells, the row grid; the tour's first scene, so it ends on play's own help

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
{"do":"note","text":"--- F1 over the window opens play's own help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"The SQL Playground","role":"label","comment":"the reader shows play's overview, not the index"}
{"do":"capture","text":"01_table_leeway_help"}
```
