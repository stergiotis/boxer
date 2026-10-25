---
type: reference
audience: contributor
status: draft
scene:
  launch: mdedit
  size: 900x700
  env:
    BOXER_MDEDIT_DOC: "# Title\n\nThe quick brown fox jumps over the lazy dog, and then it does so again, and again, until the sentence is long enough to wrap onto several rows of the open block, which is what this scene needs: a paragraph whose last row is not its first, so that the arrow keys have rows to move across before they reach an edge and leave.\n"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# mdedit — the arrows leave a block by row, not by text edge

ADR-0334 §SD4: ↑ leaves an open block only from its first row and ↓ only
from its last; between them the arrows move by row as in any field. The row
is known from the editor's cursor geometry report (ADR-0130,
`reportCursorRects`). The paragraph here wraps onto several rows at the
window's width, and the trace tells the two apart by what gets typed where:
an `X` after one ↑ from the block's end lands inside the paragraph, and a
`Y` after ↑ has climbed to the first row and once more lands in the heading.

```jsonl trace
{"do":"note","text":"ADR-0334 — ↑/↓ by the caret's row"}
{"do":"wait","role":"multiline_text_input","settleMs":600}
{"do":"click","contains":"Blocks","role":"button","settleMs":500}
{"do":"wait","valueContains":"quick brown fox","role":"label","settleMs":400}
{"do":"click","valueContains":"quick brown fox","role":"label","pointer":true,"settleMs":600,"comment":"opens the paragraph with the caret at its end, on the last row"}
{"do":"wait","role":"multiline_text_input","valueContains":"quick brown fox","settleMs":300}
{"do":"key","text":"ArrowUp","settleMs":400,"comment":"not the first row: the field moves the caret up one row"}
{"do":"type","role":"multiline_text_input","valueContains":"quick brown fox","text":"X","settleMs":400}
{"do":"wait","role":"multiline_text_input","valueContains":"X","settleMs":300,"comment":"still the paragraph, with the X inside it"}
{"do":"key","text":"ArrowUp","settleMs":400}
{"do":"key","text":"ArrowUp","settleMs":400}
{"do":"key","text":"ArrowUp","settleMs":500,"comment":"from the first row: the heading above opens, caret at its end"}
{"do":"wait","role":"multiline_text_input","value":"# Title","settleMs":300}
{"do":"type","role":"multiline_text_input","value":"# Title","text":"Y","settleMs":400}
{"do":"capture","text":"caret-rows"}
{"do":"click","contains":"Source","role":"button","settleMs":500}
{"do":"wait","role":"multiline_text_input","valueContains":"# TitleY\n\nThe quick brown fox","settleMs":400,"comment":"the Y in the heading, the X somewhere inside the paragraph and not at its end"}
```
