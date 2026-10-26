---
name: The headless driver and scenes
level: 3
parent_ids:
  - "[[imzero2/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0154
  - adr:0248
  - pkg:public/thestack/imzero2/scene
  - pkg:public/thestack/imzero2/egui2/driver
  - skill:imzero2-drive
  - howto:launch-apps-non-interactively
  - scene:01_table_leeway
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

A scene document launches an app with no compositor, reads its accessibility tree, clicks and types by widget name, asserts, and captures; the maintained tour is the regression suite for the UI.
