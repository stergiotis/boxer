---
name: Ad-hoc datasets and bundles
level: 3
parent_ids:
  - "[[sqlapplet/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0240
  - adr:0288
  - pkg:public/keelson/runtime/adhocdata
  - pkg:public/keelson/runtime/sealed
  - app:adhocdemo
  - howto:adhoc-datasets
  - scene:02_table_adhoc
  - env:BOXER_ADHOC_DIR
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0240, howto:adhoc-datasets, scene:02_table_adhoc]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0240, howto:adhoc-datasets, scene:02_table_adhoc]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0240, howto:adhoc-datasets, scene:02_table_adhoc]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

An app publishes a table it computed to SQL without storing it, under a sealed capability the store owns; a bundle carries several together, and an applet or a play window reads them by name.
