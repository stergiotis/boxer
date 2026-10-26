---
name: Sync and markdown
abbrev: SM
synopsis: guided table sync and a markdown vault as facts
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - adr:0259
  - adr:0271
  - adr:0218
  - adr:0180
  - adr:0217
  - pkg:public/db/clickhouse/jackstay
  - pkg:public/semistructured/markdown
  - app:jackstay
  - scene:jackstay
  - howto:markdown-facts-obsidian-queries
  - skill:obsidian-markdown
assessment:
  - dimension: readiness
    rating: 6
    basis: [adr:0218, howto:markdown-facts-obsidian-queries, scene:jackstay]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [adr:0218, howto:markdown-facts-obsidian-queries, scene:jackstay]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [adr:0218, howto:markdown-facts-obsidian-queries, scene:jackstay]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Moving data between two ClickHouse servers under a plan, and reading a markdown vault — its links, tags and properties — into facts a query can walk. The customer is someone with a second server to keep in step, or an Obsidian vault to ask questions of.

Its competences: [[guided-sync]], [[markdown-ingestion]], [[markdown-to-play]].

# Mechanisms

jackstay plans and runs a sync with row filters and packs; the markdown ingestor shreds documents into facts under a vocabulary; the Obsidian renderer draws them back; a document becomes a play window.

# Evidence

The markdown-facts how-to, the Obsidian skill, and the jackstay scene.

# Boundaries

Not replication: a sync is a planned copy a person runs.
