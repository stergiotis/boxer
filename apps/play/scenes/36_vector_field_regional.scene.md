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

# vector field, regional with a coast

A regional field with one step, stored in the 0…360 longitude convention and with a patch of NULL components. The pane frames the field once, the particles stop short of the missing patch and never cross it, and hovering reads the field: speed from the scalar mean, direction from the vector mean. A field of one step has no time strip

```sql
WITH
  vector_field AS (
    SELECT
      62 - 0.05 * intDiv(number, 400) AS lat,
      355 + 0.05 * (number % 400) AS lon,
      -- no data over a box in the middle: NULL is missing, and so is a non-finite value
      if(lon > 360 AND lon < 364 AND lat > 52 AND lat < 56, NULL, 8 * sin(radians(lat * 9)) + 6) AS u,
      7 * cos(radians(lon * 11)) AS v
    FROM numbers(300 * 400)
  ),
  vector_field_opts AS (SELECT 'a current round an island' AS name, 'm/s' AS unit)
SELECT count() FROM vector_field
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"wait","valueContains":"regional grid 0.05° × 0.05° · 1 steps","role":"label","timeoutMs":30000}
{"do":"wait","valueContains":"at level","role":"label","settleMs":3000,"comment":"reduced on the server: the window is a fraction of the 120000 nodes"}
{"do":"hover","x":760,"y":800,"settleMs":700,"comment":"over the field, east of the missing patch"}
{"do":"wait","valueContains":"(scalar mean) from","role":"label","comment":"the readout says which mean is which"}
{"do":"capture","text":"36_vector_field_regional"}
```
