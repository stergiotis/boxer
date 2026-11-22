---
type: how-to
audience: developer of a deployment that keeps its records in a ClickHouse table of its own
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** No deployment has adopted this. The
> steps were run against the reference carrier of
> `public/keelson/runtime/factsstore/chstore/chstoretest`, on one ClickHouse
> release, by the `integration`-tagged tests of `hostboot`, `chstore` and
> `trail`.

# How to keep facts in a table of your own

A deployment that already keeps its records in a carrier table can have
that table hold every fact the runtime writes, so that there is one place to
query, retain and delete. The decision and its limits are
[ADR-0402](../adr/0402-facts-stored-in-a-deployments-own-table-a-write-transform-at-the-insert-seam-and-one-read-table-setting.md);
this page is the order of work.

It costs something. Removing kept conversation text stops working unless
your transform answers it (see
[What stops working](#what-stops-working)); every binary that writes facts
has to carry your transform; and the mapping exists twice — once in Go,
once in SQL.

## What you write

**A transform, in Go.** It receives the facts-shaped record batches of one
insert and returns your table's name and the batches in your table's
schema (`recordstore.InsertTransformI`). It must be a pure function of its
input and do no I/O. Register it before anything builds a store, in every
binary that writes facts — hosts, workers, daemons, command-line ingestors:

```go
recordstore.RegisterInsertTransform(factsschema.QualifiedTableName, transform)
```

The `chstoretest` carrier is a transform small enough to read in one
sitting.

**A view, in SQL.** It returns every facts column under its name and with
its type; columns beyond those are ignored. `boxer keelsonddl` prints the
facts table's DDL, which is where the names and types come from.

Close the view's `SELECT` with the setting the facts table is created
under:

```sql
CREATE VIEW mydb.facts_view AS
SELECT … FROM mydb.carrier
SETTINGS allow_suspicious_low_cardinality_types = 1;
```

Several facts columns are `LowCardinality` over a number, a type ClickHouse
refuses to produce unless that setting is on (Code 455,
`SUSPICIOUS_TYPE_FOR_LOW_CARDINALITY`). Without it the `CREATE VIEW` is
refused — or, when the session that created the view had the setting on,
every later read from another session is. The setting inside the view
travels with it and needs nothing from the server's configuration. It does
not help a user held at `readonly = 1`, who may not change a setting even
through a view; for such a user set it in the user's settings profile
instead, which also covers a view written without the clause. Observed with
`clickhouse local` 26.8 and 26.9, not against a server with a `readonly = 1`
user.

**The carrier's key.** Partition and order the carrier by the column your
view returns as the facts timestamp, as stored, not by an expression of it.
The [read-cost trial](../trials/facts-read-through-a-view/README.md) states
under which carrier designs a time-bounded read through the view still
prunes; quote its §0, not this sentence.

**Which name the view gets.** Either name it `boxer.facts`, so that SQL
written against that name keeps working and nothing is configured, or name
it anything and set `BOXER_FACTS_READ_TABLE` for every process.

## Check that the two directions agree

Run the round trip from a binary that registers your transform, with a
script that creates your carrier and your view:

```sh
your-binary schema roundtrip --setup carrier.sql
```

It writes one row of every kind of the runtime vocabulary through the
transform into a clickhouse-local scratch directory, reads each back
through the view and compares every column. It reaches no server. Run it
again after every change to the facts columns, the transform or the view.

Beside the runtime kinds it carries one entity per component of every
generated store over the facts table that the binary links, written
through that store, so the memberships those stores select by are there.
A store of your own over the facts table joins in by registering a writer
(`recordstore.RegisterSampleRows`, with `recordstore.IngestSamples` doing
the writing). The rows hold sample values: a transform that depends on
what a field says, rather than on which membership it sits under, is not
exercised.

## Adopt it on a server that already holds facts

1. **Ship nothing yet.** Build the binaries with the transform and pass the
   round trip.
2. **Stop every process that writes facts** — hosts, workers, the query-run
   daemon, anything with `BOXER_LOG_FACTS` set. A row written during the
   copy stays behind in the old table.
3. **Create the carrier.**
4. **Move the old table aside and put the view in its place** — or leave
   the name unused and set `BOXER_FACTS_READ_TABLE`:

   ```sql
   RENAME TABLE boxer.facts TO boxer.facts_before;
   ```

   Do not leave a table named `boxer.facts` beside the carrier: a binary
   without the transform would write into it, unseen.
5. **Load the existing rows through your transform**, from a binary that
   registers it:

   ```sh
   your-binary schema load --from-table boxer.facts_before --yes
   ```

   The rows take the path every later write takes — the executor, your
   transform, your table — and keep their ids. The command refuses a
   window that already holds facts, and afterwards checks that your view
   shows the rows it inserted. It is not idempotent: load a large table
   month by month with `--since` and `--until`, so that a failure leaves
   one window to clear and repeat, and drop `boxer.facts_before` only
   when the counts agree.
6. **Leave `boxer.idlease` where it is.** The row ids the facts store mints
   are leased through it (ADR-0397 §SD2), and it is the only place the
   next unleased id of a stream is kept. The `boxer` database is not
   disposable for that reason, even when everything else in it is a view.
   Setup recovers a lost lease row by reading the largest id through your
   view, which is a safety net: it reads the whole id column, and it does
   not notice a lease table restored from an older backup than the facts.
   A deployment with an id authority of its own configures it instead
   (`chstore.RowIds`, `hostboot.Options.FactsRowIds`).
7. **Start the deployment's binaries.** Setup creates no facts table and
   still creates the `boxer` database, the lease table and the SQL read
   surface. The schema check judges the view by its columns and by one
   read of it that returns no row; a column it lacks or returns under
   another type, or a read the server refuses, is reported at start and in
   `keelson('schema_tables')`, and fails the start under
   `BOXER_SCHEMA_STRICT`.

**Query-run capture** (`queryrunsd`) needs the transform registered in its
binary too. At start it transforms one query-run row to learn your table
and the columns you write, and installs a view that appends the
transform's output to that table. It reads stored query runs through your
view, which must show a committed insert to the next query: a plain view
or a `Merge` over your table does; a copy refreshed on a schedule does
not, and the capture then stores events more than once. The capture
supports a transform that sends every insert to one table; one that picks
the table by what the rows say fails each pull, which
`system.view_refreshes` shows. Where the capture cannot be set up, its
start fails and names the reason, after dropping a query-run view an
earlier binary left behind. Where the service does not run, drop such a
view by hand: `DROP TABLE IF EXISTS boxer.mv_queryruns`. The design is in
[ADR-0115](../adr/0115-query-observability-data-plane-strategy.md), update
of 2026-11-22.

## What a binary without the transform does

On a server where `boxer.facts` is your view, a host built without the
transform finds at setup that the name takes no insert, falls back to the
in-memory facts store as it does after any failed setup, and reports its
trail as not durable. It writes nothing to your table. Where the name is
unused, the same binary creates an empty `boxer.facts` and writes there;
that is the case step 5 is about.

In your own code, insert through a record-store executor
(`storeexec.New`), never through the ClickHouse client's own insert calls:
only the executors apply the transform. An `INSERT` typed against
`boxer.facts` fails when the name is a view.

## What stops working

- **Removing kept conversation text**, unless your transform answers it.
  Implement `recordstore.InPlaceTransformI` on the transform and return,
  for `trail.OpDitchBodies`, the statement that empties the text section
  of the message rows in your table; it is handed the statement boxer
  composes against a facts table (`trail.DitchBodiesSQL`), which shows the
  section and the rows meant, and the app filter as an argument. The trail
  then runs yours and records the ditch. Without it the removal is refused
  and recorded as a failed ditch.
- **`Truncate` and `DropTable` of the facts store**, which are test
  helpers, are refused.
- **A capture to an Arrow IPC stream** holds the facts shape, not your
  table's. Load one with `schema load --from-stream`, which sends its
  batches through your transform.

## Checking the result

- The first lines of a host's start name the facts backend as ClickHouse,
  and `keelson('schema_tables')` lists the read relation as `ok`.
- `SELECT count() FROM boxer.facts` (or your view) grows as the host
  writes, and `system.tables` shows no MergeTree named `boxer.facts`.
- Row ids written after the switch carry the leased stream's tag; an id
  with the process-local tag means a lease failed, which the log names.
