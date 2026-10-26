---
name: Parser bounding
level: 3
parent_ids:
  - "[[nanopass/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0084
  - adr:0196
  - adr:0304
  - adr:0305
  - adr:0306
  - pkg:public/db/clickhouse/dsl/grammar2
  - pkg:public/db/clickhouse/dsl/ast
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0084, adr:0196, adr:0304, adr:0305, adr:0306, pkg:public/db/clickhouse/dsl/grammar2, pkg:public/db/clickhouse/dsl/ast]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0084, adr:0196, adr:0304]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0084, adr:0196, adr:0304]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

The ClickHouse grammar is parsed under bounds — a DFA cache, two-stage prediction, exact qualifiers, islands for the parts that resist, memoisation — so a long statement parses in predictable time.
