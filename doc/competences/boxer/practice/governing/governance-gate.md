---
name: The gate
level: 3
parent_ids:
  - "[[governing/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0179
  - pkg:public/gov/gate
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
    rating: foundational
    basis: [adr:0179, howto:adopting-boxer-downstream]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

One composite step — build tags, doclint, entry points, naming, code lint, the tab digest — run in CI here and adoptable downstream.
