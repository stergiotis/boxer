---
name: Package properties
level: 3
parent_ids:
  - "[[gov/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0080
  - pkg:public/packageprops
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0080, pkg:public/packageprops]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0080, pkg:public/packageprops]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0080, pkg:public/packageprops]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Each package declares its curated properties — which WASM targets it blocks, what kind it is — in a file a tool seeds and verifies.
