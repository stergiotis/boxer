---
name: Rewrite combinators
level: 3
parent_ids:
  - "[[nanopass/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0098
  - pkg:public/db/clickhouse/dsl/nanopass
  - skill:nanopass-sql
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0098, skill:nanopass-sql]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0098, skill:nanopass-sql]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0098, skill:nanopass-sql]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Sequence, fixed point and conditional application over passes, so a pipeline is written as a value and read as one.
