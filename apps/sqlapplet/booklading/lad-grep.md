---
type: reference
audience: end-user
status: draft
title: Search content
icon: "🧵"
endpoint: default
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Search content

`grep` with real line numbers over the stored blocks of text files. Where the
store set `text` on an entry, every block boundary falls immediately after a
newline and each block carries its first line's number, so a single-line match
never straddles two blocks and `line0 + i - 1` is the line as an editor counts
it. Files without the guarantee — binaries, and text with a line longer than a
block — are left out rather than searched inexactly.

`needle` is a RE2 regular expression; empty searches nothing. Only files whose
content was stored inline can be searched: a `ref` entry has a hash and a size
and no bytes.

```sql
SET param_m = '*';
SET param_needle = '';
SELECT
  mount AS "mount@gloss/taggedid",
  path,
  line0 + i - 1 AS lineno,
  line
FROM fsdata({m:String})
ARRAY JOIN splitByChar('\n', data) AS line, arrayEnumerate(splitByChar('\n', data)) AS i
WHERE {needle:String} != ''
  AND (mount, path) IN (SELECT mount, path FROM fs({m:String}) WHERE text)
  AND match(data, {needle:String})
  AND match(line, {needle:String})
ORDER BY mount, path, lineno
LIMIT 1000
```
