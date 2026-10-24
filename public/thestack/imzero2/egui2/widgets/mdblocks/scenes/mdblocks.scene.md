---
type: reference
audience: contributor
status: draft
scene:
  launch: widgets
  size: 1400x1000
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# mdblocks — the gallery demo

ADR-0334 M3: the block editor in the widgets gallery. Opens the demo, edits
the last paragraph, and reads the demo's own readout — the change count and
the source — back from the accessibility tree.

```jsonl trace
{"do":"note","text":"ADR-0334 — widgets/mdblocks in the gallery"}
{"do":"wait","role":"text_input","comment":"the gallery's filter box"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"markdown blocks","comment":"narrow the gallery to the demo"}
{"do":"wait","contains":"markdown blocks","settleMs":400}
{"do":"click","contains":"markdown blocks","comment":"expand the demo's section"}
{"do":"wait","name":"Reset","settleMs":600,"comment":"the demo body is up"}
{"do":"scroll_into_view","value":"The last paragraph.","role":"label","settleMs":600}
{"do":"click","value":"The last paragraph.","role":"label","pointer":true,"settleMs":600,"comment":"opens the paragraph as text"}
{"do":"wait","role":"multiline_text_input","value":"The last paragraph.","settleMs":400}
{"do":"key","text":"End","modifiers":18}
{"do":"type","role":"multiline_text_input","text":" Edited.","settleMs":400}
{"do":"wait","role":"multiline_text_input","value":"The last paragraph. Edited.","settleMs":400}
{"do":"read","valueContains":"changes:","role":"label","pattern":"changes: (?P<n>\\d+), caret: (?P<c>\\d+)","comment":"the demo's readout"}
{"do":"expect","of":"n","min":1,"comment":"the edit was reported"}
{"do":"expect","of":"c","min":300,"comment":"the caret is inside the last block, near the end of the source"}
{"do":"capture","text":"mdblocks-gallery","comment":"the demo with the last paragraph open for editing"}
```
