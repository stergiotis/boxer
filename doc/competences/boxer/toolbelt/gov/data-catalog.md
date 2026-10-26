---
name: The data catalog
level: 3
parent_ids:
  - "[[gov/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0170
  - pkg:public/gov/datacatalog
  - applet:cat-overview
  - applet:cat-shapes
  - applet:cat-unmatched
assessment:
  - dimension: readiness
    rating: 4
    basis: [adr:0170, applet:cat-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0170, applet:cat-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0170, applet:cat-overview]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What tables exist, what shapes they carry and which of them nothing reads, as tables of their own.
