---
type: reference
audience: end-user
status: draft
title: Compare two snapshots
icon: "🔀"
endpoint: default
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Compare two snapshots

What changed between two snapshots of a mount: every path that was added,
removed or modified, classified by a full outer join on the path. A file is
*modified* when its content hash or its modification time differs; unchanged
paths are left out. This is the Compare Directories of a file-transfer client
turned on time instead of on two servers — and because the store's name for
the newest complete snapshot is `'latest'`, the chapter runs as written and
shows nothing until a second instant is named.

`s1` and `s2` are snapshot instants as the ledger lists them (a datetime
literal such as `'2026-08-20 10:15:00.000000000'`) or `'latest'`. With `m` left
at `'*'` every visible mount is compared with itself; the join carries the
mount so two mounts are never mixed.

```sql
SET param_m = '*';
SET param_s1 = 'latest';
SET param_s2 = 'latest';
SELECT
  if(n.path != '', n.mount, o.mount) AS "mount@gloss/taggedid",
  if(n.path != '', n.path, o.path) AS path,
  multiIf(o.path = '', 'added',
          n.path = '', 'removed',
          n.content_hash != o.content_hash OR n.mtime != o.mtime, 'modified',
          'same') AS change,
  o.size AS "size_before@gloss/bytes",
  n.size AS "size_after@gloss/bytes",
  o.mtime AS mtime_before,
  n.mtime AS mtime_after
FROM fs({m:String}, {s2:String}) AS n
FULL OUTER JOIN fs({m:String}, {s1:String}) AS o ON n.mount = o.mount AND n.path = o.path
WHERE change != 'same'
ORDER BY path
```
