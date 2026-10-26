---
name: FSM and pipeline views
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0045
  - adr:0119
  - pkg:public/thestack/imzero2/egui2/widgets/fsmview
  - pkg:public/thestack/imzero2/egui2/widgets/pipelineview
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0045, adr:0119, pkg:public/thestack/imzero2/egui2/widgets/fsmview, pkg:public/thestack/imzero2/egui2/widgets/pipelineview]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0045, adr:0119, pkg:public/thestack/imzero2/egui2/widgets/fsmview]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0045, adr:0119, pkg:public/thestack/imzero2/egui2/widgets/fsmview]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A state machine and a pipeline drawn as diagrams from their definitions.
