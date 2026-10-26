---
name: Embedding the runtime
level: 3
parent_ids:
  - "[[keelson/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0155
  - adr:0211
  - pkg:public/keelson/runtime/hostboot
  - howto:adopting-boxer-downstream
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0155, howto:adopting-boxer-downstream]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0155, howto:adopting-boxer-downstream]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0155, howto:adopting-boxer-downstream]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A downstream program hosts the runtime inside its own binary through one seam, bringing its own apps and leaving the host's bootstrap, bus and tables as they are.
