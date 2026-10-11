---
type: adr
status: proposed
date: 2026-10-11
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0327: Evidence as a record-store profile — a chain, a vault, marks, seals and anchors any generated store opts into, and the audit event below the runtime

## Context

The tree holds two things called an audit, with no definition in common.

The **trail** ([ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md),
[ADR-0296](./0296-audit-events-as-a-trail-component.md)) is one recorder,
fourteen components on `boxer.facts`, written by host services. It is
attribution: a row's `Origin` is the bus envelope's sender, which an app
cannot forge. It is not evidence: an administrator of the host rewrites it
and nothing shows, loss is counted by the recorder's own word, and the
identity a row was committed under reaches the write observer and the
server's query log but not the row.

The **journal** ([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md),
[ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md),
[ADR-0320](./0320-journal-bodies-in-a-vault-committed-to-the-chain-under-a-per-entry-key.md),
[ADR-0325](./0325-the-journals-canonical-form-is-the-canonical-wire-over-its-rows.md))
is evidence: a chain digest per entry over the row's canonical wire item,
content in a vault behind per-entry commitments, deletion and erasure as
rows that keep the digests, one writer under a lease, heads anchored in a
store the organisation holds. Every one of those laws is written against
`agentrun.Entry`. The chain is walked three times — the verifier, the SQL
store's repair and the anchor store's head check — and the three do not
check the same things. The commitment's key-derivation context names the
package. The vault join, the tombstone, the erasure, the head entry and the
shredded attributes are each a hand-written mapping over one table's
generated builders.

Three facts make the split cost more than it did:

- The journal's row is now a facts-shaped leeway row whose canonical wire
  item is the chain preimage (ADR-0325 §SD2). plan002 recorded that the
  trail's chain "becomes a second call of the same generator rather than
  a second form". The generator cannot make that call: its canonical wire
  option is refused on a store that binds shared read-access scaffolding,
  which every facts-bound store does, and it encodes only rows not yet
  stored.
- `AuditEvent` is the component a downstream domain adopts, and it is the
  one trail row whose `Origin` is a claim (ADR-0296 §SD1). The strong
  machinery — chain, vault, anchors — is the journal's alone, so a domain
  that wants evidence writes it again.
- A downstream cell daemon links no `keelson/runtime` package but
  `buscodec`, by its own architecture decision. The trail, the audit
  event, the anchor store and the writer lease all live under
  `keelson/runtime`, so that process cannot use any of them, and has built
  a vault, per-slot commitments, an erasure and a receipt ledger of its
  own over a store this tree's generator emitted.

The laws are not domain laws. A chain needs a scope, an ordinal and a
canonical item; a commitment needs an opening and an item; a mark needs to
say what an empty field means; an anchor needs a cell, a scope, an ordinal
and a digest. What the journal adds — kinds, labels, turns, the projection
— is what a domain adds.

## Design space (QOC)

**Question.** Where do the evidence laws live, so that the journal, the
trail and a downstream domain are one chain, one vault and one verifier?

**Options.**

- **O1 — In the journal.** The status quo: `agentrun` owns the laws; the
  trail and a downstream domain copy what they need.
- **O2 — A profile of the record-store generator, and an evidence package
  below the runtime.** A generated store opts into chain, vault, marks,
  seals and the written-by component by flag; the laws are functions over
  an interface any such store satisfies; the audit event and the anchor
  contract move to packages a process without the runtime can import.
- **O3 — Everything on the trail.** One table, one recorder, every domain
  a component on `boxer.facts`; the journal moves back onto it.
- **O4 — A runtime wrapper.** A Go library that wraps any store at run
  time with chain and vault semantics, no generator change.

**Criteria.**

- **C1 — One verifier.** A regulator or an incident responder checks any
  evidence store with one law and one relation, without the domain's code.
- **C2 — Usable outside the host.** A process that links no runtime
  package writes and verifies evidence.
- **C3 — Keeps what is decided.** Leeway components, facts-shaped rows,
  the canonical wire as preimage, shredded JSON as attributes, generated
  stores (ADR-0100, ADR-0210, ADR-0325).
- **C4 — Cost to the journal.** Digests that move, code that moves,
  guarantees that weaken.
- **C5 — Multi-writer stores.** The trail's recorder and a facts lane take
  concurrent writes; a law that serialises them is not adopted.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | +  | +  |
| C2 | −− | ++ | −− | +  |
| C3 | +  | ++ | −  | −  |
| C4 | ++ | +  | −− | +  |
| C5 | +  | +  | −  | −− |

O3 was rejected twice already (ADR-0303, ADR-0308 §SD1): the facts lane's
primary key and lifecycle column are the wrong shape for a scope and an
ordinal, and the planned routing of `boxer.facts` through materialized
views commits partially across targets, which a chain cannot tolerate
(ADR-0296 §SD9). O4 keeps the preimage out of the generator, so the
canonical item is computed by a second description of the row — what
ADR-0325 removed. O2 is taken.

## Decision

We will make evidence a **profile of the record-store generator**: a
generated store opts into a **chain** over its rows, a **vault** of
per-component commitments, **marks** that say what an empty field means,
**seals** over flushes where writers are many, and a **written-by**
component that puts the committing identity in the row. The laws —
chain, marks, heads, seals — are functions in one **evidence package**
over an interface every such store satisfies, below `keelson/runtime`.
The **audit event** and the **anchor contract** move below the runtime
with them. The **journal becomes the profile's first store**, keeping its
kinds, labels and projection and handing over its chain, vault, tombstone,
erasure, lease columns and head shipping. The **trail adopts seals**, puts
the identity in the row, and gains the app-facing subject
ADR-0026 §SD3 listed and nothing built.

### SD1 — The evidence package and the scheme

Package `storage/recordstore/evidence` holds the laws and the primitives,
and imports nothing from `keelson/runtime`:

- **A scheme** names the hash constructions a store's digests outlive the
  build under: the digester (the canonical record form's keyed BLAKE3,
  ADR-0201), the chain's version, and the commitment's key-derivation
  context. A scheme's name is recorded on the row (`keyId`) and on the
  anchor, so a verifier dispatches on it; the journal's `…/agentrun-chain/v3`
  is one scheme name among others, and a downstream domain registers its
  own without touching this package.
- **Chain digest, commitment, opening**: `ChainDigest(prev, item)` over
  `prev ‖ 0 ‖ item`; `Commit(opening, item)` as keyed BLAKE3-256 under a
  key derived from the opening in the scheme's context; `Opens`;
  `NewOpening` of 32 bytes. These are the journal's functions, moved, with
  the context a parameter.
- **The row interface**: a scope, an ordinal, the predecessor's ordinal and
  digest, the canonical item with the chain fields at zero, the mark and
  its target, and the commitments and openings per vaulted component.
- **`Verify`** is law L31 over any sequence of such rows from a seed:
  every digest chains; every vaulted component present opens its
  commitment; an absent component whose commitment stands is accepted only
  when the chain explains it (SD3). **`VerifyHeads`** is law L32 over the
  rows and the anchored heads. The journal's verifier, its SQL store's
  chain walk and the anchor store's head check become calls of these two.

### SD2 — The profile in the generator

`gen.Input` gains `Evidence`, a profile with four switches, each with its
preconditions checked at generation:

| Switch | What the store gains | Requires |
| --- | --- | --- |
| `Chain` | the chain component on every row; `Append` that assigns the ordinal, computes the canonical item with the chain fields at zero, chains to the scope's head and commits; `Head(scope)`; `VerifyChain(scope, seed)`; a **chain-head row** per scope (SD9) | a u64 Key as the scope and a u64 Order as the ordinal; not `Cas` |
| `Vault` | a sibling table over the same data sections, one row per (scope, ordinal, component); the ledger's chain component carries one commitment per vaulted component and the scheme; `Destroy(scope, keep)`, `Erase(scope, ordinal, components)`; a joined read that opens every commitment and reports which did not open | `Chain`; the vaulted components named |
| `Seals` | a seal row per durable flush (SD5); `VerifySeals` | not `Chain` on the same table |
| `WrittenBy` | the written-by component on every row, filled from the call identity on `BeginCtx`'s context (SD6) | — |

The profile's components are leeway components the generator ships as
DTO sources (`evidenceChain`, `evidenceVault`, `evidenceMark`,
`evidenceWrittenBy`, `evidenceSeal`), on plain scalar and list slots so
they are readable as components on every path. Their memberships are
registered in the vocabulary the consumer's table is written against,
the way the trail's are in the runtime vocabulary; the generator takes
their ids from the same `Wrapper` as every other component, so a store
cannot bake ids its writers do not use.

Two generator changes carry the profile and stand on their own:

- **The canonical wire with shared read-access scaffolding.** `CanonWire`
  stops being refused under `SharedRA`: the encoder is emitted into the
  store's own scaffolding against the shared package's import path. Every
  facts-bound store may then compute canonical items.
- **The canonical item of a stored row.** The store gains
  `CanonicalItem(entity)`, the per-table encoder over one scanned entity.
  `DrainCanonical` keeps its meaning for rows not yet stored.
- **The shredded component.** The generator owns what plan002 B6 left to
  hand code: a `Shredded` component per vaulted JSON field — JSON Pointer
  paths and parameters on the verbatim name + parameters channel, the
  value-less sections included — written and read back by the generated
  store. The per-table `Contribute…`/`…Of` pairs go.
- **`storegen`** exposes `Revision`, `CanonWire`, `Contributions`,
  `InsertSettings` and `Evidence`, which it did not.

### SD3 — Marks: what an empty field means

A chained row carries a **mark**, a symbol with a target ordinal and an
optional component name:

| Mark | Meaning | Law |
| --- | --- | --- |
| `none` | an ordinary row | — |
| `erased` | the vaulted component named, at the target ordinal, was erased after it was written; the row names who in its written-by | an absent component with a standing commitment at the target is explained |
| `deleted` | the scope was deleted; this is its last row | every absent component with a standing commitment in the scope is explained; nothing appends after it |
| `keep` | this row survives deletion and cannot be erased | a head entry, a receipt |

The mark is a component the profile owns, so a domain's own kind enum
stays the domain's: the journal's `deleted`, `erased` and `head` kinds
keep existing for the projection and carry the mark the verifier reads.
L31's tombstone clause is restated over the mark, which is what makes one
`Verify` serve every domain, and one relation show every unexplained
absence in SQL.

A `deleted` or `erased` mark is a row in the chain, written before the
vault rows are destroyed (ADR-0320 §SD4's order), so a destruction that
fails part-way leaves a chain that says what was meant.

### SD4 — The vault, one commitment per component

A row's content lives in the vault one **component** at a time: the vault
table's envelope is the scope, the ordinal and the component's name, and
each vault row carries that component and its own 32-byte opening. The
ledger's chain component carries, per vaulted component, the commitment
over that vault row's canonical item without the opening. Erasure is per
component; a domain whose rows vault one component has the journal's
per-row behaviour as the one-component case. A shredded JSON field is one
component, so a body of attributes erases as a unit, as it does in the
journal.

The verifier opens every commitment it can and reports, per component,
opened / erased-and-explained / unexplained. A reader that joins ledger
and vault does so in SQL on the three envelope columns; the generated
store emits that read.

### SD5 — Seals for stores with many writers

A store whose rows are written by many goroutines or processes — the
trail, a facts lane — does not chain row by row, which would serialise
its writers. It **seals its flushes**: each durable flush writes one seal
row whose canonical item is the sorted list of the batch's row ids and
their row digests, chained to the **last durable seal** of the same seal
stream. A batch the server did not take has no seal; a batch dropped from
the backlog yields an `audit-gap` event that the next flush seals. Law
**L34**: every durable row of a sealed store is named by exactly one seal;
every seal chains to the previous durable seal of its stream; a row no
seal names is a gap the store reports.

Detection granularity on a sealed store is the flush, as it is the
anchoring cadence on a chained one (ADR-0314 §SD3). Seals are what the
anchor store receives for a sealed store.

### SD6 — The identity in the row

Every row of a store under the profile carries a **written-by**
component: the origin (run, app, instance), the principal, the purpose,
the correlation and the batch of the call identity on the context the row
was committed under, and two trust columns, `originBy` and `principalBy`.
`principalBy` is ADR-0296 §SD1's. `originBy` is new and says how the
origin was established:

| `originBy` | Who established the origin | Where |
| --- | --- | --- |
| `host` | the host's own service wrote the row | the trail's host verbs |
| `envelope` | the host took it from the bus envelope's sender | the `runtime.audit.{app}` subject (SD8), the journal's service |
| `self` | the writing process stated it | a process outside the host; a recorder built without the host |

The chain commits to the written-by component, so on a chained store the
identity that held the writer lease at each ordinal is under the heads.
Law **L35**: every row of an evidence store carries the identity it was
committed under, and on a chained or sealed store that identity is inside
the digest.

### SD7 — Anchors by scope

The anchor contract moves to `storage/recordstore/evidence/anchor`:
append-only `(cell, scope, ordinal, scheme, digest)` answered with a
receipt, read by `(cell, scope)`, the first receipt for an ordinal
authoritative, a different digest at an anchored ordinal refused. It is
ADR-0314 §SD2's contract with the run generalised to a scope and the
scheme recorded, so a journal run, a trail seal stream and a downstream
apply log anchor through one store. The memory store and the cadence move
with it. The bus service `runtime.anchor.*`, its client and the
introspection relation stay in `keelson/runtime/headstore`, over the
moved contract. `Ship` and the head entry become the profile's: a chained
store ships its head through the generic writer, and the head row carries
the `keep` mark.

### SD8 — The audit event below the runtime, and the subject

`AuditEvent`, its bounds, the `audit-gap` and `audit-invalid` rows and
`ForwarderI` move to `storage/recordstore/evidence/audit`. The trail's
recorder keeps `Event` and re-exports the type. A process that links no
runtime package builds a recorder over the audit package, its own
executor and a forwarder; its rows say `originBy = self`.

The subject `runtime.audit.{app}` ADR-0026 §SD3 lists as a read-only
stream becomes a **write** verb: an app sends an audit event; the host
takes `Origin` from the envelope's sender, refuses a row whose node or
origin names another app, and records it through the trail with
`originBy = envelope`. Principal and purpose remain the app's claims. This
closes ADR-0296 §SD7's deferred host wiring by a route that needs no
context handed to an app, and does not reopen the mechanism it declined.

Audit events gain **write-ahead** as an option of the recorder, the
guarantee the journal's entries have and ADR-0296 deferred: a caller that
asks is told whether the event is durable before it acts.

### SD9 — The journal on the profile

The journal keeps what is the agent run's: the kind enum and what each
kind carries, labels and their join, turns and rounds, run states, the
projection, the approval and head records, re-entry detection. It hands
the profile:

- the chain, the vault, the commitments and the openings — the ledger is
  generated with `Chain` and `Vault`, its vaulted components the prose,
  the shredded calls and arguments, and the vault's records;
- the marks — `deleted`, `erased` and `head` entries carry `deleted`,
  `erased` and `keep`; the verifier reads the mark;
- the **chain-head row** — the run row becomes the agent run's components
  on the profile's generic head row: a `Cas` + `KeyStream` table per
  chained store holding the scope's last ordinal, head digest, state and
  the writer lease (holder, pid, epoch). The agent run's own columns —
  conversation, owner, coordinator, fingerprint, profile, settings,
  label, deletion — are components on it;
- the written-by component, so the lease holder at each ordinal is in the
  chain (SD6);
- head shipping (SD7).

The canonical item changes once — the mark and the written-by component
enter it, the chain's scheme name becomes a parameter — so the digest
name moves to `…/agentrun-chain/v4` and every golden with a digest in it
is re-recorded in the same commit. No further digest move is scheduled.

### SD10 — What does not opt in

- A `Cas` table other than the chain-head row: it is rewritten in place,
  and a chain over it would be event sourcing by another name.
- A store whose rows expire by TTL or partition drop — metrics, query-run
  capture, caches: marks that keep digests grow without bound.
- A table routed through materialized views when `boxer.facts` becomes a
  Null table (ADR-0296 §SD9): an insert through views commits partially
  across targets. The generator cannot see the routing, so the ADR that
  makes the switch states, for each writer, that chained and sealed tables
  stay direct.
- Completeness: a chain says nothing about rows never written. A consumer
  that needs "no read without a record" fails closed on write-ahead; the
  profile gives it the verb, not the policy.

### SD11 — Deferred, recorded

- A durable anchor store client; the organisation's store remains the
  deployment's (ADR-0314 §SD7).
- Signatures per row under a key the process does not hold.
- The Null switch of `boxer.facts` and the retention-partitioned audit
  table (ADR-0296 §SD9).
- Audit events for vault reads through the body store seam (ADR-0320
  §SD7), which the moved audit package makes possible and a consumer
  turns on.
- A downstream domain's adoption — its scheme, its action vocabulary, its
  chained scope — is that domain's decision record.

### Milestones

- **M1 — The evidence package; the canonical wire under shared scaffolding; the canonical item of a stored row (SD1, SD2's first two generator changes).**
- **M2 — The profile: `Chain`, `Vault`, marks, `WrittenBy`, the shredded component, `storegen`'s options (SD2, SD3, SD4, SD6).**
- **M3 — The journal on the profile; digests v4; goldens re-recorded (SD9).**
- **M4 — Seals; the audit event and the forwarder below the runtime; write-ahead for events; `runtime.audit.{app}`; `originBy` (SD5, SD8).**
- **M5 — Anchors by scope; L32 generic; the verification relations served in-process (SD7).**
- **M6 — The dated Updates on the ADRs this reshapes.**

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `recordstore/gen.Input` | `Evidence` profile; `CanonWire` under `SharedRA`; `CanonicalItem`; the shredded component | every generated store's goldens; `storegen.Input` |
| `boxer.agentrunentry`, `boxer.agentrunbody`, `boxer.agentrun` DDL | the chain component replaces the entry's chain fields; the vault keyed by component; the run row over the head row | `agentrunstore` regeneration; chat's journal call sites; the preservation goldens |
| journal digests | `…/agentrun-chain/v4` | every golden with a digest; `headstore`'s L32 relation |
| runtime vocabulary | memberships for the profile's five components; the mark and `originBy` symbols | the vocabulary golden |
| `boxer.facts` trail rows | the written-by component; seal rows; `originBy` on audit events | trail views; `trailviews`; the vocabulary golden |
| capability subjects (ADR-0026 §SD3) | `runtime.audit.{app}` becomes a write verb | the subject taxonomy's dated Update; the host's handler |
| `runtime.anchor.*` | `(cell, scope, ordinal, scheme)` | the bus codec; `keelson('anchors')` |
| exported Go API | `agentrun.ChainDigest`, `Verify`, `VerifyRun`, `CommitVault`, `OpensVault`, `NewOpening`, `BodyStoreI`, `VaultedStore`, `headstore.Head/StoreI/Cadence/Ship/VerifyHeads`, `trail.AuditEvent/ForwarderI` move or take the profile's types | the package doc of each; a downstream that compiles against them |

## Alternatives

O1, O3 and O4 are weighed in the QOC. Narrower choices made on the way:

- **Marks as the domain's kinds, mapped per store.** Rejected: every
  verifier and every SQL law would carry a per-domain table of kind names,
  and a store without a kind enum would invent one for three values.
- **One commitment per row, components split by hand.** Rejected: a
  downstream domain erases one slot of a record and keeps the others; the
  per-row case is the one-component case of per-component, not the
  reverse.
- **Row-by-row chaining of the trail under one writer.** Rejected: it
  serialises every host service's writes behind one lock for an evidence
  property the flush already bounds.
- **A secret deployment key in the commitment (an HMAC).** Rejected: a
  low-entropy value under a key the deployment holds is brute-forced by
  whoever holds the key, and an erasure then cannot make it unrecoverable
  without breaking the chain. The opening is the secret, and it is
  destroyed with the content.
- **Leaving the audit event in the runtime and asking a downstream
  process to link it.** Rejected by that process's own fence, which this
  tree does not get to lift.

## Consequences

### Positive

- One `Verify`, one `VerifyHeads`, one L31 relation for the journal, the
  trail and a downstream store; a regulator's check does not read the
  domain's code.
- The identity that wrote each row is in the row and under the heads;
  lease history, which a compare-and-set row overwrote, becomes evidence.
- A downstream domain gets chain, vault, erasure, anchors and the audit
  event by generator flags over the store it already has, and retires its
  own copies.
- The trail's rows are sealed and its approval join to the journal is
  anchored at both ends.

### Negative

- One more digest move for the journal, and the goldens with it.
- The generator grows a profile: more emitted code, more preconditions,
  more generated tests.
- Three packages move below the runtime; every importer changes its path
  in the same commit.
- A sealed store's evidence is as fine as its flush; a chained store's as
  fine as its cadence. Neither is finer than before, and neither is
  completeness.

### Neutral

- The journal's behaviour does not change for the chat: the same entries,
  the same projection, the same laws, computed by the profile.
- A domain that opts into nothing is unaffected; the profile is off by
  default.

## Migration — Tier 1

- **Breaks.** The agentrun evidence symbols move; `agentrunstore`'s
  tables change shape; every journal digest and the vocabulary golden
  move; `headstore.Head` gains a scope and a scheme; `trail.AuditEvent`
  and `ForwarderI` change import path.
- **Path.** Importers follow the moved symbols; the journal's tables are
  recreated (no production journal exists; the preservation harness's
  journals are re-recorded); a process that forwards audit events
  implements the moved interface.
- **Regeneration.** `agentrunstore`, the trail store, the recordstore
  examples, the vocabulary golden, `trailviews`.
- **Old shape.** Removed outright in M3 and M4; no deprecation period,
  as nothing outside the tree compiled against the journal's evidence
  symbols.

## Verification plan — Tier 1

- **Lane.** Default: the generator's goldens for a store under each
  switch; `Verify` and `VerifyHeads` over the journal's preservation
  corpus, over a trail store with seals and over a synthetic store with
  three vaulted components; a test that changes one component and asserts
  the verifier names it and its component; a test that erases one
  component and asserts the others still open; a test that drops a
  backlog batch and asserts the next seal names the gap; a test that
  sends an audit event on `runtime.audit.{app}` naming another app and
  asserts refusal. Integration: L31, L32, L34 and L35 as relations against
  `clickhouse-local`, each returning no rows over the corpus.
- **What would fail.** A digest that does not chain; a commitment that
  opens under the wrong component's item; an absence no mark explains; a
  row no seal names; a row whose written-by is not the identity on its
  context; a head anchored under a scope the chain does not reproduce.
- **Gap.** Rewrites between anchors, flushes that never happened, and
  channels that bypass the store. Stated in SD10, not covered.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) — generated record stores, roles, the state view.
- [ADR-0201](./0201-leeway-canonical-record-form.md), [ADR-0210](./0210-leeway-canonical-wire-generator.md) — the digester and the canonical wire.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD3 — the subject taxonomy `runtime.audit.{app}` joins.
- [ADR-0277](./0277-one-audit-trail-for-model-calls-and-agent-work.md), [ADR-0295](./0295-recordstore-call-identity-observed-executors-and-write-observers.md), [ADR-0296](./0296-audit-events-as-a-trail-component.md) — the trail, the call identity, the audit event and its deferrals.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) — the readers and adversary A5.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md), [ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md), [ADR-0319](./0319-tagged-id-streams-leased-through-one-row-on-the-one-clickhouse-server.md), [ADR-0320](./0320-journal-bodies-in-a-vault-committed-to-the-chain-under-a-per-entry-key.md), [ADR-0325](./0325-the-journals-canonical-form-is-the-canonical-wire-over-its-rows.md) — the journal's laws this profile lifts.
- [plan002](../../plan002.md) — Part B, whose B5 named the second call of the generator and whose B6 the shredded component.
