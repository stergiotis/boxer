---
name: Metering and moderator rules
level: 3
parent_ids:
  - "[[llm/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0300
  - pkg:public/keelson/runtime/moderator
  - env:BOXER_LLM_MODERATORS
assessment:
  - dimension: readiness
    rating: 2
    basis: [adr:0300, pkg:public/keelson/runtime/moderator, env:BOXER_LLM_MODERATORS]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0300, pkg:public/keelson/runtime/moderator, env:BOXER_LLM_MODERATORS]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0300, pkg:public/keelson/runtime/moderator, env:BOXER_LLM_MODERATORS]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Each call's usage is metered as named quantities, and a moderator's rules decide what a call may do under a ration.
