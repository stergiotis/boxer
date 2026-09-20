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

# vector field, the window query opened

**Window query…** opens the statement the pane last sent in a playground of its own, run: the relation with the pane's reduction around it, and every value it was sent with as a `SET`, so the PARAMETERS pane lists the `ff_*` names and the result is the window's bins. The statement text is the same for every view of a relation; only those values differ (ADR-0250 §SD3)

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
{"do":"wait","valueContains":"at level","role":"label","timeoutMs":30000,"settleMs":1500}
{"do":"click","contains":"Window query","role":"button"}
{"do":"sleep","settleMs":6000,"comment":"a second window opens over the bus and runs its statement; the tree gains it only once it has drawn"}
{"do":"wait","name":"Hide prelude","role":"check_box","timeoutMs":20000,"comment":"only a buffer with a SET prelude gets this toggle: the second window has parsed the statement and pinned its parameters"}
{"do":"capture","text":"36_vector_field_window_query"}
```
