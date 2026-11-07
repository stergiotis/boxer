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
    BOXER_PLAY_FOCUS_PROJECTION: "1"
  requires: ["clickhouse", "table:boxer.facts"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# projection

Projection over the rows of `boxer.facts` as they are stored: each row a
leeway card, its features read off the card's sections (ADR-0289), laid out
by the neighbour-embedding force model with clusters as auras, its nodes tied
to the selection signal. Projection reads leeway-shaped results only; a
plain table's rows, which this scene projected before ADR-0289, are
declined with the reason. What clusters appear depends on the facts the
server holds. The scene asserts a cluster count and the projector chip's phase.

```sql
SELECT * FROM boxer.facts LIMIT 1500
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":4000}
{"do":"click","name":"Compute projection","role":"button","settleMs":600}
{"do":"read","valueContains":"cluster(s)","role":"label","pattern":"(?P<clusters>\\d+) cluster\\(s\\)"}
{"do":"expect","of":"clusters","min":1}
{"do":"read","id":9170861174924845122,"pattern":"^(?P<phase>idle|extracting|running|cancelling|done|failed|cancelled)$","comment":"the projector chip: a badge whose text is the phase, anchored by its id (projector-fsm/chip) since its text is what is read"}
{"do":"expect","of":"phase","is":"done"}
{"do":"capture","text":"09_projection","settleMs":5000}
```
