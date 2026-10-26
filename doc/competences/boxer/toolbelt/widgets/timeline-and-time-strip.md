---
name: Timeline and time strip
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0043
  - adr:0251
  - pkg:public/thestack/imzero2/egui2/widgets/timeline
  - pkg:public/thestack/imzero2/egui2/widgets/timescrubber
  - scene:04_timeline
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0043, scene:04_timeline]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0043, scene:04_timeline]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0043, scene:04_timeline]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A timeline of bands and a strip to scrub a time range, shared by play's timeline panel and the vector-field view.
