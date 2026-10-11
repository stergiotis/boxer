---
type: adr
status: proposed
date: 2026-10-11
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0320: Journal bodies in a vault, committed to the chain under a per-entry key, erased by destroying both

## Context

The run journal ([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md))
is the ledger [ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md)
makes tamper-evident: a chain digest per entry, heads anchored outside the
cell. Deletion keeps the chain by emptying bodies and keeping their content
digests (ADR-0308 §SD8, ADR-0314 §SD5). Data-subject erasure was put out of
scope by [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md)
§SD12 on the argument that a single-person cell has no data subject but the
person, and ADR-0308 §SD8 reserved a key-id column "if it returns".

Two things make it return.

- **Tool outcomes are copies.** An outcome entry holds what a query
  returned, and a query result carries whatever the data store holds about
  third parties. Erasure at the source does not reach the copy, so the
  "no data subject but the person" argument does not hold for outcomes,
  and it never held for a reply the model composed over one.
- **The kept digest is a fingerprint.** `BodyDigest` is BLAKE3 under a key
  derived from a public context string, so it is deterministic over the
  body. For a short body — a name, an address — the retained digest is
  brute-forceable. [ADR-0025](./0025-pushout-forget-architecture.md)'s legal
  context quotes WP216 on exactly this shape: a keyed hash whose key is not
  destroyed is pseudonymisation, not anonymisation; and the EDPB's
  blockchain guidance asks that an on-chain value be renderable anonymous by
  erasing off-chain data.

Prior art in the tree: ADR-0025 for the pushout VCS, and, outside it, the
dmdm vault, where personal values never enter the patch and live as salted
commitment tokens in a vault table, erased by a synchronous mutation with a
tombstone and a receipt. The journal has the same two halves already,
ledger rows and bodies; what it lacks is the separation and the secret.

## Design space (QOC)

**Question.** How does a chained, anchored record erase the content of an
entry so that nothing retained reveals it, while the chain, the heads and
the entry's metadata stay and the erasure is itself on record?

**Options.**

- **O1 — Blank the body, keep the deterministic digest** (today).
- **O2 — Encrypt bodies under a per-run key, destroy the key to erase.**
- **O3 — A commitment per entry under a per-entry random opening, bodies and
  openings in a vault table, erasure destroys both.** HMAC-style: the
  chain commits to `H_k(body)` with `k` derived from the opening.
- **O4 — Pedersen commitments**, perfectly hiding, proof-capable.
- **O5 — Keep bodies out of the record entirely**: the ledger holds only
  digests, bodies live only in process.

**Criteria.**

- **C1 — Erased means unrecoverable** from everything retained, for
  low-entropy bodies too.
- **C2 — The chain and the anchored heads keep verifying** after erasure
  (laws L31, L32), and the erasure is in the chain.
- **C3 — Per-entry erasure**, not only per run: one outcome, not the
  conversation.
- **C4 — No new dependency**, no key service, nothing the appliance
  deployment lacks.
- **C5 — Resume and the projection still read bodies** at the cost of one
  join.
- **C6 — Content identity for endorsement and dedup** stays available
  while the body exists.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | +  | ++ | ++ | ++ |
| C2 | ++ | +  | ++ | ++ | −  |
| C3 | +  | −− | ++ | ++ | −  |
| C4 | ++ | −  | ++ | −− | ++ |
| C5 | ++ | +  | +  | +  | −− |
| C6 | ++ | +  | +  | +  | −  |

O2 erases a run, not an entry, and needs a key held somewhere the record
is not. O4 is O3 with curve arithmetic and a dependency, bought for proofs
no law asks for; it stays the upgrade path, since the chain sees only the
commitment either way. O5 gives up resume. O3 is taken.

## Decision

We will keep **bodies, tool calls and attachments out of the ledger**, in
a **vault** table of their own, each row holding a **per-entry random
opening**; the ledger entry carries a **commitment** per field, keyed from
the opening and a field-scoped context; the chain commits to the
commitments; **erasure destroys the vault row**, so the retained
commitment reveals nothing; and every erasure is an entry in the chain.

### SD1 — The vault holds what could be personal

The entry's `Body` and `Calls` are the fields that copy what a person
typed, what a model wrote or what a query returned. They live in a third
store-owned table, `boxer.agentrunbody`, one row per entry that carries
either, keyed like the entry table (run, ordinal). The ledger entry table
keeps everything else: kind, key, role, name, book, form, targets, effect,
turn, labels, writer refs, chain fields, and `Attachments`, which are
references (a media type and the digest the artefact store keeps the bytes
under), not content. The ledger's body columns are written empty and
dropped in a later schema change, recorded as a deferral so the drop does
not land on a schema another change is moving.

Metadata that names a person, the deleter's name and an approver's name,
is not in the vault; it is the record of who acted, which the chain exists
to keep.

### SD2 — A commitment per field under a per-entry opening

For an entry with content the writer draws a 32-byte random **opening**
and derives one key per field by BLAKE3 `derive_key` over a context
string naming the scheme and the field, with the opening as material. The
field's **commitment** is keyed BLAKE3-256 of the field's bytes under that
key, hex. An empty field has an empty commitment. The ledger's
`BodyDigest` and `CallsDigest` hold the commitments; the `KeyId` column
names the scheme (`commit/blake3-keyed/v1`) on entries that carry one. With a random 256-bit key the keyed hash is a
pseudorandom function, so the commitment is hiding without the opening;
it is binding by collision resistance, so a body that opens to it is the
body that was written.

The opening travels with the entry on the bus and lives only in the vault
row. It never enters the canonical form, a ledger column, an
introspection table or an anchor.

### SD3 — The chain commits to commitments

The canonical form of ADR-0314 §SD1 keeps its field list, the two
digests now being commitments; the digest name becomes
`keyed-blake3-256/v1/agentrun-chain/v2`, since a v1 verifier would read
the commitments as content digests and fail. Law L31 checks each
field against its commitment when the body is present (the opening opens
it) and accepts an empty field whose commitment is set only when the
chain says the field was erased: the run's deleted entry, or an `erased`
entry naming the ordinal. The anchored heads are unchanged in kind.

### SD4 — Erasure is a vault deletion and a chain entry

- **A run's deletion** (ADR-0308 §SD8) appends the `deleted` entry, then
  deletes the run's vault rows, keeping the head entries' rows, whose
  bodies are digests the anchor law reads, then marks the run row.
- **An entry's erasure** appends one `erased` entry per ordinal, naming who
  in its name column and the ordinal as its target, then deletes those
  vault rows. It works on a closed run; it refuses a head entry.
- The deletion is an `ALTER TABLE … DELETE` with `mutations_sync = 2`: the
  part is rewritten before the call returns, so the bytes are gone from
  the table, not masked. Backups and replicas are the operator's
  (ADR-0025's R5 stands).
- The in-memory store erases by dropping the row.

### SD5 — Reads join and verify

A read of a run's entries joins the ledger rows with the vault rows by
ordinal and opens each commitment; a vault row that does not open is
ignored and logged, and the entry reads as erased, which L31 then reports
unless an erasure entry names it. The projection, resume and the
introspection tables read through this join and see the entry as before.
`agent_history` (ADR-0313) keeps passing the commitments: they are hiding,
so passing them is now safe for erased content too.

### SD6 — Content identity stays a content digest

Endorsements (ADR-0316) and copy records (ADR-0317) name content by the
deterministic content digest, and dedup by it. That digest lives in entry
bodies, in the vault, erased with them; it is never a ledger column and
never anchored. The approval request digest (ADR-0312 §SD3) is over
provider wire bytes and unaffected.

### SD7 — Audit events, deferred

Reading, writing, erasing and sending a body to another cell are events
an organisation may have to account for. The vault is reached through
one seam (`BodyStoreI`), so an event per call has one place to hook. The
erasure entries are the in-cell record today; receipts across cells and
a read log are deferred until a peer exists to send to.

### SD8 — Deferred, recorded

- Dropping the ledger's empty body columns, after the revision role of
  plan002 A1 lands on the same table; moving `Attachments` to the vault
  if it ever carries more than references.
- A classification pass over the closed shapes that will become
  components (plan002 B2), deciding per attribute whether it is vault or
  ledger.
- Pedersen commitments and proofs over erased bodies.
- Rotation or re-keying: openings are per entry and never rotate.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| journal tables (ADR-0308 §SD1) | a third table, `agentrunbody`; ledger body columns written empty | the generated stores, `hostboot` provisioning, the membership golden |
| entry (ADR-0308 §SD2) | an opening on the wire, `KeyId` names the scheme; a new `erased` kind | the kinds golden, the wire types |
| canonical form (ADR-0314 §SD1) | commitments in place of content digests; digest name v2 | every chain digest; test journals re-recorded |
| store seam | `BodyStoreI`, a vaulted store over the ledger store, `EraserI`; `runtime.agentrun.erase` on the bus | ADR-0026 §SD3's subject list, a dated Update |
| threat model (ADR-0307 §SD12) | erasure in scope for bodies | a dated Update |

## Alternatives

O1, O2, O4 and O5 are weighed in the QOC. Two narrower choices:

- **A per-run opening** instead of per entry. Rejected: one opening for a
  run makes every entry's commitment fall together when one entry is
  erased, which is O2's granularity.
- **Partitioning one table** instead of a second table. Rejected: the
  ledger must never see a heavyweight mutation once it carries the
  compare-and-set of ADR-0319 §SD7, and erasure is one; two tables keep
  that rule mechanical.

## Consequences

### Positive

- An erased body is unrecoverable from the record, and the chain, the
  heads and the entry's place in the run are untouched.
- Per-entry erasure exists, for the outcome that copied what it should not
  have, without deleting the conversation.
- The commitments can be shown to anyone: `agent_history` and a regulator's
  check see nothing of the content.

### Negative

- One join per read and one more flush per append.
- Thirty-two random bytes per entry with content, stored beside it.
- Two tables that can disagree: a vault row without its ledger row is
  dead weight; a ledger row without its vault row reads as erased and is
  reported by L31 unless the chain explains it.

### Neutral

- The chain digest of an entry is no longer a function of its content
  alone but of content and opening; two identical bodies have different
  commitments, which is the point.

## Migration — Tier 1

- **Breaks.** The form version: journals written under v1 do not verify
  under v2. None exist outside tests and scratch servers; the goldens
  are re-recorded once.
- **Path.** Provision the vault table beside the others; wrap the ledger
  store in the vaulted store at boot.
- **Old shape.** The ledger's body columns stay, empty, until SD8's drop.

## Verification plan — Tier 1

- **Lane.** Default: a commitment opens under its opening and under no
  other; an erased entry fails L31 until an erasure entry names it and
  passes after; a run's deletion keeps head bodies; the vaulted store
  round-trips bodies over the in-memory ledger and reads an entry whose
  vault row does not open as erased. Integration: the vault store under
  `clickhouse-local`, with the deletion rewriting the part.
- **What would fail.** A body column in the ledger with content; an
  opening in a canonical form, an introspection table or an anchor; a
  commitment that opens to two bodies.
- **Gap.** Backups and replicas outside the table.

## Status

Proposed 2026-10-11.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0025](./0025-pushout-forget-architecture.md) — the legal context and the vault shape.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD8 — deletion as a tombstone, the reserved key column.
- [ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md) — the chain the commitments enter.
- [ADR-0313](./0313-agent-schema-generated-relations-an-agent-may-read-in-full.md) — the content-free relation that passes the commitments.
- [ADR-0319](./0319-tagged-id-streams-leased-through-one-row-on-the-one-clickhouse-server.md) §SD7 — why the ledger must not see a mutation.
