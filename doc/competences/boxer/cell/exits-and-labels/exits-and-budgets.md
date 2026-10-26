---
name: "Exits, sinks, approval tiers and budgets"
level: 3
parent_ids:
  - "[[exits-and-labels/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0312
  - pkg:public/keelson/runtime/httpegress
  - env:BOXER_CELL_EGRESS_CLEARANCE
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0312, pkg:public/keelson/runtime/httpegress, env:BOXER_CELL_EGRESS_CLEARANCE]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0312, pkg:public/keelson/runtime/httpegress, env:BOXER_CELL_EGRESS_CLEARANCE]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0312, pkg:public/keelson/runtime/httpegress, env:BOXER_CELL_EGRESS_CLEARANCE]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Each way out of the cell is an exit with a sink, a tier that says who approves, and a budget it draws down.
