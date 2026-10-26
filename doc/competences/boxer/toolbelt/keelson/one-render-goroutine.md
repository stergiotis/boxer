---
name: One render goroutine
level: 3
parent_ids:
  - "[[keelson/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0261
  - adr:0049
  - skill:imzero2
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0261, adr:0049, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0261, adr:0049, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0261, adr:0049, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

All UI work happens on one goroutine the runtime owns; apps hand it closures rather than drawing from wherever they are, which is what keeps the id stack deterministic and the frame budget observable.
