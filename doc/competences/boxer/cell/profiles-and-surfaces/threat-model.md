---
name: The threat model
level: 3
parent_ids:
  - "[[profiles-and-surfaces/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0307
  - adr:0312
assessment:
  - dimension: readiness
    rating: 2
    basis: [adr:0307, adr:0312]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0307, adr:0312]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0307, adr:0312]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What the cell defends against and what it does not: one person, their machine, agents that may be wrong or compromised, and peers that are not trusted.
