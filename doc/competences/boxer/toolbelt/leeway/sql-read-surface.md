---
name: The SQL read surface
level: 3
parent_ids:
  - "[[leeway/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0116
  - adr:0171
  - adr:0181
  - adr:0226
  - adr:0289
  - pkg:public/semistructured/leeway/lwsql
  - pkg:public/semistructured/leeway/lwsqlsurface
  - explanation:leeway-sql-read-surface
  - howto:leeway-sql-reading-and-authoring
  - trial:jsonbench-on-facts
assessment:
  - dimension: readiness
    rating: 8
    basis: [adr:0181, explanation:leeway-sql-read-surface, trial:jsonbench-on-facts]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0181, explanation:leeway-sql-read-surface, trial:jsonbench-on-facts]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0181, explanation:leeway-sql-read-surface, trial:jsonbench-on-facts]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A query written in handles — section and column, membership and channel — is expanded client-side into the read-back expressions and the physical names, so a regeneration that re-aspects a column cannot leave a stale literal behind.
