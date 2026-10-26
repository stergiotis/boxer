---
name: Headless scenes
level: 3
parent_ids:
  - "[[verifying/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0248
  - adr:0154
  - skill:imzero2-drive
  - scene:01_table_leeway
  - scene:02_table_adhoc
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0248, skill:imzero2-drive, scene:01_table_leeway]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0248, skill:imzero2-drive, scene:01_table_leeway]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0248, skill:imzero2-drive, scene:01_table_leeway]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A scene document launches an app headless, drives it by widget name, asserts on the tree and captures; the tour under play is the maintained set.
