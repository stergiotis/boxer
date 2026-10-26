---
name: Graph views and the painter lane
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0224
  - adr:0225
  - adr:0228
  - adr:0232
  - pkg:public/thestack/imzero2/egui2/widgets/graphview
  - scene:08_graphview
  - scene:08_network
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0228, scene:08_graphview]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0228, scene:08_graphview]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0228, scene:08_graphview]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Node-link views laid out by the graph engine and drawn through a painter lane a canvas shares, so a graph on a map and a graph in a pane are one widget.
