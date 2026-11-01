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

Impact tolerance — four services against their tolerances under three scenarios (ADR-0122, 2026-11-01 update): one row per service and scenario, unfolded from an array of estimates per service. The tolerance is declared in hours and the recovery in minutes through `gloss/duration`, so the comparison is one the pane had to convert for. Table-free, so it needs a server but no dataset

```sql
WITH ['Ransomware', 'Cloud region outage', 'Key supplier failure'] AS scenarios
SELECT service, tolerance_h AS `tolerance@gloss/duration;unit=h`, scenarios[k] AS scenario,
       minutes[k] AS `recovery@gloss/duration;unit=min`
FROM values('service String, tolerance_h Float64, minutes Array(Float64)',
  ('Order intake',       24, [4320,  600,  240]),
  ('Warehouse dispatch', 12, [2880,  360, 1800]),
  ('Payroll',            72, [3600,  480,  120]),
  ('Customer support',   48, [1800, 1200,   60]))
ARRAY JOIN arrayEnumerate(minutes) AS k
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
