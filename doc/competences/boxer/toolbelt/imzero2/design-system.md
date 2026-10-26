---
name: The design system and policy as code
level: 3
parent_ids:
  - "[[imzero2/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0029
  - adr:0030
  - adr:0031
  - adr:0032
  - adr:0034
  - adr:0040
  - adr:0044
  - adr:0065
  - adr:0156
  - adr:0258
  - pkg:public/keelson/designsystem
  - skill:imzero2
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0029, adr:0156, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0029, adr:0156, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0029, adr:0156, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Palette, spacing, type and component policy are generated from tokens and checked by a lint, so a widget cannot pick its own colour and a theme change is one edit.
