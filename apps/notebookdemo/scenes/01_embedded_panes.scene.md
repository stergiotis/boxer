---
type: reference
audience: contributor
status: draft
scene:
  launch: notebookdemo
  size: 1200x1000
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Embedded notebook demo — a pane in a host's frame

The first embedder of ADR-0380's pane (M4). The host's own row picks the
notebook and shows what the host reads of the pane; under it, the pane.
The report sample is embedded by its slug and shows what it stores until
it is run; the host's own notebook stores nothing and runs when the window
opens. Both notebooks make their own rows, so any ClickHouse answers them.
Point the `CLICKHOUSE_*` variables at a server of your own before running
the scene if the default one is not to be asked.

```jsonl trace
{"do":"note","text":"ADR-0380 M4 — the registered notebook, by its slug, read stored"}
{"do":"wait","valueContains":"#report","role":"label","settleMs":600,"comment":"the runs are attributed to the host's id and the pane's key"}
{"do":"wait","value":"stored","role":"label","nth":0}
{"do":"click","name":"Run live","role":"button","settleMs":300}
{"do":"wait","valueContains":"5 records in","role":"label","nth":0,"comment":"the host's own row, reading the pane's state"}
{"do":"wait","valueContains":"5 records in","role":"label","nth":1,"comment":"and the pane's line"}
{"do":"capture","text":"notebookdemo-report-live","settleMs":800}

{"do":"note","text":"--- the host's own notebook, run when the window opened ---"}
{"do":"click","contains":"of the host's own","role":"radio_button","settleMs":600}
{"do":"wait","valueContains":"#squares","role":"label"}
{"do":"wait","valueContains":"2 records in","role":"label","nth":0}
{"do":"wait","valueContains":"285","role":"label","comment":"the figure in the sentence, as the run computed it"}
{"do":"capture","text":"notebookdemo-own-live","settleMs":800}
```
