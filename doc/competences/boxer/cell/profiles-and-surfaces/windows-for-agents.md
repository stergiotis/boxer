---
name: Windows agents read and arrange
level: 3
parent_ids:
  - "[[profiles-and-surfaces/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0276
  - scene:chat-coordinator-windows
  - scene:chat-coordinator-window-tree
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0276, scene:chat-coordinator-window-tree]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0276, scene:chat-coordinator-window-tree]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0276, scene:chat-coordinator-window-tree]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An agent reads the window tree and arranges windows through the coordinator, under the same operations a person's keyboard uses.
