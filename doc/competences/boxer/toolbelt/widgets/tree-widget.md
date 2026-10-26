---
name: The native tree
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0176
  - pkg:public/thestack/imzero2/egui2/widgets/tree
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0176, pkg:public/thestack/imzero2/egui2/widgets/tree]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0176, pkg:public/thestack/imzero2/egui2/widgets/tree]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0176, pkg:public/thestack/imzero2/egui2/widgets/tree]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A tree over any hierarchical record, with the expand state the widget owns.
