---
type: reference
audience: end-user
status: draft
title: Column-width overrides
summary: "Table columns a user dragged to a width, per app and tier"
icon: "↔️"
endpoint: introspection
tabs: [table]
keywords: [app state, column width, table, override, tier]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Column-width overrides

Every column a user dragged to a width that the app now remembers
(ADR-0151). The `instance` tier applies to one table (its scope is the
table's tag), the `column` tier to that column wherever the app shows it.
The width is in points, beside the font size it was captured at, which a
table uses to rescale it.

The key is `tier/scope/column_key`; a scope may itself contain `/`, so the
middle is everything between the first and the last segment.

```sql
SELECT
  app_id,
  splitByChar('/', key)[1] AS tier,
  arrayStringConcat(arraySlice(splitByChar('/', key), 2, length(splitByChar('/', key)) - 2), '/') AS scope,
  splitByChar('/', key)[-1] AS column_key,
  detail AS width,
  written_at,
  instance_key
FROM keelson('app_state')
WHERE kind = 'column_width'
ORDER BY app_id, tier, scope, column_key
```
