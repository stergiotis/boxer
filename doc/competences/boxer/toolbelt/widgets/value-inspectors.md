---
name: Value inspectors
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0046
  - adr:0067
  - pkg:public/thestack/imzero2/egui2/widgets/inspector
  - pkg:public/thestack/imzero2/egui2/widgets/componentview
  - pkg:public/thestack/imzero2/egui2/widgets/mappingplanview
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0046, adr:0067, pkg:public/thestack/imzero2/egui2/widgets/inspector, pkg:public/thestack/imzero2/egui2/widgets/componentview, pkg:public/thestack/imzero2/egui2/widgets/mappingplanview]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0046, adr:0067, pkg:public/thestack/imzero2/egui2/widgets/inspector]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0046, adr:0067, pkg:public/thestack/imzero2/egui2/widgets/inspector]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Inspectors for a value, a field, a component and a mapping plan — the widgets a developer reads a record through.
