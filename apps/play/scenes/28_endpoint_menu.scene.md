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
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# endpoint menu

Endpoint menu — the connection switcher, including the ad-hoc dataset endpoints of ADR-0134

```sql
SELECT 1 AS hello, now() AS ts
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"click","name":"Endpoint"}
{"do":"capture","text":"28_endpoint_menu","settleMs":600}
```
