---
type: reference
audience: end-user
status: draft
title: Watchbill abandonments
summary: "Jobs a dead worker run left behind, by the run that lost them"
icon: "🕯"
endpoint: introspection
tabs: [table]
keywords: [watchbill, abandoned, heartbeat, run, sweep, lease]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Watchbill abandonments

Every `abandoned` transition (ADR-0223 §SD4): a job was held by a run whose
heartbeat went stale, and another run's sweep took it back. The note names
the run that lost it; the `worker_run` column is the sweeper. A run that
appears here often is a process that dies with work in hand.

```sql
SELECT
  extract(note, 'worker run (\S+)') AS lost_by,
  count() AS jobs,
  min(at) AS first_seen,
  max(at) AS last_seen,
  groupArray(5)(job_id) AS sample_jobs
FROM keelson('watchbill_event')
WHERE state = 'abandoned'
GROUP BY lost_by
ORDER BY last_seen DESC
```
