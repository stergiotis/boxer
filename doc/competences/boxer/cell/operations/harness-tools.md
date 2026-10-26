---
name: Harness-exposed inference and coordinator tools
level: 3
parent_ids:
  - "[[operations/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0315
  - adr:0318
  - pkg:public/keelson/runtime/harness
  - scene:chat-coordinator-windows
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0315, scene:chat-coordinator-windows]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0315, scene:chat-coordinator-windows]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0315, scene:chat-coordinator-windows]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Inference and the coordinator's own tools — windows, captures, questions — are exposed to an agent through the harness, under the same contract as an app's operations.
