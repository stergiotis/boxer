---
type: adr
status: accepted
date: 2026-10-11
reviewed-by: "p@stergiotis"
reviewed-date: 2026-10-11
---

# ADR-0319: Tagged id streams for runtime tables, leased in blocks through one row on the one ClickHouse server

## Context

Rows in the runtime's tables have no identity that another table can point
at. `boxer.facts` mints its `id` from a per-process counter that starts at
zero and is never seeded from the table, so two processes write the same
ids. A run's id is the string the chat window chose for the conversation; a
watchbill job's id is a string; a journal entry is identified by the pair of
its run and its ordinal. The facts schema carries a `foreignKey` section for
cross-fact references and no DTO uses it, because there is nothing of a
fixed width to put in it.

The identity machinery for this exists and is used only for vocabulary
memberships. [ADR-0106](./0106-identity-fibonacci-tags-build-tag-retirement.md)
gives a `TaggedId`: a fibonacci-coded tag in the high bits, a dense body
below, prefix-free across tags, self-delimiting, with a body of at least
seventeen bits for the widest tag and sixty-two for the narrowest. `tagmint`
claims a tag value once per vocabulary and refuses a width class that cannot
hold the declared cardinality. `identgen` has a per-tag sequential generator
and an `AllocatorI` that leases blocks of body values, with the contract that
blocks never overlap or recycle, even across a crash. The record-store
generator's `Roles.Order` already names "a consumer's sequential EntityId" as
a valid u64 order column, and the `LW_ID_HAS_TAG` function prunes a primary
key by tag, so a table ordered by id reads one stream as a range.

What is missing is a decision on which tables get a stream, where the
bodies come from when more than one process writes, and what the deployment
guarantees so that the answer needs no new service. The journal's plan
(plan002, Part A) needs the same primitive for a second reason: one writer
per run ([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md)
§SD9) is enforced in memory today, so two instances can both open a run.

## Design space (QOC)

**Question.** Where does a runtime table get a row id that is unique across
every process writing to the same server, compact enough to compress and to
sit in a `foreignKey`, and cheap enough to mint per row?

**Options.**

- **O1 — Per-process counters**, as facts does today, seeded from
  `max(id)` at start.
- **O2 — Random 64-bit ids** per row.
- **O3 — Content digests** as identity, truncated to 64 bits where a fixed
  width is needed.
- **O4 — Tagged streams, blocks leased through one row on the server**:
  one tag per table, bodies leased in blocks by a compare-and-set on a lease
  row ([ADR-0223](./0223-watchbill-durable-work-on-facts.md)
  §SD3's `rowcas`), minted locally within a block.
- **O5 — A minter service** outside ClickHouse, as the lease authority.

**Criteria.**

- **C1 — Unique across processes** on one server, including after a crash.
- **C2 — Compact**: delta-encodable, fits a u64 `foreignKey`, prunes by
  table under `ORDER BY id`.
- **C3 — No new daemon** and no dependency the appliance deployment lacks.
- **C4 — Cost per id** on the write path.
- **C5 — Self-describing**: the id says which table it belongs to.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | +  | +  | ++ | ++ |
| C2 | ++ | −− | −  | ++ | ++ |
| C3 | ++ | ++ | ++ | ++ | −− |
| C4 | ++ | ++ | −  | ++ | +  |
| C5 | −  | −− | −− | ++ | ++ |

O1 cannot be made unique without a read-modify-write on the table, which is
O4 without the tag. O2 and O3 defeat compression and say nothing about
which table they belong to. O5 is O4 with a process to run and a port to
secure. O4 is taken, under one deployment guarantee stated in SD1.

## Decision

We will give each runtime table that other rows point at **one tagged id
stream**: a tag value claimed in `tagmint`, bodies leased in blocks through
a compare-and-set on **one lease row per tag** on the **one ClickHouse
server a deployment runs**, and minted from the block locally. The ordinal
of a journal entry stays the order within a run; the id is identity, never
order across processes.

### SD1 — One coordination server per deployment

A deployment runs **one ClickHouse server**, not a replicated cluster. This
is a guarantee the operator gives, and it is what makes `rowcas` a
linearizable compare-and-set (its package doc lists "one server" among its
conditions). Every claim in this ADR and in the journal's writer lease (SD7)
rests on it. A replicated deployment is out of scope and recorded as the
extension point in SD8; nothing here is to be read as working there.

### SD2 — One tag per table stream

Each table that is referenced, or whose rows must be told apart across
processes, claims one tag value in `tagmint` beside the vocabulary's claim:
the run table and the entry table of the journal first; `boxer.facts` and
the watchbill job table when their counters move (SD8). The width class
follows the expected cardinality through the `fibonacci` planning API, and
a value whose code carries leading zeros is preferred, since the high bits
then compress. The claim is a decision: a tag value changed later changes
every id minted under it.

### SD3 — The lease row and the allocator

A table `boxer.idlease`, one row per tag value, carrying the next unleased
body. An `identgen.AllocatorI` implementation over the record store
reserves a block by one `rowcas` update guarded on the row's current value
and reads the row back; the read-back is the answer, and a lost reply burns
the block, as the allocator contract requires. The table carries the
`rowcas` settings and never sees a heavyweight mutation. The sequential
generator over this allocator is what a store mints from; interning by
natural key is per instance, as `identgen/leased` documents.

### SD4 — Block size is the deployment's knob

A block is leased per process and per tag. Its size trades round trips for
gaps: a workstation leases small blocks, an appliance may lease one block
per process lifetime and never return to the server. Gaps are never
recycled and do not matter: delta encoding needs compact ids, not
contiguous ones. The tag's class is chosen with the gaps counted.

### SD5 — What an id is and is not

An id identifies a row. It is **not** an order: within a block ids
increase, across processes they interleave, and the journal's order is the
ordinal the single writer assigns, bound as the store's Order role. Body
zero is invalid in every stream. Exhaustion is per tag and surfaces as
`identgen.ErrIdSpaceExhausted`; a stream that could exhaust is a class
chosen too narrow, which the claim's declared cardinality is there to
prevent.

### SD6 — The in-memory lane mints under its own tag

Without a persist executor the journal runs in memory and there is no
lease row. Ids there come from a process counter under a **distinct tag
value**, so an id minted in memory can never be mistaken for a persisted
one, and a persisted row never references it. Anchoring
([ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md))
of a run with in-memory ids stays what it is today: local.

### SD7 — The writer lease on the run row

The same primitive fences the journal. Opening a run sets the run row's
writer to the opening instance and bumps its version by one `rowcas`
update; a second instance loses the update and is told the run is open
elsewhere. Each append batch goes claim, insert, confirm: the version is
bumped before the entries are inserted and the head recorded after.
Readers follow the chain from the run row's head rather than adopting
every row they find for the run, so a stale writer's rows, which do not
chain from the head, are invisible. A writer registers presence the way
watchbill workers do (`watchbillpresence`), and a run whose holder is dead
may be taken over; one whose holder is alive may not. This closes
ADR-0308 §SD9's one-writer rule across processes.

### SD8 — Deferred, recorded

- **Replicated ClickHouse.** The lease row and the run row would move to a
  `KeeperMap` table, whose updates are linearizable through the cluster's
  Keeper. Not designed here.
- **`boxer.facts`'s counter and watchbill's job ids** onto streams of their
  own; today's counter is a defect on its own and is fixed, by seeding or by
  a stream, in a change to facts, not in the journal's plan.
- **Natural-key interning across instances.** An interner deduplicates
  within one instance; a key seen by two instances gets two ids. No runtime
  table needs more today.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `tagmint` claims | two new tag values, journal runs and journal entries, beside `keelsonRuntime` | the committed assignment golden ([ADR-0183](./0183-leeway-component-consumer-simplification.md) D1) |
| `identgen` | an `AllocatorI` over a record-store table | its tests against `clickhouse-local` in the integration lane |
| record-store generator ([ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md)) | a store option to mint the Key from a stream; the u64 Order role as the journal's ordinal | the generator's examples and goldens |
| journal run and entry tables ([ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD1) | run id u64 tagged as the Key; entry id u64 tagged in its own stream; ordinal as the Order | the SQL store, the fold, the agent-schema relations ([ADR-0313](./0313-agent-schema-generated-relations-an-agent-may-read-in-full.md)), the chain form ([ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md) §SD1, hashing the id as an integer) |
| facts `foreignKey` section | its first users: trail rows referencing journal entries | the trail DTOs |
| deployment contract | SD1, one server | `doc/ARCHITECTURE.md`'s operation modes |

## Alternatives

O1, O2, O3 and O5 are weighed in the QOC. Two narrower choices:

- **A tag per instance instead of per table**, so instances never
  coordinate. Rejected: tag values are claimed statically and an instance is
  not a vocabulary; SD4's large block is the same effect under one tag.
- **Make the entry's id its chain digest.** Rejected for references: a
  `foreignKey` is a u64, and a truncated digest is neither compact nor
  self-describing.

## Consequences

### Positive

- Every referenced row has a u64 identity that says which table it is from,
  unique across the processes a deployment runs, and compressible.
- The journal gains its writer fence from the same row primitive, and
  ADR-0308 §SD9 holds across processes.
- No daemon: the lease authority is a table on the server every instance
  already talks to.

### Negative

- One deployment guarantee, SD1, that a replicated cluster violates
  silently; the operator must know it.
- A round trip per block and a burnt block per lost reply.
- A tag value is forever: planning it is a decision made once per table.

### Neutral

- Ids are not timestamps and not orders; readers that want an order use the
  Order role, which the journal binds to the ordinal.

## Migration — Tier 1

- **Breaks.** Run ids change from strings to u64 and are hashed as such in
  the chain form; existing journals, which exist only in tests and
  scratch servers, are re-recorded with the form version plan002 Step B5
  bumps. Nothing persisted on an operator's host is affected before this
  ships.
- **Path.** Claim the two tags; provision `boxer.idlease` at host boot
  beside the journal tables; bind the stores to the allocator.
- **Old shape.** None kept.

## Verification plan — Tier 1

- **Lane.** Default: the allocator's monotonic, non-overlapping contract
  under concurrent callers against `clickhouse-local`, with a lost reply
  injected; the claim golden for the two tag values; a two-writer test
  on one run asserting the second open is refused and a stale writer's
  rows do not chain from the head. Integration: the journal's scratch-server
  suite with two stores on one server.
- **What would fail.** Two ids equal across processes; an id with body
  zero; a run opened twice; a repair that adopts an unchained row.
- **Gap.** A replicated server, by SD1.

## Status

Accepted 2026-10-11.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-10-11 — the lease and the claims shipped; the journal half moves to plan002

**Shipped.** The four stream claims (`vocab.IdStreams`: journal runs and
entries, and SD6's in-memory twins of each) and their committed
`idstream-claims.golden`; `identgen/leased/rowalloc`, the allocator over
`boxer.idlease`; the table's provisioning at host boot; SD1 in
`doc/ARCHITECTURE.md` §3.1. The golden is a second kind beside ADR-0183 D1's
membership golden, since a stream has a claim and no name table:
`namemint/assignments` reads both, refuses a stream tag value that equals
another claim's, checks each stream against the class the planner advises for
what it declared, and requires every `tagmint.MustClaim` site to be accounted
for by one golden or the other.

**SD3, refined: which row is the lease.** The lease row is inserted the first
time a tag is leased, so two processes can both find none and both insert.
The duplicate is not harmless: a caller that read the first row at body 1 can
later guard on 1, match the duplicate, and win a block the first row has
already handed out. The lease is therefore the tag's row at the **lowest block
position** (`_block_number`, `_block_offset`), which the `rowcas` table
settings persist and merges keep. Every guard pins that position as well as
the value. Block numbers only grow on one server, so a later duplicate is
never read or updated again. A block counts as won only when the read-back
shows this call's own holder token on the row. A caller that cannot see its
token does not hand the block out, and the block is burnt.

**Verification plan, corrected.** The default lane cannot race allocators
against `clickhouse-local`: it runs one process per statement and locks its
path, so a second concurrent process fails rather than contends. The default
lane runs interleaved allocators, a late duplicate row, and three injected
lost replies (the update ran, the update never ran, the read-back failed)
under `clickhouse-local`. The concurrent race, starting from an empty table
so the first inserts race too, is in the integration lane against a server.

**Moved to plan002, Part A.** The generator's option to mint a Key from a
stream, the journal tables' u64 run and entry ids, SD6's in-memory minting,
SD7's writer lease, and the first `foreignKey` users all land together with
plan002's generated compare-and-set on the run row. They change the same
tables, the same chain seed and the same chat call sites, and doing them
separately would move each of those twice.

## References

- [ADR-0106](./0106-identity-fibonacci-tags-build-tag-retirement.md) — the tagged id scheme, the planning API, the pruning function.
- [ADR-0223](./0223-watchbill-durable-work-on-facts.md) §SD3 — the one-row compare-and-set and its conditions.
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) — the record-store generator and its Key and Order roles.
- [ADR-0308](./0308-the-run-journal-is-the-only-source-of-model-input.md) §SD9 — one writer per run.
- [ADR-0314](./0314-tamper-evident-records-chain-digests-and-anchored-heads.md) — the chain form the id enters.
- `doc/explanation/watchbill-consistency-model.md` — what the one-row claim is and is not, in Jepsen's terms.
