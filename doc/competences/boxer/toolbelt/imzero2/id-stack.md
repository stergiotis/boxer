---
name: The deterministic id stack
level: 3
parent_ids:
  - "[[imzero2/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0321
  - adr:0049
  - skill:imzero2
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0321, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0321, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0321, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every widget's identity is derived from its position in a scoped stack, so a multi-child widget scopes its children and a mismatch panics at render rather than aliasing silently.
