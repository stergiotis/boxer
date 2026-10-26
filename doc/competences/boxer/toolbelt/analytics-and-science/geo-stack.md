---
name: The geo stack
level: 3
parent_ids:
  - "[[analytics-and-science/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0003
  - adr:0244
  - adr:0292
  - adr:0256
  - adr:0099
  - pkg:public/science/geo/h3
  - pkg:public/science/geo/swisstopo
  - pkg:public/science/geo/grib
  - app:terrainscope
  - env:BOXER_GRIB_CORPUS
assessment:
  - dimension: readiness
    rating: 4
    basis: [adr:0003, app:terrainscope]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0003, app:terrainscope]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0003, app:terrainscope]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

H3 cells, Swiss coordinates, a GRIB reader for gridded fields, road routing and line of sight — the readers and transforms the map widgets need.
