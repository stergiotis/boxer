---
name: External-process chokepoint
level: 3
parent_ids:
  - "[[keelson/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0118
  - adr:0285
  - pkg:public/extbin
  - pkg:public/keelson/runtime/procpool
  - table:keelson/extbin
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0118, adr:0285, pkg:public/extbin, pkg:public/keelson/runtime/procpool, table:keelson/extbin]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0118, adr:0285, pkg:public/extbin]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0118, adr:0285, pkg:public/extbin]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Everything the runtime spawns outside its own process goes through one point that knows the binary, its arguments and its provenance, so an audit can list every external execution and a policy can refuse one.
