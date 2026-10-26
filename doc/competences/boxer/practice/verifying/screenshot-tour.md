---
name: The screenshot tour
level: 3
parent_ids:
  - "[[verifying/capability]]"
domain: boxer-practice
catalog: boxer
realised_by:
  - adr:0057
  - pkg:public/thestack/imzero2/egui2/demo
  - env:IMZERO2_SCREENSHOT_DIR
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0057, pkg:public/thestack/imzero2/egui2/demo, env:IMZERO2_SCREENSHOT_DIR]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0057, pkg:public/thestack/imzero2/egui2/demo, env:IMZERO2_SCREENSHOT_DIR]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0057, pkg:public/thestack/imzero2/egui2/demo, env:IMZERO2_SCREENSHOT_DIR]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Every registered demo is captured as one PNG and one SVG by the demo registry's driver, so a visual regression is a diff.
