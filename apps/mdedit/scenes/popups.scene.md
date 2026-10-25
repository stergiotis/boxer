---
type: reference
audience: contributor
status: draft
scene:
  launch: mdedit
  size: 1000x700
  env:
    BOXER_MDEDIT_DOC: "# Title\n\nSome words with a [link](https://example.com/a) in it.\n"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# mdedit — the popups at the caret

ADR-0334 §SD4, M6: three popups over an open block, each anchored at the
caret's reported geometry (ADR-0130). The trace walks the caret into the
link and edits its target through the popover's field, unlinks it, selects
the label with Shift+← and bolds it from the bubble toolbar, then splits
the paragraph with Enter and turns the new block into a heading from the
slash menu. The source pane at the end shows every popup's splice.

```jsonl trace
{"do":"note","text":"ADR-0334 M6 — the caret popups"}
{"do":"wait","role":"multiline_text_input","settleMs":600}
{"do":"click","contains":"Blocks","role":"button","settleMs":500}
{"do":"wait","valueContains":"Some words","role":"label","settleMs":400}
{"do":"click","valueContains":"Some words","role":"label","pointer":true,"settleMs":600,"comment":"opens the paragraph with the caret at its end"}
{"do":"wait","role":"multiline_text_input","valueContains":"Some words","settleMs":300}
{"do":"key","text":"ArrowLeft","settleMs":100}
{"do":"key","text":"ArrowLeft","settleMs":100}
{"do":"key","text":"ArrowLeft","settleMs":100}
{"do":"key","text":"ArrowLeft","settleMs":100}
{"do":"key","text":"ArrowLeft","settleMs":100}
{"do":"key","text":"ArrowLeft","settleMs":100}
{"do":"key","text":"ArrowLeft","settleMs":100}
{"do":"key","text":"ArrowLeft","settleMs":500,"comment":"eight steps left: the caret sits just inside the link's closing parenthesis"}
{"do":"wait","role":"text_input","value":"https://example.com/a","settleMs":400,"comment":"the link popover, its field holding the target"}
{"do":"click","role":"text_input","value":"https://example.com/a","pointer":true,"settleMs":500,"comment":"a pointer press takes focus into the field; the driver's accessibility focus request does not reach a field inside the popup window (ADR-0334 M6)"}
{"do":"type","role":"text_input","value":"https://example.com/a","text":"b","settleMs":500}
{"do":"wait","role":"multiline_text_input","valueContains":"example.com/ab","settleMs":400,"comment":"the field wrote through to the block"}
{"do":"capture","text":"popups-link"}
{"do":"click","name":"Unlink","role":"button","settleMs":600}
{"do":"wait","role":"multiline_text_input","value":"Some words with a link in it.","settleMs":400,"comment":"the label stands in for the link; the caret is after it"}
{"do":"key","text":"ArrowLeft","modifiers":4,"settleMs":100}
{"do":"key","text":"ArrowLeft","modifiers":4,"settleMs":100}
{"do":"key","text":"ArrowLeft","modifiers":4,"settleMs":100}
{"do":"key","text":"ArrowLeft","modifiers":4,"settleMs":500,"comment":"Shift+← four times selects the word"}
{"do":"wait","name":"B","role":"button","settleMs":400,"comment":"the bubble toolbar over the selection"}
{"do":"capture","text":"popups-bubble"}
{"do":"click","name":"B","role":"button","settleMs":600}
{"do":"wait","role":"multiline_text_input","value":"Some words with a **link** in it.","settleMs":400}
{"do":"key","text":"Enter","settleMs":600,"comment":"Enter inside the paragraph splits it; the caret opens the new block"}
{"do":"wait","role":"multiline_text_input","value":" in it.","settleMs":400}
{"do":"type","role":"multiline_text_input","value":" in it.","text":"/","settleMs":500}
{"do":"wait","name":"Heading 2","role":"button","settleMs":400,"comment":"the slash menu"}
{"do":"capture","text":"popups-slash"}
{"do":"click","name":"Heading 2","role":"button","settleMs":600}
{"do":"wait","role":"multiline_text_input","value":"##  in it.","settleMs":400}
{"do":"click","contains":"Source","role":"button","settleMs":500}
{"do":"wait","role":"multiline_text_input","value":"# Title\n\nSome words with a **link**\n\n##  in it.\n","settleMs":400,"comment":"the bytes: the target edited then the link unlinked, the word wrapped, the split-off block made a heading"}
```
