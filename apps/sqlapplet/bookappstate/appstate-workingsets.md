---
type: reference
audience: end-user
status: draft
title: Saved workingsets
summary: "The window state each app restores on its next plain open"
icon: "🪟"
endpoint: introspection
tabs: [table]
keywords: [app state, workingset, restore, launch config]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Saved workingsets

What a plain open of each participating app would restore (ADR-0148): the
launch config the last closed window left behind, its kind, its size, and
why that window closed. Read from `keelson('workingsets')`, which carries
the kind's own columns; the same rows appear in `keelson('app_state')` as
kind `workingset`.

```sql
SELECT app_id, name, kind, config_bytes, reason, saved_at, run_id, tile_key
FROM keelson('workingsets')
ORDER BY app_id, name
```
