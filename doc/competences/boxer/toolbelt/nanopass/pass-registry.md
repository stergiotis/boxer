---
name: The pass registry
level: 3
parent_ids:
  - "[[nanopass/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0108
  - pkg:public/keelson/data/passreg
  - table:keelson/sql_passes
  - scene:12_passes
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0108, scene:12_passes]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0108, scene:12_passes]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0108, scene:12_passes]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every pass is registered by name with its properties, so a host can list them as a table and a reader can find which pass produced a rewrite.
