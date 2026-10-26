---
name: The tab host digest
level: 3
parent_ids:
  - "[[distribution-and-hosts/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0278
  - pkg:public/thestack/cmd/imzero2tab
  - howto:imzero2-in-the-browser
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0278, howto:imzero2-in-the-browser]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0278, howto:imzero2-in-the-browser]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0278, howto:imzero2-in-the-browser]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

The browser host's Rust build has a recorded digest, refreshed in the commit that changes it and checked by a workflow.
