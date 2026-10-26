---
name: Instance attribution and effects
level: 3
parent_ids:
  - "[[keelson/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0188
  - adr:0191
  - adr:0322
  - adr:0272
  - pkg:public/keelson/runtime/launchlimit
  - pkg:public/keelson/runtime/runid
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0188, adr:0191, adr:0322, adr:0272, pkg:public/keelson/runtime/launchlimit, pkg:public/keelson/runtime/runid]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0188, adr:0191, adr:0322]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0188, adr:0191, adr:0322]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every effect an app instance has — a write, a launch, a call — is attributed to the instance and recorded, so a reader can ask what a run did and a limit can be enforced per instance.
