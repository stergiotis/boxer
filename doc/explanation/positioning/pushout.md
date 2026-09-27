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

# pushout — positioning

Positions the patch-based version-control engine: a patch log whose
identities are content hashes, with pluggable storage, codec and transport
interfaces, a durable retention ledger, and an erasure architecture that is
designed and only partly built. The ClickHouse adapter it lands in is a
generated record store, positioned by [recordstore](./recordstore.md).

## Short form

For a team versioning records that must both merge across peers and be
forgotten on demand, pushout is a patch-based[^patch] version-control
engine: patches named by a content hash over their changes and dependencies,
independent patches that commute, conflicts kept as data, and storage,
encoding and transport behind interfaces that each carry a conformance
suite.

Unlike a snapshot version-control system, where every commit is chained to
its whole ancestry and erasure means rewriting history, pushout keeps a
patch's identity stable under cherry-pick and sync, and makes retention an
explicit, checked mode of the store with a purge that is durable once the
call returns. The cost is no convergence at the value level and envelopes
that cannot be grepped. Forgetting a subject without touching the graph is
designed and not yet built.

## Full form

**For** a team versioning records that must merge across peers without a
shared clock, and that carry personal data a controller must be able to
erase on request,

**pushout is** a patch-based[^patch] version-control engine: a patch log
with tombstones, identities that are content hashes over deterministic CBOR,
storage, encoding and transport behind interfaces with conformance suites, a
ClickHouse store beside a file store, and a retention ledger whose purges
are durable,

**that** lets independent patches commute — property-tested — so sync never
rests on a timestamp and a cherry-picked patch stays the same patch, keeps
conflicts as data rather than a merge-time heuristic, and gives a deployment
one guarantees value to read, with a retention mode the store refuses to
open below (proposed),

**unlike** a snapshot version-control system, where a commit is chained to
its full ancestry, merge is a heuristic, and erasing a record means
rewriting history and re-coordinating every peer,

**pushout** treats forgetting as an operation of the store rather than a
rewrite of the graph: sweep and purge today, subject-level forget by vault
commitments as designed. The cost is no value-level convergence, envelopes
that cannot be grepped, and no un-record past the purge horizon.

## What each clause rests on

| Slot | Clause | Rests on |
| --- | --- | --- |
| For | merge across peers without a shared clock | [pushout-distributed-operation](../pushout-distributed-operation.md) §2 |
| For | personal data a controller must erase | [ADR-0025](../../adr/0025-pushout-forget-architecture.md) Context (GDPR Art 17, FADP Art 32) |
| is a | patch log, tombstones, pseudo-edges, conflicts | [ADR-0079](../../adr/0079-pushout-production-storage-codec-exchange.md); the engine under `public/algebraicarch/pushout/repo` |
| is a | keyed content hash over deterministic CBOR | [ADR-0209](../../adr/0209-pushout-cbor-identity-and-wire.md) |
| is a | seams with conformance suites; ClickHouse and file stores | [ADR-0079](../../adr/0079-pushout-production-storage-codec-exchange.md); [ADR-0100](../../adr/0100-recordstore-generated-leeway-clickhouse-store.md) (the `pushoutstore` mapping) |
| is a | a retention ledger with durable purges | [ADR-0079](../../adr/0079-pushout-production-storage-codec-exchange.md) Updates; [pushout-sweep-and-purge-durability](../pushout-sweep-and-purge-durability.md) |
| that | independent patches commute; sync without timestamps | [pushout-distributed-operation](../pushout-distributed-operation.md) §2–3 |
| that | a cherry-picked patch stays the same patch | [pushout-distributed-operation](../pushout-distributed-operation.md) §5 |
| that | one `Guarantees` value; a retention mode checked at open — *proposed* | [ADR-0220](../../adr/0220-pushout-storage-capabilities-and-retention-mode.md) (code landed, ADR proposed) |
| unlike | a snapshot VCS chained to its ancestry, erasure by history rewrite | [ADR-0025](../../adr/0025-pushout-forget-architecture.md) Forces and Alternative E (history rewrite destroys the hash = patch invariant; no data-protection authority endorses it); [pushout-distributed-operation](../pushout-distributed-operation.md) §5 |
| pushout | sweep and purge, durable and auditable | [ADR-0025 §SD8](../../adr/0025-pushout-forget-architecture.md) (the one built sub-item); [ADR-0079](../../adr/0079-pushout-production-storage-codec-exchange.md) Q5 |
| pushout | subject-level forget by vault commitments — *designed, unbuilt* | [ADR-0025](../../adr/0025-pushout-forget-architecture.md), proposed; its Update records that no vault, nonce, commitment or forget symbol exists |
| trade | no value-level convergence, no value diffs for vaulted fields; envelopes not greppable; unrecord blocked past the horizon | [ADR-0025](../../adr/0025-pushout-forget-architecture.md) Consequences; [ADR-0209](../../adr/0209-pushout-cbor-identity-and-wire.md) Consequences; [ADR-0220](../../adr/0220-pushout-storage-capabilities-and-retention-mode.md) |

## Boundary

- **Designed, not built:** the vault, commitments and the forget verbs
  ([ADR-0025](../../adr/0025-pushout-forget-architecture.md), proposed,
  waiting on counsel); antiquing
  ([ADR-0039](../../adr/0039-pushout-antiquing.md), deferred, Decision
  empty); frontier sync, signatures, a NATS transport. The statement's
  erasure clause is qualified accordingly and should not be read as shipped.
- Storage capabilities, retention modes and the batch verbs are in the
  tree while their ADRs
  ([0220](../../adr/0220-pushout-storage-capabilities-and-retention-mode.md),
  [0221](../../adr/0221-pushout-storage-batch-verbs-delta-ledger.md)) remain
  proposed; the table marks those clauses.
- The Swiss-only erasure design
  ([ADR-0027](../../adr/0027-pushout-forget-swiss-fadp.md)) is superseded and
  kept as a legal reference.
- The pijul demo adapter is a demonstrator, not a clause.

## Further reading

- [why-boxer](../why-boxer.md) — the premises the clauses compress.
- [positioning-statement](../positioning-statement.md) — the product-level statement.
- [pushout-distributed-operation](../pushout-distributed-operation.md),
  [pushout-sweep-and-purge-durability](../pushout-sweep-and-purge-durability.md),
  [erasure-design-space](../erasure-design-space.md).
- Decisions: [ADR-0025](../../adr/0025-pushout-forget-architecture.md),
  [ADR-0039](../../adr/0039-pushout-antiquing.md),
  [ADR-0079](../../adr/0079-pushout-production-storage-codec-exchange.md),
  [ADR-0209](../../adr/0209-pushout-cbor-identity-and-wire.md),
  [ADR-0220](../../adr/0220-pushout-storage-capabilities-and-retention-mode.md),
  [ADR-0221](../../adr/0221-pushout-storage-batch-verbs-delta-ledger.md).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/algebraicarch/pushout/repo

[^patch]: A patch records a change and the patches it depends on; two patches that touch unrelated things apply in either order, which is what "commute" means here.
