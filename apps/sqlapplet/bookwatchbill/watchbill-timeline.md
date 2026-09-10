---
type: reference
audience: end-user
status: draft
title: Watchbill job timeline
summary: "Every transition of one job, in order, with who made it"
icon: "🧵"
endpoint: introspection
tabs: [table]
keywords: [watchbill, job, timeline, transition, event, history]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Watchbill job timeline

One job's life as its transition rows (ADR-0223 §SD2): claimed, then
succeeded, failed and re-queued, cancelled, abandoned — each with the
attempt it belonged to, the run that wrote it, and the note or error it
carried. Set `param_job` to the id from the in-flight or failures applet;
the default shows the newest job's trail.

```sql
SET param_job = '';

SELECT at, state, attempt, worker_run, note, splitByChar('\n', error)[1] AS message
FROM keelson('watchbill_event')
WHERE job_id = if({job:String} = '',
                  (SELECT job_id FROM keelson('watchbill_event') ORDER BY at DESC LIMIT 1),
                  {job:String})
ORDER BY at
```
