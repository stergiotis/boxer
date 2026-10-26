---
name: Plots
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0149
  - pkg:public/thestack/imzero2/egui2/widgets/implot
  - explanation:implot-adoption-survey
  - scene:08_chart_series
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0149, explanation:implot-adoption-survey, scene:08_chart_series]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [adr:0149, explanation:implot-adoption-survey, scene:08_chart_series]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0149, explanation:implot-adoption-survey, scene:08_chart_series]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Line, bar, scatter and heat-map plots over an Arrow record, through an adopted plotting library behind one Go surface.
