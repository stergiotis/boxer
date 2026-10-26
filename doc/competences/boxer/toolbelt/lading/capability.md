---
name: File-tree snapshots
abbrev: Ld
synopsis: "a file tree in ClickHouse, diffed and browsed"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:lading
  - adr:0198
  - adr:0200
  - adr:0222
  - pkg:public/fs/lading
  - howto:lading-snapshot-store
  - applet:lad-browse
  - applet:lad-diff
  - trial:fs-snapshot-store-m0
  - explanation:rclone-architecture-lessons
assessment:
  - dimension: readiness
    rating: 8
    basis: [positioning:lading, howto:lading-snapshot-store, trial:fs-snapshot-store-m0]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:lading, howto:lading-snapshot-store, trial:fs-snapshot-store-m0]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [positioning:lading, howto:lading-snapshot-store, trial:fs-snapshot-store-m0]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Snapshotting a file tree into ClickHouse — paths, sizes, hashes, content where asked — and querying it: what changed between two snapshots, where the space goes, which file holds a string. The customer is someone who keeps data as files and wants to ask about them in SQL.

Its competences: [[snapshot-store]], [[snapshot-lenses]], [[composition-surface]].

# Mechanisms

The snapshot store writes a tree as rows; the tally browser and the applet book read them; the composition surface mounts a snapshot beside live data; rclone mounts bring remote trees in.

# Evidence

The positioning page, the snapshot-store how-to, the lading applet book, and the fs-snapshot-store trial.

# Boundaries

Not a file system and not a backup tool: a snapshot is a record of what was there, not a copy a person restores from.
