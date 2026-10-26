---
name: The SQL editor and completion
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0147
  - adr:0190
  - pkg:public/thestack/imzero2/egui2/widgets/sqleditor
  - pkg:public/db/clickhouse/dsl/sqlcomplete
  - explanation:sql-completion-survey
  - explanation:sql-editor-highlighting-survey
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0190, explanation:sql-completion-survey]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0190, explanation:sql-completion-survey]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0190, explanation:sql-completion-survey]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A ClickHouse SQL editor with highlighting from the parser and completion from the schema and the vocabulary.
