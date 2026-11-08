---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1600x1000
  stepSettleMs: 300
  env:
    BOXER_PLAY_WINDOW_SIZE: "1568x900"
    BOXER_PLAY_AUTORUN: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# announce

A settled run is announced: the row count reaches the accessibility tree as a
live region, which a screen reader speaks and a scene can wait on
(ADR-0363 (proposed) §M2). The Tab presses after it are traced: focus must
move once per key and never fall to the window root.

```sql
SELECT number FROM system.numbers LIMIT 3
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"wait","value":"3 rows","role":"label","comment":"the announcement, not the status bar's longer line"}
{"do":"tree","text":"3 rows","role":"label"}
{"do":"key","text":"Tab","focusTrace":true}
{"do":"key","text":"Tab","focusTrace":true}
{"do":"key","text":"Tab","focusTrace":true}
```
