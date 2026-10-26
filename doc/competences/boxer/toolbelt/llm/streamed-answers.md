---
name: Streamed answers
level: 3
parent_ids:
  - "[[llm/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0286
  - pkg:public/llm/openaichat
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0286, pkg:public/llm/openaichat]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: commodity
    basis: [adr:0286, pkg:public/llm/openaichat]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0286, pkg:public/llm/openaichat]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A completion streams tokens and assembles the same response the plain call returns.
