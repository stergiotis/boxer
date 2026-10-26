---
name: Call identity and write observers
level: 3
parent_ids:
  - "[[recordstore/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0295
  - pkg:public/identity/callident
  - pkg:public/storage/recordstore
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0295, pkg:public/identity/callident, pkg:public/storage/recordstore]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0295, pkg:public/identity/callident, pkg:public/storage/recordstore]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0295, pkg:public/identity/callident, pkg:public/storage/recordstore]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every write carries the identity of the call that made it, and an observer can watch writes as they happen — the hook the audit trail and the evidence profile attach to.
