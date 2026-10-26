---
name: The patch model and wire
level: 3
parent_ids:
  - "[[pushout/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0079
  - adr:0209
  - pkg:public/semistructured/cbor
  - explanation:pushout-distributed-operation
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0079, adr:0209, explanation:pushout-distributed-operation]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0079, adr:0209, explanation:pushout-distributed-operation]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0079, adr:0209, explanation:pushout-distributed-operation]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A patch names what is forgotten and carries its own identity on the wire, so two nodes agree on what was forgotten without comparing tables.
