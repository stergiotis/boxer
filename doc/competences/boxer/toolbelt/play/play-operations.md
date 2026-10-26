---
name: Operations catalog and agent limits
level: 3
parent_ids:
  - "[[play/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0270
  - adr:0269
  - adr:0272
  - app:play
  - scene:agentconsole-play
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0270, scene:agentconsole-play]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0270, scene:agentconsole-play]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0270, scene:agentconsole-play]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What play can be asked to do by an agent is a catalog of operations under a task grant, with a limit on what the agent may launch.
