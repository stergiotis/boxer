---
name: Toolbelt
abbrev: TB
synopsis: what a consumer of the toolkit gets
level: 1
parent_ids: []
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - explanation:positioning-statement
  - explanation:why-boxer
  - explanation:iso25010-assessment
  - adr:0026
  - adr:0066
  - scene:01_table_leeway
assessment:
  - dimension: readiness
    rating: 7
    basis: [explanation:positioning-statement, explanation:iso25010-assessment]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [explanation:positioning-statement, explanation:iso25010-assessment]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [explanation:positioning-statement, explanation:iso25010-assessment]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

What boxer does for someone who builds on it: a runtime for data apps over ClickHouse, a way to map records into columns and read them back, a UI stack that runs on several hosts, and the subsystems around them — durable work, ingestion, snapshots, forgetting, observability, governance. Level 2 is the positioning set: each subsystem has a customer of its own and a page that says what it rests on.

Its subsystems: [[keelson/capability]], [[leeway/capability]], [[recordstore/capability]], [[identity/capability]], [[nanopass/capability]], [[imzero2/capability]], [[widgets/capability]], [[play/capability]], [[sqlapplet/capability]], [[watchbill/capability]], [[lading/capability]], [[stevedore/capability]], [[pushout/capability]], [[observability/capability]], [[gov/capability]], [[llm/capability]], [[analytics-and-science/capability]], [[distribution-and-hosts/capability]], [[sync-and-markdown/capability]].

# Mechanisms

The premises in the why-boxer page decide the posture: own the data-centric parts, adopt the commodity ones, keep every effect queryable.

# Evidence

The positioning statement and the per-subsystem positioning pages; the ISO 25010 assessment.

# Boundaries

The cell — a person operating agents — and the practice — the maintainer's own work — are roots of their own, because their customers differ.
