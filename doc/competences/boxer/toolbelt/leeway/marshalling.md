---
name: Marshalling
level: 3
parent_ids:
  - "[[leeway/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0074
  - adr:0101
  - adr:0103
  - adr:0109
  - adr:0113
  - pkg:public/semistructured/leeway/marshall
  - howto:leeway-marshalling
  - skill:leeway-advanced
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0074, howto:leeway-marshalling]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0074, howto:leeway-marshalling]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0074, howto:leeway-marshalling]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A Go struct tagged with sections and memberships becomes rows through a generated codec or a reflecting one, and the two agree; stacked entities, attribute ordering and every channel are covered.
