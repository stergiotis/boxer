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
    BOXER_PLAY_FOCUS_VECTORFIELD: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# vector field

Vector field — the `vector_field` CTE drawn as particles drifting on a map (ADR-0250). The field is computed from `numbers()`, so the scene needs a server and no table. The pane reads only the CTE's schema; every window on screen is a reduction query keyed by parameters, which the status line counts

```sql
WITH
  nodes AS (
    -- a one-degree global grid, nine three-hourly steps, no table behind it
    SELECT
      intDiv(number, 181 * 360) AS s,
      90 - toFloat64(intDiv(number % (181 * 360), 360)) AS lat,
      -180 + toFloat64(number % 360) AS lon
    FROM numbers(9 * 181 * 360)
  ),
  vector_field AS (
    SELECT
      toDateTime('2026-03-01 00:00:00', 'UTC') + toIntervalHour(3 * s) AS t,
      lat,
      lon,
      -- a meandering westerly jet in each hemisphere, the trades between them,
      -- and one vortex that drifts east with the step
      lon - (-60 + 6 * s) AS dx,
      lat - 38 AS dy,
      exp(-(dx * dx + dy * dy) / 160) AS eddy,
      22 * exp(-pow((abs(lat) - 45 - 7 * sin(radians(3 * lon + 25 * s))) / 11, 2))
        - 7 * exp(-pow(lat / 14, 2))
        - 2.2 * dy * eddy AS u,
      5 * cos(radians(3 * lon + 25 * s)) * exp(-pow((abs(lat) - 45) / 16, 2))
        + 2.2 * dx * eddy AS v
    FROM nodes
  ),
  vector_field_opts AS (
    SELECT 'synthetic jets and an eddy' AS name, 'm/s' AS unit, 28 AS speed_max
  )
SELECT t, round(max(sqrt(u * u + v * v)), 1) AS strongest, round(avg(sqrt(u * u + v * v)), 1) AS mean_speed
FROM vector_field
GROUP BY t
ORDER BY t
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"wait","valueContains":"global grid 1° × 1° · 9 steps","role":"label","timeoutMs":20000,"comment":"the relation was described: periodic, one degree, nine steps"}
{"do":"wait","valueContains":"at level","role":"label","settleMs":2500,"comment":"a window arrived and the particles have run for a while"}
{"do":"wait","valueContains":"synthetic jets and an eddy","role":"label","comment":"the name comes from vector_field_opts"}
{"do":"wait","valueContains":"step 1 of 9","role":"label","comment":"the time strip reads the first step"}
{"do":"click","contains":"Next","role":"button","comment":"one step on: the layer asks for that step's window"}
{"do":"wait","valueContains":"2026-03-01 03:00 UTC · step 2 of 9","role":"label","comment":"the display time is the second step's valid time"}
{"do":"wait","valueContains":"2 requests","role":"label","timeoutMs":20000,"settleMs":2000,"comment":"two requests, and the step moved to was in the first of them: a request carries the bracket and the steps the strip says playback reaches next (ADR-0251 SD9), so stepping on asks only for the step that came into reach"}
{"do":"capture","text":"36_vector_field"}
```
