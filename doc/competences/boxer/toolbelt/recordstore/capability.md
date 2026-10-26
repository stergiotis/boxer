---
name: Record stores
abbrev: RS
synopsis: "generated stores over facts-shaped tables, with an evidence profile"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:recordstore
  - adr:0100
  - adr:0105
  - adr:0089
  - adr:0295
  - adr:0327
  - pkg:public/storage/recordstore
  - explanation:facts-bound-record-stores
  - app:adhocdemo
  - scene:02_table_adhoc
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:recordstore, adr:0100, explanation:facts-bound-record-stores]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:recordstore, adr:0100, explanation:facts-bound-record-stores]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [positioning:recordstore, adr:0100, explanation:facts-bound-record-stores]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

A typed store over a leeway table, generated from the components it holds: insert, scan, filter, and — when the store asks for it — a tamper-evident trail of what was written. The customer is someone who wants a record type persisted and queryable without hand-writing the DML and the read-back for each one.

Its competences: [[generated-stores]], [[write-observers]], [[evidence-profile]], [[audit-trail-component]].

# Mechanisms

The generator reads the kind-tagged DTOs, binds them to sections under a registry's ids, and emits the store; call identity and write observers attach to every write; the evidence profile adds a chain, a vault, marks, seals and anchors as components on the same rows.

# Evidence

The positioning page; the facts-bound-stores explanation; the store's examples and the conformance tests.

# Boundaries

One store per domain on a shared table, not one store for everything: the generator requires the components of a store to bind disjoint sections under its default ids.
