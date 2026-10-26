---
name: Loading applets from an origin
level: 3
parent_ids:
  - "[[sqlapplet/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0299
  - applet:docs-search
assessment:
  - dimension: readiness
    rating: 2
    basis: [adr:0299, applet:docs-search]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0299, applet:docs-search]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0299, applet:docs-search]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A book can be loaded from an origin other than the embedded one — a directory, a store — so a consumer ships lenses without rebuilding the host.
