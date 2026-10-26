---
name: Bodies in a vault under per-entry keys
level: 3
parent_ids:
  - "[[journal/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0320
  - pkg:public/keelson/runtime/inscribe
  - scene:chat-inscribe
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0320, scene:chat-inscribe]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0320, scene:chat-inscribe]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0320, scene:chat-inscribe]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Large bodies — a model's answer, a captured image — live in a vault keyed per entry, so the journal's rows stay small and a body can be forgotten alone.
