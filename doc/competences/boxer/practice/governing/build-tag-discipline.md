---
name: Build-tag discipline
level: 3
parent_ids:
  - "[[governing/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0212
  - adr:0106
  - pkg:public/gov/buildtags
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0212, adr:0106, pkg:public/gov/buildtags]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0212, adr:0106, pkg:public/gov/buildtags]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0212, adr:0106, pkg:public/gov/buildtags]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every build, test and vet reads the tags file rather than hardcoding its contents, so a tag added later reaches every invocation at once.
