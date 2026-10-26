---
name: Framework-driven ingestion
level: 3
parent_ids:
  - "[[stevedore/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0252
  - pkg:public/streaming/stevedore
  - howto:stevedore-processors
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0252, howto:stevedore-processors]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0252, howto:stevedore-processors]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0252, howto:stevedore-processors]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Routing, failure handling and state for a processor's items are the framework's job; the processor turns items into rows and nothing else.
