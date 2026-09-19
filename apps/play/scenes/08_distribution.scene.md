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
    BOXER_PLAY_FOCUS_DIST: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# distribution

Distribution — a result read as a distribution rather than a table (ADR-0161): an ECDF with its simultaneous band per series, the shift function against a chosen baseline, and the letter-value boxen ladder — three readings of the same three series

```sql
WITH
  draws AS (
    SELECT ['A baseline', 'B shift +30ms', 'C scale x1.25'][1 + (number % 3)] AS arm,
           randNormal(120, 25)                                                AS draw
    FROM numbers(3000000)
  ),
  trial AS (
    SELECT arm,
           multiIf(arm = 'B shift +30ms', draw + 30,
                   arm = 'C scale x1.25', draw * 1.25,
                   draw) AS latency_ms
    FROM draws
  )
SELECT descriptiveStatistics(latency_ms)
FROM trial
GROUP BY arm
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2500}
{"do":"capture","text":"08_distribution","settleMs":600}
{"do":"click","contains":"A baseline","comment":"pin the baseline by name, not by row order"}
{"do":"click","name":"Shift"}
{"do":"capture","text":"08_distribution_shift","settleMs":600}
{"do":"click","name":"Boxen"}
{"do":"capture","text":"08_distribution_boxen","settleMs":600}
```
