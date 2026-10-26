---
name: Snapshot lenses
level: 3
parent_ids:
  - "[[lading/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0200
  - applet:lad-browse
  - applet:lad-diff
  - applet:lad-find
  - applet:lad-grep
  - applet:lad-history
  - applet:lad-problems
  - app:tally
  - scene:tally
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0200, applet:lad-diff, scene:tally]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0200, applet:lad-diff, scene:tally]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0200, applet:lad-diff, scene:tally]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Browse, diff, find, grep, history, ledger, problems and audit over snapshots, each a SQL applet a person launches by name.
