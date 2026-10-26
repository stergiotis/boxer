---
name: Forgetting
abbrev: Po
synopsis: a patch model for erasing and antiquing records
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:pushout
  - adr:0079
  - adr:0209
  - adr:0025
  - adr:0220
  - adr:0221
  - pkg:public/storage/recordstore/pushoutstore
  - explanation:erasure-design-space
  - explanation:pushout-distributed-operation
  - explanation:pushout-sweep-and-purge-durability
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:pushout, explanation:erasure-design-space, explanation:pushout-distributed-operation]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: genesis
    basis: [positioning:pushout, explanation:erasure-design-space, explanation:pushout-distributed-operation]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: innovating
    basis: [positioning:pushout, explanation:erasure-design-space, explanation:pushout-distributed-operation]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Removing what must no longer be kept — a person's data, a retention window's end — as a patch over the records rather than a rewrite of the table, with storage capabilities deciding who may forget what. The customer is someone with a retention obligation and an append-only store.

Its competences: [[patch-model-and-wire]], [[forgetting-and-antiquing]], [[storage-capabilities-and-retention]].

# Mechanisms

A patch is a record of what is forgotten and how; the wire carries it in CBOR with an identity; forgetting and antiquing are the two verbs; a capability gates each and a batch verb applies many.

# Evidence

The positioning page, the erasure design-space and distributed-operation explanations, and the sweep-and-purge durability explanation.

# Boundaries

Not a GDPR product: pushout forgets what a patch names; deciding what a request obliges is the person's.
