---
name: The composition surface
level: 3
parent_ids:
  - "[[lading/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0222
  - pkg:public/fs/lading
  - explanation:data-product-composition
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0222, explanation:data-product-composition]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0222, explanation:data-product-composition]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0222, explanation:data-product-composition]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A snapshot is mounted beside live data as one SQL surface, so a query joins files to facts.
