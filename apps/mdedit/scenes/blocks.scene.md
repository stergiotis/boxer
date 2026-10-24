---
type: reference
audience: contributor
status: draft
scene:
  launch: mdedit
  size: 1400x1000
  env:
    BOXER_MDEDIT_DOC: "# Title\n\nFirst para.\n\n- one\n- two\n"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# mdedit — the Blocks mode

ADR-0334 M3: the document edited as blocks over its source. A click on a
rendered block opens it as text; Enter at its end starts a new block; typing
into the new block pads it with the blank lines that make it one; Ctrl+Z is
the document's own undo; and the source pane, opened at the end, shows the
bytes every gesture left — the untouched ones unchanged.

```jsonl trace
{"do":"note","text":"ADR-0334 M3 — blocks over the source"}
{"do":"wait","role":"multiline_text_input","settleMs":600,"comment":"the window is up in Split; the source pane holds the seeded document"}
{"do":"click","contains":"Blocks","role":"button","settleMs":500}
{"do":"wait","value":"First para.","role":"label","settleMs":400,"comment":"the paragraph renders as a block"}
{"do":"click","value":"First para.","role":"label","pointer":true,"settleMs":500,"comment":"the hit frame under the label opens the block for editing"}
{"do":"wait","role":"multiline_text_input","value":"First para.","settleMs":300}
{"do":"key","text":"End","modifiers":18}
{"do":"type","role":"multiline_text_input","text":" More","settleMs":300}
{"do":"wait","role":"multiline_text_input","value":"First para. More","settleMs":300}
{"do":"key","text":"Enter","settleMs":500,"comment":"Enter at the end of a paragraph: the caret moves into the gap below, an empty block"}
{"do":"wait","role":"multiline_text_input","value":"","settleMs":300}
{"do":"type","role":"multiline_text_input","text":"New block","settleMs":400}
{"do":"wait","role":"multiline_text_input","value":"New block","settleMs":300}
{"do":"key","text":"Z","modifiers":18,"settleMs":500,"comment":"Ctrl+Z: the document's undo takes the typed block back"}
{"do":"wait","role":"multiline_text_input","value":"","settleMs":300}
{"do":"key","text":"Y","modifiers":18,"settleMs":500,"comment":"Ctrl+Y: and redo brings it back"}
{"do":"wait","role":"multiline_text_input","value":"New block","settleMs":300}
{"do":"capture","text":"blocks-editing","comment":"Blocks mode: the heading and the list rendered, the new block open as text"}
{"do":"click","contains":"Source","role":"button","settleMs":500}
{"do":"wait","role":"multiline_text_input","value":"# Title\n\nFirst para. More\n\nNew block\n\n- one\n- two\n","settleMs":400,"comment":"the source pane shows the bytes: the heading and the list untouched, the paragraph edited in place, the new block padded by blank lines"}
```
