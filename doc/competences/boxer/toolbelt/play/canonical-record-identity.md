---
name: Canonical record identity in results
level: 3
parent_ids:
  - "[[play/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0219
  - adr:0201
  - app:play
  - scene:02_table_taggedid
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0219, scene:02_table_taggedid]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0219, scene:02_table_taggedid]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0219, scene:02_table_taggedid]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A row in a result carries the identity its canonical form gives it, so a selection survives a re-run and a detail pane can find its record again.
