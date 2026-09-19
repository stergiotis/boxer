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
    BOXER_PLAY_FOCUS_EXPERIMENTS: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# experiments sparks

Experiments — the same fixture through the text sinks: the topology sparkline, one line per entity, encoding section arity, column canonical types and membership counts without printing a single value

```sql
SELECT 1 AS ok
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"click","name":"topo","comment":"the single-line topology sparkline","settleMs":600}
{"do":"capture","text":"32_experiments_sparks_topo","settleMs":600}
{"do":"click","name":"braille","comment":"the braille density variant","settleMs":600}
{"do":"capture","text":"32_experiments_sparks_braille","settleMs":600}
{"do":"click","name":"json","comment":"the canonical card-JSON of ADR-0018","settleMs":600}
{"do":"capture","text":"32_experiments_sparks_json","settleMs":800}
```
