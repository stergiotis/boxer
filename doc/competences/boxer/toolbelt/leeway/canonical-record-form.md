---
name: Canonical record form and wire
level: 3
parent_ids:
  - "[[leeway/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0201
  - adr:0210
  - adr:0325
  - pkg:public/semistructured/leeway/canonform
  - pkg:public/semistructured/leeway/canonwire
  - explanation:signing-leeway-records
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0201, explanation:signing-leeway-records]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0201, explanation:signing-leeway-records]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0201, explanation:signing-leeway-records]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A record has one canonical form and one wire encoding, so two encoders agree byte for byte and an identity can be derived from the bytes.
