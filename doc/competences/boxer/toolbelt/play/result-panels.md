---
name: Result panels
level: 3
parent_ids:
  - "[[play/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0172
  - adr:0161
  - adr:0245
  - adr:0227
  - adr:0153
  - adr:0163
  - adr:0231
  - app:play
  - scene:08_chart_bars
  - scene:08_distribution
  - scene:07_kanban
  - scene:08_cards
  - scene:15_flow
  - table:keelson/panel_shapes
  - explanation:kanban-ux
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0172, explanation:kanban-ux, scene:08_chart_bars]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0172, explanation:kanban-ux, scene:08_chart_bars]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0172, explanation:kanban-ux, scene:08_chart_bars]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A result is shown by whichever panel accepts its shape — chart, distribution, kanban, cards, treemap, geo raster, choropleth, sankey, layered graph, graph view, flow graph, chat, time series, vector field — each registered with the shape it needs.
