---
name: The built-in moderator loop and escalation
level: 3
parent_ids:
  - "[[moderation/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0302
  - pkg:public/keelson/runtime/moderator
  - env:BOXER_MODERATOR
  - scene:chat-watch
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0302, scene:chat-watch]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0302, scene:chat-watch]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0302, scene:chat-watch]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A moderator watches each agent's acts against rules and escalates to the person when a rule says so.
