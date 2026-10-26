---
name: Guided ClickHouse-to-ClickHouse sync
level: 3
parent_ids:
  - "[[sync-and-markdown/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0259
  - adr:0271
  - pkg:public/db/clickhouse/jackstay
  - app:jackstay
  - scene:jackstay
  - env:BOXER_JACKSTAY_PLAN
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0259, app:jackstay, scene:jackstay]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0259, app:jackstay, scene:jackstay]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0259, app:jackstay, scene:jackstay]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A plan names tables, filters and packs; the app runs it against a target and shows what moved.
