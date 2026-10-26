---
name: Model inference as a capability
level: 3
parent_ids:
  - "[[llm/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0254
  - pkg:public/keelson/runtime/llm
  - env:BOXER_LLM_ENDPOINT
  - env:BOXER_LLM_MODEL
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0254, pkg:public/keelson/runtime/llm, env:BOXER_LLM_ENDPOINT, env:BOXER_LLM_MODEL]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: product
    basis: [adr:0254, pkg:public/keelson/runtime/llm, env:BOXER_LLM_ENDPOINT]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0254, pkg:public/keelson/runtime/llm, env:BOXER_LLM_ENDPOINT]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A model call is an effect the runtime grants and records, with its endpoint, model and limits from the environment registry.
