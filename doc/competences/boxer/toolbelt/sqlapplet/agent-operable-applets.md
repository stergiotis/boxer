---
name: Agent-operable applet windows
level: 3
parent_ids:
  - "[[sqlapplet/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0274
  - adr:0269
  - applet:docs-search
assessment:
  - dimension: readiness
    rating: 2
    basis: [adr:0274, adr:0269, applet:docs-search]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0274, adr:0269, applet:docs-search]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0274, adr:0269, applet:docs-search]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An applet window exposes its knobs and its selection as operations an agent can drive under a task grant.
