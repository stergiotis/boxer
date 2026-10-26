---
name: Ingestion
abbrev: Sv
synopsis: a processor runs under a streaming framework and lands its items
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:stevedore
  - adr:0252
  - adr:0005
  - pkg:public/streaming/stevedore
  - pkg:public/streaming/persisted
  - howto:stevedore-processors
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:stevedore, howto:stevedore-processors]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:stevedore, howto:stevedore-processors]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: differentiating
    basis: [positioning:stevedore, howto:stevedore-processors]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Running an ingestion processor — something that turns a source's items into rows — under a streaming framework that owns routing, failure and state, and landing the rows through the record stores. The customer is someone who has a feed and wants the framework's retries and checkpoints without writing them.

Its competences: [[framework-driven-ingestion]], [[persisted-streams]].

# Mechanisms

A processor implements one interface; stevedore routes its items, handles failure and keeps state under the framework; persisted streams keep what came in.

# Evidence

The positioning page and the processors how-to.

# Boundaries

Not a connector catalog: stevedore runs the processor a consumer writes, and the source-specific reading is theirs.
