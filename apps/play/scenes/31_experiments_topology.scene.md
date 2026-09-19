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

# experiments topology

Experiments — the leeway sink playground: the built-in fixture driven through the TopologySink, whose treemap draws an entity's shape (plain sections, the co-section group, attributes) with value/tag presence as cell colour

```sql
SELECT 1 AS ok
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"click","name":"topology","comment":"switch the sink from the default card view","settleMs":600}
{"do":"capture","text":"31_experiments_topology","settleMs":800}
```
