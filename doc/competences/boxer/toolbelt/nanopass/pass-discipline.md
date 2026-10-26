---
name: Pass discipline and the environment
level: 3
parent_ids:
  - "[[nanopass/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0002
  - adr:0006
  - pkg:public/db/clickhouse/dsl/nanopass
  - pkg:public/db/clickhouse/dsl/env
  - skill:nanopass-sql
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0002, skill:nanopass-sql]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0002, skill:nanopass-sql]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0002, skill:nanopass-sql]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A pass declares its properties, reads and writes one environment, and is small enough to be read whole; the discipline is what makes a pipeline inspectable.
