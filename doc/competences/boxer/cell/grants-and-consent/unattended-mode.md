---
name: Unattended mode within bounds
level: 3
parent_ids:
  - "[[grants-and-consent/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0298
  - env:BOXER_AGENT_UNATTENDED
  - scene:chat-away
  - scene:chat-afk-stop-withdraws
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0298, scene:chat-away]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0298, scene:chat-away]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0298, scene:chat-away]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

When the person is away an agent proceeds within bounds they set beforehand, and a grant that would widen them waits.
