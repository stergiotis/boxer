---
name: The downstream gate and skeleton
level: 3
parent_ids:
  - "[[gov/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0179
  - pkg:public/gov/gate
  - pkg:public/gov/skeleton
  - howto:adopting-boxer-downstream
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0179, howto:adopting-boxer-downstream]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0179, howto:adopting-boxer-downstream]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0179, howto:adopting-boxer-downstream]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

One composite gate — build tags, doclint, entry points, naming, code lint, the tab digest — published for consuming repositories, with a skeleton that starts a new one under it.
