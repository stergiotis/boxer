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
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# series change points

Change points on the Series pane (ADR-0163 Update 2026-11-11): the PELT segments as bands whose boundaries are the changes, the causal CUSUM chart below with the threshold its method supplies, and the Change points menu that writes such CTEs. The series is generated, with a shift up at sample 200 and down at 420, so the scene needs no table.

```sql
WITH
  base AS (
    SELECT toDateTime64('2026-01-01 00:00:00', 3, 'UTC') + toIntervalMinute(number) AS t,
           multiIf(number < 200, 10, number < 420, 14, 11)
             + (sipHash64(number) % 2000) / 1000 - 1 AS v
    FROM numbers(600)
    ORDER BY t
  ),
  spans  AS (SELECT tsPeltMean(t, v, 5, 1) FROM base),
  scores AS (SELECT tsCusumScores(t, v, 60) FROM base)
SELECT * FROM base
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"read","valueContains":"segment(s), so","role":"label","pattern":"(?P<segments>\\d+) segment\\(s\\), so (?P<changes>\\d+) change\\(s\\), by tsPeltMean","comment":"the pane reads the bands as segments and counts the boundaries"}
{"do":"expect","of":"changes","eq":2,"comment":"PELT finds the two planted shifts and no others"}
{"do":"wait","valueContains":"tsCusumScores, warmup 60","role":"label","comment":"the score lane names its detector and the argument it was given"}
{"do":"wait","valueContains":"mean and scale are KNOWN","role":"label","comment":"the threshold is drawn with the condition it holds under"}
{"do":"capture","text":"08_series_changepoints"}
{"do":"click","name":"Change points"}
{"do":"wait","name":"shifts in level","role":"button","comment":"the menu lists the questions; each writes a scaffold"}
{"do":"capture","text":"08_series_changepoints_menu"}
```
