---
name: SQL-defined applets
level: 3
parent_ids:
  - "[[sqlapplet/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0132
  - applet:docs-search
  - howto:sqlapplet-authoring
  - applet:docs-search
  - scene:comp-browser
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0132, howto:sqlapplet-authoring, scene:comp-browser]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0132, howto:sqlapplet-authoring, scene:comp-browser]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0132, howto:sqlapplet-authoring, scene:comp-browser]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A markdown document with a front-matter stanza and a fenced query is minted as an app: a title, a summary, keywords, tabs and knobs, launched by name and read through the same tables as everything else.
