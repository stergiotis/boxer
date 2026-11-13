---
type: reference
audience: contributor
status: draft
scene:
  launch: riskviews
  size: 1400x1000
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# riskviews — the nine views

One capture per view, plus a hover capture on the views whose marks carry a
tooltip. The stock host does not link this app; run the scene with the
`apps/riskviews/cmd/riskviews` binary's `imzero2 scene`. Every mark with a
tooltip is a hit region with an accessibility node named by its title (role
`unknown`), so the trace hovers marks by name. The matrix's risk dots are
also Tab stops; one capture shows a dot focused from the keyboard side, with
the pointer parked off the canvas.

```jsonl trace
{"do":"wait","name":"1 Matrix","role":"button"}
{"do":"capture","text":"v1-matrix-banded","settleMs":800}
{"do":"hover","name":"R1 · Ransomware encrypts core ERP","role":"unknown"}
{"do":"capture","text":"v1-matrix-banded-hover","settleMs":600}
{"do":"hover","x":5,"y":995}
{"do":"focus","name":"R4 · Payment fraud via fake invoice","role":"unknown"}
{"do":"capture","text":"v1-matrix-banded-focus","settleMs":600}
{"do":"click","name":"Continuous with ranges","role":"button"}
{"do":"click","name":"Show inherent → residual"}
{"do":"capture","text":"v1-matrix-continuous-inherent","settleMs":600}
{"do":"click","name":"2 Loss exceedance","role":"button"}
{"do":"capture","text":"v2-lec","settleMs":600}
{"do":"hover","name":"Loss exceedance","role":"unknown"}
{"do":"capture","text":"v2-lec-hover","settleMs":600}
{"do":"click","name":"3 Dotplot","role":"button"}
{"do":"capture","text":"v3-dots","settleMs":600}
{"do":"click","name":"4 Indicators","role":"button"}
{"do":"capture","text":"v4-kri","settleMs":600}
{"do":"hover","name":"Phishing test click rate","role":"unknown","nth":0}
{"do":"capture","text":"v4-kri-bullet-hover","settleMs":600}
{"do":"click","name":"5 Bow-tie","role":"button"}
{"do":"capture","text":"v5-bowtie","settleMs":600}
{"do":"hover","name":"Top event","role":"unknown"}
{"do":"capture","text":"v5-bowtie-hover","settleMs":600}
{"do":"click","name":"6 Tornado","role":"button"}
{"do":"capture","text":"v6-tornado","settleMs":600}
{"do":"click","name":"7 Calibration","role":"button"}
{"do":"capture","text":"v7-calibration","settleMs":600}
{"do":"hover","name":"2024 register · estimated 15%","role":"unknown"}
{"do":"capture","text":"v7-calibration-hover","settleMs":600}
{"do":"click","name":"8 Incidents","role":"button"}
{"do":"capture","text":"v8-incidents","settleMs":600}
{"do":"click","name":"9 Impact tolerance","role":"button"}
{"do":"capture","text":"v9-tolerance","settleMs":600}
{"do":"hover","name":"Warehouse dispatch · Ransomware","role":"unknown"}
{"do":"capture","text":"v9-tolerance-hover","settleMs":600}
```
