---
name: "Window arrangement, captures and trees"
level: 3
parent_ids:
  - "[[imzero2/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0275
  - adr:0276
  - adr:0281
  - adr:0301
  - pkg:public/keelson/runtime/windowhost
  - pkg:public/keelson/runtime/capture
  - scene:chat-coordinator-windows
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0276, scene:chat-coordinator-windows]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0276, scene:chat-coordinator-windows]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0276, scene:chat-coordinator-windows]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Windows are arranged, captured and read as trees through one policy point, which is what an agent drives and what a scene asserts on.
