---
type: reference
audience: contributor
status: draft
scene:
  launch: "subject_alias = 'report'"
  size: 1300x1000
  requires: [clickhouse]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Notebooklet — the report sample, read live

The `report` sample minted as a notebooklet (ADR-0380 §SD6). The window
runs the notebook's records on the server when it opens, read-only, and
shows the report over them; nothing is stored. The sample's records make
their own rows, so any ClickHouse answers them. Point the `CLICKHOUSE_*`
variables at a server of your own before running the scene if the default
one is not to be asked.

```jsonl trace
{"do":"note","text":"ADR-0380 §SD6 — a notebooklet opens and runs live"}
{"do":"wait","valueContains":"5 records in","role":"label","settleMs":600,"comment":"the run on open answered"}
{"do":"wait","valueContains":"519.5 ms","role":"label","comment":"the figure in the sentence, as the run computed it"}
{"do":"wait","name":"Open in notebook","role":"button"}
{"do":"capture","text":"notebooklet-report-live","settleMs":800}

{"do":"click","name":"Show stored","role":"button","settleMs":600}
{"do":"wait","value":"stored","role":"label"}
{"do":"click","name":"Run live","role":"button","settleMs":300}
{"do":"wait","valueContains":"5 records in","role":"label","comment":"run again by the gesture"}
```
