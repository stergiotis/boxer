---
name: Vector fields and particles
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0249
  - adr:0250
  - pkg:public/science/geo/vectorfield
  - howto:vector-field-on-a-map
  - trial:flow-particles-frame-cost
  - scene:36_vector_field
assessment:
  - dimension: readiness
    rating: 8
    basis: [adr:0250, howto:vector-field-on-a-map, trial:flow-particles-frame-cost]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0250, howto:vector-field-on-a-map, trial:flow-particles-frame-cost]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0250, howto:vector-field-on-a-map, trial:flow-particles-frame-cost]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A gridded vector field — wind, currents — drawn as particles over a map, with a time strip to scrub through the grid's steps.
