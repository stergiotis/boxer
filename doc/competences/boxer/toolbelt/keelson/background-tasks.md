---
name: Background tasks
level: 3
parent_ids:
  - "[[keelson/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0038
  - pkg:public/keelson/runtime/bgjob
  - pkg:public/keelson/runtime/task
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0038, pkg:public/keelson/runtime/bgjob, pkg:public/keelson/runtime/task]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0038, pkg:public/keelson/runtime/bgjob, pkg:public/keelson/runtime/task]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0038, pkg:public/keelson/runtime/bgjob, pkg:public/keelson/runtime/task]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Work an app starts and does not wait for: a task runs under the runtime's supervision, reports progress, and can be watched from a widget or a table.
