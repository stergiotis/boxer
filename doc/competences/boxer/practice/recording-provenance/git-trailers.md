---
name: Git trailers
level: 3
parent_ids:
  - "[[recording-provenance/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0083
  - pkg:public/gov/repo
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0083, pkg:public/gov/repo]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0083, pkg:public/gov/repo]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0083, pkg:public/gov/repo]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A commit's trailers name the model that assisted, and the authorship analysis counts lines by them; the former in-file build tags are retired.
