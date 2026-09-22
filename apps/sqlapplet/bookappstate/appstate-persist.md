---
type: reference
audience: end-user
status: draft
title: Persisted app values
summary: "Each value an app persisted under a key, with its size and writer"
icon: "💾"
endpoint: introspection
tabs: [table]
keywords: [app state, persist, runtime.persist, key, value]
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Persisted app values

The values apps stored through `runtime.persist` (ADR-0026 §SD3), newest
version per key. The value itself is not shown — it is bytes the app chose,
readable only by that app (ADR-0185 §SD7) — only its size, when it was
written, and which run and window wrote it.

```sql
SELECT app_id, key, payload_bytes, written_at, run_id, instance_key
FROM keelson('app_state')
WHERE kind = 'persist'
ORDER BY app_id, key
```
