---
name: The decision corpus as tables
level: 3
parent_ids:
  - "[[gov/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0092
  - adr:0122
  - pkg:public/gov/adrcorpus
  - howto:adr-overview
  - applet:adr-timeline
  - table:keelson/adr
  - table:keelson/subtask
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0092, howto:adr-overview, applet:adr-timeline]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0092, howto:adr-overview, applet:adr-timeline]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0092, howto:adr-overview, applet:adr-timeline]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every decision record, its milestones and the code that cites it, read from the tree and served as tables, so drift between what was decided and what was built is a query.
