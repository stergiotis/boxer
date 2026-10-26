---
name: Markdown editing and blocks
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0178
  - adr:0334
  - pkg:public/thestack/imzero2/egui2/widgets/mdblocks
  - pkg:public/thestack/imzero2/egui2/widgets/markdown
  - app:mdedit
  - scene:blocks
  - scene:gestures
assessment:
  - dimension: readiness
    rating: 5
    basis: [adr:0334, app:mdedit, scene:blocks]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0334, app:mdedit, scene:blocks]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0334, app:mdedit, scene:blocks]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A rich-text editor over a markdown document, block by block, with the source and the rendered form kept in step.
