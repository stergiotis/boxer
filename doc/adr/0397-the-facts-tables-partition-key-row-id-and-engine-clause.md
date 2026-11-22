---
type: adr
status: accepted
date: 2026-11-20
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-21
---

# ADR-0397: The facts table's partition key, row id and engine clause

## Context

A partition key and a sorting key cannot be changed on an existing
MergeTree table: changing either means creating a second table and copying
the rows. Whatever `boxer.facts` is created with is what a deployment keeps,
so the default deserves a decision rather than a constant.

Before this decision every boot path created the table with
`MergeTree() ORDER BY ts`: no partition key and no TTL. Retention on such a
table is a row-level mutation over the whole table. Only one command-line
flag could override the clause, and nothing reported a table created with
another one.

The table's `id` came from a counter in each process that started at one, so
ids repeated across runs and across concurrent processes. ADR-0319 §SD8
recorded this as a defect to be fixed in a change to the facts table. The
generated stores bound to `boxer.facts` treat `id` as their key (their
point reads select the newest row per `id`), and the log viewer uses it to
tell apart two lines that agree on timestamp, caller and message. Those
stores write ids of their own (a hash of a key, a mount's tagged id); the
counter's small values sat among them.

The DDL goldens of the facts-bound stores stated `ORDER BY (id, ts)`, the
record-store generator's default, while the table applied was
`ORDER BY ts`. A reader of a golden learned the wrong layout.

The reads the tree runs against the table: the log rows, the app trail and
the run events bound by or order by `ts`; the per-kind reads test a
membership with `has()`; the trail and the generated stores' point reads
select by run or by `id` with no time bound; the launch statistics fold
every lifecycle row. Writes are small and frequent and land at the current
time — the log bridge's batches, the heartbeat, lifecycle rows, the trail's
flushes — except imports and replays, which carry older timestamps.

## Design space (QOC)

**Question.** What partition key does a newly created facts table get?

**Options.**

- **O1 — None**, as before.
- **O2 — Calendar month of `ts`**, `toYYYYMM(ts)`.
- **O3 — Calendar day of `ts`**.
- **O4 — Expiry day**, `toYYYYMMDD(expiresAt)`, as the snapshot store
  partitions (ADR-0198), so a TTL drops whole partitions.

**Criteria.**

- **C1 — Retention by partition drop**: removing old rows is a metadata
  operation.
- **C2 — Parts under the tree's writes**: small frequent inserts at the
  current time, and imports with older timestamps.
- **C3 — The reads**: time-bounded reads skip partitions; unbounded reads
  pay little for crossing them.
- **C4 — Needs no per-kind policy**: works without every writer deciding an
  expiry.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | +  | ++ | ++ |
| C2 | ++ | +  | −  | +  |
| C3 | +  | +  | −  | +  |
| C4 | ++ | ++ | ++ | −− |

O1 leaves retention to mutations over a table that only grows. O3 gives
365 partitions a year; an import spanning more than 100 days in one insert
block exceeds the server's `max_partitions_per_insert_block` default, and
an unbounded read opens every day. O4 needs an expiry on every row, which
only grants carry; with none, every row lands in one partition and the key
does nothing. O2 is taken: a year is twelve partitions, live writes touch
one, and an import is refused only past 100 months in one block.

## Decision

**SD1 — A newly created facts table is partitioned by month and sorted by
time.** The default engine clause is
`MergeTree() PARTITION BY toYYYYMM(ts) ORDER BY ts`
(`factsschema.DefaultEngineClause`). No TTL is imposed: deleting rows stays
the deployment's decision, and with month partitions a TTL can drop whole
parts (`ttl_only_drop_parts`). The table also keeps the insert
deduplication window ADR-0387 §SD6 gives a plain MergeTree table, which the
facts-bound stores' retry tokens need.

**SD2 — The facts store mints row ids from a leased stream.** `chstore`
draws ids from the tagged stream `factsRow` (ADR-0319), leased in blocks
through `boxer.idlease` in the facts table's database, so an id is unique
across every process writing to the server and across runs. Setup
provisions the lease table beside the facts table. Building a store leases
nothing; the first write leases the first block, and a block lasts 1024
rows. While no block can be leased the store mints under a second stream,
`factsRowLocal`, counted in process, logs once, and tries the lease again
after a minute: the row lands, and its tag says its id is unique only
within its process. The in-memory store is unchanged.

Where the blocks come from is the store's configuration (`chstore.RowIds`,
which a host passes as `hostboot.Options.FactsRowIds`): the lease table is
the default, and a deployment with an id authority of its own supplies an
`identgen.AllocatorI`, the tag its ids carry and the block size. The
uniqueness of an id is then that authority's promise — blocks for the tag
that overlap no other writer's — and setup leaves the lease table alone.
The process-local fallback is the same under either source.

The lease row is the only place a stream's next unleased id is kept, so
setup guards the one way it is lost while the rows are not: when the lease
table holds no row for the store's stream and the facts hold ids of that
stream, the row is created continuing after the largest of them
(`rowalloc.Seed`). The ids of a stream are one range of the id column, so
that is one aggregate over the column, read at setup and never on a write.
A lease table that holds a row older than the facts — restored from an
earlier backup — is not detected.

**SD3 — The engine clause is a setting with one source.**
`BOXER_FACTS_ENGINE` selects the clause a newly created table gets; its
default is `factsschema.DefaultEngineClause`. The record-store generator
emits the facts-bound stores' DDL goldens with the same constant, so a
golden states the layout a new table has. A clause carrying `SETTINGS` is
refused: ClickHouse reads a second `SETTINGS` clause after the table's as
the statement's, which would drop the table's settings without an error.

**SD4 — A table in another layout is reported, not failed.** A table
contract may declare a layout (engine, partition key, sorting key). The
schema check compares it with what `system.tables` reports, after reducing
quoting and tuple parentheses, and a difference is a note — "layout
differs (informational)" — pointing at
[the migration recipe](../migration/2026-11-facts-layout.md). The verdict
stays what the columns make it, `BOXER_SCHEMA_STRICT` ignores the note, and
the host logs it at info. The facts table's `CREATE` carries a stamp with
the column digest and a `layout=` digest; `schema plan` does not restamp a
table whose layout differs, since the stamp would record a layout the table
lacks.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| facts table DDL | `PARTITION BY toYYYYMM(ts)`, the deduplication window, a `COMMENT` stamp | `chstore.ComposeSetupSQL`, `keelsonddl`'s output |
| facts-bound DDL goldens | engine clause from `factsschema.DefaultEngineClause` | the goldens of trail, sysmfacts, watchbillpresence, ladingpolicy, mddocfacts, stevedorefacts, meshdemo, regenerated |
| facts row ids | tagged ids from `factsRow` / `factsRowLocal` | `vocab.IdStreams` and the stream claims golden |
| setup | provisions `<database>.idlease`, unless the configuration brings an allocator | `SetupTable`, `SetupStatements` |
| `chstore.Config`, `hostboot.Options` | `RowIds` / `FactsRowIds`: allocator, stream, block size | additive; the zero value is the lease table |
| env registry | `BOXER_FACTS_ENGINE` | `doc/env-vars.md` |
| `recordstore` | `TableLayout`, `ParseEngineClause`, `TableContract.Layout`, `SchemaVerdict.LayoutDiffers`, `TableStamp.Layout` | additive |

## Alternatives

- **Declare `id` non-identifying and stop treating it as a key.** Rejected:
  the generated stores' point reads key on it and the log viewer
  disambiguates by it, and ADR-0319 already built the stream; seeding a
  counter from `max(id)` at start is a read-modify-write that two processes
  starting together still race.
- **Fail the write when no block can be leased.** Rejected: a log line or a
  heartbeat lost because the lease table is unavailable costs more than an
  id whose tag says it is process-local.
- **A strict mode that fails the write when a configured allocator fails.**
  Rejected by the code owner on 2026-11-21: a consumer that wants it has
  what it needs at the application level — the id a write returns carries
  its stream's tag.
- **Partition by `(kind, month)`.** Rejected: the kind is a membership in an
  array column, not a column a key can name, and per-kind retention is not
  decided.
- **Make a differing layout `migratable` or `incompatible`.** Rejected: the
  store reads and writes either layout, and no statement `schema apply`
  could run changes a partition key.
- **Keep the engine clause a constant and let the flag override it.**
  Rejected: a deployment that wants the old clause, or its own, has no way
  to say so at boot.

## Consequences

### Positive

- Retention can drop a month at a time, and time-bounded reads skip
  partitions.
- A row id names one row on the server, so the log viewer's key and the
  generated stores' point reads no longer meet another process's row.
- A golden states the layout a new table has.

### Negative

- An existing table keeps `ORDER BY ts` unpartitioned until the operator
  copies it; the host reports the difference at every start.
- An insert block spanning more than 100 months is refused by the server's
  default; an import of old rows is split.
- A write may cost a lease round trip once per block, bounded by the
  store's write timeout.
- Rows written while no lease could be had carry ids that repeat across
  processes, as every row did before.
- The lease table is state: it is backed up, restored and moved with the
  facts. Setup recovers a lost one from the rows; it does not notice one
  that is older than the rows, and a store built without setup beside a
  lost lease table starts the stream over.
- Recovering a lost lease row reads the whole id column once.

### Neutral

- Ids are larger numbers (a tag in the high bits); within a process the
  ids of one stream still increase, so the reads that order by `ts, id`
  keep their tie-break.
- Rows already stored keep their counter ids.

## Migration — Tier 1

- **Breaks.** The DDL a new facts table is created with, and the form of
  the ids the facts store writes. An existing table is left as it is and
  keeps working.
- **Path.** Nothing is required. To move an existing table into the new
  layout, [doc/migration/2026-11-facts-layout.md](../migration/2026-11-facts-layout.md).
  To keep creating the old layout, set
  `BOXER_FACTS_ENGINE='MergeTree() ORDER BY "ts:ts:z64:47::0:"'`.
- **Regeneration.** The facts-bound stores' gen tests; `go generate
  ./public/config/env/...`.
- **Old shape.** Kept indefinitely: the store works over either layout.

## Verification plan — Tier 1

- **Lane.** Default `go test`: the composed DDL, the contract's layout,
  the local-stream fallback, the golden against the applied clause, and the
  layout comparison against the server's spellings. The `integration` lane,
  on clickhouse-local: a table created with the default clause takes a log
  row whose id is leased and lands in its month's partition; a table with
  the old clause is ok with the note, and the recipe's statements copy it
  into the new layout with every row. The same lane, on a server, each
  test in a scratch database: the layout, window and stamp of a fresh
  table as `system.tables` reports them; the recipe on a legacy table,
  through `EXCHANGE TABLES`, with one partition per month afterwards;
  stores in one process and other processes leasing small blocks at once
  with no id repeated; a lease table out of reach and back; a re-sent
  batch folded by the window, and landing twice in a table without it; a
  table in a third layout; a lease table dropped and set up again
  continuing after the largest id.
- **What would fail.** A golden whose engine clause differs from the
  applied one; a facts store minting an id under no stream; a differing
  layout reported as anything but a note.
- **Gap.** One server release, 26.9, on one machine; the comparison's
  normalisation is checked against the spellings of that release. No
  table of a deployment's size, and no restore from a backup.

## Choices accepted with the decision

Reviewed by the code owner on 2026-11-21 and accepted as listed.

- Month, not day or expiry day, as the partition key (the QOC above).
- The sorting key stays `ts`. Leading with `id`, the generator's default,
  would serve the generated stores' point reads and cost every
  time-bounded read its range.
- No TTL by default.
- The facts table gets the deduplication window and a stamp; ADR-0387 left
  both to this decision.
- Ids leased, with a process-local stream as the fallback instead of a
  failed write; the lease retried after one minute.
- A configured allocator keeps the process-local fallback: a deployment
  whose authority is unreachable still gets the row, under boxer's
  `factsRowLocal` tag rather than a failed write.
- The stream is configurable beside the allocator, so a deployment can mint
  facts ids under a tag it claimed; rows written under one tag keep it.
- Setup that cannot create the lease table fails, as a failed read-surface
  install does, so a host falls back to the in-memory store rather than
  run on a table whose ids it cannot lease.
- The two tag values, 57314 and 53133, in the journal entries' class, and
  the expected cardinality of 2^39 rows.
- A clause with `SETTINGS` refused rather than merged.
- The env var's name `BOXER_FACTS_ENGINE` and its category `database`.
- The recipe stops every writer for the copy instead of copying behind live
  writes.
- A lost lease table is recovered at setup, from the largest id of the
  stream the read relation returns; a failed read there fails setup rather
  than seeding by guess.
- No strict mode for a configured allocator.

## Status

Accepted 2026-11-21.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-11-22 — what "a row id names one row" covers, and what the seed counts

*Consequences* says a row id names one row on the server. That holds for
the rows the facts store writes, whose ids are leased, and for nothing
else in the table: the generated stores over it put keys of their own in
the same column — hashes of a natural key, one id per series or per
entity — as *Context* says in passing.
[Facts-bound record stores](../explanation/facts-bound-record-stores.md#what-the-id-column-holds)
lists them. Their point reads select by `id` alone, so they rely on no
other writer holding their key, which a 64-bit hash makes unlikely and
nothing makes impossible. Whether they move onto streams is open.

SD2's recovery of a lost lease row counts only the rows of the kinds the
facts store writes. A hash falls into the stream's range about once in
sixteen million rows; counted, it would have restarted the stream at a
body drawn at random from its whole space.

## References

- [ADR-0319](./0319-tagged-id-streams-leased-through-one-row-on-the-one-clickhouse-server.md) — the id streams and the lease table.
- [ADR-0387](./0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md) — verdicts, stamps and `boxer schema plan`.
- [ADR-0198](./0198-fs-snapshot-store.md) — the expiry-day partitions of the snapshot store.
- [ADR-0184](./0184-sysmetrics-persistence-tee.md) — `chstore` as the facts table's one DDL author.
- [doc/migration/2026-11-facts-layout.md](../migration/2026-11-facts-layout.md) — copying a table into the new layout.
