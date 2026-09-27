---
type: explanation
audience: prospective consumers and integrators evaluating adoption
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

> Where this page and an ADR disagree, the ADR is the record. The
> product-level statement is [positioning-statement](../positioning-statement.md);
> this page applies the same template to one subsystem. The set and its
> review checklist are in the [folder README](./README.md).

# identity — positioning

Positions the tagged identifier scheme as one product: the Fibonacci-coded
id format, its ClickHouse functions, and the authority that claims tag
values. The data model whose tags these ids carry is positioned by
[leeway](./leeway.md).

## Short form

For anyone minting identifiers that both Go and SQL must split into a kind
and a number without a lookup, identity is a tagged-id scheme: a variable-
width tag[^tag] coded so no tag is a prefix of another, a number behind it,
one split routine kept identical in Go and ClickHouse by a shared test, and
one authority that hands out tag values.

Unlike an opaque id whose kind lives in a column beside it, or a fixed-width
tag every reader must know the width of, identity puts the kind in the id
itself, so a test for one kind becomes a range the primary key can prune.
The cost is ordering across kinds that follows code bits, a slower split for
unknown tags, and a one-time breaking migration.

## Full form

**For** a vocabulary author, a SQL user or a pipeline maintainer who mints
identifiers that Go and ClickHouse must both split into a kind and a number
— in a filter, a join, a compressed column — without a dictionary or a
registry call,

**identity is** a self-delimiting tagged-id scheme: a Fibonacci-coded
tag[^tag] of adaptive width, prefix-free so tags stay unique across widths,
a number behind it, one split routine kept identical in Go and SQL by a
shared test, and one authority that hands out tag values,

**that** turns a kind test into a range the primary key can prune, keeps ids
of one kind contiguous for compression, and refuses a colliding tag value
when it is claimed rather than after it is used,

**unlike** an opaque id whose kind is stored beside it, or a fixed-width tag
whose width every reader must know in advance,

**identity** carries the kind in the id, readable anywhere without context.
The cost is ordering across kinds that follows code bits, a slower split for
unknown tags, an unsupported path under the old query analyzer, and a
breaking switch from the earlier scheme.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | vocabulary authors, SQL users, pipeline maintainers | the claim sites listed in [ADR-0183](../../adr/0183-leeway-component-consumer-simplification.md) D0; the importers of the SQL surface named in [ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) Updates; [fibonacci-tagged-ids](../../howto/fibonacci-tagged-ids.md) |
| is a | Fibonacci-coded, adaptive width, prefix-free | [ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) |
| is a | one split algorithm, golden-locked Go and SQL | [ADR-0106 §SD5](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) |
| is a | a claiming authority with an unforgeable token | [ADR-0183](../../adr/0183-leeway-component-consumer-simplification.md) D0 ("tag values are claimed, not constructed") |
| that | tag test folds to a range; primary-key pruning | [ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) Update 2026-09-22; [clickhouse-udf-primary-key-pruning](../clickhouse-udf-primary-key-pruning.md) |
| that | contiguous runs per tag | [ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) |
| that | collisions refused at claim time | [ADR-0183](../../adr/0183-leeway-component-consumer-simplification.md) Alternatives (goldens alone detect collisions only after the fact) |
| unlike | a fixed-width tag with out-of-band width | [ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) Alternatives (the prior fixed-width scheme; needs a build-tag axis, no frequency adaptivity) |
| unlike | an opaque or hashed id | [ADR-0183](../../adr/0183-leeway-component-consumer-simplification.md) Alternatives (content-derived ids: "opaque to humans, needs collision management"); no ADR names UUIDs, and the foil is worded to rest on this rejection |
| identity | the kind in the id, decodable without context | [ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) ("no out-of-band width … in any language") |
| trade | ordering, unknown-tag split cost, analyzer limit, breaking switch | [ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md) Consequences and Updates |

## Boundary

- The leased, technology-neutral id generation interface
  ([ADR-0111](../../adr/0111-identity-technology-neutral-leased-id-generation.md))
  is proposed; its reference code exists and nothing outside the package
  consumes it. Not a clause.
- Deferred and absent from the clauses: the id-to-name dictionary, wiring
  the SQL function DDL into the leeway DDL generator, an "install
  functions" action.
- The measured split costs in [ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md)
  are that ADR's frozen evidence; this page states the direction of the
  trade only.

## Further reading

- [why-boxer](../why-boxer.md) — P5 (pruning, compression) and P6 (the
  server-truth harness) are the premises evident here.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [fibonacci-tagged-ids](../../howto/fibonacci-tagged-ids.md); [clickhouse-udf-primary-key-pruning](../clickhouse-udf-primary-key-pruning.md).
- Decisions: [ADR-0106](../../adr/0106-identity-fibonacci-tags-build-tag-retirement.md),
  [ADR-0111](../../adr/0111-identity-technology-neutral-leased-id-generation.md),
  [ADR-0183](../../adr/0183-leeway-component-consumer-simplification.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/identity/identifier

[^tag]: The tag is the part of the identifier that says what kind of thing it names; Fibonacci coding lets frequent kinds get short tags.
