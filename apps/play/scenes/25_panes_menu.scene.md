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

# panes menu

Panes menu — the ADR-0097 prose surface: every pane, and for a rejecting one the reason its channels cannot be filled

```sql
SELECT t, count() AS n FROM default.planes_mercator_sample100 WHERE t != '' GROUP BY t ORDER BY n DESC
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"click","name":"Panes","comment":"a menu, not a launch state"}
{"do":"capture","text":"25_panes_menu","settleMs":600}
```
