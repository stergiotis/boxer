---
type: reference
audience: end-user
status: draft
title: Recent queries
icon: "🕘"
tabs: [table, detail]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Recent queries

The last completed statements of the env-configured ClickHouse, straight
from `system.query_log`, newest first. The `lim` parameter renders as a
widget in the applet's params strip; edit it and press Run for a longer or
shorter window. Needs a reachable server with the query log enabled (the
default endpoint, not the in-process introspection one).

```sql
SET param_lim = 50;
SELECT
    event_time,
    type,
    query_duration_ms,
    read_rows,
    substring(query, 1, 120) AS query_head
FROM system.query_log
WHERE type != 'QueryStart'
ORDER BY event_time DESC
LIMIT {lim:UInt64}
```
