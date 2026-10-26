---
name: The air-gapped bundle
level: 3
parent_ids:
  - "[[distribution-and-hosts/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0095
  - howto:airgapped-build
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0095, howto:airgapped-build]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0095, howto:airgapped-build]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0095, howto:airgapped-build]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Toolchain, modules and sources in one archive that builds with no network.
