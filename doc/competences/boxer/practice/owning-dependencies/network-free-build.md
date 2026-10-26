---
name: Building without the network
level: 3
parent_ids:
  - "[[owning-dependencies/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0095
  - adr:0215
  - adr:0199
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
    rating: foundational
    basis: [adr:0095, howto:airgapped-build]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

The bundle carries the toolchain and every module, and the build environment is two files, so a build on an air-gapped machine is the same build.
