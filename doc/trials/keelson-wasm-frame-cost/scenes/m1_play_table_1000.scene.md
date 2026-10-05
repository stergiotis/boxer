---
type: reference
audience: package maintainer
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1888x1100"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_TABLE: "1"
  requires: ["clickhouse", "table:sailing.events"]
---
> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
# M1 — play, Table tab, 1000 rows
The keelson-wasm-frame-cost trial's M1 workload: play's Table tab over a real
result, held still for a few seconds so the counting pass-through
(`fffi2stub tee`) sees steady-state frames. The query is any populated table;
what is measured is the frame, not the data.
```sql
SELECT * FROM sailing.events ORDER BY 1 LIMIT 1000
```
```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":9000}
{"do":"capture","text":"m1_play_table"}
{"do":"click","name":"2026-05-02T12:56:27Z","comment":"row 3's timestamp cell, on a frame the cells were replayed from the cache"}
{"do":"sleep","settleMs":600}
{"do":"read","valueContains":"detail · row","role":"label","pattern":"detail · row (?P<r>\\d+)"}
{"do":"expect","of":"r","eq":3}
```
