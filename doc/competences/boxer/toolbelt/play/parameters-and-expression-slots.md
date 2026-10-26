---
name: Parameters and expression slots
level: 3
parent_ids:
  - "[[play/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0124
  - adr:0187
  - app:play
  - scene:18_params_live
  - scene:19_params_expr
  - scene:19_params_pinned
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0124, scene:19_params_expr]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0124, scene:19_params_expr]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0124, scene:19_params_expr]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A query declares parameters that become controls, and an expression slot lets a reader supply a fragment the query embeds, with overflow and pinning handled.
