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

# recordstore — positioning

Positions the generated record store and the one table shape it lands in:
the store under the product statement's "every durable fact lands in
one queryable table shape". The data model it generates from is positioned
by [leeway](./leeway.md); the runtime that writes most of the rows by
[keelson](./keelson.md).

## Short form

For a library or app author who needs an append-only record store over
ClickHouse — an event store, a version-control backend, a metrics log — and
does not want to own a table, a writer, a reader and a cache for it,
recordstore generates all four from one description, over a table in the
facts shape[^facts] that the rest of the toolkit can read.

Unlike a hand-built table per kind of record, with its own schema, insert
path and reader that shared tooling cannot see, recordstore makes a new kind
cost a data type and a few vocabulary entries, never a schema change. The
cost is no control over indexes and retention, about twice the insert time
of a hand-built table, and a generator that inherits drift from the four it
drives.

## Full form

**For** a library or app author who needs an append-only record store over
ClickHouse — an event store, a version-control backend, a metrics log, a job
table — with records written, read back by key, scanned and joined with
every other kind,

**recordstore is** a generated store: one description yields the table, the
Arrow writer, batched key-value reads, a scan per kind and an optional
latest-state view, over a table in the facts shape[^facts],

**that** makes a new kind of record a data type plus vocabulary entries and
never a schema change, keeps append-only enforced by the schema, and checks
a store's behaviour against a conformance suite it does not own,

**unlike** a hand-built table per kind, or a flat payload column that gives
up typed queries, compression and access control,

**recordstore** binds one description to every form the record takes. The
cost is no control over indexes and retention, about twice the insert time
of a hand-built table, single-threaded stores, and a generator that inherits
drift from the four it drives.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | a library or app author needing an append-only store | [ADR-0100](../../adr/0100-recordstore-generated-leeway-clickhouse-store.md) Context (an event store and the pushout storage seam as the first consumers); the in-tree stores listed in [facts-bound-record-stores](../facts-bound-record-stores.md) |
| is a | one input composes description, plans, cache, executor | [ADR-0100](../../adr/0100-recordstore-generated-leeway-clickhouse-store.md) Decision |
| is a | facts row shape, shared table or store-owned | [ADR-0026 §SD6](../../adr/0026-app-runtime-and-capability-subjects.md); [ADR-0198 §SD2](../../adr/0198-fs-snapshot-store.md) (store-owned, same shape); [ADR-0148](../../adr/0148-app-workingsets.md) Update ("names a substrate, not a table") |
| that | a new kind is a DTO plus vocabulary, never a schema change | [ARCHITECTURE §3.3](../../ARCHITECTURE.md) |
| that | append-only enforced by the schema | [ADR-0184](../../adr/0184-sysmetrics-persistence-tee.md) |
| that | read access shared with the bus codecs | [ADR-0184](../../adr/0184-sysmetrics-persistence-tee.md); [ADR-0042](../../adr/0042-keelson-leeway-codec-soa-generator.md) |
| that | verbs validated against an external conformance suite | [ADR-0100](../../adr/0100-recordstore-generated-leeway-clickhouse-store.md) |
| unlike | a bespoke table per kind | [ADR-0198](../../adr/0198-fs-snapshot-store.md) Alternatives O1 (kept as the fallback and the measurement baseline) |
| unlike | a flat payload column | [ADR-0026](../../adr/0026-app-runtime-and-capability-subjects.md) Alternatives |
| recordstore | one bind point replaces the hand-wired seams | [ADR-0100](../../adr/0100-recordstore-generated-leeway-clickhouse-store.md) ("one bind point replaces ~10 hand-wired seams") |
| trade | no index or retention control; insert cost; single goroutine; generator drift | [ADR-0184](../../adr/0184-sysmetrics-persistence-tee.md), [ADR-0198](../../adr/0198-fs-snapshot-store.md) M0, [ADR-0100](../../adr/0100-recordstore-generated-leeway-clickhouse-store.md) Consequences |

## Boundary

- **"One shape", not "one table".** The tree holds the shared facts table,
  a state table, the snapshot store's own tables and plain catalog tables.
  The invariant names a substrate, not a table
  ([ADR-0148](../../adr/0148-app-workingsets.md) Update). The statement is
  worded accordingly.
- **The core runtime kinds are not generated.** Grant, audit, log, run,
  heartbeat, lifecycle and launch still write through the hand-rolled
  store; their generated adoption is recorded as not built
  ([ADR-0105](../../adr/0105-keelson-adopts-generated-record-stores.md)).
  Two persistence mechanisms coexist, and that ADR says so.
- Deferred in [ADR-0100](../../adr/0100-recordstore-generated-leeway-clickhouse-store.md)
  and absent from the clauses: compare-and-swap, a streaming executor,
  negative caching, projection fetch.
- The interned dimension store ([ADR-0112](../../adr/0112-dimensionstore-interned-facts-additive-memberships.md))
  is proposed and not positioned.

## Further reading

- [why-boxer](../why-boxer.md) P3 — the premise this store enacts.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [facts-bound-record-stores](../facts-bound-record-stores.md) — what a new kind must satisfy.
- Decisions: [ADR-0100](../../adr/0100-recordstore-generated-leeway-clickhouse-store.md),
  [ADR-0105](../../adr/0105-keelson-adopts-generated-record-stores.md),
  [ADR-0184](../../adr/0184-sysmetrics-persistence-tee.md),
  [ADR-0198](../../adr/0198-fs-snapshot-store.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/storage/recordstore

[^facts]: `boxer.facts`: the one ClickHouse table shape every durable record lands in, so any record can be joined with any other in SQL.
