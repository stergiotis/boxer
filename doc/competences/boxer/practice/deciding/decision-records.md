---
name: Decision records and their tiers
level: 3
parent_ids:
  - "[[deciding/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0001
  - howto:adr-overview
  - table:keelson/adr
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0001, howto:adr-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0001, howto:adr-overview]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0001, howto:adr-overview]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every architectural decision is a numbered record with a status lifecycle and an edit policy by tier: proposed edits in place, accepted changes by dated update, superseded by a new record.
