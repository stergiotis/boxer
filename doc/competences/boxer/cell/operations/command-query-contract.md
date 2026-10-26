---
name: The command and query contract
level: 3
parent_ids:
  - "[[operations/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0269
  - pkg:public/keelson/runtime/appops
  - table:keelson/app_operations
  - app:opsdemo
assessment:
  - dimension: readiness
    rating: 4
    basis: [adr:0269, app:opsdemo]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0269, app:opsdemo]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0269, app:opsdemo]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An app's operations are declared once, carry their parameters and effects, and run only under a task grant that names them.
