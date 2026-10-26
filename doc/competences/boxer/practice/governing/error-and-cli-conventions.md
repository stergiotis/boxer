---
name: Error and CLI conventions
level: 3
parent_ids:
  - "[[governing/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0293
  - adr:0294
  - pkg:public/observability/eh
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0293, adr:0294, pkg:public/observability/eh]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0293, adr:0294, pkg:public/observability/eh]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0293, adr:0294, pkg:public/observability/eh]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Errors are built with fields through one package and commands are wired through one CLI framework, so a message and a flag read the same everywhere.
