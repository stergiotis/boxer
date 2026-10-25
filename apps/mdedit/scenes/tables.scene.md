---
type: reference
audience: contributor
status: draft
scene:
  launch: mdedit
  size: 1400x1000
  env:
    BOXER_MDEDIT_DOC: "# Data\n\n| name | n |\n| --- | --- |\n| beta | 2 |\n| alpha | 10 |\n\nAfter.\n"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# mdedit — the table grid's handles and keys

ADR-0334 §SD3: an open table is a grid with a handle before every row and
above every column, and the handles' menus hold what the productivity tools
offer on a table — insert, move, delete, align, sort — over the one GFM
source; Ctrl+↑/↓ moves the row the caret is in. The source pane at the end
shows the bytes: the rows re-emitted in their new order, the delimiter row
carrying the alignment, the heading untouched.

The handles are one Phosphor glyph, U+E208 (dots-three-vertical), written
into the JSON as the character, so a trace anchors them by `nth` in the
order they are emitted: the table's handle first, then the column handles
left to right, then the row handles down from the header. Handles and
items are pressed
with `pointer`: a menu opens on a press and closes on a click, and neither
is the accessibility click.

```jsonl trace
{"do":"note","text":"ADR-0334 — table grid: handles and keys"}
{"do":"wait","role":"multiline_text_input","settleMs":600}
{"do":"click","contains":"Blocks","role":"button","settleMs":500}
{"do":"wait","value":"beta","role":"label","settleMs":400}
{"do":"click","value":"beta","role":"label","pointer":true,"settleMs":600,"comment":"the hit frame opens the table as a grid"}
{"do":"wait","role":"multiline_text_input","value":"beta","settleMs":300}
{"do":"click","name":"","role":"button","pointer":true,"nth":4,"settleMs":400,"comment":"the beta row's handle: the table's and the two columns' come first"}
{"do":"click","name":"Insert row below","role":"button","pointer":true,"settleMs":500}
{"do":"type","id":16008724364808582617,"text":"gamma","settleMs":400,"comment":"the new row's first cell, by its id: it sits at the row ordinal alpha had"}
{"do":"wait","role":"multiline_text_input","value":"gamma","settleMs":300}
{"do":"focus","role":"multiline_text_input","value":"gamma"}
{"do":"key","text":"ArrowDown","modifiers":18,"settleMs":500,"comment":"Ctrl+↓ moves the gamma row below alpha"}
{"do":"wait","role":"multiline_text_input","value":"gamma","settleMs":300}
{"do":"click","name":"","role":"button","pointer":true,"nth":2,"settleMs":400,"comment":"the n column's handle"}
{"do":"click","name":"Sort descending","role":"button","pointer":true,"settleMs":500}
{"do":"capture","text":"tables-sorted","comment":"the body sorted by n, the empty cell last"}
{"do":"click","name":"","role":"button","pointer":true,"nth":5,"settleMs":400,"comment":"the beta row's handle, now the second body row"}
{"do":"click","name":"Move row up","role":"button","pointer":true,"settleMs":500}
{"do":"click","name":"","role":"button","pointer":true,"nth":0,"settleMs":400,"comment":"the table's handle"}
{"do":"click","name":"Add column","role":"button","pointer":true,"settleMs":500}
{"do":"click","name":"","role":"button","pointer":true,"nth":2,"settleMs":400,"comment":"the n column's handle again; the added column's handle sits after it"}
{"do":"click","name":"Align right","role":"button","pointer":true,"settleMs":500}
{"do":"capture","text":"tables-reshaped"}
{"do":"click","contains":"Source","role":"button","settleMs":500}
{"do":"wait","role":"multiline_text_input","value":"# Data\n\n| name | n |  |\n| --- | --: | --- |\n| beta | 2 |  |\n| alpha | 10 |  |\n| gamma |  |  |\n\nAfter.\n","settleMs":400,"comment":"the bytes: every row re-emitted since the column was added, the delimiter row aligned, the heading and the paragraph untouched"}
```
