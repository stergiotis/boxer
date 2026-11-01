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
    BOXER_PLAY_FOCUS_RISKMATRIX: "1"
  requires: ["clickhouse"]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# risk matrix

Risk matrix — a register of eight risks as likelihood and impact ranges (ADR-0122, 2026-11-01 update): the banded 5×5 first, then the continuous reading with the inherent → residual arrows. Table-free, so it needs a server but no dataset

```sql
SELECT *
FROM values('id String, name String, p_lo Float64, p_hi Float64, impact_lo Float64, impact_hi Float64,
             inherent_p_lo Float64, inherent_p_hi Float64, inherent_impact_lo Float64, inherent_impact_hi Float64',
  ('R1', 'Ransomware encrypts core ERP',   0.03,  0.15, 800e3,  12e6, 0.10,  0.35, 1.5e6, 25e6),
  ('R2', 'Cloud region outage over 24 h',  0.02,  0.08, 200e3,   3e6, 0.02,  0.08, 600e3,  6e6),
  ('R3', 'Key supplier insolvency',        0.05,  0.20, 300e3,   4e6, 0.05,  0.20,   1e6, 10e6),
  ('R4', 'Payment fraud via fake invoice', 0.20,  0.60,  20e3, 400e3, 0.40,  0.90,  50e3, 900e3),
  ('R5', 'Customer data breach',           0.02,  0.10, 500e3,  20e6, 0.05,  0.20,   1e6, 30e6),
  ('R6', 'Late incident reporting fine',   0.01,  0.05, 100e3,   5e6, 0.04,  0.15, 100e3,  5e6),
  ('R7', 'Loss of platform lead',          0.10,  0.30,  50e3, 600e3, 0.15,  0.40,  80e3, 900e3),
  ('R8', 'Warehouse fire',                 0.002, 0.01,   2e6,  40e6, 0.004, 0.02,   3e6, 60e6))
```

```jsonl trace
{"do":"wait","name":"Run","comment":"the app has mounted"}
{"do":"wait","name":"Continuous with ranges","role":"button","comment":"the pane has claimed the result"}
{"do":"sleep","settleMs":1500}
{"do":"capture","text":"38_risk_matrix"}
{"do":"hover","name":"R8 · Warehouse fire","role":"unknown","comment":"the dot, by the accessible name its hit region carries"}
{"do":"read","valueContains":"Impact €2m","role":"label","pattern":"Likelihood (?P<lo>[\\d.]+)%","comment":"the tooltip is up and describes R8"}
{"do":"expect","of":"lo","eq":0.2}
{"do":"capture","text":"38_risk_matrix_hover"}
{"do":"click","name":"Continuous with ranges","role":"button"}
{"do":"click","name":"Show inherent → residual"}
{"do":"sleep","settleMs":800}
{"do":"capture","text":"38_risk_matrix_continuous"}
```
