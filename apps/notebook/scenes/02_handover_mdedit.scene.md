---
type: reference
audience: contributor
status: draft
scene:
  launch: notebook
  size: 1500x1000
  env:
    BOXER_NOTEBOOK_SAMPLE: minimal
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Notebook — the text handed to mdedit and back

The notebook window has no editor for prose and presentation cells
(ADR-0371 §SD10). **Edit in mdedit** opens mdedit on the notebook's text;
mdedit, holding a text that is a notebook, offers **Open in notebook**,
which opens a notebook window on the text as it stands there.

The sample is not of a snapshot, so the text travels alone and the window
it comes back to finds no stored result beside it: the status row says
not sealed, and the origin says the text was handed over.

```jsonl trace
{"do":"note","text":"ADR-0371 §SD10 — the text to mdedit and back"}
{"do":"wait","value":"sealed","role":"label","nth":0,"settleMs":600}
{"do":"click","name":"Edit in mdedit","role":"button","settleMs":800}
{"do":"wait","valueContains":"opened in mdedit","role":"label","comment":"said once the host answered the open"}
{"do":"wait","name":"Open in notebook","role":"button","settleMs":800,"comment":"mdedit holds a text with a record, so it offers the way back"}
{"do":"capture","text":"handover-in-mdedit","settleMs":600}
{"do":"click","name":"Open in notebook","role":"button","settleMs":1000}
{"do":"wait","value":"handed over as minimal.md","role":"label","settleMs":600,"comment":"a second notebook window, on the text mdedit held"}
{"do":"wait","value":"not sealed","role":"label","nth":0,"comment":"the text came alone: nothing it stores is beside it"}
{"do":"capture","text":"handover-back-in-notebook","settleMs":600}
```
