---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "1888x1100"
    BOXER_PLAY_AUTORUN: ""
    BOXER_PLAY_FOCUS_SERIES: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# series fixture

The fixture lab (ADR-0163 M4) from an EMPTY workbench: kind and seed, generating a labelled synthetic series published as ORDINARY ad-hoc datasets — fixture_series and fixture_truth, queried with keelson() like anything else, with no demo mode anywhere

```sql
-- Nothing has run yet. The fixture lab below generates a labelled
-- series with known ground truth, and publishes it as ordinary tables.
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2000}
{"do":"capture","text":"08_series_fixture","settleMs":600}
{"do":"click","name":"generate"}
{"do":"capture","text":"08_series_fixture_generated","settleMs":3000}
{"do":"click","name":"Run"}
{"do":"wait","name":"Run","comment":"Run is Cancel while the query runs, so this resolves when it lands"}
{"do":"sleep","settleMs":800}
{"do":"capture","text":"08_series_fixture_queried"}
```
