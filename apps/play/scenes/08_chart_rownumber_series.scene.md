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
    BOXER_PLAY_FOCUS_CHART: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# chart rownumber series

Chart — the implicit abscissa with a series column (ADR-0172 §SD2): the numbering restarts inside each group, so unequal groups overlay rather than being laid end to end

```sql
SELECT * FROM values(
  'series String, latency_ms Float64',
  ('run A', 82), ('run B', 140),
  ('run A', 74), ('run B', 132),
  ('run A', 79), ('run B', 121),
  ('run A', 71), ('run B', 118),
  ('run A', 68),
  ('run A', 65))
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":1800}
{"do":"capture","text":"08_chart_rownumber_series"}
```
