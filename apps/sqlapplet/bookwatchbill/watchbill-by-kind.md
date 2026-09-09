---
type: reference
audience: end-user
status: draft
title: Watchbill by kind
summary: "Outcomes and attempts per job kind, from the transition rows"
icon: "📊"
endpoint: introspection
tabs: [table, chart]
keywords: [watchbill, kind, outcome, attempts, throughput]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Watchbill by kind

How each kind of work fares: how many runs started, how many succeeded,
failed, were discarded, cancelled or abandoned, and how many attempts the
average job needed. Joins the transition rows to the job rows on the id;
a job whose row has since expired keeps its events and shows under its
events' kind as unknown.

```sql
SELECT
  coalesce(j.kind, '(expired)') AS x,
  countIf(e.state = 'running')   AS started,
  countIf(e.state = 'succeeded') AS succeeded,
  countIf(e.state = 'failed')    AS failed,
  countIf(e.state = 'discarded') AS discarded,
  countIf(e.state = 'cancelled') AS cancelled,
  countIf(e.state = 'abandoned') AS abandoned,
  countIf(e.state = 'succeeded') AS y
FROM keelson('watchbill_event') AS e
LEFT JOIN keelson('watchbill') AS j ON j.id = e.job_id
GROUP BY x
ORDER BY started DESC
```
