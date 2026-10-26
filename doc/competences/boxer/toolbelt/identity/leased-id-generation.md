---
name: Leased id generation
level: 3
parent_ids:
  - "[[identity/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0111
  - adr:0319
  - pkg:public/identity/identgen
  - pkg:public/identity/identsql
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0111, adr:0319, pkg:public/identity/identgen, pkg:public/identity/identsql]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0111, adr:0319, pkg:public/identity/identgen]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0111, adr:0319, pkg:public/identity/identgen]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A generator takes a lease on a range through one row the store serialises on, so ids are unique across processes without a central service and without a technology-specific sequence.
