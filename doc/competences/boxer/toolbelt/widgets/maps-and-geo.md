---
name: Maps and geo
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0204
  - adr:0096
  - adr:0114
  - adr:0244
  - pkg:public/thestack/imzero2/egui2/widgets/basemap
  - pkg:public/thestack/imzero2/egui2/widgets/worldmap
  - pkg:public/thestack/imzero2/egui2/widgets/portolan
  - howto:graph-on-a-map
  - howto:play-adsb-map
  - trial:map-tile-addressing
  - trial:portolan-land-frame-cost
  - scene:05_map_raster
assessment:
  - dimension: readiness
    rating: 8
    basis: [adr:0204, howto:graph-on-a-map, trial:map-tile-addressing]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [adr:0204, howto:graph-on-a-map, trial:map-tile-addressing]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0204, howto:graph-on-a-map, trial:map-tile-addressing]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A slippy map with raster tiles, a world map with choropleths, a graph drawn over a map, a land-aware chart, and Swiss coordinates where a consumer needs them.
