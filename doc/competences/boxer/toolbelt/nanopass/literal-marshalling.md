---
name: Literal marshalling
level: 3
parent_ids:
  - "[[nanopass/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - pkg:public/db/clickhouse/dsl/marshalling
  - skill:nanopass-marshalling
  - howto:chclient-parameter-binding
assessment:
  - dimension: readiness
    rating: 6
    basis: [skill:nanopass-marshalling, howto:chclient-parameter-binding]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [skill:nanopass-marshalling, howto:chclient-parameter-binding]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [skill:nanopass-marshalling, howto:chclient-parameter-binding]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Go values become ClickHouse literal text and back — scalars, arrays, tuples — with the cast types that make the round trip lossless.
