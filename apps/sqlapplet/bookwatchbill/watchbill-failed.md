---
type: reference
audience: end-user
status: draft
title: Watchbill failures
summary: "Failed attempts and discarded jobs, with the error each carried"
icon: "💥"
endpoint: introspection
tabs: [table]
keywords: [watchbill, failed, discarded, error, retry, attempt]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Watchbill failures

Every attempt that ended in error, newest first, from
`keelson('watchbill_event')`: a `failed` row is an attempt that will be
retried under the job's policy, a `discarded` row the last one. The error
column is the boxer error chain as the task primitive renders it, so the
first line is the message and the rest is where it came from.

```sql
SELECT
  at, job_id, state, attempt, worker_run,
  splitByChar('\n', error)[1] AS message,
  note
FROM keelson('watchbill_event')
WHERE state IN ('failed', 'discarded')
ORDER BY at DESC
LIMIT 500
```
