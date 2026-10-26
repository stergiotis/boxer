---
name: Compression-distance similarity
level: 3
parent_ids:
  - "[[analytics-and-science/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0175
  - pkg:public/analytics/similarity
  - pkg:public/gov/capmapsimilarity
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0175, pkg:public/analytics/similarity, pkg:public/gov/capmapsimilarity]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0175, pkg:public/analytics/similarity, pkg:public/gov/capmapsimilarity]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0175, pkg:public/analytics/similarity, pkg:public/gov/capmapsimilarity]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

How alike two texts are by normalised compression distance, used to rank competences and documents by resemblance.
