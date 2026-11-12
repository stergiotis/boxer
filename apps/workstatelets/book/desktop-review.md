---
type: reference
audience: end-user
status: draft
title: Desktop review
summary: "Query the open windows beside the app center"
topics: [runtime, observability]
keywords: [windows, desktop, layout, workstatelet]
desktop:
  layout: main-stack
  mainRatio: 0.6
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Desktop review

Two windows for looking at what is open on this desktop: a playground on
`keelson('windows')`, and the app center, under the main-and-stack layout
([ADR-0369](../../../doc/adr/0369-workstatelets-a-desktops-windows-and-layout-as-a-markdown-document.md),
proposed).

## 1. The open windows, as rows

The window names a launch kind and no app, so the host opens whichever
playground this process designates. Its config seeds the query below and runs
it against the in-process tables; the package's test states the same value in
Go.

```sql
SELECT key, app_id, title, tiled, floating, "order"
FROM keelson('windows')
ORDER BY "order"
```

```yaml window
kind: playLaunch
config: |
  gbRiaWQAam5hdHVyYWxLZXlAYnRzGxjnIyC6LwAAaWV4cGlyZXNBdDteTfwUwuX//290ZXh0QXJy
  YXkudmFsdWWCeFxTRUxFQ1Qga2V5LCBhcHBfaWQsIHRpdGxlLCB0aWxlZCwgZmxvYXRpbmcsICJv
  cmRlciIKRlJPTSBrZWVsc29uKCd3aW5kb3dzJykKT1JERVIgQlkgIm9yZGVyImBsdGV4dEFycmF5
  LmxyghsAAAADAAAAhRsAAAADAAAAiG10ZXh0QXJyYXkubGVuggEBcHRleHRBcnJheS5ocmNhcmSC
  AABwdGV4dEFycmF5LmxyY2FyZIIBAXF0ZXh0QXJyYXkubG1yY2FyZIIAAGxzeW1ib2wudmFsdWWD
  YG1pbnRyb3NwZWN0aW9uYGlzeW1ib2wubHKDGwAAAAMAAACJGwAAAAMAAACKGwAAAAMAAADubXN5
  bWJvbC5ocmNhcmSDAAAAbXN5bWJvbC5scmNhcmSDAQEBbnN5bWJvbC5sbXJjYXJkgwAAAGpib29s
  LnZhbHVlgvX0Z2Jvb2wubHKCGwAAAAMAAACGGwAAAAMAAACHa2Jvb2wuaHJjYXJkggAAa2Jvb2wu
  bHJjYXJkggEBbGJvb2wubG1yY2FyZIIAAA==
```

## 2. The app center

Opened as a click on it would.

```yaml window
app: appcenter
```
