---
type: adr
status: accepted
date: 2026-11-09
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-11
---

# ADR-0366: The facts envelope says what a row is, where it lives and how long

## Context

A facts table is one table of record on one schema: append-only, the
newest row per natural key wins, every store a generated binding of the
same description. keelson's own stores write one, and a consumer binds the
schema unmodified so that a pin bump moves its table with boxer's. This
ADR keeps that commitment and changes what the record knows about each
row.

The envelope — the plain columns every row carries, declared by
`LoadFactsEnvelope` in
[`public/keelson/runtime/factsschema`](../../public/keelson/runtime/factsschema)
— is four columns: `id`, `naturalKey`, `ts` and `expiresAt`. A table
generated from it is ordered by `(id, ts)`, has no partition key and
declares no TTL (a consumer's generated DDL, 2026-11-09). Where row ids
are hashes, as they are in that consumer, rows
of every kind and every dataset are spread evenly over the table. Three
things the record therefore cannot say, with what each has cost there:

- **What a row is.** A row's kind is recoverable only from its
  tagged-value arrays, so a read that is not by id scans every kind. A
  `WHERE` over a current-state view pivots the whole table (0.4–1.2 s over
  a corpus of about half a million entities, measured 2026-11-06); five domains there
  carry derived tables of their own for that reason, and one left for a
  facts-shaped table with another order.
- **Which dataset a row belongs to.** An imported corpus and a person's
  daily data are told apart by a predicate evaluated after the scan.
  Nothing can be rebuilt, measured or removed as a unit.
- **How long a row lives.** Supersession by a newer row is the only
  lifecycle. `expiresAt` is written and no table clause enforces it.
  Removal of one entity is invented per kind; one store alone has
  four spellings of it.

Two further things are reinvented per ingest because the record does not
carry them: which run wrote a row — each reader keeps its own
skip-if-unchanged test, and a derived table's timestamp watermark has
needed correcting twice (2026-11-06 and 2026-11-09) — and when the thing a row describes happened, the write
instant being the only time every row has.

The question arose in a consumer's application, which was asked
(2026-11-09) to serve three ways of working: a large corpus worked on
intensively, a glimpse at a live source removable at once, and daily use.
They differ in scope and in lifecycle and in nothing else, and those are
the dimensions the envelope lacks.

One fact shapes the timing. A MergeTree's partition key and sorting key
cannot be altered once the table exists. The owner stated (2026-11-09)
that every database on the present envelope may be dropped to get this
right, rows a person made included, so the design is made for the long
run and carries no in-place migration.

## Design space (QOC)

**Question.** How does one record serve datasets that differ in scope and
lifetime, without the read cost, the removal cost and the bookkeeping
growing with every new one?

**Options.**

- **O1** — The record as it is; scope by convention in each consumer,
  removal not offered.
- **O2** — A database per dataset, each with its own facts table, lading
  tables and derived tables.
- **O3** — One table, a batch column, removal by `ALTER TABLE … DELETE`.
- **O4** — One table whose envelope carries genus, scope, lifecycle and
  provenance, partitioned by scope.

**Criteria.**

- **C1** — One source of truth: one schema, one table of record, joins
  across datasets with no ceremony.
- **C2** — A dataset is removed as a unit, cheaply, with a record that it
  was.
- **C3** — Reading one dataset or one genus does not pay for the others.
- **C4** — The number of things a person has to manage does not grow with
  the number of datasets.
- **C5** — A new lifecycle case is data, not a rebuild of the table.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −− | ++ | ++ |
| C2 | −− | ++ | −  | +  |
| C3 | −− | ++ | −− | ++ |
| C4 | +  | −− | +  | +  |
| C5 | −  | +  | −  | ++ |

O4 scores `+` and not `++` on C2 because a dataset with bytes spans the
facts table and the lading tables: the physical removal is one statement
per table of record (§SD12).

## Decision

We will extend the facts envelope so that every row states what it is, the
bucket it belongs to, its lifecycle and the run that wrote it; partition
every table of record by bucket; and generate the views, the TTL and the
removal path from those columns. The cases — a corpus, a glimpse, live
data, an edition of a register — are rows in a bucket registry, not
databases, tables or code paths.

Data-centric here means one source of truth. Derived tables are part of
the model as disposable projections of it (§SD14); they are not a
departure from it.

### Subsidiary design decisions

- **SD1 — The envelope.** Twelve plain columns in four roles, the roles
  being leeway's plain item types:

  | Role | Column | Says |
  | --- | --- | --- |
  | identity | `id` | the row |
  | identity | `naturalKey` | the entity, stable across writes |
  | time | `ts` | the write instant |
  | time | `eventTime` | when the thing described happened; zero where the genus has no such instant (§SD10) |
  | routing | `genus` | what the row is (§SD2) |
  | routing | `locality` | a 64-bit value rows that are read together share (§SD4) |
  | routing | `run` | the run that wrote the row (§SD9) |
  | lifecycle | `dataClass` | the lifecycle case (§SD5) |
  | lifecycle | `bucket` | the unit kept or dropped together (§SD3) |
  | lifecycle | `slice` | the sub-partition within the bucket (§SD3) |
  | lifecycle | `retracted` | the row withdraws its entity (§SD8) |
  | lifecycle | `expiresAt` | the instant after which the server may delete the row (§SD11) |

- **SD2 — Genus is a stored column, and it leads the sorting key.** What a
  row is — a grant, a job, a message, a photograph — is called its genus,
  because `kind` already names a leeway DTO concept. It is a
  low-cardinality string, qualified by its vocabulary, written by the
  generated store from the DTO and never derived at read time: deriving it
  means matching array columns no index covers. The sorting
  key names it first, so a view reads its own genus's granules, and a read
  by id gives the genus beside the id and stays a primary-key read. Row
  ids are unchanged in form; a store whose ids are hashes adds the bucket
  to what it hashes (§SD7).

  Carrying the genus as the tag of a tagged row id was considered and
  declined (Alternatives).

- **SD3 — A table of record is partitioned by `(bucket, slice)`, and the clause never changes.**
  A bucket is a `u32` naming a unit that is kept or dropped together.
  `slice` divides a bucket where its policy says so
  — by month for live data, not at all for a corpus. The writer computes
  `slice` from the bucket's declared rule and stores it. A `multiIf` over
  the data class inside the partition clause was the first form of this
  and lost for one reason: the clause is frozen at table creation, so
  every future case would have to be foreseen in it. With the value
  stored, a new slicing rule is a registry row.

- **SD4 — Locality, and the sorting key is measured before it is fixed.**
  `locality` is the value rows read together share — a document's items
  carry the document's. Whether the sorting key is `(genus, id, ts)`,
  which clusters by genus alone, or `(genus, locality, id, ts)`, which
  also puts a document's items side by side and asks a read by id to give
  the locality as well, is decided by measurement in §M1 on the
  corpus named in the Context, because the sorting key is as immutable as
  the partition key.

- **SD5 — Four data classes.**

  | Class | Is | Slice | Ends |
  | --- | --- | --- | --- |
  | `record` | what a person or the system decided or observed once and keeps | none | never |
  | `corpus` | a body of data ingested whole to be worked on | none | when the bucket is withdrawn |
  | `live` | data that keeps arriving from a source | by month of `ts` | by retention, a slice at a time |
  | `ephemeral` | data brought in to be looked at and discarded | none | when withdrawn, or at `expiresAt` |

  A class is a name for a policy the bucket's registry row spells out; a
  fifth class is a row and a writer rule.

- **SD6 — The registries are facts.** A bucket, a run and a derived table
  are each a genus in the same table: the bucket's row carries its name,
  class, slicing rule and retention; the run's its source, reader version,
  digest, counts and instant; the derived table's its definition hash, the
  genera it reads and the run it is built up to. "Which datasets exist,
  who wrote what, which projections are current" are queries. The
  registries live in bucket 0.

- **SD7 — A bucket is closed under supersession, by construction.** The
  bucket is part of the key the generated views deduplicate by, and of
  what a hashed row id hashes, so a row in one bucket can neither
  supersede nor be superseded by a row in another. Dropping a bucket
  therefore leaves no hole elsewhere. What a store owes on top of that is
  narrower: a source writes into one bucket, and a computation that
  rewrites rows reads its own bucket only.

- **SD8 — `retracted` is the one tombstone.** A row with `retracted` set
  withdraws its entity; the generated current-state views drop a natural
  key whose newest row is retracted. It replaces per-kind spellings. A
  domain value that happens to mean "no" — a decision that un-decides —
  stays a value.

- **SD9 — Every row names its run.** A run is one ingest, one job, one
  session of a window. Its registry row is written first and completed
  last, so a run that died is a row without an end. Skip-if-unchanged
  becomes one question asked of the run registry instead of a test each
  reader carries, and a derived table is built incrementally by the runs
  it has not seen instead of by a timestamp watermark.

- **SD10 — `eventTime` is the domain instant.** `ts` stays the write
  instant and stays what the views order by, because a domain time ties
  across re-ingests. With the domain instant in the envelope, a timeline
  across genera is one query and no store has to be known to it.

- **SD11 — `expiresAt` is enforced.** The table declares a TTL over it. An
  `ephemeral` bucket's rows carry one as the net under a withdrawal that
  never came.

- **SD12 — Withdrawing a bucket is one fact and one statement per table of record.**
  The logical act is a retracted row in the bucket registry: from that
  insert on, every view excludes the bucket, and the row says who withdrew
  it and when. The physical act is `ALTER TABLE … DROP PARTITION` for the
  bucket on the facts table and on each lading table, issued by one
  command, after which the derived tables drop the same partition. A
  literal single statement would need the bytes in the facts table, which
  was declined (Alternatives).

- **SD13 — The lading tables take the lifecycle columns and the same partition key.**
  Bytes belong to the bucket of the rows that name them.

- **SD14 — Derived tables are one mechanism.** A derived table is declared
  — its definition, the genera it reads — registered (§SD6), partitioned
  by bucket like the record, written by one manager and by nothing else,
  and built by run (§SD9). The manager is a package here, so a consumer
  stops carrying a build-and-exchange helper of its own.

- **SD15 — One envelope for every facts table.** `LoadFactsEnvelope` stays
  the single definition. keelson's stores carry the same twelve columns as
  a consumer's, so the registries, the views and the withdrawal path are
  written once.

### Envisioned use

- **A corpus.** An import creates a `corpus` bucket and writes into it.
  Re-deriving, rebuilding its projections or measuring a query touches
  that partition alone.
- **A glimpse.** A read-only fetch from a live source creates an
  `ephemeral` bucket with an expiry. Withdrawing it removes all of it and
  moves nothing else.
- **Live data.** One `live` bucket per source, sliced by month. The
  projections follow runs, so what a job wrote is seen without a rebuild
  being remembered by anyone.
- **Editions.** A new edition of a register is ingested into a bucket
  beside the old one; the views switch; the old bucket is dropped.
- **keelson's own rows** — grants, jobs, app state, policies — are
  `record` rows, with `expiresAt` now doing what it says where one is set.
- **Across stores.** One timeline over `eventTime`; one retention view
  over the bucket registry; one answer to "what did this run write" for every ingest.

### Milestones

- **M1 — The envelope.** The columns, the DDL (partition key, sorting key
  led by `genus`, TTL), the generator setting what it can from the DTO;
  the sorting-key measurement of §SD4.
- **M2 — Registries and views.** The bucket, run and derived-table genera;
  generated views that filter by genus, deduplicate within the bucket and
  honour `retracted` and withdrawn buckets.
- **M3 — Lading.** The lifecycle columns and the partition key on the
  lading tables; the withdrawal command.
- **M4 — The derived-table manager.** The catalogue and builds by run.
- **M5 — keelson's stores.** Every generated store in this repository on
  the new envelope.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| The facts schema's envelope (`LoadFactsEnvelope`) | four plain columns become twelve | every generated store here and in each consumer |
| Facts-table DDL, through [`public/semistructured/leeway/ddl/clickhouse`](../../public/semistructured/leeway/ddl/clickhouse) | gains `PARTITION BY (bucket, slice)`, a sorting key led by `genus`, a TTL | every database is recreated; nothing can alter a table in place |
| The store generator's DTO contract | a store writes `genus` from the DTO and takes the bucket and the run at write time | every DTO package; the generated writers' signatures |
| Generated current-state views | filter by genus, deduplicate by bucket and natural key, honour `retracted` and withdrawn buckets | every consumer's hand-written view tail |
| Lading's tables, [`public/fs/lading/ladingschema`](../../public/fs/lading/ladingschema) | gain the lifecycle columns and the partition key | every mount; the lading stores |
| Exported API | a derived-table manager package; a bucket and a run registry | consumers' copies of the build-and-exchange helper |

## Alternatives

O1–O3 are assessed above. Their kill-reasons: O1 leaves all three gaps
open; O2 gives up the single schema and multiplies every table by the
number of datasets, which is the sprawl the model exists to avoid; O3 is
neither one statement nor append-only and makes removal a mutation of the
shared table.

- **A `multiIf` over the data class in the partition clause.** Frozen at
  table creation; see §SD3.
- **Genus as the tag of a tagged row id** (ADR-0106), which would cluster
  the table by genus with no extra column. Declined on three counts
  (owner, 2026-11-09). Tags are Fibonacci-coded, so reading or comparing a
  tag in SQL is arithmetic ClickHouse does per row. A cell has a base tag
  to which the application's tags are added, so the tag of one genus
  differs from cell to cell and a query cannot name it as a constant. And
  the tag takes its bits from the hash a row id is made of where ids are
  hashes. A stored column costs a few bytes per part and none of that.
- **Bytes in the facts table**, so that withdrawal is literally one
  statement. Large values in the table every query reads make every merge
  rewrite them; lading's tables exist for that reason.
- **Removal by TTL alone.** An expiry removes rows when the server gets to
  it and says nothing about who decided; it is the net (§SD11), not the
  act.

## Consequences

### Positive

- A dataset is a row in a registry and a partition, not a database.
- A view reads its own genus and its own bucket; the cost of a large
  corpus is the corpus's.
- Removal, provenance and expiry are uniform across stores and visible as
  facts.
- A new lifecycle case, a new slicing rule and a new derived table are
  data.

### Negative

- Every store is regenerated and every database recreated; what was in
  them is lost unless it can be ingested again (Migration).
- A genus name becomes load-bearing: renaming one orphans its rows from
  their views.
- The partition count becomes something to govern: every ephemeral bucket
  is a partition until it is dropped.
- Twelve envelope columns on every row, most of them constant within a
  part and compressing accordingly, but present in every generated DTO
  and insert.

## Migration — Tier 1

- **Breaks.** Every table generated from the present envelope, every
  generated store's writer signature, every hand-written view tail that
  names the envelope's columns, and every stored row id where the id's
  hash gains the bucket.
- **Path.** None in place: a partition key and a sorting key cannot be
  altered. A database on the old envelope is dropped and recreated, and
  what can be ingested again is. The owner accepted the loss of the rest
  (2026-11-09) — what a person decided and what the runtime kept, such as
  persistent app state.
- **Regeneration.** Every generated store, here and in each consumer.
- **Old shape.** Removed outright with the first store that lands on the
  new one; the two do not coexist in one database.

## Verification plan — Tier 1

- **The envelope is complete on every write.** Default lane: a generated
  store refuses a row without a genus, a bucket and a run.
- **Closure.** Integration lane: one natural key written into two buckets
  reads back as each bucket's own row; dropping one leaves the other
  unchanged.
- **Withdrawal.** Integration lane: a bucket with bytes is ingested and
  withdrawn; zero rows remain in the facts table, the lading tables and
  every registered derived table, and the registry row stands.
- **Slice stability.** A golden over the partition ids a bucket's rule
  yields.
- **Genus names.** A golden over the genus each store writes.
- **Partition count.** Default lane: the bucket registry refuses a bucket
  past its cap.
- **Gap.** Nothing checks that a consumer's hand-written SQL filters by
  genus where it could; a missing predicate is slow, not wrong.

## Open questions

- The sorting key (§SD4).
- Whether `retracted` on a registry row is enough to exclude a withdrawn
  bucket from every view at no measurable cost, or the views need the
  bucket set handed to them (§SD12).
- How a bucket relates to a cell: whether a bucket id is local to a cell's
  table or is allotted like a tag.

## Status

Accepted 2026-11-11. It records the design reached on
2026-11-09.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- ADR-0106 — tagged ids, the declined carrier of the genus.
- [`public/keelson/runtime/factsschema`](../../public/keelson/runtime/factsschema)
  — the envelope's definition.
