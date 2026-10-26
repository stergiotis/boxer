---
name: "Labels, integrity levels and sharing tiers"
level: 3
parent_ids:
  - "[[exits-and-labels/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0311
  - pkg:public/keelson/runtime/cell
  - env:BOXER_CELL_CONFINED_TIER
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0311, pkg:public/keelson/runtime/cell, env:BOXER_CELL_CONFINED_TIER]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0311, pkg:public/keelson/runtime/cell, env:BOXER_CELL_CONFINED_TIER]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0311, pkg:public/keelson/runtime/cell, env:BOXER_CELL_CONFINED_TIER]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A record carries a label, an integrity level and a sharing tier, and an exit compares them with the sink's clearance.
