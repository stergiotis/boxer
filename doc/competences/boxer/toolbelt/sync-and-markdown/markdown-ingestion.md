---
name: The markdown ingestor and Obsidian rendering
level: 3
parent_ids:
  - "[[sync-and-markdown/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0218
  - adr:0180
  - pkg:public/semistructured/markdown/mddocfacts
  - pkg:public/semistructured/markdown/obsidian
  - howto:markdown-facts-obsidian-queries
  - skill:obsidian-markdown
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0218, howto:markdown-facts-obsidian-queries]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0218, howto:markdown-facts-obsidian-queries]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0218, howto:markdown-facts-obsidian-queries]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A vault's documents become facts — spans, links, tags, properties — under one vocabulary, and the renderer draws wikilinks and callouts back.
