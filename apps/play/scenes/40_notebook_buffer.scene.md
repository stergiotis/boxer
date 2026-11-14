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
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# notebook buffer

The buffer as a sealed notebook's records (ADR-0371 §SD6): **Copy as notebook** writes each WITH query as a record cell and the buffer's own SELECT as the last, and says so in the status bar. A buffer with a WITH expression is refused, with the part a notebook cannot hold. Nothing runs: the scene needs no server.

```sql
WITH recent AS (SELECT number AS n FROM numbers(10)),
     by_kind AS (SELECT n % 2 AS kind, count() AS c FROM recent GROUP BY kind)
SELECT * FROM by_kind ORDER BY kind
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"click","name":"Copy as notebook","role":"button"}
{"do":"wait","valueContains":"copied notebook","role":"label"}
{"do":"capture","text":"40_notebook_buffer"}
```
