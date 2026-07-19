---
type: reference
audience: end-user
status: draft
title: Runtime environment
icon: "🌡"
endpoint: introspection
tabs: [table, detail]
---

# Runtime environment

The boxer environment-variable registry (ADR-0009) with live values, read
from the `keelson.env` introspection table: every declared knob, its
category, description, and this process's current (redacted where
sensitive) value. Runs against the in-process introspection endpoint — no
external ClickHouse needed.

This document is itself an ADR-0132 SQL applet; its `tabs:` list pins the
surface to the Table and Detail panes.

```sql
SELECT * FROM keelson('env')
```
