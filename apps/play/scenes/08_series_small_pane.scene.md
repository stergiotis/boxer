---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "900x900"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_SERIES: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# series small pane

Series in a SMALL window with a score plot: the pane's height is a BUDGET the two x-linked plots split, so both keep the UTC tick labels implot draws along their bottom edge instead of the lower one being pushed under the fold

```sql
WITH
  base AS (
    SELECT toDateTime64('2026-07-05 00:00:00', 3) + toIntervalMinute(5 * number) AS t,
           420000 + 60000 * sin(number / 22.0) + (rand() % 9000)                 AS bytes_per_min
    FROM numbers(576)
  ),
  scores AS (SELECT tsAnomalyScores(t, bytes_per_min, 24) FROM base)
SELECT * FROM base
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":3000}
{"do":"capture","text":"08_series_small_pane"}
```
