---
name: Owning dependencies
abbrev: Dep
synopsis: "reference, vendor, port or retire — the posture per dependency"
level: 2
parent_ids:
  - "[[practice/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - explanation:why-boxer
  - adr:0149
  - adr:0204
  - adr:0194
  - adr:0195
  - adr:0202
  - adr:0215
  - adr:0268
  - adr:0095
  - adr:0199
  - howto:airgapped-build
assessment:
  - dimension: readiness
    rating: 6
    basis: [explanation:why-boxer, howto:airgapped-build]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [explanation:why-boxer, howto:airgapped-build]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [explanation:why-boxer, howto:airgapped-build]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Each dependency is taken deliberately: referenced where it is a commodity, vendored where the build must not depend on a network, ported where the data-centric parts need to be owned, retired where it costs reproducibility; the air-gapped bundle is the proof that the posture holds.

Its competences: [[dependency-posture]], [[network-free-build]].

# Mechanisms

The premises' posture; the per-dependency decisions; the bundle; the toolchain-adoption decision.

# Evidence

The why-boxer premises and the air-gapped build how-to.

# Boundaries

Owning a dependency means being able to build without it reaching out, not forking everything.
