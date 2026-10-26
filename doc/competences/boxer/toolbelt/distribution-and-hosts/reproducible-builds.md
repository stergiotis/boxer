---
name: Reproducible builds
level: 3
parent_ids:
  - "[[distribution-and-hosts/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0215
  - howto:airgapped-build
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0215, howto:airgapped-build]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: commodity
    basis: [adr:0215, howto:airgapped-build]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0215, howto:airgapped-build]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every shipped binary is built from the same two environment files, trimmed and pinned, so two builds of one commit are the same bytes.
