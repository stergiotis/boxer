---
name: Persisted streams
level: 3
parent_ids:
  - "[[stevedore/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0005
  - pkg:public/streaming/persisted
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0005, pkg:public/streaming/persisted]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0005, pkg:public/streaming/persisted]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0005, pkg:public/streaming/persisted]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A stream of events is kept as it arrives, so an ingestion can be replayed from what was received rather than from the source.
