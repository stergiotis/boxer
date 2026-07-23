---
type: reference
audience: end-user
status: draft
title: Runtime apps
icon: "🧩"
endpoint: introspection
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Runtime apps

Every app registered in this boxer process, read live from the
`keelson.apps` introspection table (ADR-0094) — the same inventory the
Apps menu lists, as a queryable relation. The applet speaks to the
in-process introspection endpoint, so it works with no external ClickHouse.

This document is itself an ADR-0132 SQL applet: the query below is the
whole definition. Paste it into the SQL Playground to explore further.

```sql
SELECT * FROM keelson('apps')
```
