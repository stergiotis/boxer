---
type: adr
status: proposed
date: 2026-11-22
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** The direction — every writer mints, entities are named by the natural key, stored rows stay — was chosen by the code owner on 2026-11-22. The choices under it, listed at the end, are not reviewed.

# ADR-0405: Every row of the facts table carries a minted id, and an entity is named by its natural key

## Context

The facts table has one `id` column and, until this decision, no one
meaning for it. The hand-written facts store leases its ids from a tagged
stream ([ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md)
§SD2), so each names one row. Every other writer computed its own: a
64-bit hash of a natural-key string, of a host and a metric domain, of a
document's content, of a message's position in a topic.
[Facts-bound record stores](../explanation/facts-bound-record-stores.md#what-the-id-column-holds)
lists them.

Those hashes did three different jobs under one name. For most rows the
key was unique per event, so the hash was a row id made without a round
trip. For some it was an *entity* key that many rows share — a metric
series, a worker's run — which a reader recomputes to find the rows. And
for a few it was what made a second write of the same thing recognisable:
the same content, the same message, the same query event.

Two things follow from sharing a column that way. Ids of different writers
differ only by the odds of a 64-bit hash, and at the row counts a facts
table is meant for those odds are not small: among a billion hashed ids a
collision somewhere is a few percent. And the generated stores' point reads
select by `id` alone, naming no kind, so a collision is not only two rows
with one number: it is one store reading another's row.

The envelope already has the column an entity key belongs in. Every writer
fills `naturalKey` with the string or digest its hash was taken from.

## Decision

**SD1 — `id` names one row.** Every row written to the facts table carries
an id minted from the table's stream, `factsRow`, leased as ADR-0397 §SD2
describes, or from `factsRowLocal` while no block can be leased. One
stream serves every writer: a tag is claimed per table
([ADR-0319](./0319-tagged-id-streams-leased-through-one-row-on-the-one-clickhouse-server.md)
§SD2), and the tag of an id then says what it always should have, that
the row is the facts table's. No writer derives an id from anything.

**SD2 — `naturalKey` names the entity.** What a row is about — the series,
the run, the document, the message, the query event — is its natural key,
as bytes, and stays what each writer already writes there. A reader that
wants an entity's rows, a writer that asks whether it wrote something
before, and a row that has to be recognised across ingests all go by
`naturalKey`. A natural key is unique within its writer's kinds, not
across the table, so such a read names the kind as well.

**SD3 — A store over the facts table mints its key.** The generator's
key-stream option is on for every facts-bound store: its configuration
takes the id generator and the store has `MintKey`. Whoever builds the
store hands it the process's generator for the facts table
(`chstore.RowIdGenerator`), which is the one the hand-written store mints
from, so a deployment's own allocator and stream (ADR-0397 §SD2) reach
every writer. A store built without one cannot mint and says so.

**SD4 — A reference is to a row.** The `foreignKey` section holds a `u64`,
so a row that points at another holds that row's id: the id minted for
the row it means, in the same ingest or read back. A reader that follows
a reference to "the entity as it is now" reads the row, takes its natural
key, and reads the newest row of that key.

**SD5 — Rows already stored keep their ids.** Nothing is rewritten. Their
natural keys are what new rows' are, so a read by natural key spans both;
a read that named an old id still finds the old row. A hashed id written
before this decision can still equal another id, as it could before.

**SD6 — The lease seed counts every row.** Recovering a lost lease row
(ADR-0397 §SD2) takes the largest id of the stream over every kind, since
every kind mints from it. A hashed id from before this decision that falls
into the stream's range is counted with them: the stream then continues
above it, which wastes part of the id space and repeats nothing.

### Deferred

- The lading mount policy. Its id is the mount's own tagged id, minted
  elsewhere and shared by a mount's versions; it is an entity id in the
  `id` column, and not a hash. Moving it means deciding where a mount's id
  lives in a policy row.
- An index for reads by natural key. The table is sorted by time; a read
  by natural key within a time window scans the window, as a read by id
  did.
- Stores outside this repository. A consumer's store over the facts schema
  follows when it is regenerated and handed a generator; until then it
  writes what it wrote.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| stored shape | `id` of every new facts row is a leased tagged id; `naturalKey` unchanged | readers that recomputed an id |
| record-store generator | facts-bound generation sets `KeyStream` | the seven stores, regenerated: `Keys` in the configuration, `MintKey` |
| facts store | `chstore.RowIdGenerator`, the process's id generator for the facts table | host boot and every builder of a store over the facts table |
| audit recorder | takes an id generator; without one it derives ids as before | the trail; a store elsewhere that uses the recorder is unaffected until it passes one |
| system metrics | rows carry minted ids; the replay reads a series by natural key | `sysmreplay` |
| presence, dead letters, markdown documents | minted ids; references within an ingest are to the minted rows | their writers |
| capability map, query runs | minted ids; the newest row of an entity and the capture's deduplication go by natural key | their readers; the capture's view |
| lease seed | counts every kind | — |

## Alternatives

- **Keep derived ids, in a band of their own.** A reserved high bit would
  keep a derived id from ever equalling a minted one, at almost no cost.
  Not taken: `id` would go on meaning three things, and derived ids would
  still collide with each other.
- **A directory from natural key to minted id.** One table, updated by
  compare-and-set, would let an entity keep one id across processes and
  keep reads by id. Not taken: it adds a lookup to every first write and a
  second table that is state, to preserve a meaning of `id` the natural
  key already has.
- **A stream per writer.** The tag would then name the writer. Not taken:
  ADR-0319 claims a tag per table, the kind already names the writer, and
  one stream is one lease row to recover.
- **Rewrite the stored rows.** Not taken: the trail's seals name row ids,
  so rewriting them breaks sealed history, and every reference would have
  to be rewritten with them.

## Consequences

### Positive

- An id names one row, whoever wrote it, and its tag says the row is the
  facts table's.
- A store's read by id cannot meet another store's row.
- The entity key is in the column named for it, in full, not as a 64-bit
  digest of it.

### Negative

- A writer needs a generator, and so a server or the process-local
  fallback, where it needed nothing; a row written under the fallback has
  an id unique only within its process, as the facts store's has.
- A read by natural key compares bytes where a read by id compared a
  number, and names a kind.
- Writing the same thing twice no longer restates an id. Where that was
  relied on — the capture's deduplication, the capability map's newest
  row per entity — the natural key is what is compared.
- Two eras in one table: rows from before carry hashed ids, and a reader
  that joins by id across the change sees them as other rows.
- A lost lease row on a table with a long history of hashed ids continues
  the stream above the largest of those that fell into its range.

### Neutral

- The natural keys do not change, except where one did not identify the
  entity its id digested; those are named in the migration.

## Migration — Tier 1

- **Breaks.** The value of `id` in new rows of every writer but the facts
  store; `Latest` and `Replay` of a facts-bound store by a recomputed key.
- **Path.** Regenerate a store over the facts table and hand it the
  generator. A reader that recomputed a key reads by natural key. Nothing
  is done to stored rows.
- **Regeneration.** The gen tests of the stores over the facts table.
- **Old shape.** Rows with hashed ids stay readable by natural key and by
  scan indefinitely.

## Verification plan — Tier 1

- **Lane.** Default `go test`: each writer's rows carry ids of the stream
  and distinct ones; a store without a generator refuses to mint; the
  replay finds a series by natural key over rows of both eras. The
  `integration` lane, on a server: writers of several stores and the facts
  store minting at once with no id repeated; the lease seed over rows of
  every kind.
- **What would fail.** A writer that derives an id; a read that finds an
  entity by a recomputed id; a reference that holds something other than
  a row id.
- **Gap.** No measurement of reads by natural key against reads by id.

## Choices for review

- One stream for the table, not one per writer.
- The generator is handed to a store by whoever builds it; there is no
  process-wide default a store finds on its own.
- A store without a generator fails the write rather than deriving an id.
- The audit recorder keeps deriving ids when it is given no generator, so
  a store elsewhere that uses it changes only when its owner says so.
- The lease seed counts every kind and accepts the lost id space.
- The lading mount policy is left as it is.
- No index for natural-key reads now.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md) §SD2 — the facts row stream, its lease and its recovery.
- [ADR-0319](./0319-tagged-id-streams-leased-through-one-row-on-the-one-clickhouse-server.md) — tagged id streams, a tag per table.
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) — the generated stores, and the key-stream option in its update of 2026-10-11.
- [Facts-bound record stores](../explanation/facts-bound-record-stores.md) — what each writer put in `id` before this decision.
