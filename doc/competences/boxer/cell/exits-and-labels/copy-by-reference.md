---
name: Copy by reference through one choke point
level: 3
parent_ids:
  - "[[exits-and-labels/capability]]"
domain: boxer-cell
catalog: boxer
realised_by:
  - adr:0317
  - pkg:public/keelson/runtime/clipboardbroker
  - env:BOXER_CELL_CLIPBOARD_CLEARANCE
assessment:
  - dimension: readiness
    rating: 3
    basis: [adr:0317, pkg:public/keelson/runtime/clipboardbroker, env:BOXER_CELL_CLIPBOARD_CLEARANCE]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [adr:0317, pkg:public/keelson/runtime/clipboardbroker, env:BOXER_CELL_CLIPBOARD_CLEARANCE]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [adr:0317, pkg:public/keelson/runtime/clipboardbroker, env:BOXER_CELL_CLIPBOARD_CLEARANCE]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A copy to the clipboard or to another app is a reference the broker resolves, so the content crosses one point where the label is checked.
