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
    BOXER_PLAY_FOCUS_VOCABULARY: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# vocabulary

Vocabulary — what this buffer may call and where each name runs (ADR-0174), as an outline: population, then declaring family, then the call prototypes in a column of their own, marked against what the endpoint carries

```sql
SELECT count() AS pings FROM default.planes_mercator_sample100
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":4000}
{"do":"capture","text":"15_vocabulary","settleMs":800}
{"do":"focus","role":"text_input","settleMs":400}
{"do":"type","role":"text_input","text":"gather"}
{"do":"capture","text":"15_vocabulary_filtered","settleMs":800}
```
