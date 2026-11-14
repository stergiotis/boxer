---
type: reference
audience: contributor
status: draft
scene:
  launch: notebook
  size: 1400x1100
  env:
    BOXER_NOTEBOOK_SAMPLE: report
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Notebook — one document, two faces

The notebook window of ADR-0371 §SD10 over the `report` sample. The Read
face draws the notebook from what it stores; the Records face is a play
window whose buffer is the notebook's records. Taking the buffer back
without a change leaves the text as it was, and the status row says so.
Collection is opened as far as its confirmation, where the person states
the tier, and cancelled: a scene has no store to publish to.

The assertions are on the status row: the badge, and the line that counts
the records, the problems, the stale records and the refused cells.

```jsonl trace
{"do":"note","text":"ADR-0371 §SD10 — the report sample, read"}
{"do":"wait","value":"sealed","role":"label","nth":0,"settleMs":600,"comment":"the stored results were read and the notebook shows what it stores"}
{"do":"read","valueContains":"check:","role":"label","pattern":"check: (?P<records>\\d+) records · (?P<problems>\\d+) problems · (?P<stale>\\d+) stale · (?P<refused>\\d+) cells refused"}
{"do":"expect","of":"records","eq":5}
{"do":"expect","of":"problems","eq":0}
{"do":"expect","of":"stale","eq":0}
{"do":"expect","of":"refused","eq":0}
{"do":"wait","valueContains":"519.5 ms","role":"label","comment":"the figure in the sentence, as stored"}
{"do":"capture","text":"notebook-read","settleMs":800}

{"do":"note","text":"--- the records as a play buffer ---"}
{"do":"click","name":"Records","role":"radio_button","settleMs":800}
{"do":"wait","name":"Take the buffer back","role":"button","settleMs":600}
{"do":"wait","role":"multiline_text_input","valueContains":"p99_by_hour","comment":"the buffer holds the notebook's records as WITH queries"}
{"do":"capture","text":"notebook-records","settleMs":800}

{"do":"note","text":"--- the buffer taken back unchanged ---"}
{"do":"click","name":"Take the buffer back","role":"button","settleMs":500}
{"do":"wait","valueContains":"nothing changed","role":"label"}
{"do":"click","name":"Read","role":"radio_button","settleMs":600}
{"do":"wait","value":"sealed","role":"label","nth":0}

{"do":"note","text":"--- collection asks for the tier before anything runs ---"}
{"do":"click","name":"Collect…","role":"button","settleMs":500}
{"do":"wait","valueContains":"State how confidential what the records read is","role":"label"}
{"do":"wait","name":"AMBER","role":"radio_button"}
{"do":"capture","text":"notebook-collect-confirm","settleMs":600}
{"do":"click","name":"Cancel","role":"button","settleMs":400}

{"do":"note","text":"--- F1 opens the window's own help ---"}
{"do":"key","text":"F1","settleMs":1200}
{"do":"wait","contains":"The notebook window","settleMs":400}
{"do":"capture","text":"notebook-help","settleMs":600}
```
