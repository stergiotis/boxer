---
name: Co-ragged function pack
level: 3
parent_ids:
  - "[[leeway/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0162
  - adr:0181
  - pkg:public/semistructured/leeway/lwsqlsurface
  - howto:leeway-clickhouse-array-idioms
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0162, howto:leeway-clickhouse-array-idioms]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0162, howto:leeway-clickhouse-array-idioms]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0162, howto:leeway-clickhouse-array-idioms]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

SQL functions over the ragged, co-indexed lanes a section carries — gather, element, select — installed once on the server and expanded into by the read surface.
