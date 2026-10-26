---
name: "The person's decisions queue"
level: 3
parent_ids:
  - "[[grants-and-consent/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0324
  - app:agentconsole
  - scene:agentconsole-approve
  - scene:agentconsole-proposals
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0324, scene:agentconsole-approve]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0324, scene:agentconsole-approve]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0324, scene:agentconsole-approve]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What an agent needs decided waits in one queue the person works through, with each item saying what proceeds on yes.
