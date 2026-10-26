---
name: The shared audit recorder
level: 3
parent_ids:
  - "[[evidence-and-audit/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0330
  - pkg:public/keelson/runtime/audit
assessment:
  - dimension: readiness
    rating: 2
    basis: [adr:0330, pkg:public/keelson/runtime/audit]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0330, pkg:public/keelson/runtime/audit]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0330, pkg:public/keelson/runtime/audit]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

One recorder every writer goes through, so no path to the trail is unrecorded.
