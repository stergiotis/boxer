---
type: reference
audience: contributor
status: draft
scene:
  launch: mdedit
  size: 1400x1000
  env:
    BOXER_MDEDIT_DOC: "# Data\n\n| name | n |\n| --- | --- |\n| alpha | 1 |\n\nBefore the code.\n\n```go\nx := 1\n```\n"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# mdedit — the Blocks mode's sub-editors

ADR-0334 M4: a table edits as a grid of cells and a fenced code block as a
language field over its body. Tab in the last cell adds a row; the language
field rewrites the fence line and nothing else. The source pane, opened at
the end, shows the bytes: the heading untouched, the edited row re-emitted,
the new row appended, the fence relabelled.

```jsonl trace
{"do":"note","text":"ADR-0334 M4 — table grid and code fence"}
{"do":"wait","role":"multiline_text_input","settleMs":600}
{"do":"click","contains":"Blocks","role":"button","settleMs":500}
{"do":"wait","value":"alpha","role":"label","settleMs":400,"comment":"the table renders as a block"}
{"do":"click","value":"alpha","role":"label","pointer":true,"settleMs":600,"comment":"the hit frame opens the table as a grid"}
{"do":"wait","role":"multiline_text_input","value":"alpha","settleMs":300,"comment":"the cell is a field now"}
{"do":"focus","role":"multiline_text_input","value":"alpha"}
{"do":"key","text":"End"}
{"do":"type","role":"multiline_text_input","value":"alpha","text":"s","settleMs":300}
{"do":"wait","role":"multiline_text_input","value":"alphas","settleMs":300}
{"do":"focus","role":"multiline_text_input","value":"1"}
{"do":"key","text":"Tab","settleMs":500,"comment":"Tab in the last cell adds a row and moves into it"}
{"do":"type","id":16008724364808582617,"text":"beta","settleMs":400,"comment":"the new row's first cell, by its id: an empty value is no anchor, and a node id is a function of the widget's path"}
{"do":"wait","role":"multiline_text_input","value":"beta","settleMs":300}
{"do":"capture","text":"subeditors-table","comment":"the table grid: a row edited, a row added by Tab"}
{"do":"click","value":"Before the code.","role":"label","pointer":true,"settleMs":600,"comment":"a rendered code block has no text to anchor on; open the paragraph above it and arrow down"}
{"do":"wait","role":"multiline_text_input","value":"Before the code.","settleMs":300}
{"do":"key","text":"End","modifiers":18}
{"do":"key","text":"ArrowDown","settleMs":600,"comment":"ArrowDown at the end of a block moves into the next: the code block opens with its language field"}
{"do":"wait","role":"text_input","value":"go","settleMs":300}
{"do":"focus","role":"text_input","value":"go"}
{"do":"key","text":"A","modifiers":18}
{"do":"type","role":"text_input","value":"go","text":"rust","settleMs":400}
{"do":"wait","role":"text_input","value":"rust","settleMs":300}
{"do":"capture","text":"subeditors-code","comment":"the code fence: the language field and the body"}
{"do":"click","contains":"Source","role":"button","settleMs":500}
{"do":"wait","role":"multiline_text_input","value":"# Data\n\n| name | n |\n| --- | --- |\n| alphas | 1 |\n| beta |  |\n\nBefore the code.\n\n```rust\nx := 1\n```\n","settleMs":400,"comment":"the bytes: the heading untouched, the edited row re-emitted, the added row, the fence relabelled, the body untouched"}
```
