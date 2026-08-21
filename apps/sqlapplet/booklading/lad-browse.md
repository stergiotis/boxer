---
type: reference
audience: end-user
status: draft
title: Browse a directory
icon: "📁"
endpoint: default
tabs: [table, detail@side]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Browse a directory

One directory of a snapshot as a listing: directories first, then files, with
size, modification time, extension, whether the content was stored inline,
referenced or not at all, whether the text guarantee holds, and the BLAKE3
hash. The Detail pane beside it shows the selected row's every attribute.

`dir` is an `io/fs` path relative to the snapshot root: `'.'` lists the root,
`'a/b'` lists that directory, no leading slash. `m` is a mount id or `'*'` for
every visible mount (each row says which). The newest complete snapshot is
read; pin another in SQL by giving `fs()` a second argument.

A directory row's size is whatever the source reported — the local filesystem's
own bookkeeping, not a recursive total. The `du` chapter has the totals.

```sql
SET param_m = '*';
SET param_dir = '.';
SELECT
  mount AS "mount@gloss/taggedid",
  name,
  is_dir,
  node_kind,
  size AS "size@gloss/bytes",
  mtime,
  ext,
  content,
  text,
  link_target,
  lower(hex(content_hash)) AS hash,
  path
FROM fs({m:String})
WHERE dir = {dir:String}
ORDER BY mount, is_dir DESC, name
```
