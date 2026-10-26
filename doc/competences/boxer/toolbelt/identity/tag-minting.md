---
name: Tag minting
level: 3
parent_ids:
  - "[[identity/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0183
  - pkg:public/identity/tagmint
  - pkg:public/semistructured/leeway/namemint
  - howto:tagmint-id-spaces
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0183, howto:tagmint-id-spaces]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0183, howto:tagmint-id-spaces]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0183, howto:tagmint-id-spaces]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A vocabulary claims its tag value from a width class at init, the claim is refused if taken, and the committed assignment tables are compared across the repository so two vocabularies cannot share an id.
