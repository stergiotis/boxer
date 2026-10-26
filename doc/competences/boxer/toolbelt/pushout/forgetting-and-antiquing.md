---
name: Forgetting and antiquing
level: 3
parent_ids:
  - "[[pushout/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0025
  - pkg:public/storage/recordstore/pushoutstore
  - explanation:erasure-design-space
  - explanation:pushout-sweep-and-purge-durability
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0025, explanation:erasure-design-space]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0025, explanation:erasure-design-space]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0025, explanation:erasure-design-space]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Forgetting removes; antiquing coarsens — a record kept at lower fidelity — and both are patches the sweep applies durably.
