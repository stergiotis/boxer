---
name: Interaction record and replay
level: 3
parent_ids:
  - "[[imzero2/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0127
  - pkg:public/thestack/imzero2/egui2/driver
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0127, pkg:public/thestack/imzero2/egui2/driver]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0127, pkg:public/thestack/imzero2/egui2/driver]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0127, pkg:public/thestack/imzero2/egui2/driver]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What a person did in a session is recorded as an interaction trace and can be replayed against a later build.
