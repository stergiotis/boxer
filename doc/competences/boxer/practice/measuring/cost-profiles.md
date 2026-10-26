---
name: Cost profiles
level: 3
parent_ids:
  - "[[measuring/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0192
  - scene:12_passes_cost
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0192, scene:12_passes_cost]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0192, scene:12_passes_cost]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0192, scene:12_passes_cost]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Where a computation is a pipeline, each stage is timed and the profile is shown beside the result.
