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
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_SERIES: "1"
  requires: ["clickhouse", "table:default.planes_mercator_sample100"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# series adjudication

Adjudication (ADR-0163 M3): one row per flagged extent with confirm / false-alarm, writing an append-only tslabels row — and, once a span is confirmed, the VUS readout scoring the detector against its own baseline on the adjudicated spans

```sql
WITH
  base AS (
    SELECT toDateTime64(toStartOfInterval(time, INTERVAL 5 MINUTE), 3) AS t,
           count()                                                     AS v
    FROM default.planes_mercator_sample100
    GROUP BY t
    ORDER BY t
  ),
  scores AS (SELECT tsAnomalyScores(t, v, 24) FROM base),
  spans  AS (SELECT tsAnomalySpans(t, v, 24, 3) FROM base)
SELECT * FROM base
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":4000}
{"do":"capture","text":"08_series_adjudication","settleMs":600}
{"do":"click","name":"confirm #1"}
{"do":"capture","text":"08_series_adjudication_recorded","settleMs":2500}
```
