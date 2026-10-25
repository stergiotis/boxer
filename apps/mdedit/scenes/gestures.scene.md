---
type: reference
audience: contributor
status: draft
scene:
  launch: mdedit
  size: 1000x700
  env:
    BOXER_MDEDIT_DOC: "# Title\n\n- [ ] open\n- [x] done\n\nSome docs here.\n\nLast.\n"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# mdedit — block gestures: task boxes, heading chords, move, duplicate, a pasted address

ADR-0334, update of 2026-10-25: a click on a rendered task box flips it in
the source without opening the block; Ctrl+2 makes the open block a
heading; Ctrl+↓ moves it past the block below; Ctrl+D duplicates it; and
an address typed over a selection — one write-back, as a paste is — makes
the selection a link. The source pane at the end shows every splice. The
task box's button is its Phosphor glyph (U+E45E, square), written into the
JSON as the character.

```jsonl trace
{"do":"note","text":"ADR-0334 — block gestures"}
{"do":"wait","role":"multiline_text_input","settleMs":600}
{"do":"click","contains":"Blocks","role":"button","settleMs":500}
{"do":"wait","value":"open","role":"label","settleMs":400}
{"do":"click","name":"","role":"button","settleMs":600,"comment":"the open task's box: a click flips it to done"}
{"do":"wait","value":"Some docs here.","role":"label","settleMs":300}
{"do":"click","value":"Some docs here.","role":"label","pointer":true,"settleMs":600}
{"do":"wait","role":"multiline_text_input","value":"Some docs here.","settleMs":300}
{"do":"key","text":"Digit2","modifiers":18,"settleMs":500,"comment":"Ctrl+2: the line becomes a level-two heading"}
{"do":"wait","role":"multiline_text_input","value":"## Some docs here.","settleMs":400}
{"do":"key","text":"ArrowDown","modifiers":18,"settleMs":500,"comment":"Ctrl+↓ moves the heading below Last."}
{"do":"key","text":"D","modifiers":18,"settleMs":500,"comment":"Ctrl+D duplicates it; the caret moves into the copy"}
{"do":"capture","text":"gestures-moved"}
{"do":"key","text":"ArrowLeft","modifiers":4,"settleMs":100}
{"do":"key","text":"ArrowLeft","modifiers":4,"settleMs":100}
{"do":"key","text":"ArrowLeft","modifiers":4,"settleMs":100}
{"do":"key","text":"ArrowLeft","modifiers":4,"settleMs":100}
{"do":"key","text":"ArrowLeft","modifiers":4,"settleMs":400,"comment":"Shift+← five times selects \"here.\""}
{"do":"type","role":"multiline_text_input","value":"## Some docs here.","text":"https://x.y/d","settleMs":600,"comment":"an address over the selection, in one write-back: the selection becomes the label"}
{"do":"wait","role":"multiline_text_input","value":"## Some docs [here.](https://x.y/d)","settleMs":400}
{"do":"click","contains":"Source","role":"button","settleMs":500}
{"do":"wait","role":"multiline_text_input","value":"# Title\n\n- [x] open\n- [x] done\n\nLast.\n\n## Some docs here.\n\n## Some docs [here.](https://x.y/d)\n","settleMs":400,"comment":"the box flipped in place, the heading moved below Last. and duplicated, the copy's selection made a link"}
```
