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

# vector field, time strip

The time strip over a field that changes (ADR-0251): steps at their own instants, so the hourly steps crowd the left and the three-hourly ones spread out; a bar per step for the mean speed inside the view and a stem to the largest, in the particles' colours; a loop range dragged on the top band, with what playback leaves out dimmed; playback inside it, the readout saying which two steps the picture is between

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
SELECT count() FROM vector_field
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"wait","valueContains":"regional grid 0.25° × 0.25° · 17 steps","role":"label","timeoutMs":30000,"comment":"described: regional, seventeen steps"}
{"do":"wait","valueContains":"at level","role":"label","settleMs":2500,"comment":"a window and the per-step summary have arrived"}
{"do":"drag","x":400,"y":646,"toX":1000,"toY":646,"steps":10,"durationMs":500,"settleMs":600,"comment":"a drag that starts on the top band sets the loop range"}
{"do":"wait","valueContains":"· range ","role":"label","comment":"the readout names the range"}
{"do":"click","contains":"Play","role":"button"}
{"do":"wait","valueContains":"between steps","role":"label","timeoutMs":20000,"settleMs":1500,"comment":"playback entered a bracket, which it does only once both of its steps have windows"}
{"do":"capture","text":"36_vector_field_time_strip"}
```
