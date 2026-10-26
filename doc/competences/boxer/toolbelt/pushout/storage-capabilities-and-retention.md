---
name: "Storage capabilities, retention and batch verbs"
level: 3
parent_ids:
  - "[[pushout/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0220
  - adr:0221
  - pkg:public/storage/recordstore/pushoutstore
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0220, adr:0221, pkg:public/storage/recordstore/pushoutstore]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0220, adr:0221, pkg:public/storage/recordstore/pushoutstore]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0220, adr:0221, pkg:public/storage/recordstore/pushoutstore]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Who may forget what is a storage capability; a retention rule is a standing patch; a batch verb applies many patches in one pass.
