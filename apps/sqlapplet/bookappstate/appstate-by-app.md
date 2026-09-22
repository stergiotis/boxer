---
type: reference
audience: end-user
status: draft
title: App state by app
summary: "What each app keeps across restarts, by kind, with its size"
icon: "🗄️"
endpoint: introspection
tabs: [table]
keywords: [app state, persist, workingset, column width, storage, forget]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# App state by app

Everything an app keeps across restarts, one row per app, from
`keelson('app_state')` (ADR-0185 §SD1): how many persisted values,
workingsets and column-width overrides it holds, their payload size, and
when the newest of them was written. The counts are *live* entries — what a
restart would find — not the write trail; a cleared entry is not counted.

`unknown` counts entries of a kind this build does not recognise. They are
still stored state, and are listed rather than hidden.

```sql
SELECT
  app_id,
  countIf(kind = 'persist')      AS persisted,
  countIf(kind = 'workingset')   AS workingsets,
  countIf(kind = 'column_width') AS column_widths,
  countIf(kind = 'unknown')      AS unknown,
  sum(payload_bytes)             AS payload_bytes,
  max(written_at)                AS last_written
FROM keelson('app_state')
GROUP BY app_id
ORDER BY app_id
```
