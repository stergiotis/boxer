---
name: A run journal written before each act
level: 3
parent_ids:
  - "[[journal/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0308
  - adr:0325
  - pkg:public/keelson/runtime/agentrun
  - scene:chat-trail
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0308, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0308, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0308, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Each act is journaled before it runs, so a process that dies mid-act leaves a record of what it was about to do, and the person resumes with grants asked again.
