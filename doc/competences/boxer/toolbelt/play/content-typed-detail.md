---
name: Content-typed detail
level: 3
parent_ids:
  - "[[play/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0123
  - app:play
  - howto:play-pluggable-detail
  - scene:03_detail_card
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0123, howto:play-pluggable-detail, scene:03_detail_card]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0123, howto:play-pluggable-detail, scene:03_detail_card]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0123, howto:play-pluggable-detail, scene:03_detail_card]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A column declares its media type in its name, and the detail pane renders the cell accordingly — markdown as markdown, JSON as a tree — with the rendering pluggable.
