---
name: Widget contracts
level: 3
parent_ids:
  - "[[imzero2/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0012
  - adr:0013
  - adr:0267
  - pkg:public/thestack/imzero2/egui2/widgets/conformance
  - skill:imzero2
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0267, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0267, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0267, skill:imzero2]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A Go widget is one of three shapes — fluid, immediate-mode, semi-retained — with one contract each, and a conformance package checks the mechanical rules over every widget package.
