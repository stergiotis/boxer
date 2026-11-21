---
type: adr
status: proposed
date: 2026-11-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted. Nothing in it is built, and no statement about ClickHouse behaviour here has been run for this ADR.

# ADR-0402: Facts stored in a deployment's own table — a write transform at the insert seam, and one read-table setting

## Context

A consumer of this module may already keep its records in a carrier table
of its own, with its own schema, retention and id authority, and want that
table to hold every fact — the ones the runtime writes about itself
included — so that there is one place to query, retain and delete.

Every fact reaches the server the same way: a store builds Arrow record
batches in the facts columns and hands them, with a table name, to an
executor's insert. The generated stores do so through the record-store
executor interface, which is already wrapped once for observation
([ADR-0295](./0295-recordstore-call-identity-observed-executors-and-write-observers.md));
the hand-written facts store calls its client directly. One writer does not
pass through Go at all: a refreshable view on the server appends query-run
rows it pulls from a service
([ADR-0115](./0115-query-observability-data-plane-strategy.md)).

Reads are less uniform. The generated stores and the trail's recorder can
name a read table apart from the written one
([ADR-0296 §SD8, §SD9 (d)](./0296-audit-events-as-a-trail-component.md));
nothing sets it. The hand-written readers, the trail views and the DDL
command name the one fixed table. The generated stores read by column name
([ADR-0387](./0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md)).

Three things in the tree assume the facts table is a table boxer owns:
setup creates it when absent and the schema check judges its layout and
stamp ([ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md));
one statement changes stored rows in place, the trail's removal of kept
conversation text ([ADR-0264 §SD6](./0264-retained-model-conversations-on-facts.md));
and the query-run view names it as its destination.

ADR-0296 §SD9 records another direction for the same table — `boxer.facts`
as a table that keeps nothing, routing rows to boxer-owned storage tables
through materialized views — and the
[background analysis](../adr-background-work/facts-storage-table-switch-options.md)
weighs it. That concerns boxer's own storage. This ADR concerns storage
boxer does not own, and shares one piece with it: the read table.

The id a row carries is answered separately: a store's allocator and
stream are its configuration (ADR-0397 §SD2).

## Design space (QOC)

**Question.** How does a deployment make a table of its own the only place
facts are stored, while every writer and reader in the tree and in consumer
code keeps working?

**Options.**

- **O1 — Copy forward on the server.** `boxer.facts` stays the table it
  is; the deployment adds a materialized view copying each insert into its
  carrier.
- **O2 — Replace the facts store in Go.** The deployment implements the
  facts store interface over its carrier.
- **O3 — Route on the server.** The deployment makes `boxer.facts` a table
  that keeps nothing, with a materialized view transforming each insert
  into its carrier, and a view back in the facts columns that readers are
  pointed at.
- **O4 — Transform in Go at the insert seam, and name a read table.** A
  hook the deployment registers receives the facts-shaped batches of an
  insert and returns the table to insert into and the batches in that
  table's schema. A setting names the relation reads go to, which returns
  the facts columns and may be a view over the carrier.
- **O5 — Generate boxer's stores against the deployment's table.**

**Criteria.**

- **C1 — One copy**: a fact is stored once.
- **C2 — One insert, one table**: a write commits or fails as a whole, and
  a retried batch is folded by the table's own window.
- **C3 — Covers the writers**: hand-written verbs, generated stores, stores
  a consumer generated, the query-run view.
- **C4 — The name people type keeps working**: SQL written against
  `boxer.facts` returns the facts.
- **C5 — A missing piece fails loudly**: a process or reader that was not
  adapted errors instead of writing or reading nothing.
- **C6 — Boxer's size of change.**

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | +  | ++ | ++ | ++ |
| C2 | −  | ++ | −− | ++ | ++ |
| C3 | ++ | −− | ++ | +  | −  |
| C4 | ++ | −  | −− | ++ | −  |
| C5 | ++ | +  | −− | +  | +  |
| C6 | ++ | +  | +  | +  | −− |

O1 keeps two copies. O2 reaches only the hand-written verbs: a generated
store inserts through an executor and never meets the interface. O5 forks
every store, since their scaffolding derives from the table.

O3 needs no Go from the deployment and covers the server-side writer, and
loses on three counts. Every insert becomes the views' inserts, with the
partial commits and retry settings ADR-0296 §SD9 (e) lists as untested.
`boxer.facts` becomes the name written to, so SQL typed against it returns
nothing, silently. And the forward transform is one SQL statement over
every section's parallel array columns.

O4 is taken. The transform runs before the one insert, so the write is an
ordinary insert into one table and the retry token (ADR-0387 §SD6) works
as on any table. Because no row is inserted under the name `boxer.facts`
any more, the deployment is free to make that name its view back, and SQL
typed against it keeps working. What O4 gives up is the writers that are
not Go processes carrying the hook (SD5).

## Decision

**SD1 — A write transform at the insert seam.** A deployment registers a
transform for the facts table. For one insert it receives the table name
and the facts-shaped record batches and returns the table to insert into
and the batches in that table's schema. It sits in a wrapping executor,
as the observing one does, so no generated store changes; the hand-written
facts store sends its inserts through the same executor. The insert's
settings pass through untouched. The transform is a pure function of its
input: the same batches give the same output, so a re-sent batch is the
same block under the same token.

**SD2 — The transform is registered once per process, before any store is
built.** Every process that writes facts to the server must carry it — a
host, a daemon, a command-line ingestor — so it is registered by the
binary, not passed to one host, and the executors boxer constructs pick it
up. A binary without it sends facts-shaped batches to `boxer.facts`.

**SD3 — One read table per process, from one setting.** A registered
variable, `BOXER_FACTS_READ_TABLE`, names the relation every read of facts
goes to; empty means `boxer.facts`. The host resolves it once and hands it
to everything that reads: the generated stores' `ReadTable`, the recorder,
the hand-written readers, the trail views, and the readers that carry the
name as a literal. This is ADR-0296 §SD9 (d) completed, and serves a
`Merge` over boxer-owned storage tables equally.

**SD4 — The read relation has the facts shape, and need not be a table.**
It returns every facts column under its name and with its type; the schema
check verifies exactly that and nothing about engine, layout or stamp,
which a view does not have. A deployment may name its view `boxer.facts`
and leave the variable unset.

**SD5 — With a transform registered, boxer issues no DDL for the facts
table and refuses what assumes one.** Setup does not create `boxer.facts`:
an empty table of that name beside the real storage is one that reads
drain from without an error. `schema plan` proposes nothing for it. The
statements that change or empty stored rows by the facts table's name —
the removal of kept conversation text, `Truncate`, `DropTable` — report
that the storage is the deployment's. The query-run view is not installed:
it appends on the server, where no transform runs. Setup still installs
the SQL read surface, and provisions the id lease table unless the store
was given an allocator.

**SD6 — A round-trip check the deployment can run.** A command writes one
row of every kind the runtime vocabulary registers through the transform
into a scratch table, reads each back through a scratch read relation, and
compares every column. The forward transform is the deployment's Go and
the way back is its SQL; this is what shows that the two agree, and that
they still agree after a change to the facts columns.

### Deferred

- The query-run capture under a transform: the service would serve
  transformed batches and the view would be composed against the
  deployment's table.
- A hook for the in-place statements of SD5, so that the removal of kept
  text can run against a deployment's storage.
- Whether boxer takes the storage switch of ADR-0296 §SD9 for itself. SD3
  serves either outcome; nothing here decides it.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| record-store executors | a wrapping executor applying a registered transform to inserts into the facts table | the hand-written facts store inserts through it |
| registration | one call a binary makes before stores are built | the executors boxer constructs |
| env registry | `BOXER_FACTS_READ_TABLE` | `doc/env-vars.md` |
| facts store configuration, host boot | the read table, resolved once and handed to every reader | the hand-written readers' composed SQL; `ReadTable` on the stores the host builds |
| trail views, DDL command | take the read table | the views golden |
| setup, schema check, `schema plan` | no DDL for the facts table under a transform; a relation judged by columns | the plan's output |
| removal of kept text, `Truncate`, `DropTable`, query-run view | refused or not installed under a transform | their callers' messages |
| a round-trip command | new | — |

## Alternatives

- **O1, O2, O3, O5** — see the assessment. O3 stays open to a deployment
  on its own: SD3 is all it needs from boxer.
- **Pass the transform to one host instead of registering it for the
  process.** Rejected: the daemons and command-line ingestors write facts
  without a host, and a writer that misses it writes the wrong shape.
- **Describe the mapping to boxer and let it generate both directions.**
  Rejected for now: a mapping language between two leeway tables is a
  product of its own. Code for one direction, SQL for the other and a
  check that they agree is the smaller contract.
- **Detect a deployment's storage from the server instead of from the
  registration.** Rejected: what boxer must not do — create the table —
  has to be known before the first statement.

## Consequences

### Positive

- A deployment holds every fact in one table of its own, written by one
  ordinary insert per batch.
- SQL written against `boxer.facts` keeps working where the deployment
  names its view so.
- A consumer's generated stores need no regeneration.
- The read-table half of ADR-0296 §SD9 is completed for every reader.

### Negative

- Only processes that carry the transform can write facts. A stock binary,
  a hand-written `INSERT`, and the query-run view cannot; where
  `boxer.facts` is a view or absent their inserts fail at the server, and
  where a table of that name was left behind they land in it unseen.
- The two directions are in two languages and two places, and agree only
  as far as the round-trip check is run.
- The transform is coupled to the facts columns: a change to them is a
  change to the deployment's code and view.
- Reads go through the deployment's view. Whether a time-bounded read
  still prunes by the carrier's key depends on that view and is not
  measured here.
- Removing kept conversation text and capturing query runs are not
  available under a transform until their deferrals are built.

### Neutral

- A deployment that registers nothing and sets nothing sees no change.
- The transform runs in the writing process, per batch, before the
  network round trip.

## Migration — Tier 1

- **Breaks.** Nothing for a deployment that registers no transform and
  leaves the variable unset.
- **Path.** A deployment adopting it writes the transform and the view,
  runs the round-trip check, copies existing rows into its carrier, replaces
  `boxer.facts` with the view (or sets the variable), and ships binaries
  that register the transform. The order, and stopping writers for it, is
  the subject of a migration note written with the implementation.
- **Old shape.** One table, read and written, stays the default.

## Verification plan — Tier 1

- **Lane.** Default `go test`: an insert into the facts table reaches the
  inner executor under the transform's table and schema, with its settings
  intact, from a generated store and from the hand-written one; every
  composed read names the read table when one is set; setup issues no
  facts DDL under a transform; the refusals of SD5. The `integration`
  lane, on clickhouse-local: a transform into a table with a different
  column set and a view back named as the read table, the round-trip
  command passing over every registered kind, and a re-sent batch landing
  once.
- **What would fail.** A facts insert that bypasses the wrapper; a reader
  that returns rows from another relation than the read table; a `CREATE`
  for the facts table under a transform; a kind whose row does not survive
  the round trip.
- **Gap.** No server has run any of it.

## Choices for review

- The transform is registered per process, not per host.
- Registering a transform is what tells boxer the storage is the
  deployment's; there is no second setting for it.
- The read relation must return every facts column with its type; extra
  columns are tolerated, since reads select by name.
- Under a transform the removal of kept text, `Truncate`, `DropTable` and
  the query-run view are refused or left out, not redirected.
- The transform maps one insert to one insert into one table: no fan-out
  to several tables.
- The name `BOXER_FACTS_READ_TABLE`.
- This is drafted apart from the decision on boxer's own storage switch.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0296](./0296-audit-events-as-a-trail-component.md) §SD8, §SD9 — the read table on generated stores and what keeps `boxer.facts` dispatchable.
- [The storage-table switch, background](../adr-background-work/facts-storage-table-switch-options.md) — the writers, the readers and what each inherits.
- [ADR-0295](./0295-recordstore-call-identity-observed-executors-and-write-observers.md) — the wrapping executor this one follows.
- [ADR-0387](./0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md) — verdicts, reads by name, the retry token.
- [ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md) — the facts table's layout, and the store's configurable id source.
- [ADR-0264](./0264-retained-model-conversations-on-facts.md) §SD6 — the removal of kept conversation text.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — the query-run view.
- [Facts-bound record stores](../explanation/facts-bound-record-stores.md) — the two lanes that write the table.
