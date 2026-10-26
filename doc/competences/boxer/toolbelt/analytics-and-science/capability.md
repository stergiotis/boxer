---
name: Analytics and science
abbrev: Sci
synopsis: "anomaly detection, similarity, graph analytics, geo and units"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0150
  - adr:0175
  - adr:0229
  - adr:0230
  - adr:0235
  - adr:0003
  - adr:0244
  - adr:0256
  - pkg:public/analytics
  - pkg:public/science
  - explanation:timeseries-motif-anomaly-survey
  - scene:08_graphview
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0229, explanation:timeseries-motif-anomaly-survey]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0229, explanation:timeseries-motif-anomaly-survey]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0229, explanation:timeseries-motif-anomaly-survey]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Computations a data app reaches for: anomaly detection over subsequences, compression-distance similarity, a graph engine with a force model and clustering explanations, projection feature sets, smoothing, distributions, and the geo stack — H3, Swiss coordinates, GRIB, routing, line of sight. The customer is someone who wants these as Go packages with a result that lands in a record.

Its competences: [[subsequence-anomaly-detection]], [[compression-similarity]], [[graph-engine]], [[projections-and-smoothing]], [[geo-stack]], [[units]].

# Mechanisms

Each is a package with one surface and a test corpus; the graph engine and the geo readers are the larger ones.

# Evidence

The motif-anomaly survey, the explanation of clustering thresholds, and play's scenes that draw the results.

# Boundaries

Not a statistics library: the packages exist where a widget or a lens needed the computation.
