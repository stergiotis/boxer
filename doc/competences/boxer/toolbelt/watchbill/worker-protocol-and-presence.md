---
name: The client protocol and worker presence
level: 3
parent_ids:
  - "[[watchbill/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0234
  - adr:0237
  - pkg:public/keelson/runtime/watchbill
  - applet:watchbill-workers
  - applet:watchbill-abandoned
  - explanation:watchbill-architecture
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0237, explanation:watchbill-architecture]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [adr:0237, explanation:watchbill-architecture]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0237, explanation:watchbill-architecture]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A worker speaks one protocol — claim, heartbeat, finish — and its presence is a fact, so an abandoned job is one whose worker stopped heartbeating.
