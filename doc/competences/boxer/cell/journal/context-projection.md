---
name: Context projection as SQL
level: 3
parent_ids:
  - "[[journal/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0309
  - scene:chat-coordinator-projection
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0309, scene:chat-coordinator-projection]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0309, scene:chat-coordinator-projection]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0309, scene:chat-coordinator-projection]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What an agent sees of its history is a SQL projection over the journal, so the context window is a query a person can read and change.
