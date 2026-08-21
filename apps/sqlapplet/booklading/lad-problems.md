---
type: reference
audience: end-user
status: draft
title: Problems
icon: "⚠️"
endpoint: default
tabs: [table]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Problems

Entries the walk could not read. A node that fails to open becomes a row with
its error rather than aborting the snapshot, so a tree with one unreadable
directory is still a snapshot — and the failure is a query, not a log line
somebody has to find. Each row is the path, what kind of node it is, and the
error text as the walker recorded it.

An empty result is the good case. For the bytes themselves, the `audit`
chapter checks every stored block against its digest.

```sql
SET param_m = '*';
SELECT
  mount AS "mount@gloss/taggedid",
  path,
  node_kind,
  content,
  err
FROM fs({m:String})
WHERE err != ''
ORDER BY mount, path
```
