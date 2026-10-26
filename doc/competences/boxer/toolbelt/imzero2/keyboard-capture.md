---
name: Keyboard capture
level: 3
parent_ids:
  - "[[imzero2/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0177
  - adr:0279
  - skill:imzero2
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0177, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0177, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0177, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Keys are captured under a policy that says which window may see which key, so a shortcut cannot be stolen by a widget that happens to have focus.
