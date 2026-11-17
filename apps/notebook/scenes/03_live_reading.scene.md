---
type: reference
audience: contributor
status: draft
scene:
  launch: notebook
  size: 1400x1000
  env:
    BOXER_NOTEBOOK_SAMPLE: minimal
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Notebook — the live reading

The Read face of ADR-0380 over the `minimal` sample: read stored, run
live, and returned to what it stores. The run sends the sample's two
records to the server the `CLICKHOUSE_*` variables name, read-only; the
sample's records make their own rows, so any ClickHouse answers them.
Point the variables at a server of your own before running the scene if
the default one is not to be asked.

A run that fails and keeps the earlier reading is not driven here: it
needs a server that fails one record. `TestAFailedRunKeepsTheEarlierReading`
covers it.

```jsonl trace
{"do":"note","text":"ADR-0380 — the minimal sample, read stored"}
{"do":"wait","value":"sealed","role":"label","nth":0,"settleMs":600,"comment":"the stored results were read"}
{"do":"wait","value":"stored","role":"label","comment":"the line says which reading is on screen"}
{"do":"capture","text":"notebook-stored","settleMs":600}

{"do":"note","text":"--- run live: the records computed on the server, nothing stored ---"}
{"do":"click","name":"Run live","role":"button","settleMs":300}
{"do":"wait","valueContains":"2 records in","role":"label","comment":"every record answered"}
{"do":"wait","value":"live","role":"label","nth":0,"comment":"the phase chip and the line both say so"}
{"do":"wait","valueContains":"302.2 ms","role":"label","comment":"the figure in the sentence, as the run computed it"}
{"do":"capture","text":"notebook-live","settleMs":800}

{"do":"note","text":"--- back to what the notebook stores ---"}
{"do":"click","name":"Show stored","role":"button","settleMs":600}
{"do":"wait","value":"stored","role":"label"}
{"do":"wait","name":"Run live","role":"button"}
{"do":"capture","text":"notebook-stored-again","settleMs":600}
```
