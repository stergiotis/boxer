---
name: The graph analytics engine
level: 3
parent_ids:
  - "[[analytics-and-science/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0229
  - adr:0230
  - adr:0235
  - pkg:public/analytics/graph/engine
  - pkg:public/analytics/graph/algo
  - pkg:public/analytics/explain
  - scene:08_graphview
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0229, scene:08_graphview]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0229, scene:08_graphview]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0229, scene:08_graphview]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A CSR graph with algorithms, nearest neighbours and a force model, driving the graph views and the clustering explanations.
