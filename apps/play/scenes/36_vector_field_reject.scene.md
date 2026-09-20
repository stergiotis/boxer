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

# vector field, a column misnamed

The claim is on column names, so a misspelt one is a stated reason and not a guess (ADR-0250 §SD1): `lng` for `lon` leaves the pane saying which column it needs. No row of the field was read to find out — the pane's channel is the CTE's schema

```sql
WITH
  vector_field AS (
    SELECT 60 - 0.5 * intDiv(number, 80) AS lat, -20 + 0.5 * (number % 80) AS lng, 5 AS u, 2 AS v
    FROM numbers(60 * 80)
  )
SELECT count() FROM vector_field
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"wait","valueContains":"needs a column `lon`","role":"label","timeoutMs":30000,"comment":"the reason names the column"}
{"do":"capture","text":"36_vector_field_reject"}
```
