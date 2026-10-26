---
name: Agent-readable schema
level: 3
parent_ids:
  - "[[operations/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0313
  - pkg:public/keelson/runtime/agentschema
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0313, pkg:public/keelson/runtime/agentschema]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0313, pkg:public/keelson/runtime/agentschema]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0313, pkg:public/keelson/runtime/agentschema]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What an agent may query is described in a schema it reads, the same tables a person sees, with the vocabulary beside them.
