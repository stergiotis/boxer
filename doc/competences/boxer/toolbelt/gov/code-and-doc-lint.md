---
name: Code and doc lint
level: 3
parent_ids:
  - "[[gov/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0011
  - adr:0048
  - pkg:public/gov/codelint
  - pkg:public/gov/doclint
  - pkg:public/gov/filenaming
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0011, adr:0048, pkg:public/gov/codelint, pkg:public/gov/doclint, pkg:public/gov/filenaming]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0011, adr:0048, pkg:public/gov/codelint]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0011, adr:0048, pkg:public/gov/codelint]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Rules over the Go sources and the markdown tree — naming, front matter, links, banners, path hygiene — each with a number and a message that says what to change.
