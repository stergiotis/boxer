---
type: reference
audience: end-user
status: draft
title: Merges and inserts
summary: "Lay merges, inserts and mutations out on a per-table timeline"
icon: "⏱"
endpoint: default
tabs: [timeline, table]
keywords: [part_log, merge, mutation, insert, parts, background]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Merges and inserts

What happened to the data parts of each table, from `system.part_log`: every
new part (an insert), merge, mutation and replica download as an interval on
the table's own lane, ending when the server logged it and starting
`duration_ms` earlier. Overlapping bars on one lane are concurrent merges;
a lane solid with short bars is a table taking many small inserts.

**The knob.** `minutes` is how far back to read.

**Needs the log.** `part_log` exists only when the server config enables it,
and it is flushed on an interval, so the last few seconds may be missing. An
insert's duration covers writing the part, not the client's whole request.

```sql
SET param_minutes = 60;
SELECT
    toDateTime64(event_time_microseconds - toIntervalMillisecond(duration_ms), 6) AS _tl_time,
    toDateTime64(event_time_microseconds, 6) AS _tl_time_end,
    concat(database, '.', table) AS _tl_lane,
    toString(event_type) AS event_type, part_name, rows, size_in_bytes, duration_ms, error
FROM system.part_log
WHERE event_time >= now() - toIntervalMinute({minutes:UInt32})
  AND event_type IN ('NewPart', 'MergeParts', 'MutatePart', 'DownloadPart')
ORDER BY _tl_time
```
