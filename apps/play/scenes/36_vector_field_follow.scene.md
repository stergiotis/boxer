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

# vector field, a query that follows the pane

The pane publishes the step on display as `vf_t` and its settled view as `vf_min_lat` … `vf_max_lon` (ADR-0250 §SD6), and this buffer's own sink reads them: the strongest wind inside the view at the step on display. The names are seeded, so the first Run goes through and selects nothing; with Live ticked, stepping the time strip re-runs the sink, and the Detail pane beside the map shows the row for the step on display. The describe step is not repeated for a Live re-run, so the particles keep moving through it. The scene unticks Live as its last step, because the toggle outlives the launch

```sql
WITH
  nodes AS (
    -- a quarter-degree regional grid; seventeen steps, hourly and then three-hourly
    SELECT intDiv(number, 100 * 160) AS s,
           65 - 0.25 * intDiv(number % (100 * 160), 160) AS lat,
           -25 + 0.25 * (number % 160) AS lon
    FROM numbers(17 * 100 * 160)
  ),
  vector_field AS (
    SELECT
      toDateTime('2026-03-01 00:00:00', 'UTC') + toIntervalHour(if(s < 6, s, 6 + (s - 6) * 3)) AS t,
      lat, lon,
      -- a storm that crosses the grid from the west as the hours pass
      lon - (-30 + 1.1 * if(s < 6, s, 6 + (s - 6) * 3)) AS dx,
      lat - 52 AS dy,
      exp(-(dx * dx + dy * dy) / 60) AS storm,
      4 + 3 * sin(radians(lat * 6)) - 5.5 * dy * storm AS u,
      2 * cos(radians(lon * 5)) + 5.5 * dx * storm AS v
    FROM nodes
  ),
  vector_field_opts AS (SELECT 'a storm crossing' AS name, 'm/s' AS unit, 30 AS speed_max)
SELECT toString(t) AS step_on_display,
       round(max(sqrt(u * u + v * v)), 1) AS strongest_in_view,
       count() AS nodes_in_view
FROM vector_field
WHERE t = {vf_t:DateTime64(3, 'UTC')}
  AND lat >= {vf_min_lat:Float64} AND lat <= {vf_max_lat:Float64}
  AND lon >= {vf_min_lon:Float64} AND lon <= {vf_max_lon:Float64}
GROUP BY t
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted; the first Run is not blocked by the unwritten names, and selects nothing"}
{"do":"wait","valueContains":"at level","role":"label","timeoutMs":30000,"settleMs":1500,"comment":"the field is described and the pane has written its time and view"}
{"do":"click","name":"Live","role":"check_box","comment":"the sink reads signals, so the top bar offers Live; its inputs have moved since the first Run, so it runs"}
{"do":"wait","valueContains":"2026-03-01 00:00:00","role":"label","timeoutMs":20000,"comment":"the sink ran against the step on display"}
{"do":"click","contains":"Next","role":"button"}
{"do":"wait","valueContains":"2026-03-01 01:00:00","role":"label","timeoutMs":20000,"settleMs":1500,"comment":"one step on, and the sink followed without a Run"}
{"do":"capture","text":"36_vector_field_follow"}
{"do":"click","name":"Live","role":"check_box","comment":"off again: the toggle outlives the launch, and the next scene must not inherit it"}
```
