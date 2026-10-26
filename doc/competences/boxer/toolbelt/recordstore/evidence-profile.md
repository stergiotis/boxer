---
name: The evidence profile
level: 3
parent_ids:
  - "[[recordstore/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0327
  - adr:0314
  - pkg:public/storage/recordstore/evidence
  - explanation:signing-leeway-records
  - env:BOXER_ANCHOR_STORE_URL
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0327, explanation:signing-leeway-records]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0327, explanation:signing-leeway-records]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0327, explanation:signing-leeway-records]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A store that opts in gets a hash chain over its rows, a vault for bodies, marks, seals and anchors into an external store, so a reader can show a record was not altered after it was written.
