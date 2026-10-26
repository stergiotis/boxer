---
name: Governing
abbrev: Gvn
synopsis: "the gate, the lints, naming, error and CLI conventions"
level: 2
parent_ids:
  - "[[practice/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - positioning:gov
  - adr:0179
  - adr:0011
  - adr:0004
  - adr:0246
  - adr:0212
  - adr:0048
  - adr:0293
  - adr:0294
  - pkg:public/gov/gate
  - howto:adopting-boxer-downstream
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:gov, howto:adopting-boxer-downstream]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:gov, howto:adopting-boxer-downstream]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [positioning:gov, howto:adopting-boxer-downstream]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

The repository is checked by the governance tools it ships: the composite gate, code and doc lint, the licence gate, build-tag discipline, file and package naming, one error convention and one CLI framework.

Its competences: [[governance-gate]], [[build-tag-discipline]], [[naming-conventions]], [[error-and-cli-conventions]].

# Mechanisms

The gate runs in CI and downstream; each lint is a rule set; the conventions are decisions the lints enforce where they can.

# Evidence

The governance positioning page and the lint scripts under CI.

# Boundaries

A convention the lint cannot check is a review item, and the coding standards say which.
