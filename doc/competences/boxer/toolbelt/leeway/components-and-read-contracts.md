---
name: Components and read contracts
level: 3
parent_ids:
  - "[[leeway/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0075
  - adr:0146
  - adr:0183
  - adr:0189
  - adr:0326
  - pkg:public/semistructured/leeway/mappingplan
  - pkg:public/semistructured/leeway/marshall/component
  - skill:leeway-components
  - explanation:facts-bound-record-stores
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0146, adr:0183, skill:leeway-components, explanation:facts-bound-record-stores]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0146, adr:0183, skill:leeway-components]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0146, adr:0183, skill:leeway-components]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A component is a structurally typed view over the slots a row carries: satisfied by shape, formulated after the data exists, read with a presence verdict and an arity rule rather than a declared type.
