---
name: Record tables
level: 3
parent_ids:
  - "[[widgets/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0151
  - adr:0336
  - pkg:public/thestack/imzero2/egui2/widgets/recordtable
  - howto:recordtable-grids
  - howto:table-column-widths
  - scene:01_table_leeway
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0151, howto:recordtable-grids, scene:01_table_leeway]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0151, howto:recordtable-grids, scene:01_table_leeway]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [adr:0151, howto:recordtable-grids, scene:01_table_leeway]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An Arrow record as a grid: sortable headers, full-cell row selection, widths that persist, and row grouping over a permutation.
