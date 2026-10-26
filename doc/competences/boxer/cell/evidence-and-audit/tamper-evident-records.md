---
name: Tamper-evident records
level: 3
parent_ids:
  - "[[evidence-and-audit/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0314
  - adr:0327
  - pkg:public/storage/recordstore/evidence
  - explanation:signing-leeway-records
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0314, explanation:signing-leeway-records]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0314, explanation:signing-leeway-records]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0314, explanation:signing-leeway-records]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

The trail's rows are chained and sealed through the evidence profile, so an altered record shows.
