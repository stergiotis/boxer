---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0314: Tamper-evident records — a chain digest per entry, heads anchored in an org-held store, a regulator's check that needs neither the person nor the software

## Context

The threat model ([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) §SD3, §SD10) adds an adversary the earlier drafts did not have: a
person who administers their own host and falsifies the record (A5). It asks
that the record convince four readers, among them an external regulator who
checks "without trusting the person or the software", and it says plainly
what such a regulator cannot get from a host the person administers:
completeness.

Nothing in the tree gives that. The journal of
[ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) is append-only by convention and a table engine cannot defend a
row against its administrator; the trail of
[ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) is
written ahead and host-stamped, which protects against an app, not against
the host's owner. What the tree does give is an identity for records that is
independent of serialisation — the canonical record form of
[ADR-0201](./0201-leeway-canonical-record-form.md) — and the property that a
request re-derives from the journal to a recorded digest (law L4,
[ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md) §SD3).

## Design space (QOC)

**Question.** How does a record made on a host the person administers
convince a reader who trusts neither the person nor the software?

**Options.**

- **O1 — Trust the host**: the record as written, protected by the host's
  own controls.
- **O2 — Chain digests with anchored heads**: each entry commits to its
  predecessor; heads are shipped to a store outside the person's control.
- **O3 — Replicate the whole journal** to the org-held store.
- **O4 — Sign each entry** with a key the person does not hold.

**Criteria.**

- **C1 — Detects tampering** after the fact, without the host's cooperation.
- **C2 — Keeps bodies in the cell**: the org store holds no content.
- **C3 — Lets a regulator check** with stock tools and the recorded data.
- **C4 — Cost** per entry and in infrastructure.
- **C5 — Compatible with deletion** as ADR-0308 §SD8 defines it.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | ++ | +  |
| C2 | ++ | ++ | −− | ++ |
| C3 | −− | ++ | +  | −  |
| C4 | ++ | +  | −− | −  |
| C5 | ++ | +  | −  | +  |

O4 needs a key the person's process does not hold yet uses, which is a key
service the threat model deferred, and a signature per entry says who wrote
it, not that nothing was removed. O2 is taken.

## Decision

We will make the journal **tamper-evident**, and the trail after it: every
entry carries a chain digest over its predecessor's digest and its own
canonical form; **heads** are shipped at a declared cadence to an **anchor
store** the organisation holds; deletion keeps digests (ADR-0308 §SD8); and
the regulator's check is that the anchored heads match the cell's record
and that every recorded request re-derives to its digest.

### SD1 — The chain

Each journal entry's `digest` is keyed BLAKE3 over its predecessor's
`digest` and the entry's canonical form, with the run row's own digest as
the first predecessor; `prev` names the predecessor's ordinal. The
canonical form is deterministic CBOR of an explicit field list — the
entry's identity, time, kind, key, role, name, form, targets, turn, label
components with the marks as arrays, writer metadata, and the content
digests of the body and the tool calls rather than the bodies themselves —
under the keyed hash ADR-0201's canonical form uses; the entry is not a
leeway record, so the record form itself is not what is hashed. Committing
to content digests kept in their own columns is what lets a tombstone
(SD5) empty bodies without breaking the chain, and verification requires
each body present to match its digest. The single writer computes the
digest before the append
([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)
§SD9), so the worker's input and the server's record commit to the same
prefix. Trail rows are to gain the same per row within a run, or per batch
where a run is not the unit; deferred (SD7). Law **L31**: every entry's
digest equals the digest of its predecessor's digest and its own canonical
form; the first entry chains to the run row; a tombstoned entry is accepted
only on a deleted chain.

### SD2 — Heads and the anchor store

A **`head` entry** records each shipped head: the ordinal it covers, the
digest, the anchor store's receipt (id and time), the cadence, and whether
the receipt is **durable**. The anchor store is a service the organisation
runs outside any person's control; its contract is the bus subjects
`runtime.anchor.append` and `runtime.anchor.read`, append-only `(cell, run,
ordinal, digest)` answered with a receipt, a read by `(cell, run)`, the
first receipt for an ordinal authoritative and a different digest at an
anchored ordinal refused; what it holds is served as `keelson('anchors')`.
Its implementation — a ClickHouse table on an org host, a write-once object
store — is the deployment's; the contract is boxer's. The shipped default
is a store in the host's own memory, whose receipts are not durable: a head
it receipts is journaled with the form `local`, recorded and anchoring
nothing, and the laws read past such heads. Law **L32**, in both
directions: every durable `head` entry's digest equals the chain digest at
the ordinal it covers and the anchor store holds the same digest for it;
every anchored head is reproduced by the chain and recorded by a `head`
entry.

### SD3 — Cadence

The default cadence is **one head per turn end and one at run close**,
the turn's head shipped after the `turn` entry so that it covers the turn;
a deployment may add one per sync. Detection granularity against A5 equals
the cadence: a person who administers the host can rewrite anything between
two anchors, and the record says so. The cadence is profile data
([ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md)
§SD7, the chat profile's `AnchorCadence`) and is itself recorded in the
`head` entry.

### SD4 — What the heads cover

For a regulator to re-derive a request without trusting the software, the
profile and the projection must be inside what the heads cover. The
fingerprint of ADR-0308 §SD3 is a `profile` entry in the chain and covers
the prompt book, the tool schemas and the constants; the projection's text
and an engine version enter it when the worker becomes the call path
(ADR-0309 §SD8), and until then the fold's behaviour is pinned by the
build. The regulator's check is then two queries over the cell's record
plus a read of the anchor store: the anchored heads match (L32), and every
recorded request digest equals the digest of the projection rebuilt (L4).

### SD5 — Deletion keeps the chain

Deleting a run appends a `deleted` entry and empties bodies (ADR-0308
§SD8); chain digests, content digests and metadata stay, so L31 and L32
keep holding and the deletion is itself in the chain. A regulator sees that
a run was deleted, by whom and when.

### SD6 — Completeness is not promised

An exit that bypassed boxer on a host the person administers leaves no entry
and no head. The record is tamper-evident for what it holds and silent about
what it does not; the `head` entry's cadence tells a reader the window within
which a rewrite would go unnoticed. On an org-managed host the operating
system is trusted by assumption (ADR-0307 §SD11), and the chain's job is the
narrower one of detecting a rewrite through the database.

### SD7 — Deferred, recorded

- Signatures per entry (O4), if a key service ever exists.
- An anchor store the organisation does not run (a public ledger).
- Heads over synced runs from peer cells, which arrive with their own chain.
- The chain over trail rows; a durable anchor store client (the shipped
  store is in process).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| journal entries (ADR-0308 §SD1) | `prev` and `digest` columns; the `head` and `deleted` kinds | the store's generator test |
| trail rows (ADR-0277) | a chain digest per row or batch; deferred (SD7) | the trail's components and goldens, with it |
| anchor store contract | a new bus subject family, `runtime.anchor.*` (append, read), served in process by default and by a client of the organisation's store where one exists; `keelson('anchors')` | the capability taxonomy of [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD3, a dated Update |
| canonical forms (ADR-0201) | the entry digests under the canonical form's keyed hash over its own field list; no change to the record | — |
| profile (ADR-0310) | the anchoring cadence | the profile's validation |

## Alternatives

O1, O3 and O4 are weighed in the QOC. One narrower choice:

- **A head over a row count or a `max(ordinal)`** instead of a chain.
  Rejected: a table engine cannot defend a count against its administrator;
  only a digest that commits to the whole prefix does.

## Consequences

### Positive

- A regulator checks the record with the anchored heads, a stock engine and
  the projection, and trusts neither the person nor the software.
- An incident responder can establish from which ordinal on a record is
  unaltered.
- Deletion and tamper evidence do not fight.

### Negative

- One digest per entry, computed by the writer before every append; an
  anchor store the organisation must run; without it this record is a
  promise with no reader.
- Between two anchors the record is as trustworthy as the host.

### Neutral

- Bodies never leave the cell for the sake of tamper evidence; the anchor
  store holds 32-byte digests.

## Migration — Tier 1

- **Breaks.** Nothing: the chain ships with the journal's first version;
  without an organisation's store the entries are chained and their heads
  recorded as local, anchoring nothing.
- **Path.** Provision the anchor store; set the cadence in the profile.
- **Old shape.** None kept.

## Verification plan — Tier 1

- **Lane.** Default: L31 over the preservation harness's journals and the
  writer's tests; L32 against an in-process store, both directions, with
  dropped and re-anchored heads; a test that changes one body and asserts
  L31 names it; a test that deletes a run and asserts L31 and L32 still
  hold. Integration: the SQL store's chain after deletion against
  `clickhouse-local`.
- **What would fail.** An entry whose digest does not chain; a `head` the
  store does not hold; a rewrite that no law names.
- **Gap.** Rewrites between anchors; channels that bypass boxer.

## Status

Accepted 2026-10-11.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-10-11 — §SD1, §SD5: the chain commits to commitments

Under [ADR-0320](./0320-journal-bodies-in-a-vault-committed-to-the-chain-under-a-per-entry-key.md)
the body and calls digests the canonical form carries are commitments under
a per-entry opening held only by the vault, so an erased body leaves nothing
retained that reveals it; the digest name is `…/agentrun-chain/v2`, since a
v1 verifier would read them as content digests and fail. L31 accepts an
empty field whose commitment stands only when the chain explains it: the
run's `deleted` entry at its end, or an `erased` entry naming the ordinal.
Deletion still keeps the chain and the heads; so does the erasure of one
entry.


## References

- [ADR-0201](./0201-leeway-canonical-record-form.md) — the canonical form the digest is over.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md) — the trail; write-ahead; host-stamped origin.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD3 — the subject taxonomy the anchor client joins.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md), [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md), [ADR-0309](./0309-context-projection-as-sql-over-the-journal-in-a-local-worker.md), [ADR-0310](./0310-harness-profile-and-registry-composition-declared-per-seam.md) — the threat model, the journal, L4, the cadence.
- [agent-harness](../adr-background-work/agent-harness.md) §4.10 — the analysis.
