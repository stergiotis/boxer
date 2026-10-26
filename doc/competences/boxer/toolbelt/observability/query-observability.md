---
name: Query observability
level: 3
parent_ids:
  - "[[observability/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0115
  - pkg:public/keelson/runtime/queryrunfacts
  - pkg:public/keelson/runtime/queryrunsvc
  - explanation:query-observability
  - applet:ch-pipeline
  - applet:ch-readflow
  - scene:13_diagnostics
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0115, explanation:query-observability, scene:13_diagnostics]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0115, explanation:query-observability, scene:13_diagnostics]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0115, explanation:query-observability, scene:13_diagnostics]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What queries ran, what they cost and what they read, captured from the engine's own logs into facts and shown beside the query that caused them.
