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
    BOXER_PLAY_FOCUS_DOCS: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# docs

Docs — the reference for what the caret is on, read from the server actually being queried (system.documentation): the look-up box that reaches a name the buffer does not have yet, and the kind selector a name carrying several kinds gets

```sql
SELECT t AS aircraft_type, count() AS pings, argMax(icao, ground_speed) AS fastest
FROM default.planes_mercator_sample100
WHERE altitude > 0 AND t != ''
GROUP BY t
ORDER BY pings DESC
LIMIT 15
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"focus","role":"text_input","comment":"the look-up box, nameless so by role","settleMs":400}
{"do":"type","role":"text_input","text":"Merge","settleMs":2500}
{"do":"capture","text":"14_docs","settleMs":600}
{"do":"click","name":"Table Engine","comment":"the same name, a different kind — not the one the pane opened on"}
{"do":"capture","text":"14_docs_kind","settleMs":800}
```
