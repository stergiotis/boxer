---
name: Subsequence anomaly detection
level: 3
parent_ids:
  - "[[analytics-and-science/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0150
  - pkg:public/analytics/timeseries/matrixprofile
  - pkg:public/analytics/timeseries/adscore
  - explanation:timeseries-motif-anomaly-survey
  - scene:08_series_adjudication
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0150, explanation:timeseries-motif-anomaly-survey, scene:08_series_adjudication]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0150, explanation:timeseries-motif-anomaly-survey, scene:08_series_adjudication]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0150, explanation:timeseries-motif-anomaly-survey, scene:08_series_adjudication]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Motifs and discords in a time series by matrix profile, with a score a series panel can draw.
