---
name: Attention levels
level: 3
parent_ids:
  - "[[moderation/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0323
  - pkg:public/keelson/runtime/attention
  - scene:chat-urgent
  - scene:chat-away-idle
  - env:BOXER_ATTENTION_ESCALATE
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0323, scene:chat-urgent]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0323, scene:chat-urgent]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0323, scene:chat-urgent]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What reaches the person is paced by attention levels — away, idle, flood — so an urgent item interrupts and a routine one waits.
