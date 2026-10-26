---
name: The reactive query graph
level: 3
parent_ids:
  - "[[play/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0097
  - adr:0187
  - app:play
  - scene:18_params_live
  - scene:29_history_two_runs
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0097, scene:18_params_live]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0097, scene:18_params_live]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0097, scene:18_params_live]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A query's parameters, sub-queries and panels form a graph; changing one re-runs what depends on it and nothing else, and a run's history is kept.
