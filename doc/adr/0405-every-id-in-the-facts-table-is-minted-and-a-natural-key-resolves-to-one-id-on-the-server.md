---
type: adr
status: accepted
date: 2026-11-22
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-22
---

# ADR-0405: Every id in the facts table is minted, and a natural key resolves to one id on the server

## Context

The facts table has one `id` column and, until this decision, no one
source for it. The hand-written facts store leases its ids from a tagged
stream ([ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md)
§SD2). Every other writer computed its own: a 64-bit hash of a natural-key
string, of a host and a metric domain, of a document's content, of a
message's position in a topic.
[Facts-bound record stores](../explanation/facts-bound-record-stores.md#what-the-id-column-holds)
lists them.

Those hashes did two jobs. For most rows the key was unique per event, so
the hash was a row id made without a round trip. For some it was an
*entity* id that many rows share — a metric series, a worker's run, a
message delivered twice — which a reader recomputes to find the rows, and
which makes a second write of the same thing restate the first.

The second job is a real one, and the hash does it badly. Ids of different
writers differ only by the odds of 64 bits, and at the row counts a facts
table is meant for those odds are not small: among a billion hashed ids a
collision somewhere is a few percent. The generated stores' point reads
select by `id` alone, naming no kind, so a collision is not only two rows
with one number: it is one store reading another's row.

The id generators already have the shape the second job needs.
`identifier.IdGeneratorI` resolves a natural key to an id, the same id for
the same key. What the generators in the tree lacked is a place for that
mapping that every process of a deployment shares: one keeps it in a
process, one in a file of one machine.

## Decision

**SD1 — Every id is minted.** Every row written to the facts table carries
an id of the table's stream, `factsRow`, leased as ADR-0397 §SD2
describes — but for the two writers of SD8, which keep the id they have.
One stream serves every writer: a tag is claimed per table
([ADR-0319](./0319-tagged-id-streams-leased-through-one-row-on-the-one-clickhouse-server.md)
§SD2), and the tag of an id then says the row is the facts table's. No
other writer computes an id from anything.

**SD2 — An id names an entity where the writer has one, and a row where
it has none.** A writer whose rows belong to something that recurs — the
samples of a metric series, the phases of a worker's run, a failed message
and its redelivery, a competence across loads of its corpus — resolves the
id from the natural key, so those rows
share it and a reader finds them by it. A writer whose every row is its
own thing — an audit event, the rows of one ingest of a document, the
facts store's own events — draws the next id of the stream. Which of the
two a store is depends on the generator it is handed (SD4), not on its
generated code. `naturalKey` stays what each writer already writes there.

**SD3 — The mapping from natural key to id is a table on the one server.**
The id directory sits beside the lease table and holds a row per key that
was ever resolved: the stream's tag value, a scope, the key, the id. A key
without a row is given the next leased id and inserted, and then read
back; the key's mapping is its row at the lowest block position, which is
the ordering the lease row's first insert already rests on (ADR-0319 §SD1,
and its update refining §SD3). Two
processes that insert for one key at once therefore agree after their
read, and the id that lost is never handed out. Keys live in scopes, one
per writer, because a natural key is unique within its writer and not
across the table. A new key takes a leased id and never a process-local
one: an id written into the directory is every process's.

**SD4 — A store over the facts table is handed its generator.** The
record-store generator's key-stream option is on for every facts-bound
store: its configuration takes an id generator, and the store resolves a
row's id from the row's natural key through it. Whoever builds the store
hands it the facts table's keyed generator for its scope, or the table's
sequential one; both come from the facts store's generator, so a
deployment's own allocator and stream (ADR-0397 §SD2) reach every writer.
A store built without a generator cannot write and says so. A reader is
handed a lookup, which resolves keys and gives none an id.

**SD5 — Rows already stored keep their ids.** Nothing is rewritten. A
reader of an entity whose older rows carry a hash reads by both: the id
the directory gives the key, and the hash, which it can still compute. A
hashed id written before this decision can still equal another id, as it
could before.

**SD6 — The lease seed counts every row.** Recovering a lost lease row
(ADR-0397 §SD2) takes the largest id of the stream over every kind, since
every kind mints from it. A hashed id from before this decision that falls
into the stream's range is counted with them: the stream then continues
above it, which wastes part of the id space and repeats nothing.

**SD7 — The directory is state.** The ids it holds are in rows of the
facts table, and nothing else says which key each belongs to: a directory
dropped and created again gives every key a new id. It is backed up,
restored and moved with the facts table, as the lease table is.

**SD8 — Two writers keep the id they have.** A query-run event's id stays
a digest of the event, in the band it has to itself: the top bit, which no
leased or process-local id carries (ADR-0115 SD2, ADR-0397 §SD2). The
capture deduplicates by that id, every event is its own entity, and an id
from the directory would cost a directory row per event; such an id can
equal another capture id by the odds of 63 bits and nothing else's. The
lading mount policy keeps the mount's own tagged id, minted elsewhere and
shared by a mount's versions: an entity id already, and not a hash.

### Deferred

- Expiry of directory rows. Facts rows expire; the directory row of an
  entity whose rows have all expired stays.
- Rebuilding a lost directory from the facts rows, which carry each keyed
  writer's natural key beside the id. Nothing does it.
- Stores outside this repository. A consumer's store over the facts schema
  follows when it is regenerated and handed a generator; until then it
  writes what it wrote.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| stored shape | `id` of every new facts row is a leased tagged id; `naturalKey` unchanged | readers that recomputed an id |
| id directory | a new table in the facts database, provisioned by the facts store's setup | backup and restore of the facts table |
| keyed generator | `rowintern`: resolve, batch resolve, lookup without minting, a read-only form | — |
| record-store generator | facts-bound generation sets `KeyStream`; `MintKeyFor` beside `MintKey` | the stores over the facts table, regenerated |
| facts store | `chstore.RowIdGenerator` and, from it, the keyed generator per scope and the reader's lookup | host boot and every builder of a store over the facts table |
| system metrics, presence, dead letters | an id per series, per run, per message position, resolved from the natural key | the metrics replay, which looks the id up and reads by it and by the older hash |
| capability map | an id per competence and per relation, resolved from the natural key; a relation's endpoints resolve the same way | the encoder takes a generator; the read-back takes the newest row per natural key and accepts the older id for an endpoint; the vault's tables show the natural key where they showed an id (ADR-0168, update of 2026-11-22) |
| the trail, markdown documents | an id per row, drawn from the stream | — |
| audit recorder | takes an id generator; without one it derives ids as before | a store elsewhere that uses the recorder is unaffected until it passes one |
| query runs, lading mount policy | unchanged (SD8) | — |
| lease seed | counts every kind | — |

## Alternatives

- **Sequential ids for every writer, and the entity named by the natural
  key alone.** Built first under this number, and replaced. A reader of an
  entity compared bytes and had to name a kind, a second write of the same
  thing was another row under another id, and the generators' keyed
  interface went unused. Killed by the code owner's choice of keyed
  minting on 2026-11-22.
- **Keep derived ids, in a band of their own.** A reserved high bit keeps
  a derived id from ever equalling a leased one, at almost no cost. Not
  taken for the table: derived ids would still collide with each other.
  It is what the query runs keep (SD8), where only they are in the band.
- **The mapping kept per process** (`leased.Internalizer`). No table and
  no round trip. Not taken: two processes that write one series would give
  it two ids, and a restart would give it a third.
- **A claim per key by compare-and-set**, as the lease row is advanced.
  Not taken: a key's row is written once and never changed, so the lowest
  block position decides it without an update.
- **A stream per writer.** The tag would then name the writer. Not taken:
  ADR-0319 claims a tag per table, the kind already names the writer, and
  one stream is one lease row to recover.
- **Rewrite the stored rows.** Not taken: the trail's seals name row ids,
  so rewriting them breaks sealed history, and every reference would have
  to be rewritten with them.

## Consequences

### Positive

- An id is unique by construction, whoever wrote it, and its tag says the
  row is the facts table's.
- An entity has one id from every process that writes it, and a reader
  finds it by a number.
- A store's read by id cannot meet another store's row.

### Negative

- A second table that is state. Losing it does not lose a fact; it
  detaches every entity's new rows from its old ones.
- A key's first write in a process costs the server up to three statements
  before the insert it belongs to, and a key never seen before cannot be
  written while the server is unreachable. The facts store's own rows keep
  their process-local fallback; a keyed writer has none.
- A directory row per entity, kept for good. For a writer whose entities
  mostly have one row — a dead letter that is never redelivered — that is
  a directory row per facts row.
- A reader of an entity needs the lookup, where it needed only arithmetic.
- An id that loses the race for a key is spent.
- Two eras in one table: rows from before carry hashed ids, and a reader
  takes both until the old rows have expired.
- A lost lease row on a table with a long history of hashed ids continues
  the stream above the largest of those that fell into its range.

### Neutral

- The natural keys do not change, except where one did not identify the
  entity its id digested; those are named in the migration.

## Migration — Tier 1

- **Breaks.** The value of `id` in new rows of every writer but the facts
  store; a reader that recomputed an entity's id finds only the rows from
  before.
- **Path.** Run the facts store's setup, which creates the directory.
  Regenerate a store over the facts table and hand it a generator: keyed
  for its scope where its rows share an entity, sequential otherwise. A
  reader of an entity looks the id up and reads by it and by the hash.
  Nothing is done to stored rows.
- **Regeneration.** The gen tests of the stores over the facts table.
- **Old shape.** Rows with hashed ids stay readable indefinitely.

## Verification plan — Tier 1

- **Lane.** Default `go test`, on `clickhouse-local`: the same key
  resolves to the same id from two generators that share only the
  directory; a later row for a key is not its mapping; a lookup does not
  mint; an id of another tag never enters the directory; a series' samples
  share an id; a redelivered dead letter restates its id; the trail's rows
  carry distinct ids and its seals hold over them. The `integration` lane,
  on a server: sixteen generators resolving the same fresh keys at once
  agree on every key; a run's presence rows share the run's id, and
  another process resolves the run to it; two loads of a corpus through
  two generators restate the same ids, and a table holding a corpus from
  before and a load since reads each competence once; the replay reads a series over
  rows written before and after; the lease seed over rows of every kind.
- **What would fail.** Two processes holding different ids for one key; a
  writer that derives an id; a reader that gives a key an id.
- **Gap.** That the row at the lowest block position is the same for
  every reader rests on the server's ordering of inserts, which is raced
  here and not proven. No measurement of what the directory costs a writer
  with many new keys. No test of a directory restored from a backup older
  than the facts it maps.

## Choices accepted with the decision

Reviewed by the code owner on 2026-11-22 and accepted as listed.

- Which writers are keyed: metric series, presence and dead letters are;
  the trail and markdown ingests are not, because each of their rows is
  its own thing.
- Dead letters are keyed although most have one row, to keep a redelivery
  restating its id.
- A scope per writer, named by a constant beside the writer.
- A keyed writer has no process-local fallback; a new key fails while no
  id can be leased.
- The generator is handed to a store by whoever builds it; there is no
  process-wide default a store finds on its own.
- The audit recorder keeps deriving ids when it is given no generator, so
  a store elsewhere that uses it changes only when its owner says so.
- The lease seed counts every kind and accepts the lost id space.
- The capability map's encoder no longer runs without a generator, and its
  vault tables show the natural key where they showed an id (the code
  owner chose on 2026-11-22 to move it).
- Query runs are not migrated (SD8).

## Status

Accepted 2026-11-22.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md) §SD2 — the facts row stream, its lease and its recovery.
- [ADR-0319](./0319-tagged-id-streams-leased-through-one-row-on-the-one-clickhouse-server.md) — tagged id streams, a tag per table, and the ordering one server gives.
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) — the generated stores, and the key-stream option in its update of 2026-10-11.
- [Facts-bound record stores](../explanation/facts-bound-record-stores.md) — what each writer puts in `id`.
