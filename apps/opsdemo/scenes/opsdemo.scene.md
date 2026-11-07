---
type: reference
audience: contributor
status: draft
scene:
  launch: opsdemo
  size: 1400x900
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# opsdemo — the seeded note and level, the Clear gesture, and the help

The operations demo (ADR-0269) as it opens on the headless host: the note
reads its seeded text, the summary line reports its 28 characters and the
level at 50, and nothing has been cleared or copied. The Clear button then
runs `clear_note` as the person's gesture — the count moves and the summary
reads zero characters — and F1 ends the scene on the window's own help.

```jsonl trace
{"do":"note","text":"--- the seeded state ---"}
{"do":"wait","name":"Clear","role":"button","settleMs":1000}
{"do":"read","value":"A note both of you can edit.","role":"multiline_text_input","pattern":"^(?P<note>.*)$"}
{"do":"expect","of":"note","is":"A note both of you can edit."}
{"do":"read","valueContains":"note: ","role":"label","pattern":"note: (?P<chars>\\d+) characters · level (?P<level>[\\d.]+)"}
{"do":"expect","of":"chars","eq":28}
{"do":"expect","of":"level","eq":50}
{"do":"wait","value":"cleared 0×","role":"label"}
{"do":"wait","value":"copied 0×","role":"label"}
{"do":"capture","text":"opsdemo-seeded"}

{"do":"note","text":"--- Clear is the clear_note command as the person's gesture ---"}
{"do":"click","name":"Clear","role":"button"}
{"do":"wait","value":"cleared 1×","role":"label"}
{"do":"read","valueContains":"note: ","role":"label","pattern":"note: (?P<cleared>\\d+) characters"}
{"do":"expect","of":"cleared","eq":0}
{"do":"capture","text":"opsdemo-cleared"}

{"do":"note","text":"--- F1 opens the window's help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"Help center","role":"window"}
{"do":"wait","value":"The operations demo","role":"label","comment":"the reader shows the demo's overview"}
{"do":"capture","text":"opsdemo-help"}
```
