---
name: Harness profile and registry composition
level: 3
parent_ids:
  - "[[profiles-and-surfaces/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0310
  - adr:0335
  - pkg:public/keelson/runtime/harness
  - env:BOXER_CHAT_PROFILE
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0310, adr:0335, pkg:public/keelson/runtime/harness, env:BOXER_CHAT_PROFILE]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0310, adr:0335, pkg:public/keelson/runtime/harness]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0310, adr:0335, pkg:public/keelson/runtime/harness]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A profile says which apps and tools an agent's registry holds, composed from what the host registered and what the person allowed.
