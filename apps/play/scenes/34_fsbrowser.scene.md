---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 960x720
  stepSettleMs: 350
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# fsbrowser

File browser widget (ADR-0200 §SD2) — the gallery demo over an in-memory io/fs tree: a row click selects, Enter enters the selected directory, Backspace goes up, and the outline mode renders the same tree. What this scene asserts is the demo's READOUT lines, not the picture — a wrong read-back would still look right — so the two captures are a by-product and the exit status is the assertion

```jsonl trace
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"sleep","settleMs":200}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"file browser","comment":"narrow the gallery to the one demo"}
{"do":"wait","contains":"file browser","settleMs":400}
{"do":"click","contains":"file browser","comment":"expand the demo's section"}
{"do":"wait","name":"clear selection","settleMs":400,"comment":"the demo body is up"}
{"do":"scroll_into_view","name":"clear selection","settleMs":600}
{"do":"note","text":"--- baseline: the root directory, nothing selected ---"}
{"do":"wait","value":"dir: .","role":"label"}
{"do":"wait","value":"selected: (nothing)","role":"label"}
{"do":"note","text":"--- click-to-select: the row, past its label, by pointer ---"}
{"do":"click","value":"internal","role":"label","pointer":true}
{"do":"wait","value":"selected: internal","role":"label","comment":"the click reached the row's sense region behind the label"}
{"do":"capture","text":"34_fsbrowser_list","comment":"list mode with one directory row selected"}
{"do":"note","text":"--- Enter enters the selected directory; the breadcrumb and the readout follow ---"}
{"do":"key","text":"Enter"}
{"do":"wait","value":"dir: internal","role":"label","settleMs":300}
{"do":"wait","value":"selected: (nothing)","role":"label","comment":"selection is per directory"}
{"do":"note","text":"--- Backspace goes up ---"}
{"do":"click","value":"store","role":"label","pointer":true,"comment":"take focus in the new listing first: focus is what the key capture keys on"}
{"do":"wait","value":"selected: internal/store","role":"label"}
{"do":"key","text":"Backspace"}
{"do":"wait","value":"dir: .","role":"label","settleMs":300}
{"do":"note","text":"--- the outline mode renders the same tree as an outline ---"}
{"do":"click","contains":"outline","role":"button"}
{"do":"wait","value":"last action: mode outline","role":"label","settleMs":400}
{"do":"capture","text":"34_fsbrowser_outline","comment":"outline mode: unread directories carry a disclosure control"}
```
