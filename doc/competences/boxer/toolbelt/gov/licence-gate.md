---
name: The licence gate
level: 3
parent_ids:
  - "[[gov/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0004
  - adr:0246
  - pkg:public/gov/licensegate
  - pkg:public/gov/cargolicenses
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0004, adr:0246, pkg:public/gov/licensegate, pkg:public/gov/cargolicenses]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: commodity
    basis: [adr:0004, adr:0246, pkg:public/gov/licensegate]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0004, adr:0246, pkg:public/gov/licensegate]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every dependency's licence is known and allowed, for Go and for Cargo, and a new one fails the build until it is decided.
