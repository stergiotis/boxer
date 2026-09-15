---
type: reference
audience: end-user
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-15
title: Watchbill workers
summary: "Every worker run seen on the cell, what it drains, and whether it is alive"
icon: "🧑‍✈️"
endpoint: introspection
tabs: [table]
keywords: [watchbill, worker, kinds, queues, running, alive, presence]
---

# Watchbill workers

Every worker run the cell has seen in the last day, from
`keelson('watchbill_worker')` (ADR-0237 §SD4): the host it ran on, the
kinds it has handlers for and the queues it drains — an empty `queues` is
every queue — its per-kind concurrency, when it started and, for a clean
stop, when it stopped. `alive` is the heartbeat's verdict, by the same rule
the sweep uses for a dead run. A job of a kind no live row lists waits for
a process that links its handler.

The row marked `local` is this process's own worker, and it alone carries
the live fields: the job ids it holds, its last poll, whether it serves
`watchbill.job.*` on the bus and whether it reads heartbeats and so rescues
what a dead run left.

```sql
SELECT
  run_id, host, kinds, queues, max_workers, started_at, stopped_at, alive, local,
  running, last_tick, ticks, poll_ms, abandon_after_ms, keep_ms, serving, sweeping
FROM keelson('watchbill_worker')
ORDER BY alive DESC, local DESC, started_at DESC
```
