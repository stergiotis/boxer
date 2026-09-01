---
type: reference
audience: end-user
status: draft
title: History of a path
summary: "Follow one path across every snapshot of a mount"
icon: "🕰"
endpoint: default
tabs: [table, timeline]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# History of a path

One path across every complete snapshot of a mount: its size, modification
time and content hash each time it was seen, oldest first — the Versions tab
of a cloud browser, except that every snapshot is a version and the store
keeps them by policy rather than by accident. A snapshot in which the path was
absent simply has no row.

`path` is relative to the snapshot root; the default `'.'` is the root itself,
so the chapter opens as the list of the mount's snapshots with the root's
modification time on each. The Timeline tab places the rows by snapshot
instant.

```sql
SET param_m = '*';
SET param_path = '.';
SELECT
  mount AS "mount@gloss/taggedid",
  snap AS _tl_time,
  lower(hex(mount)) AS _tl_lane,
  node_kind,
  size AS "size@gloss/bytes",
  mtime,
  lower(hex(content_hash)) AS hash,
  content,
  expires_at
FROM fs({m:String}, '*')
WHERE path = {path:String}
ORDER BY mount, snap
```
