---
name: Durable work on the facts table
level: 3
parent_ids:
  - "[[watchbill/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0223
  - pkg:public/keelson/runtime/watchbill
  - table:boxer.facts
  - howto:watchbill-jobs
  - explanation:watchbill-consistency-model
  - applet:watchbill-timeline
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0223, howto:watchbill-jobs, explanation:watchbill-consistency-model]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [adr:0223, howto:watchbill-jobs, explanation:watchbill-consistency-model]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0223, howto:watchbill-jobs, explanation:watchbill-consistency-model]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A job is rows on the facts table — submitted, claimed, finished, failed — so it needs no queue of its own and its history is a query.
