---
name: "Flows, icicle, treemap and sankey"
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0159
  - adr:0160
  - adr:0166
  - pkg:public/thestack/imzero2/egui2/widgets/treemap
  - pkg:public/thestack/imzero2/egui2/widgets/icicle
  - pkg:public/thestack/imzero2/egui2/widgets/sankey
  - scene:08_treemap
  - scene:08_icicle
  - scene:08_sankey
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0166, scene:08_treemap]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0166, scene:08_treemap]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0166, scene:08_treemap]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Part-of-whole and flow views — treemap, icicle, sankey — sized by a measure and coloured by a category, over one record shape.
