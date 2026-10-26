---
name: Layered graphs and entity diagrams
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0069
  - adr:0129
  - adr:0167
  - adr:0331
  - pkg:public/thestack/imzero2/egui2/widgets/layeredgraph
  - pkg:public/thestack/imzero2/egui2/widgets/schemaview
  - scene:10_schema
  - scene:11_graph
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0129, scene:11_graph]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0129, scene:11_graph]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0129, scene:11_graph]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Layered (hierarchical) graph layout for pipelines and dependency diagrams, and entity diagrams drawn from a schema.
