---
name: Resource monitors and replay
level: 3
parent_ids:
  - "[[observability/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0020
  - adr:0061
  - adr:0197
  - pkg:public/keelson/runtime/sysmreplay
  - pkg:public/keelson/runtime/sysmscrape
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0020, adr:0061, adr:0197, pkg:public/keelson/runtime/sysmreplay, pkg:public/keelson/runtime/sysmscrape]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0020, adr:0061, adr:0197]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0020, adr:0061, adr:0197]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Monitors watch a resource, their samples are kept, and a replay plays a recorded window back as if live.
