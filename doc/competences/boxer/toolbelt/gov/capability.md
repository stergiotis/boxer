---
name: Governance
abbrev: Gov
synopsis: "gates, lints, corpora and classification for a repository"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:gov
  - adr:0004
  - adr:0011
  - adr:0179
  - adr:0092
  - adr:0168
  - adr:0170
  - pkg:public/gov
  - pkg:public/gov/gate
  - howto:adopting-boxer-downstream
  - howto:adr-overview
  - applet:adr-timeline
  - applet:cat-overview
  - scene:comp-browser
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:gov, howto:adopting-boxer-downstream, howto:adr-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:gov, howto:adopting-boxer-downstream, howto:adr-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [positioning:gov, howto:adopting-boxer-downstream, howto:adr-overview]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Keeping a repository honest by tools that run over it: a licence gate, code and doc lint, a composite gate downstream repositories adopt, and the decision, competence and data corpora served as tables. The customer is a maintainer of a repository that wants its own rules checked by the same binary that builds it.

Its competences: [[licence-gate]], [[code-and-doc-lint]], [[downstream-gate]], [[decision-corpus]], [[competence-corpus]], [[data-catalog]], [[dispatch-classification]], [[package-properties]].

# Mechanisms

Each lint is a rule set over a walk; the gate composes the steps; a corpus is a parser plus providers; classification reads the call sites and the dispatch shapes.

# Evidence

The positioning page, the adopting-downstream how-to, the ADR-overview how-to, and the governance applet books.

# Boundaries

Not CI: the gate runs under whatever CI a repository has, and says nothing about pipelines.
