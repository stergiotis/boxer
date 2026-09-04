---
type: explanation
audience: package maintainer, storage-backend author, data-protection reviewer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-04
---

# Sweep and purge durability under a data vault

What `Repo.Sweep` destroys, what "purge durability" protects, and how
much of either is left to protect once personal data lives in a vault
and patches carry only tokens. The answer decides what a storage
backend must persist: with a vault, the snapshot carries nothing a
replay cannot rebuild; without one, it carries the one fact that
cannot be replayed. Decisions live in
[ADR-0025](../adr/0025-pushout-forget-architecture.md) (the vault) and
[ADR-0079](../adr/0079-pushout-production-storage-codec-exchange.md)
(the storage seam); this page explains the mechanism they share.

## Background

A pushout replica holds the same information twice, in two shapes:

- **Envelopes** — the patches as received, content-addressed and
  immutable. A patch's identity is a digest over its dependencies and
  its changes, content included, so a change's bytes cannot be removed
  from an envelope without changing the patch's identity and orphaning
  everything downstream.
- **The materialised graph** — the state obtained by applying the
  applied set of patches. It is a *function* of that set: any replica
  that applies the same patches, in any dependency-respecting order,
  reaches the same graph.

Deleting content does not remove it from the graph. A deleted node
becomes a *tombstone*: it keeps its content so that unapplying the
deleting patch can resurrect it, and it stays in the topology so that
pseudo-edges can bridge the live text across the deleted region. A
tombstone is therefore a copy of deleted content that would otherwise
persist for the lifetime of the replica.

## What Sweep does

`Repo.Sweep(now, horizon)` calls `PushoutGraph.SweepTombstones`, which
drops the content bytes of every tombstone older than `now − horizon`,
keeps the tombstone itself, and records the node as *purged*.
`NodeContentStatus` then distinguishes three states an audit needs to
tell apart: *present*, *purged* (deliberately destroyed), and
*missing* (never recorded). The purged ids come back in deterministic
order for audit logging.

A purged node cannot be resurrected. Unapplying the patch that
deleted it fails with `ErrRetentionBlocked`; past the horizon the
deletion is permanent on this replica. That is the intended trade-off
and the reason the horizon must cover the longest unrecord workflow a
deployment wants to keep reversible.

Sweep is a *storage-limitation* mechanism in the sense of GDPR
Art 5(1)(e) and FADP Art 6(4): stop holding data once it is no longer
needed. It is not erasure in the sense of GDPR Art 17. Two copies of
the swept bytes survive every sweep, by construction:

- the envelopes on the same replica, one read away, because content
  is inside the identity digest;
- every peer that has pulled the patch.

Sweep bounds the *third* copy, the ghost in the materialised state,
and nothing else. The tombstone clock is replica-local: each replica
stamps a tombstone when it applies the delete and purges on its own
schedule, so clock skew shifts *when* a replica purges, never *what*
the converged live state is.

## What purge durability protects

The materialised graph is a function of the applied set — with one
exception. A purge is the only operation that changes the graph
without changing the applied set, and the only one that destroys
information the envelopes still hold. Every other fact in the graph
can be rebuilt by replaying the envelopes; a purge cannot, because
replaying the deleting patch runs the delete again and the tombstone
comes back with its content.

That is the failure purge durability names. A sweep is an action a
controller may record as done. If the process then crashes and
recovery rebuilds the graph from envelopes, the purge silently
un-happens: the content is back in the graph, visible to reads and
renders, and resurrectable by unrecord. The controller's record and
the replica's state now disagree.

The engine closes this with two rules on the storage seam:

1. **Sweep persists before it acknowledges.** The swept graph is
   snapshotted, together with the applied set it corresponds to,
   before `Sweep` returns. The snapshot is the only durable carrier of
   purge markers.
2. **Recovery never discards a snapshot the log covers.** Coverage is
   a *subset* test, not a prefix test: independent patches commute, so
   any snapshot whose applied set is contained in the log can be
   restored and the remaining log entries replayed on top. A prefix
   rule would throw away the snapshot written mid-unrecord and replay
   from empty, re-materialising what an earlier sweep purged.

A companion rule covers the horizon rather than the purge. A
tombstone's stamp is replica-local time that replay would reset to
replay time, so the pending horizon is mirrored to a *retention
ledger* that recovery re-seeds. Without it a replica that restarted
faster than its horizon could defer a purge indefinitely.

Purge durability is therefore a guarantee about the materialised
state on one store: once `Sweep` has returned, no restart of that
store shows the purged content again. It says nothing about the
envelopes, and nothing about other replicas.

## Under a data vault

ADR-0025 selects vault-by-design: fields that carry personal data are
written to a controller-side vault, and the patch's change content
carries a fixed-size *carrier token* — a reference into the vault plus
a keyed commitment — instead of the value. The value and the
per-occurrence nonce exist only in the vault row. Erasure is the
destruction of that row: what remains in every envelope, every
tombstone, and every peer is a commitment that no party can open.

This relocates both mechanisms:

| Concern | Without a vault | With a vault |
| --- | --- | --- |
| Where personal data lives | inside change content, hence inside patch identity | in vault rows; patches carry tokens |
| What a tombstone holds after delete | the personal data, for the horizon | a token |
| What `Sweep` destroys | the last *graph* copy of personal data; envelopes and peers keep it | tokens |
| Storage limitation (Art 5(1)(e), FADP Art 6(4)) | discharged for the graph copy only | discharged by the vault's own retention sweep over superseded rows |
| Erasure (Art 17, FADP Art 32(2)(c)) | not available | destruction of the vault row (and the key-shred backstop) |
| Compliance weight of purge durability | high: a lost purge re-exposes personal data on this replica | none: a lost purge re-exposes a token |

Under a vault, then, `Sweep` is state hygiene. It still bounds the
size of the materialised graph and still makes old deletions
permanent, and both are useful properties — but a sweep that
un-happens on restart re-exposes nothing a peer did not already hold,
and nothing that means anything without the vault. Purge durability
becomes a consistency property (a replica should not silently undo a
verb it acknowledged) rather than a compliance property.

The vault's own storage limitation follows a different mechanism
altogether: value changes mint new rows, so superseded rows are
exactly the retention sweep's targets, and their destruction is
irreversible in the vault's own store. That sweep has no snapshot, no
replay, and no purge-durability question, because a vault row that is
gone cannot be re-derived from anything.

## Consequences for a storage backend

The storage seam persists four things: envelopes, the applied log,
the retention ledger, and the snapshot. Only the snapshot is an
accelerator, and only one fact in it — the purge set — is not a
function of the other three. That yields two honest positions for a
backend that does not want to store snapshots:

- **Under a vault, drop them.** Recovery replays the log in full;
  the purge set resets to empty; nothing of compliance value is lost.
  The costs are linear recovery time and a patch cache that holds the
  whole decoded history rather than being lazy. `Sweep` will
  acknowledge a purge that the next restart undoes, so the backend
  should say so, and a deployment that wants the permanence property
  must re-sweep after recovery.
- **Without a vault, the purge set needs its own durable carrier.**
  The natural shape is an additive one beside the retention ledger:
  one record per purged node, written before `Sweep` acknowledges,
  re-applied by recovery after replay. With that carrier the snapshot
  holds nothing that cannot be rebuilt and becomes optional
  everywhere. This is a seam change and belongs in an ADR; it is
  recorded here only as the property a backend would rely on.

Either way the envelopes keep the bytes. A deployment that needs the
bytes gone from a replica's disk needs the vault; no sweep, durable or
not, reaches them.

## Invariants

- The materialised graph equals the replay of the applied set, except
  for the purge set. Everything else in a snapshot is derivable.
- A purged node stays in the topology; only its content is gone.
  Pseudo-edges and the live subgraph do not depend on tombstone
  content.
- `Sweep` writes its purge markers durably before it returns, and
  recovery uses any snapshot whose applied set the log contains.
- The pending horizon survives recovery on the same store through the
  retention ledger; a fresh clone starts its horizon at clone time.
- Under a vault, no personal data is inside patch identity, so no
  sweep, unrecord, or replay can expose or re-expose it; only the
  vault row can.

## Trade-offs

- **Permanence versus reversibility.** A purge makes the deleting
  patch permanent on the replica. The horizon is the single knob, and
  it trades ghost residue against the window in which unrecord still
  works.
- **Replica-local time.** Retention runs on each replica's own clock,
  which keeps sync clock-free at the cost of replicas purging at
  different moments. The observable divergence is that one replica
  refuses an unrecord another still accepts.
- **The snapshot as the purge carrier.** Making the accelerator carry
  a non-derivable fact couples an optimisation to a correctness rule.
  It is the smallest change that made purges durable; its price is
  that a backend cannot treat snapshots as optional until the purge
  set has another carrier.

## Further reading

- Decisions: [ADR-0025: Right-to-Erasure Architecture for the Pushout VCS](../adr/0025-pushout-forget-architecture.md) (Architecture A, SD5 vault, SD8 sweep); [ADR-0079: pushout production storage, codec, exchange](../adr/0079-pushout-production-storage-codec-exchange.md) (Q5 purge durability, the retention ledger).
- The erasure families this sits in: [erasure-design-space.md](./erasure-design-space.md).
- Sync and the clock: [pushout-distributed-operation.md §2](./pushout-distributed-operation.md#2-why-there-is-no-clock-problem).
- Reference: https://pkg.go.dev/github.com/stergiotis/boxer/public/algebraicarch/pushout/repo
