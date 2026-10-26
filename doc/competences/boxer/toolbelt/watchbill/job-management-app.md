---
name: The management app
level: 3
parent_ids:
  - "[[watchbill/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0236
  - app:watchbill
  - applet:watchbill-failed
  - applet:watchbill-by-kind
  - scene:watchbill-auto-refresh
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0236, app:watchbill, scene:watchbill-auto-refresh]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [adr:0236, app:watchbill, scene:watchbill-auto-refresh]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0236, app:watchbill, scene:watchbill-auto-refresh]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Inspect, cancel and retry jobs, and see their trail and workers, from an app over the same rows the workers write.
