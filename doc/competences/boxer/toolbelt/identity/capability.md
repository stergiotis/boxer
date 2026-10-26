---
name: Identifiers
abbrev: Id
synopsis: "tagged ids, leased generation, minted tag spaces"
level: 2
parent_ids:
  - "[[toolbelt/capability]]"
domain: boxer-toolbelt
catalog: boxer
realised_by:
  - positioning:identity
  - adr:0106
  - adr:0111
  - adr:0319
  - adr:0183
  - pkg:public/identity
  - howto:fibonacci-tagged-ids
  - howto:tagmint-id-spaces
  - app:fibscope
  - scene:02_table_taggedid
assessment:
  - dimension: readiness
    rating: 7
    basis: [positioning:identity, howto:fibonacci-tagged-ids, app:fibscope]
    by: claude-fable
    at: 2026-10-26
  - dimension: evolution
    rating: custom
    basis: [positioning:identity, howto:fibonacci-tagged-ids, app:fibscope]
    by: claude-fable
    at: 2026-10-26
  - dimension: importance
    rating: foundational
    basis: [positioning:identity, howto:fibonacci-tagged-ids, app:fibscope]
    by: claude-fable
    at: 2026-10-26
tags: [needs-review]
---

# Scope

Ids that say what they are: a fibonacci-tagged id carries its tag and its body in one integer, a tag is claimed from a minted space, and a generator leases ranges so two processes never mint the same one. The customer is someone who needs identifiers that survive a merge of two systems' data.

Its competences: [[fibonacci-tagged-ids]], [[leased-id-generation]], [[tag-minting]].

# Mechanisms

Fibonacci coding of the tag width; a registry of claimed tag values checked across the repository; leased generation through one row the generators contend on.

# Evidence

The positioning page and the two how-tos.

# Boundaries

Not an identity provider: nothing here authenticates a person.
