---
name: Passthrough classification
level: 3
parent_ids:
  - "[[nanopass/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0117
  - adr:0107
  - pkg:public/db/clickhouse/dsl/nanopass
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0117, adr:0107, pkg:public/db/clickhouse/dsl/nanopass]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0117, adr:0107, pkg:public/db/clickhouse/dsl/nanopass]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0117, adr:0107, pkg:public/db/clickhouse/dsl/nanopass]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A statement is classified by what it touches and whether a pass may let it through untouched, which is what keeps a write from slipping through a read-only surface.
