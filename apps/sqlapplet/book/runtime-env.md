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
external ClickHouse needed. The `pattern` parameter filters by variable
name (SQL LIKE syntax; `%` matches everything) and renders as a widget in
the applet's params strip — parameter binding against the in-process
endpoint per ADR-0133.

This document is itself an ADR-0132 SQL applet; its `tabs:` list pins the
surface to the Table and Detail panes.

```sql
SET param_pattern = '%';
SELECT * FROM keelson('env')
WHERE name LIKE {pattern:String}
```
