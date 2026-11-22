---
type: adr
status: accepted
date: 2026-11-21
reviewed-by: "p@stergiotis"
reviewed-date: 2026-11-21
---

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
before this ADR the hand-written facts store called its client directly.
One writer does not pass through Go at all: a refreshable view on the
server appends query-run rows it pulls from a service
([ADR-0115](./0115-query-observability-data-plane-strategy.md)).

Reads are less uniform. The generated stores and the trail's recorder can
name a read table apart from the written one
([ADR-0296 §SD8, §SD9 (d)](./0296-audit-events-as-a-trail-component.md));
before this ADR nothing set it, and the hand-written readers, the trail
views and the DDL command named the one fixed table. The generated stores read by column name
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
and the batches in that table's schema
(`recordstore.InsertTransformI`). It sits in a wrapping executor, as the
observing one does, so no generated store changes
(`recordstore.TransformExecutor`, `recordstore.RegisteredTransforms`); the
HTTP executor applies the registered one itself (`storeexec.Executor`),
and the hand-written facts store sends its inserts through that executor.
The insert's settings, the deduplication token among them, pass through
untouched. The transform is a pure function of its input: the same batches
give the same output, so a re-sent batch is the same block under the same
token. It takes no context, since it does no I/O.

A transform applies once per insert, however many executors on the way
consult it: the one that applied it marks the context the insert goes on
under (`recordstore.WithTransformApplied`), and a marked insert is sent as
it is. A transform's target may therefore be any table, a registered one
included. An observing executor above the transform reports the table the
store named and, beside it, the table the rows went to
(`recordstore.CallEvent.Target`).

**SD2 — The transform is registered once per process, before any store is
built.** Every process that writes facts to the server must carry it — a
host, a daemon, a command-line ingestor — so it is registered by the
binary (`recordstore.RegisterInsertTransform`), not passed to one host,
and the executors that reach a server or a clickhouse-local directory
consult the registry on each insert. A second registration for one table
panics: two transforms for one table would make which one ran depend on
the order of init.

A binary without it sends facts-shaped batches to `boxer.facts`. Where
that name is a deployment's view, the facts store finds at setup that it
takes no insert and fails setup
(`chstore.ErrFactsTableTakesNoInsert`); a host then falls back to the
in-memory store, as after any failed setup, and reports its trail as not
durable. `CREATE TABLE IF NOT EXISTS` over a view succeeds, so the DDL
alone would not tell.

A registration names the table with its database, and an insert matches it
under any spelling of that reference — quoted or not
(`recordstore.CanonicalTableRef`). An insert that names the table without a
database is refused (`recordstore.ErrUnqualifiedTransformedTable`): which
database a bare name resolves in is the server session's, so the registry
can neither match it nor rule it out. A facts-shaped table under another
name — a scratch database a command was pointed at — is another table and
is not transformed.

An insert handed to the ClickHouse client itself passes no executor and so
no transform; codelint `CS021` reports one outside the HTTP executor.

**SD3 — One read table per process, from one setting.** A registered
variable, `BOXER_FACTS_READ_TABLE`, names the relation every read of facts
goes to; empty means `boxer.facts`. It must be database-qualified: the
readers resolve the facts columns against the relation's database. The
registry resolves it once per process (`factsschema.ReadTable`,
`factsschema.ReadTableFor`), a host refuses to boot on a malformed value,
and it reaches everything that reads: the hand-written readers
(`chstore.Config.ReadTable`), the recorder and the watchbill presence,
which take it themselves when none is given, every generated store over
the facts table, the trail views, `keelsonddl --trail-views`, and the readers
that carried the name as a literal. This is ADR-0296 §SD9 (d) completed,
and serves a `Merge` over boxer-owned storage tables equally.

A store generated against the facts table asks for the setting in its
constructor when its configuration names no read table, so a consumer's
store follows it once regenerated, without its builder passing anything.
The generator does not decide this from a table's name: it takes a
read-table default as an input (`gen.Input.ReadTableDefault`), which the
facts-bound generator sets and no other store's generation does, so a
store that owns its table is emitted as before. What the caller names
wins; a store pointed at another table than `boxer.facts` reads that
table.

**SD4 — The read relation has the facts shape, and need not be a table.**
It returns every facts column under its name and with its type; the schema
check verifies exactly that and nothing about engine, layout or stamp,
which a view does not have (`recordstore.TableContract.ColumnsOnly`).
Columns it returns beyond the facts columns are tolerated, since reads
select by name. `schema plan` proposes no statement for such a relation.
A deployment may name its view `boxer.facts` and leave the variable unset.

A generated store judges the relation it reads the same way whenever that
relation is not the table it writes, or a transform sends the table's
inserts elsewhere (`recordstore.ReadRelationContract`). The other way
round, a table a store does write to is incompatible when it is a view, a
`Merge` or a dictionary, whatever its columns
(`recordstore.TakesNoInsert`).

**SD5 — With a transform registered, boxer issues no DDL for the facts
table and refuses what assumes one.** Setup does not create `boxer.facts`:
an empty table of that name beside the real storage is one that reads
drain from without an error. It still creates the facts database, which
the lease table and the trail views live in. `schema plan` proposes
nothing for it, and `keelsonddl` prints no table DDL. The statements that
change or empty stored rows by the facts table's name — the removal of
kept conversation text, `Truncate`, `DropTable` — refuse with
`recordstore.ErrTransformedTable`; the removal is recorded on the trail
as a failed ditch. The query-run view is not installed: it appends on the
server, where no transform runs, so its reconcile refuses, after dropping
a view an earlier binary left behind. Setup still
installs the SQL read surface, and provisions the id lease table unless
the store was given an allocator.

**SD6 — A round-trip check the deployment can run.** A command
(`boxer schema roundtrip --setup <script>`) writes one row of every kind
the runtime vocabulary registers through the transform into a scratch
table, reads each back through a scratch read relation, and compares every
column by row id (`chstore.RoundTrip`). The scratch is a clickhouse-local
directory the deployment's setup script — its carrier and its view — runs
in, so the check reaches no server. The kinds the hand-written store
writes come from its own encoders with every optional field set; every
other kind is a synthetic row with an attribute in each section. The
forward transform is the deployment's Go and the way back is its SQL;
this is what shows that the two agree, and that they still agree after a
change to the facts columns.

### Deferred

- The query-run capture under a transform: the service would serve
  transformed batches and the view would be composed against the
  deployment's table.
- A hook for the in-place statements of SD5, so that the removal of kept
  text can run against a deployment's storage. Until then the operator
  writes the statement for the carrier
  ([how-to](../howto/facts-in-your-own-table.md)), and it leaves no trail
  row.
- A copy of existing rows through the Go transform. Adoption on a server
  that holds facts copies them with SQL the deployment writes, which the
  round trip does not check.
- The IPC capture executor writes the store's own shape to its stream,
  whatever is registered: a stream has one schema, fixed when it is
  opened. Loading such a capture into a deployment's storage needs a
  loader that carries the transform, which is not built.
- The round trip's synthetic rows carry the kind and generic memberships,
  not the memberships each generated store writes; a transform that
  routes by one of those is exercised by the hand-written kinds only.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| record-store executors | `InsertTransformI`, the wrapping `TransformingExecutor`, `ErrTransformedTable`, `ErrUnqualifiedTransformedTable`, `WithTransformApplied` | the HTTP and clickhouse-local executors apply the registered transform; the hand-written facts store inserts through the former |
| registration | `RegisterInsertTransform`, one call a binary makes before stores are built; `CanonicalTableRef` | the executors boxer constructs |
| call observation | `CallEvent.Target` | — |
| codelint | `CS021`, an insert through the ClickHouse client outside the HTTP executor | `CODINGSTANDARDS.md` |
| schema verdict | `recordstore.ReadRelationContract`, `recordstore.TakesNoInsert`; `chstore.ErrFactsTableTakesNoInsert` at setup | the generated stores' `SchemaContract`, regenerated |
| test support | `chstoretest`, a reference carrier | — |
| env registry | `BOXER_FACTS_READ_TABLE` | `doc/env-vars.md` |
| facts store configuration, host boot | `chstore.Config.ReadTable`; the host refuses a malformed setting | the hand-written readers' composed SQL; the recorder and presence |
| record-store generator | `gen.Input.ReadTableDefault`, set by the facts-bound generator | the stores over the facts table, regenerated; their schema checks and scans |
| trail views, DDL command | `trailviews.ComposeFrom`; `keelsonddl --read-table` | — |
| setup, schema check, `schema plan` | no table DDL under a transform; `TableContract.ColumnsOnly`; `chstore.SchemaContracts` | the plan's output |
| removal of kept text, `Truncate`, `DropTable`, query-run view | refused under a transform | their callers' messages |
| a round-trip command | `boxer schema roundtrip`, `chstore.RoundTrip` | — |

## Alternatives

- **O1, O2, O3, O5** — see the assessment. O3 stays open to a deployment
  on its own: SD3 is all it needs from boxer.
- **Redirect the write path inside the database for boxer's own storage
  too — the switch of ADR-0296 §SD9.** Not taken, by the code owner on
  2026-11-21: §SD9 is an in-database answer to redirecting writes, and the
  redirection is now done in the record store, at the insert seam. Storage
  tables of boxer's own, should they come, would be a registered transform
  and a read table like a deployment's.
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
- **Have `storeexec.New` return the wrapping executor.** Rejected: it
  returns the concrete type, which callers hold for parameter binding, and
  a changed return type breaks every one of them.

## Consequences

### Positive

- A deployment holds every fact in one table of its own, written by one
  ordinary insert per batch.
- SQL written against `boxer.facts` keeps working where the deployment
  names its view so.
- A consumer's generated stores need no regeneration to be written
  through a transform, and one regeneration to follow the read table.
- The read-table half of ADR-0296 §SD9 is completed for every reader in
  the tree.

### Negative

- Only processes that carry the transform can write facts. A stock binary,
  a hand-written `INSERT`, and the query-run view cannot; where
  `boxer.facts` is a view their inserts fail, a host among them saying so
  at setup, and where the name is unused or a table of that name was left
  behind they land in a table nothing reads.
- The `boxer` database still holds state under a transform: the id lease
  table. It looks disposable — a view, the trail views — and is not. Setup
  recovers a lost lease row from the ids the read relation returns
  (ADR-0397 §SD2), which is a safety net, not a reason to drop it.
- An `INSERT` composed as SQL text and sent as a statement — an
  `INSERT … SELECT` among them — passes no insert seam. Neither the
  registry nor `CS021` sees it; it reaches whatever the name is on the
  server.
- A reader that composes its own SQL over the facts table reads the read
  table only if it asks for it; one in a consumer's code that names
  `boxer.facts` as a literal is found by nothing here, and reads the
  deployment's view only if that is what the name is.
- The two directions are in two languages and two places, and agree only
  as far as the round-trip check is run.
- The transform is coupled to the facts columns: a change to them is a
  change to the deployment's code and view.
- Reads go through the deployment's view, and what a time-bounded read
  costs is then the carrier's design: the
  [read-cost trial](../trials/facts-read-through-a-view/README.md) §0
  states under which carrier keys such a read prunes as it does on the
  facts table and under which it does not.
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
  runs `boxer schema roundtrip` from a binary that registers the
  transform, copies existing rows into its carrier, replaces
  `boxer.facts` with the view (or sets the variable), and ships binaries
  that register the transform. The order, and stopping writers for it:
  [how to keep facts in a table of your own](../howto/facts-in-your-own-table.md).
- **Regeneration.** The gen tests of the stores over the facts table; a
  consumer regenerates its own to have them follow the read table.
- **Old shape.** One table, read and written, stays the default.

## Verification plan — Tier 1

- **Lane.** Default `go test`: an insert into the facts table reaches the
  inner executor under the transform's table and schema, with its settings
  intact, through the wrapper, the HTTP executor and the hand-written
  store, under every spelling of the table, and once; a bare name that
  could be the table sends nothing; every reader of the hand-written store, found by reflection,
  composes against the read table and never the written one when one is
  set; the replay's and the trail views' SQL follow the read table; setup
  issues no facts DDL under a transform; the refusals of SD5; a contract
  judged by columns only. The `integration` lane, on clickhouse-local: a
  transform into a table with a different column set and a view back
  named as the read table, the round trip passing over every registered
  kind, failing on a view missing a column and on a transform that loses
  a value, and a re-sent transformed batch landing once. The same lane, on
  a server: setup failing over a view under the written name and passing
  under a transform; a leftover query-run view dropped; and, on a
  throwaway server only, since they use the runtime's own names, child
  hosts booted over a reference carrier (`chstoretest`) with the view
  named `boxer.facts` and with the view named by the setting, a host
  whose binary lacks the transform, a view lacking a column with and
  without `BOXER_SCHEMA_STRICT`, and a malformed setting.
- **What would fail.** A facts insert that bypasses the wrapper; a reader
  that returns rows from another relation than the read table; a `CREATE`
  for the facts table under a transform; a kind whose row does not survive
  the round trip.
- **Gap.** One server release, 26.9, and a reference carrier whose
  transform renames columns and adds one; no deployment's transform. The
  copy of existing rows and the operator's removal of kept text have no
  test.

## Choices accepted with the decision

Reviewed by the code owner on 2026-11-21 and accepted as listed.

- The transform is registered per process, not per host.
- Registering a transform is what tells boxer the storage is the
  deployment's; there is no second setting for it.
- The registry lives in `recordstore`, keyed by the database-qualified
  table reference and matched under any quoting of it, case kept.
  `RegisterInsertTransform` returns an unregister function, which a binary
  does not call and a test does.
- An insert naming a registered table's bare name without a database is
  refused, not resolved against a guessed session database.
- A transform applies once per insert, marked on the insert's context; a
  transform's output is never transformed again, so transforms do not
  chain.
- The HTTP and clickhouse-local executors consult the registry on each
  insert instead of their constructors returning a wrapper; the IPC
  capture executor does not, and holds the store's shape.
- `CallEvent` keeps `Table` as the table the store named and gains
  `Target` for where a transform sent the rows; an intent event carries no
  target.
- `CS021` covers the client's Arrow and stream inserts and is an error;
  the copy of stored blocks between servers is exempted by a directive.
- A stock binary on a deployment's server fails setup and its host falls
  back to memory, rather than refusing to start.
- A written table that is a view is `incompatible`, not a layout note.
- A reconcile under a transform drops the query-run view it finds, without
  asking.
- The tests that need the runtime's own names recognise a throwaway server
  by a marker database its start script creates.
- The transform takes no context and returns batches the caller releases.
- The read relation must return every facts column with its type; extra
  columns are tolerated, since reads select by name.
- `BOXER_FACTS_READ_TABLE` lives in `factsschema`, beside the table's
  coordinates, and must be database-qualified.
- The recorder and the watchbill presence take the read table from the
  setting themselves rather than from the host, so daemons and commands
  that build them without a host read the same relation; the host checks
  the setting at boot and refuses a malformed one.
- The read table setting applies to a store bound to `boxer.facts` only; a
  config pointed at a scratch table keeps reading the table it writes.
- A store generated against the facts table resolves the setting in its
  constructor and panics on a value it cannot use, as it does on a
  malformed table reference; a binary that wants an error instead checks
  `factsschema.ReadTable` first, as the host does at boot.
- The generator takes the default as an input rather than recognising the
  facts table, so the stores that own their tables cannot start following
  a setting meant for another.
- The trail views stay in the facts database and read the read relation.
- Under a transform setup still creates the facts database, for the lease
  table and the trail views.
- Under a transform the removal of kept text, `Truncate`, `DropTable` and
  the query-run view are refused, not redirected; the refused removal is
  recorded as a failed ditch, and the query-run reconcile returns an error
  rather than skipping quietly.
- `keelsonddl` under a transform refuses without `--trail-views`.
- The transform maps one insert to one insert into one table: no fan-out
  to several tables.
- The name `BOXER_FACTS_READ_TABLE`.
- The round trip is a `schema` subcommand that runs only on
  clickhouse-local; "every kind" is every `runtime-kind-*` membership of
  the runtime vocabulary, the ones the hand-written store does not write
  as synthetic rows.
- Callers that only write lading policy rows are not given the read table.
- Boxer does not take ADR-0296 §SD9's in-database switch for its own
  storage.

## Status

Accepted 2026-11-21. SD1–SD6 are built; what is left out is under
*Deferred*. The tests have run on clickhouse-local and on one ClickHouse
server release, against a reference carrier; no deployment has adopted it.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-11-22 — four of the deferrals built; the query-run capture stays deferred

- **Existing rows and IPC captures go through the seam.** `boxer schema
  load` (`chstore.LoadFacts`) reads facts-shaped rows from a relation — a
  facts table moved aside — or from an Arrow IPC stream and inserts them
  through the executor, so the registered transform applies to them. This
  replaces the SQL copy of *Migration* and is the loader the IPC capture
  lacked. Batches read from the server arrive in its Arrow rendering of
  the columns and are brought to the stores' schema by renaming types,
  values untouched. A load is not idempotent: it refuses a window that
  holds facts and checks the read relation afterwards.
- **A transform may answer the in-place statements.** A transform that
  also implements `recordstore.InPlaceTransformI` says what a statement
  that changes stored rows by the facts table's name is in the
  deployment's storage; the removal of kept text asks it
  (`trail.OpDitchBodies`), runs the answer and records the ditch. A
  transform that maps inserts only leaves SD5's refusal as it was.
  `Truncate` and `DropTable` stay refused either way.
- **The round trip carries the generated stores' rows.** A store package
  registers a writer of sample rows for the table it shares
  (`recordstore.RegisterSampleRows`), and SD6's check runs every writer
  the binary links: one entity per component, written through the store,
  under its own memberships. The values are samples, so a transform that
  depends on what a field says is still not exercised.
- **Still deferred: the query-run capture.** The view appends on the
  server and deduplicates by an anti-join over the destination's facts
  columns. Under a transform both would have to move: the service would
  serve transformed batches and drop the rows already stored itself, and
  the view would append them to the deployment's table as they come.

### 2026-11-22 — the read relation is also read once

SD4 judged the read relation by its columns as `system.columns` lists
them. The catalogue lists a view's columns whether or not a `SELECT` from
it is accepted, so a view could pass the check and fail every read: the
case met is a view returning the facts table's `LowCardinality` columns
over numbers, which a server refuses to produce without
`allow_suspicious_low_cardinality_types` (Code 455). A release run on the
pinned 26.8 build refused such a view at `CREATE VIEW`; a view that casts
to those types and was created in a session with the setting on is created
and then refused at each read, on 26.8 and 26.9 alike.

A relation whose engine computes its rows when read — a view, a Merge —
is now read once for no row when its facts are gathered
(`recordstore.ReadTableFacts`, `recordstore.ComputesOnRead`). A failed
read makes the verdict `incompatible` with the server's refusal as the
reason (`SchemaVerdict.ReadError`), and for the refusal above the reason
names the two remedies: the setting closing the view's `SELECT`, or in the
profile of the user that reads. The
[how-to](../howto/facts-in-your-own-table.md) shows the first as the
recipe and says when the second is needed. The probe is one statement per
such relation per check; a table is not probed.

A reader held at `readonly = 1` does not get the setting from the view: a
26.8 server, asked with `readonly = 1` on the request, answers from the
view with its settings left out, so the read is refused with Code 455 and
the profile is the remedy. `clickhouse local` refuses the same read
earlier, while loading the view.

### 2026-11-22 — the query-run capture under a transform

The last deferral is built, and SD5's sentence "the query-run view is not
installed" no longer holds. Under a registered transform the reconcile
installs the view, which appends the transform's output to the
transform's target. The deduplication this needs is recorded on
[ADR-0115](./0115-query-observability-data-plane-strategy.md) (update of
2026-11-22).

- **The target is found by asking the transform.** The reconcile encodes
  one query-run row as a pull would and transforms it
  (`recordstore.TransformInsert`). The table and the Arrow schema it gets
  back are what the view is composed against. The url() structure lists
  the returned columns, in their order, with the types the server reports
  for them in the target. A target column the transform does not return
  keeps its default. The target must be database-qualified, must take
  inserts (`recordstore.TakesNoInsert`), and must have a column for every
  column the transform returns.
- **Every pull is held to what was reconciled.** A pull transforms its
  rows the same way. A batch for another table, or in another schema,
  fails the pull, so the refresh fails and says so in
  `system.view_refreshes`, nothing is appended, and the events wait in
  `query_log`. The capture therefore supports a transform that sends
  every insert to one table. A transform that picks its table by what the
  rows say cannot be captured, and the failure is loud.
- **The capture reads through the read relation.** The watermark, the
  extract's filter of already-stored events and the check before the view
  is created all go to the read relation (SD3; `queryrunsvc.Config.ReadTable`,
  which follows `BOXER_FACTS_READ_TABLE` for `boxer.facts`). The check
  judges that relation as the schema check does (`chstore.SchemaContracts`):
  by its columns, and by the one read of the entry above.
- **Where the capture cannot be set up, the reconcile refuses.** That
  covers a transform that fails on the probe row or names no qualified
  table, a target that is missing, takes no insert or lacks a returned
  column, and a read relation that lacks a facts column. Each refusal
  wraps `recordstore.ErrTransformedTable`, after dropping a view an
  earlier binary left behind, as before.
- **Without a transform nothing changes.** The view, the extract and their
  SQL are as they were.

The `integration` lane checks this on one server release (26.9), with the
reference carrier registered as the transform
(`TestLivePipelineUnderATransform`). A stamped query lands once in the
carrier and is read back through the view. Re-served rows do not land
again, and the catch-up after the endpoint was down lands once. The
default lane covers the refusals and a pull for another table or schema.
Agreed with the code owner on 2026-11-22.

## References

- [ADR-0296](./0296-audit-events-as-a-trail-component.md) §SD8, §SD9 — the read table on generated stores and what keeps `boxer.facts` dispatchable.
- [The storage-table switch, background](../adr-background-work/facts-storage-table-switch-options.md) — the writers, the readers and what each inherits.
- [ADR-0295](./0295-recordstore-call-identity-observed-executors-and-write-observers.md) — the wrapping executor this one follows.
- [ADR-0387](./0387-what-a-binary-does-when-a-stored-table-is-older-or-newer-than-its-schema.md) — verdicts, reads by name, the retry token.
- [ADR-0397](./0397-the-facts-tables-partition-key-row-id-and-engine-clause.md) — the facts table's layout, and the store's configurable id source.
- [ADR-0264](./0264-retained-model-conversations-on-facts.md) §SD6 — the removal of kept conversation text.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — the query-run view.
- [Facts-bound record stores](../explanation/facts-bound-record-stores.md) — the two lanes that write the table.
