---
name: Metered calls and rules
level: 3
parent_ids:
  - "[[moderation/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0300
  - env:BOXER_LLM_RATION_UNRULED
  - scene:chat-statistics
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0300, scene:chat-statistics]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0300, scene:chat-statistics]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0300, scene:chat-statistics]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every model call is metered as named quantities and a ration's rules say what an unruled call may do.
