---
type: reference
audience: end-user
status: draft
title: Browse a snapshot
icon: "🗂️"
endpoint: default
tabs: [files, detail@side]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Browse a snapshot

The whole snapshot as a tree. The Files tab (ADR-0200) reads the `path` column
and browses what the query returned — one directory at a time as a listing, or
the subtree below it as an outline — with the store's own columns beside name,
size and modified: the extension, whether the content was stored inline,
referenced or not at all, whether the text guarantee holds, and the BLAKE3
hash. The Detail pane shows the selected entry's row in full.

The **Browse a directory** chapter answers the same question one directory at a
time and hands its rows to a table; this one hands the whole tree to a browser,
which is the difference between reading a listing and walking one.

`m` is a mount id or `'*'` for every visible mount — under `'*'` the mounts
merge into one tree and the `mount` column says which each entry came from.
`under` scopes the read to a subtree: `'.'` is the whole snapshot, `'a/b'` that
directory and everything below it. `top` bounds the read; the browser interns
what arrives and says in its status line what it did not.

```sql
SET param_m = '*';
SET param_under = '.';
SET param_top = 20000;
SELECT
  path,
  is_dir,
  size AS "size@gloss/bytes",
  mtime,
  ext,
  content,
  text,
  link_target,
  lower(hex(content_hash)) AS hash,
  mount AS "mount@gloss/taggedid"
FROM fs({m:String})
WHERE {under:String} = '.' OR startsWith(path, concat({under:String}, '/'))
ORDER BY path
LIMIT {top:UInt64}
```
