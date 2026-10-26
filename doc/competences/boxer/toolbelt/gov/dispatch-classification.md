---
name: Dispatch classification
level: 3
parent_ids:
  - "[[gov/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0107
  - pkg:public/gov/callsites
  - howto:callsite-dispatch-survey
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0107, howto:callsite-dispatch-survey]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0107, howto:callsite-dispatch-survey]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0107, howto:callsite-dispatch-survey]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Call sites are classified by how they dispatch — static, interface, reflective, external — so a review can find the ones a type checker does not see.
