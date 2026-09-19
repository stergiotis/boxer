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
    BOXER_PLAY_FOCUS_CHART: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chart reject

Chart — the SCHEMA-level reject (ADR-0172 §SD1): a result satisfying neither reading draws the contract and names its own columns back, and the dock strip carries the shape mark

```sql
SELECT database, name, engine
FROM system.tables
WHERE database NOT IN ('INFORMATION_SCHEMA', 'information_schema')
ORDER BY database, name
LIMIT 20
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1500}
{"do":"capture","text":"08_chart_reject"}
```
