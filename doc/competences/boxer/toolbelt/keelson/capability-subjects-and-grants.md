---
name: Capability subjects and grants
level: 3
parent_ids:
  - "[[keelson/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0026
  - adr:0253
  - adr:0262
  - pkg:public/keelson/runtime/capbroker
  - pkg:public/keelson/security/capslock
  - app:capinspector
  - scene:capinspector
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0026, adr:0253, app:capinspector, scene:capinspector]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0026, adr:0253, app:capinspector]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0026, adr:0253, app:capinspector]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What an app may read, write or do is a capability subject, granted explicitly and recorded; a query against a keelson table or a facts-bound store runs under the grant the manifest declares for it, and the audit trail records the grant.
