---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1600x1000
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1600x1000"
    BOXER_PLAY_FOCUS_COMPLETION: "1"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Completion pane — the field domain, decided by the sibling argument

The Completion pane (ADR-0190) against an unterminated buffer: the caret sits
inside a call, and the pane shows the domain that position draws from. The
buffer is seeded at launch, which is why each case is a scene of its own.

Anchors are against role `label`: egui puts a label's text in the accessible
value and leaves the name empty, and the pane's cells are all labels. `role` is
always pinned, because egui emits a `text_run` child under some labels carrying
the same text. Row anchors are exact (`value`, not `valueContains`): the
domains are full of superstrings — SysCpu inside SysCpuInfo — and a substring
anchor matches both, which is an ambiguity rather than a hit.

```sql
SELECT tupleElement(LW_COMPONENT('SysMem'), 'Tot
```

```jsonl trace
{"do":"note","text":"ADR-0190 M1 — the field domain, decided by the sibling argument"}
{"do":"sleep","settleMs":2500,"comment":"as above"}
{"do":"wait","valueContains":"tuple element","role":"label","settleMs":600,"comment":"the heading says the domain came from the tuple, not from the clause"}
{"do":"wait","value":"TotalBytes","role":"label","comment":"a field of the kind named beside it"}
{"do":"wait","value":"DateTime64(9, \u0027UTC\u0027)","role":"label","comment":"the type column: the elements carry their own types, which is what tells two same-named fields apart"}
```
