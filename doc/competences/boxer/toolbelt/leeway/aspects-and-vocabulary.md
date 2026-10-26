---
name: Aspects and the timeless vocabulary
level: 3
parent_ids:
  - "[[leeway/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0182
  - adr:0183
  - pkg:public/semistructured/leeway/aspectcodec
  - pkg:public/semistructured/leeway/namemint
  - pkg:public/identity/tagmint
  - howto:tagmint-id-spaces
  - applet:lw-aspects
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0182, adr:0183, howto:tagmint-id-spaces]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0182, adr:0183, howto:tagmint-id-spaces]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0182, adr:0183, howto:tagmint-id-spaces]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

How a column is encoded — its aspects — is declared separately from what it holds, and a vocabulary's ids are composed from a claimed tag value and an ordinal so they never move.
