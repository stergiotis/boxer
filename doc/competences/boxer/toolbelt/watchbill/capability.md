---
name: Durable work
abbrev: WB
synopsis: "jobs that outlive a process, on the facts table"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:watchbill
  - adr:0223
  - adr:0234
  - adr:0237
  - adr:0236
  - pkg:public/keelson/runtime/watchbill
  - app:watchbill
  - app:watchbilldemo
  - howto:watchbill-jobs
  - explanation:watchbill-architecture
  - explanation:watchbill-consistency-model
  - applet:watchbill-inflight
  - scene:watchbill-auto-refresh
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:watchbill, howto:watchbill-jobs, explanation:watchbill-architecture, scene:watchbill-auto-refresh]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [positioning:watchbill, howto:watchbill-jobs, explanation:watchbill-architecture]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [positioning:watchbill, howto:watchbill-jobs, explanation:watchbill-architecture]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Giving an app or a headless binary work that survives a restart: a job is a fact, a worker claims it under a lease, its trail is queryable, and a person can inspect, cancel and retry from a management app. The customer is someone who has long-running or retried work and one ClickHouse table to keep it in.

Its competences: [[durable-work-on-facts]], [[worker-protocol-and-presence]], [[job-management-app]].

# Mechanisms

Claims and leases through one row; a client protocol workers speak; worker presence as heartbeats; the management app and its applets over the same rows.

# Evidence

The positioning page, the architecture and consistency-model explanations, the jobs how-to, and the watchbill applet book.

# Boundaries

Not a workflow engine: one job at a time per worker, no DAGs, and the ordering guarantees are the ones the consistency model states.
