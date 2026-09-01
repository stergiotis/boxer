---
type: reference
audience: end-user
status: draft
title: Find files
summary: "Match snapshot files by path, extension and size"
icon: "🔎"
endpoint: default
tabs: [table, detail@side]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Find files

Files of the newest snapshot matched by path pattern, extension and minimum
size — the Find dialog of a file-transfer client as one `WHERE` clause over the
store, across every visible mount or one.

`pattern` is a RE2 regular expression over the whole path (`''` matches
everything); `ext` is an extension with its dot (`'.go'`, `''` for any);
`min_size` is bytes. The largest matches come first and the list is capped,
because a pattern that matches a whole mount is a question for `du`, not for
a list.

```sql
SET param_m = '*';
SET param_pattern = '';
SET param_ext = '';
SET param_min_size = 0;
SELECT
  mount AS "mount@gloss/taggedid",
  path,
  size AS "size@gloss/bytes",
  mtime,
  ext,
  content,
  lower(hex(content_hash)) AS hash
FROM fs({m:String})
WHERE NOT is_dir
  AND ({pattern:String} = '' OR match(path, {pattern:String}))
  AND ({ext:String} = '' OR ext = {ext:String})
  AND size >= {min_size:UInt64}
ORDER BY size DESC
LIMIT 500
```
