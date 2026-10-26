---
name: Generated stores on facts-shaped tables
level: 3
parent_ids:
  - "[[recordstore/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0100
  - adr:0105
  - adr:0089
  - pkg:public/storage/recordstore/gen
  - pkg:public/storage/recordstore/example
  - explanation:facts-bound-record-stores
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0100, explanation:facts-bound-record-stores]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0100, explanation:facts-bound-record-stores]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0100, explanation:facts-bound-record-stores]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A store is generated from its DTOs: typed inserts, a scan with the presence filter baked in, and membership ids from the vocabulary the DTOs name.
