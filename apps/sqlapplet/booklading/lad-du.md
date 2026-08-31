---
type: reference
audience: end-user
status: draft
title: Disk usage
summary: "Find where a snapshot's bytes are, by directory and file"
icon: "📊"
endpoint: default
tabs: ["treemap:files", table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Disk usage

Where the bytes are. The Treemap tab nests every file of the newest snapshot
under its directories — one rectangle per file, area by size, the mount as the
outermost box so several mounts stay apart — and the Table tab is `du`: every
directory's recursive total in one pass, largest first, computed by unfolding
each file's ancestor prefixes rather than by walking.

`top` caps the table. The sizes are the files' recorded sizes, not the store's
block storage; a `ref` entry counts its full size although the store holds no
bytes for it.

```sql
SET param_m = '*';
SET param_top = 200;
WITH
  files AS (
    SELECT arrayConcat([lower(hex(mount))], splitByChar('/', path)) AS stack,
           toFloat64(size) AS value,
           'B' AS unit
    FROM fs({m:String})
    WHERE NOT is_dir
  )
SELECT
  mount AS "mount@gloss/taggedid",
  anc AS directory,
  sum(size) AS "bytes@gloss/bytes",
  count() AS files
FROM fs({m:String})
ARRAY JOIN arrayMap(k -> arrayStringConcat(arraySlice(splitByChar('/', path), 1, k), '/'), range(1, depth)) AS anc
WHERE NOT is_dir
GROUP BY mount, anc
ORDER BY sum(size) DESC
LIMIT {top:UInt64}
```
