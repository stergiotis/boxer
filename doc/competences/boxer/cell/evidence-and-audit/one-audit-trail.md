---
name: One audit trail for model calls and agent work
level: 3
parent_ids:
  - "[[evidence-and-audit/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0277
  - adr:0296
  - pkg:public/keelson/runtime/trail
  - scene:chat-trail
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0277, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0277, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0277, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Model calls, agent actions, grants, captures and disclosures are components on one trail, read through one path.
