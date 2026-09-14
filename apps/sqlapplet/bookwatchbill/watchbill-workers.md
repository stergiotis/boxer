---
type: reference
audience: end-user
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-14
title: Watchbill workers
summary: "What this process's worker drains, holds and polls"
icon: "🧑‍✈️"
endpoint: introspection
tabs: [table]
keywords: [watchbill, worker, kinds, queues, running, poll]
---

# Watchbill workers

The worker standing in this process, from `keelson('watchbill_worker')`
(ADR-0234 §SD5): the kinds it has handlers for and the queues it drains —
an empty `queues` is every queue — its per-kind concurrency, the job ids it
holds, when it last polled and how often it does. `serving` says it answers
`watchbill.job.*` on the bus, `sweeping` that it reads heartbeats and so
rescues what a dead run left. A job of a kind not listed here waits for a
process that links its handler.

One row, for this process only: what other processes serve is a presence
fact the ADR defers.

```sql
SELECT
  run_id, kinds, queues, max_workers, running,
  last_tick, ticks, poll_ms, abandon_after_ms, keep_ms, serving, sweeping
FROM keelson('watchbill_worker')
```
