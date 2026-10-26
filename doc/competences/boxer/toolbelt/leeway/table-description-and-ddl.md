---
name: Table description and DDL generation
level: 3
parent_ids:
  - "[[leeway/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0066
  - pkg:public/semistructured/leeway/ddl
  - pkg:public/semistructured/leeway/schema
  - explanation:leeway-column-names
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0066, explanation:leeway-column-names]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0066, explanation:leeway-column-names]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0066, explanation:leeway-column-names]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A table is described once as sections, channels and membership specs, and the DDL that creates it in ClickHouse is generated from that description, so the physical column names are an output rather than something a person spells.
