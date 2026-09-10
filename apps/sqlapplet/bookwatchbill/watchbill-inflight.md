---
type: reference
audience: end-user
status: draft
title: Watchbill in flight
summary: "Every job that is queued, running or waiting on a cancel"
icon: "⏳"
endpoint: introspection
tabs: [table]
keywords: [watchbill, job, queue, running, worker, durable]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Watchbill in flight

The queue as it stands: every job row not yet in a final state, read live
from `keelson('watchbill')` (ADR-0223 §SD7). A `queued` row is waiting for
a worker — from `run_after` at the earliest — a `running` one names the run
that holds it in `worker_run`, and a `cancel` one has been asked to stop and
waits for its holder to notice, which happens within a poll.

```sql
SELECT
  id, kind, subject, state, attempt, max_attempts, priority,
  run_after, worker_run, last_error
FROM keelson('watchbill')
WHERE state IN ('queued', 'running', 'cancel')
ORDER BY state, priority, run_after
```
