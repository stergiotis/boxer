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

# Completion pane — a silence carries its reason, never an empty table

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
SELECT LW_GET('
```

```jsonl trace
{"do":"note","text":"ADR-0190 SD1 — a silence carries its reason, never an empty table"}
{"do":"sleep","settleMs":2500,"comment":"as above"}
{"do":"wait","value":"LW_GET argument 1 — leeway section","role":"label","settleMs":600,"comment":"the position still resolves to a domain, named in the heading"}
{"do":"wait","value":"nothing here answers leeway section","role":"label","comment":"and the pane says why it cannot fill it, rather than showing an empty table"}
```
