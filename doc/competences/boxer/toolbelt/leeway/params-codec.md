---
name: Parameters codec
level: 3
parent_ids:
  - "[[leeway/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0233
  - adr:0109
  - pkg:public/semistructured/leeway/encodingaspects
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0233, adr:0109, pkg:public/semistructured/leeway/encodingaspects]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0233, adr:0109, pkg:public/semistructured/leeway/encodingaspects]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0233, adr:0109, pkg:public/semistructured/leeway/encodingaspects]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A membership's high-cardinality parameter — a heading, a phase, a dimension — rides a lane of its own under a codec, which is what lets one registered name label many authored values.
