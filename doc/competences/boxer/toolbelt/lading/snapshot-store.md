---
name: The snapshot store
level: 3
parent_ids:
  - "[[lading/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0198
  - pkg:public/fs/lading
  - howto:lading-snapshot-store
  - applet:lad-tree
  - applet:lad-du
  - trial:fs-snapshot-store-m0
assessment:
  - dimension: readiness
    rating: 8
    basis: [adr:0198, howto:lading-snapshot-store, trial:fs-snapshot-store-m0]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0198, howto:lading-snapshot-store, trial:fs-snapshot-store-m0]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0198, howto:lading-snapshot-store, trial:fs-snapshot-store-m0]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A walk of a tree becomes rows keyed by snapshot and path, with hashes and optional content, written through the record store and read with the applet book's lenses.
