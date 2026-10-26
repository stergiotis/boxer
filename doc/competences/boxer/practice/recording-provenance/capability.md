---
name: Recording provenance
abbrev: Prov
synopsis: who and what wrote each change
level: 2
parent_ids:
  - "[[practice/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0083
  - adr:0131
  - pkg:public/gov/repo
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0083, adr:0131, pkg:public/gov/repo]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0083, adr:0131, pkg:public/gov/repo]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0083, adr:0131, pkg:public/gov/repo]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Authorship and AI assistance are recorded as git trailers rather than in-file markers; a change by an agent is reviewed adversarially where the decision says so; documentation written with assistance follows the standard's rules for it.

Its competences: [[git-trailers]], [[adversarial-review]].

# Mechanisms

Co-authorship trailers; the authorship analysis over git history; the adversarial-review decision.

# Evidence

The repository's governance report over its history.

# Boundaries

Provenance says who wrote a line; it does not say whether the line is right.
