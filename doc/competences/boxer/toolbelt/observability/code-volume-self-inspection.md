---
name: Code-volume self-inspection
level: 3
parent_ids:
  - "[[observability/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0173
  - howto:code-volume
  - applet:vol-overview
  - applet:go-overview
  - table:keelson/go_modules
  - table:keelson/go_symbols
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0173, howto:code-volume, applet:vol-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0173, howto:code-volume, applet:vol-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0173, howto:code-volume, applet:vol-overview]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

The binary reports what it is made of — modules, packages, symbols, volume — as tables, and a book of applets reads them.
