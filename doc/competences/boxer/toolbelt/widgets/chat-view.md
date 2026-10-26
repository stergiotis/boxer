---
name: The chat view
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0239
  - pkg:public/thestack/imzero2/egui2/widgets/chatview
  - app:chat
  - scene:37_chat
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0239, app:chat, scene:37_chat]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0239, app:chat, scene:37_chat]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0239, app:chat, scene:37_chat]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A conversation as a widget: messages, actions, artefacts and questions, the same view the chat app and play's chat panel use.
