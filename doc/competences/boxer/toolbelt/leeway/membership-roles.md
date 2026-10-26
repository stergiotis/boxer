---
name: Membership roles and single-membership declaration
level: 3
parent_ids:
  - "[[leeway/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0072
  - adr:0073
  - adr:0213
  - pkg:public/semistructured/leeway/membershiprole
  - skill:leeway-streamreadaccess
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0072, adr:0073, adr:0213, pkg:public/semistructured/leeway/membershiprole, skill:leeway-streamreadaccess]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0072, adr:0073, adr:0213]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0072, adr:0073, adr:0213]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A membership plays a role — primary, secondary, identity — that a reader classifies by, and an attribute may be declared under exactly one membership where the ambiguity would otherwise be silent.
