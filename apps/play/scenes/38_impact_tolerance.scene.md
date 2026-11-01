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
    BOXER_PLAY_FOCUS_TOLERANCE: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# impact tolerance

Impact tolerance — four services against their tolerances under three scenarios (ADR-0122, 2026-11-01 update): one row per service and scenario, unfolded from an array of estimates per service. Table-free, so it needs a server but no dataset

```sql
WITH ['Ransomware', 'Cloud region outage', 'Key supplier failure'] AS scenarios
SELECT service, tolerance_hours, scenarios[k] AS scenario, estimates[k] AS recovery_hours
FROM values('service String, tolerance_hours Float64, estimates Array(Float64)',
  ('Order intake',       24, [72, 10,  4]),
  ('Warehouse dispatch', 12, [48,  6, 30]),
  ('Payroll',            72, [60,  8,  2]),
  ('Customer support',   48, [30, 20,  1]))
ARRAY JOIN arrayEnumerate(estimates) AS k
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"sleep","settleMs":2500}
{"do":"capture","text":"38_impact_tolerance"}
{"do":"hover","name":"Warehouse dispatch · Key supplier failure","role":"unknown","comment":"the estimate, by the accessible name its hit region carries"}
{"do":"read","valueContains":"Recovery about","role":"label","pattern":"Tolerance (?P<tol>\\d+) h · breached","comment":"the tooltip is up, and says the estimate breaches"}
{"do":"expect","of":"tol","eq":12}
{"do":"capture","text":"38_impact_tolerance_hover"}
```
