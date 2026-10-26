---
name: Audit events as a trail component
level: 3
parent_ids:
  - "[[recordstore/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0296
  - adr:0277
  - pkg:public/keelson/runtime/trail
  - pkg:public/keelson/runtime/audit
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0296, adr:0277, pkg:public/keelson/runtime/trail, pkg:public/keelson/runtime/audit]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0296, adr:0277, pkg:public/keelson/runtime/trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0296, adr:0277, pkg:public/keelson/runtime/trail]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An audit event is a component on the trail's rows rather than a table of its own, so the same read path that finds a conversation finds what was done to it.
