---
name: Captures under one policy point
level: 3
parent_ids:
  - "[[profiles-and-surfaces/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0281
  - adr:0284
  - adr:0287
  - adr:0301
  - pkg:public/keelson/runtime/capture
  - scene:agentconsole-capture-png
  - scene:chat-pixels-host-cap
  - env:BOXER_CHAT_PIXELS
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0281, scene:agentconsole-capture-png]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0281, scene:agentconsole-capture-png]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0281, scene:agentconsole-capture-png]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Screen, window and pixel captures pass one policy point that decides what an agent may see and records that it saw it.
