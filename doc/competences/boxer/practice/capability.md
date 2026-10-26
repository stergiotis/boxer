---
name: Practice
abbrev: Pr
synopsis: how the maintainer of this repository works
level: 1
parent_ids: []
domain: boxer-practice
catalog: boxer
realised_by:
  - positioning:gov
  - explanation:why-boxer
  - adr:0001
  - adr:0179
  - pkg:public/gov
  - scene:comp-browser
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:gov, adr:0001]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:gov, adr:0001]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [positioning:gov, adr:0001]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What the repository does for the person who maintains it: it records why it is shaped as it is, measures what it claims, verifies in two lanes, governs itself with the same tools it ships, owns its dependencies deliberately, records who wrote what, and describes itself in pages a reader can check. The customer is the maintainer, which is why this is a root of its own.

Its subsystems: [[deciding/capability]], [[measuring/capability]], [[verifying/capability]], [[governing/capability]], [[owning-dependencies/capability]], [[recording-provenance/capability]], [[describing-itself/capability]].

# Mechanisms

The documentation standard and the ADR tiers; the trials protocol; the test lanes; the governance gate; the dependency posture; provenance trailers; the self-describing pages and corpora.

# Evidence

The engineering-practices page, the documentation standard and the coding standards; the governance positioning page.

# Boundaries

A practice is how this repository works; a downstream repository adopts what it wants of it through the gate.
