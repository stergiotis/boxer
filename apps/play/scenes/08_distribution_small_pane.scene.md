---
type: reference
audience: contributor
status: draft
scene:
  launch: play
  size: 1920x1200
  stepSettleMs: 350
  env:
    BOXER_PLAY_WINDOW_SIZE: "900x640"
    BOXER_PLAY_AUTORUN: "1"
    BOXER_PLAY_FOCUS_DIST: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# distribution small pane

Distribution in a SMALL window: the plot box follows the pane instead of a fixed height, so the ECDF and the boxen ladder each keep the x tick labels implot draws along their BOTTOM edge

```sql
WITH
  draws AS (
    SELECT ['A control', 'B treatment'][1 + (number % 2)] AS arm,
           randNormal(420000, 18000)                      AS draw
    FROM numbers(1000000)
  ),
  trial AS (
    SELECT arm,
           multiIf(arm = 'B treatment', draw * 1.08, draw) AS response_bytes
    FROM draws
  )
SELECT descriptiveStatistics(response_bytes)
FROM trial
GROUP BY arm
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2500}
{"do":"capture","text":"08_distribution_small_pane","settleMs":600}
{"do":"click","name":"Boxen"}
{"do":"capture","text":"08_distribution_small_pane_boxen","settleMs":600}
```
