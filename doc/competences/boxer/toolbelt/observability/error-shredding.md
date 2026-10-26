---
name: Error shredding
level: 3
parent_ids:
  - "[[observability/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0041
  - adr:0293
  - pkg:public/observability/eh
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0041, adr:0293, pkg:public/observability/eh]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0041, adr:0293, pkg:public/observability/eh]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0041, adr:0293, pkg:public/observability/eh]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An error is built with fields, wrapped with context, and shredded into those fields when logged or stored, so an error is queryable rather than a string.
