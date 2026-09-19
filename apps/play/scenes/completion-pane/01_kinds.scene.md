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

# Completion pane — the component-kind domain, rendered

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
SELECT LW_COMPONENT('Sys
```

```jsonl trace
{"do":"note","text":"ADR-0190 M1 — the component-kind domain, rendered"}
{"do":"sleep","settleMs":2500,"comment":"play mounts a dock, restores a layout and runs its pass pipeline before the pane has anything to draw"}
{"do":"wait","valueContains":"component kind","role":"label","settleMs":600,"comment":"the heading names the position: which call, which argument, which domain"}
{"do":"wait","value":"SysMem","role":"label","comment":"a registered kind reached a row"}
{"do":"wait","value":"SysCpu","role":"label","comment":"and so did a second one — the pane shows the whole domain, not only the row the caret matches"}
{"do":"wait","value":"components","role":"label","nth":0,"comment":"the provenance column: SD1 asks every candidate to carry where it came from"}
```
