---
type: adr
status: proposed
date: 2026-11-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted. Nothing in it is built, and no statement about ClickHouse behaviour here has been run for this ADR.

# ADR-0402: Facts stored in a deployment's own table — one read-table setting, and a facts table the deployment provisions

## Context

A consumer of this module may already keep its records in a carrier table
of its own, with its own schema, retention and id authority, and want that
table to hold every fact — the ones the runtime writes about itself
included — so that there is one place to query, retain and delete.
`boxer.facts` would then be a route into that table and a shape read back
out of it, not a second store.

[ADR-0296 §SD9](./0296-audit-events-as-a-trail-component.md) already binds
the mechanics this needs. Its direction is that `boxer.facts` may become a
table that keeps nothing and whose materialized views route rows to storage
tables, with writers unchanged, and its constraint (d) — reads and writes
name different tables — is built for the generated stores (`ReadTable` on
every store's configuration) and for the trail's recorder. It defers the
same for the hand-written readers, the trail views and the DDL command,
which name the one fixed table. The
[background analysis](../adr-background-work/facts-storage-table-switch-options.md)
sets that switch against staying on one table and lists the forks; its
fifth is what an empty read table should mean.

That analysis assumes the storage tables are boxer's: `SetupTable` creates
them and their views. The case here differs in two respects. The storage
is the deployment's, in a schema boxer does not know, so the transform in
each direction is the deployment's SQL. And boxer must then leave the facts
table's DDL alone: setup creates the table when it is absent, the schema
check ([ADR-0387](./0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md))
judges its layout and stamp, and `schema plan` proposes statements for it
([ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md)),
none of which fits a table that is a route.

Three properties of the tree shape the options. Every writer names
`boxer.facts`, including stores a consumer generated against it, whose
table name is baked. The generated stores read by column name and accept
extra columns (ADR-0387), so a read relation need only return the facts
columns under their names. One statement changes stored rows in place —
the trail's removal of kept conversation text
([ADR-0264 §SD6](./0264-retained-model-conversations-on-facts.md)) — and a
refreshable view that appends query-run rows excludes the ones already
stored by reading its own destination
([ADR-0115](./0115-query-observability-data-plane-strategy.md)).

The id a row carries is a separate question, and is answered: a store's
allocator and stream are its configuration (ADR-0397 §SD2).

## Design space (QOC)

**Question.** How does a deployment make a table of its own the only place
facts are stored, while every writer and reader in the tree and in consumer
code keeps working?

**Options.**

- **O1 — Copy forward.** `boxer.facts` stays the MergeTree table it is; the
  deployment adds a materialized view that copies each insert into its
  carrier. Needs nothing from boxer.
- **O2 — Replace the store in Go.** The deployment supplies its own
  implementation of the facts store interface, writing to its carrier.
- **O3 — The deployment provisions `boxer.facts` as a route, and names a
  read relation.** Writers insert into `boxer.facts` as before; the
  deployment's view transforms each insert into its carrier; a view of the
  deployment's presents the carrier in the facts columns, and every reader
  is pointed at it by one setting.
- **O4 — Keep `boxer.facts` as the name read from, and move the writers to
  another name.** The deployment makes `boxer.facts` its backward view and
  receives inserts under an intake name.
- **O5 — Generate boxer's stores against the deployment's table.** The
  deployment's table description replaces the facts schema.

**Criteria.**

- **C1 — One copy**: a fact is stored once.
- **C2 — Covers every writer**: the hand-written verbs, the generated
  stores, the query-run view, and stores a consumer generated.
- **C3 — Writers unchanged**: no regeneration, no new name baked into
  consumer code.
- **C4 — Readers found**: a reader that was not retargeted fails loudly
  rather than returning nothing.
- **C5 — Boxer's size of change**, and whether it also serves the switch of
  ADR-0296 §SD9 should boxer take it for its own storage.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | +  | ++ | ++ | ++ |
| C2 | ++ | −− | ++ | +  | −  |
| C3 | ++ | +  | ++ | −− | −− |
| C4 | ++ | +  | −− | +  | +  |
| C5 | ++ | +  | +  | −  | −− |

O1 is available now and keeps two copies; a short retention on
`boxer.facts` narrows the second copy and does not remove it. O2 reaches
only the hand-written verbs: a generated store inserts through an executor
into the table it names, and never meets the interface. O5 forks every
store, since their scaffolding derives from the table. O4 keeps the name
people and documents type working for reads, and costs what §SD9 was
written to avoid — every writer, in this tree and in consumers, renamed and
regenerated. O3 is §SD9's shape with the storage owned by the deployment,
and is taken. Its weakness is C4: a reader left naming `boxer.facts` reads
a table that keeps nothing and gets no rows and no error, which SD5 below
addresses in part.

## Decision

**SD1 — `boxer.facts` stays the name written to.** No writer changes. A
deployment that stores facts elsewhere creates `boxer.facts` itself — as a
table that keeps nothing, or keeps rows briefly — together with the view
that transforms each insert into its carrier. The carrier's name and
schema never appear in boxer.

**SD2 — One read table per process, from one setting.** A registered
variable, `BOXER_FACTS_READ_TABLE`, names the relation every read of facts
goes to; empty means the table written to, which is today's behaviour.
The host resolves it once and hands it to everything that reads: the
generated stores' `ReadTable`, the recorder, the hand-written readers, the
trail views, the query-run view's exclusion of rows already stored, and
the readers that carry the name as a literal. This is §SD9 (d) completed,
and settles the background analysis's fifth fork as "set per process" —
for a deployment-owned read relation and equally for a `Merge` over
boxer-owned storage tables.

**SD3 — The read relation returns the facts columns by name.** That is its
whole contract. It may carry further columns. The schema check judges it
by its columns only: a relation has no layout to compare and no stamp to
read.

**SD4 — A facts table the deployment provisions is declared, and boxer
then issues no DDL for it.** A second setting, `BOXER_FACTS_PROVISIONING`
with the values `boxer` (the default) and `deployment`, says who owns the
table's DDL. Under `deployment`, setup creates nothing for the facts table
and fails when it is absent; the schema check verifies that the table
written to has the columns boxer inserts and that the read relation
returns them, and reports nothing about layout; `schema plan` proposes no
statement for it. Setup still installs the SQL read surface, and still
provisions the id lease table unless the store was given an allocator.

**SD5 — A statement that changes or reads stored rows by the written
table's name is refused under `deployment` provisioning.** The removal of
kept conversation text is a mutation on the stored rows, and a table that
is a route has none; boxer does not know the carrier, so the verb reports
that the deployment's storage is the deployment's to change. Likewise
`Truncate` and `DropTable`. A reader inside the tree that still composes
against the written table while a read table is set is a defect; a test
over the composed statements guards it. A statement a person types against
`boxer.facts` cannot be guarded, and returns no rows.

**SD6 — Delivery is what the deployment's views make it.** With a view
between the insert and the carrier, an insert fails when the view's insert
fails, and a retry duplicates unless the carrier folds it. Boxer's part is
unchanged: a generated store re-sends an unacknowledged batch under the
token of its first attempt (ADR-0387 §SD6). Whether that token reaches a
view's target, and under which settings, is a property of the server and
of the deployment's DDL; this ADR states the requirement — a re-sent batch
lands once in the carrier — and leaves how to the deployment.

**SD7 — A round-trip check the deployment can run.** A command writes one
row of every kind the runtime vocabulary registers into the written table
and reads each back through the read relation, comparing every column.
It is the acceptance test for a deployment's pair of transforms, run
against a scratch pair of names, and it is how a change to the facts
columns shows up as a transform that no longer round-trips.

### Deferred

- Whether boxer takes the switch for its own storage (the background
  analysis's Option A against Option B). SD2 serves either; nothing here
  decides it.
- A name for mutations to go to, so that the removal of kept text can run
  against a deployment's storage. Refusal (SD5) is the light cut.
- A second database. The read surface and the lease table stay in the
  runtime's database.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| env registry | `BOXER_FACTS_READ_TABLE`, `BOXER_FACTS_PROVISIONING` | `doc/env-vars.md` |
| facts store configuration | a read table beside the written one; provisioning | the hand-written readers' composed SQL |
| host boot | resolves both settings once; sets `ReadTable` on the stores it builds and the recorder's read table | — |
| trail views, DDL command | take the read table | the views golden |
| query-run view | the exclusion reads the read table | its composed-SQL test |
| schema check, `schema plan` | a relation judged by columns; nothing proposed for a deployment's table | the plan's output |
| trail removal of kept text, `Truncate`, `DropTable` | refused under `deployment` | their callers' messages |
| a round-trip command | new | — |

## Alternatives

- **O1, O2, O4, O5** — see the assessment.
- **A transform described to boxer** (a mapping from facts columns to the
  carrier's, from which boxer would write the views). Rejected: the two
  schemas are both leeway tables, and a mapping language between them is a
  product of its own; SQL the deployment writes and a check that it
  round-trips is the smaller contract.
- **Detect a route instead of declaring it** (probe the engine of
  `boxer.facts`). Rejected as the only signal: a deployment may keep rows
  briefly in a MergeTree intake, which looks like the default table.
  The engine remains a useful line in the check's report.
- **A read table per store instead of one per process.** It is what the
  tree has, and it is why nothing sets it: there is no one place that
  knows. Per-store configuration stays available under the process-wide
  default.

## Consequences

### Positive

- A deployment can hold every fact in one table of its own, with its own
  retention and deletion, and without a second copy.
- The read-table half of ADR-0296 §SD9 is completed for every reader, which
  boxer's own switch would need as well.
- A consumer's generated stores need no regeneration: they already carry
  `ReadTable`.

### Negative

- SQL written by hand against `boxer.facts` — in notebooks, saved queries
  and documents — returns nothing under such a deployment, silently.
  The name people have learnt to read from becomes the name to write to.
- Reads go through the deployment's view. Whether a time-bounded read
  still prunes by the carrier's key depends on that view, and is not
  measured here.
- Removing kept conversation text is not available from boxer under such a
  deployment.
- Two more settings, and a class of failure — a transform that drops or
  reshapes a column — that only the round-trip check catches.
- Every writer inherits the semantics of an insert driven through a view,
  as ADR-0296 §SD9 states for the switch.

### Neutral

- A deployment that sets neither variable sees no change.
- The facts columns remain boxer's to change; a deployment's transforms
  follow them, as a deployed table's columns do.

## Migration — Tier 1

- **Breaks.** Nothing for a deployment that sets neither variable.
- **Path.** A deployment adopting it creates its carrier and the two views,
  copies existing rows into the carrier through the same forward
  transform, replaces `boxer.facts` with its route, and sets both
  variables for every process. The order, and stopping writers for it, is
  the subject of a migration note written with the implementation.
- **Old shape.** One table, read and written, stays the default.

## Verification plan — Tier 1

- **Lane.** Default `go test`: every composed read names the read table
  when one is set; setup and `schema plan` issue no statement for a
  deployment's table; the refusals of SD5. The `integration` lane, on
  clickhouse-local: a route into a second table with a different column
  set and a view back, with the round-trip command passing over every
  registered kind, and a re-sent batch landing once.
- **What would fail.** A reader that returns rows from the written table
  while a read table is set; a DDL statement issued for a deployment's
  table; a kind whose row does not survive the round trip.
- **Gap.** No server has run any of it. The delivery semantics SD6 leans
  on are the ones the background analysis lists as recalled and unverified.

## Choices for review

- `boxer.facts` remains the written name and becomes a route (O3), rather
  than remaining the read name (O4).
- One process-wide read table, from the environment, with per-store
  configuration still possible.
- Provisioning is declared by a setting, not detected.
- Under a deployment's provisioning the removal of kept text, `Truncate`
  and `DropTable` are refused rather than redirected.
- The transforms are the deployment's SQL, checked by a round-trip
  command, not generated from a mapping.
- The names `BOXER_FACTS_READ_TABLE` and `BOXER_FACTS_PROVISIONING`.
- This is drafted apart from the decision on boxer's own storage switch;
  folding the two into one ADR is the alternative.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0296](./0296-audit-events-as-a-trail-component.md) §SD8, §SD9 — the read table on generated stores and what keeps `boxer.facts` dispatchable.
- [The storage-table switch, background](../adr-background-work/facts-storage-table-switch-options.md) — the writers, the readers and what each inherits.
- [ADR-0387](./0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md) — verdicts, reads by name, the retry token.
- [ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md) — the facts table's layout, and the store's configurable id source.
- [ADR-0319](./0319-tagged-id-streams-leased-through-one-row-on-the-one-clickhouse-server.md) — the id streams.
- [ADR-0264](./0264-retained-model-conversations-on-facts.md) §SD6 — the removal of kept conversation text.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — the query-run view.
- [Facts-bound record stores](../explanation/facts-bound-record-stores.md) — the two lanes that write the table.
