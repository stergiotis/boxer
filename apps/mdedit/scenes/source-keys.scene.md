---
type: reference
audience: contributor
status: draft
scene:
  launch: mdedit
  size: 1400x1000
  env:
    BOXER_MDEDIT_DOC: "# Title\n\n- one"
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# mdedit — the source pane's keyboard

ADR-0334 M2: the formatted source pane answers Enter, Tab and the formatting
chords. The window opens on a seeded document (`BOXER_MDEDIT_DOC`), so the
person's own draft is never touched, and every step asserts the editor's
value afterwards — the bytes that would be saved.

Each captured key is applied one frame after the press, so a step waits for
the value rather than reading it at once.

```jsonl trace
{"do":"note","text":"ADR-0334 M2 — Enter continues a list, exits on an empty item; Tab indents; Ctrl+B wraps"}
{"do":"wait","role":"multiline_text_input","settleMs":600}
{"do":"focus","role":"multiline_text_input"}
{"do":"key","text":"End","modifiers":18,"comment":"Ctrl+End: the caret to the end of the document"}
{"do":"key","text":"Enter","settleMs":400}
{"do":"wait","role":"multiline_text_input","value":"# Title\n\n- one\n- ","settleMs":200,"comment":"Enter on a list item continues the marker"}
{"do":"type","role":"multiline_text_input","text":"two","settleMs":300}
{"do":"key","text":"Enter","settleMs":400}
{"do":"key","text":"Enter","settleMs":400}
{"do":"wait","role":"multiline_text_input","value":"# Title\n\n- one\n- two\n","settleMs":200,"comment":"Enter on an empty item removes the marker: the list is left"}
{"do":"type","role":"multiline_text_input","text":"para","settleMs":300}
{"do":"key","text":"Home","modifiers":4,"comment":"Shift+Home selects the word back to the line start"}
{"do":"key","text":"B","modifiers":18,"settleMs":400,"comment":"Ctrl+B wraps the selection"}
{"do":"wait","role":"multiline_text_input","value":"# Title\n\n- one\n- two\n**para**","settleMs":200}
{"do":"key","text":"End","modifiers":18}
{"do":"key","text":"Enter","settleMs":400}
{"do":"type","role":"multiline_text_input","text":"- x","settleMs":300}
{"do":"key","text":"Tab","settleMs":400,"comment":"Tab on a list line indents it by one tab"}
{"do":"wait","role":"multiline_text_input","value":"# Title\n\n- one\n- two\n**para**\n\t- x","settleMs":200}
{"do":"key","text":"Tab","modifiers":4,"settleMs":400,"comment":"Shift+Tab takes it back"}
{"do":"wait","role":"multiline_text_input","value":"# Title\n\n- one\n- two\n**para**\n- x","settleMs":200}
{"do":"capture","text":"source-keys","comment":"the formatted source pane: heading scaled, prose proportional, the list continued and wrapped by the keyboard"}
```
