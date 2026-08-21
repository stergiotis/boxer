---
type: reference
audience: end-user
status: draft
title: Block audit
icon: "🔏"
endpoint: default
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Block audit

Every stored block of the newest snapshot, recomputed and compared with the
BLAKE3 digest the walker wrote beside it. The store keeps a standalone digest
per block precisely so that this check is one statement the server runs —
`BLAKE3()` in ClickHouse agrees with the Go implementation the walker used —
and a row here means bytes that changed after they were written, which
should not happen.

This reads every block of the mount, so it costs what the mount weighs; narrow
`m` to one mount before running it over a large store. The result lists at
most the first mismatches; the `bad` total is in the first row.

```sql
SET param_m = '*';
SELECT
  mount AS "mount@gloss/taggedid",
  path,
  seq,
  lower(hex(hash)) AS recorded,
  lower(hex(BLAKE3(data))) AS recomputed,
  count() OVER () AS bad
FROM fsdata({m:String})
WHERE BLAKE3(data) != hash
ORDER BY mount, path, seq
LIMIT 100
```
