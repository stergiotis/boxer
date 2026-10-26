---
name: Retained conversations and the chat app
level: 3
parent_ids:
  - "[[llm/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0264
  - adr:0265
  - pkg:public/keelson/runtime/trail
  - app:chat
  - scene:chat-trail
  - scene:chat-message-actions
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0264, app:chat, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [adr:0264, app:chat, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0264, app:chat, scene:chat-trail]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A conversation is rows on the trail, kept across processes, and the chat app is one view over them.
