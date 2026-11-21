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
> release, by the `integration`-tagged tests of `hostboot` and `chstore`.
> The copy of existing rows in step 4 has no tool and has not been run.

# How to keep facts in a table of your own

A deployment that already keeps its records in a carrier table can have
that table hold every fact the runtime writes, so that there is one place to
query, retain and delete. The decision and its limits are
[ADR-0402](../adr/0402-facts-stored-in-a-deployments-own-table-a-write-transform-at-the-insert-seam-and-one-read-table-setting.md);
this page is the order of work.

It costs something. Removing kept conversation text and capturing query
runs stop working (see [What stops working](#what-stops-working)), every
binary that writes facts has to carry your transform, and the mapping
exists twice — once in Go, once in SQL.

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
Its rows for the kinds the generated stores write are synthetic: a
transform that treats rows differently by a membership those stores write
is not exercised by it.

## Adopt it on a server that already holds facts

1. **Ship nothing yet.** Build the binaries with the transform and pass the
   round trip.
2. **Stop every process that writes facts** — hosts, workers, the query-run
   daemon, anything with `BOXER_LOG_FACTS` set. A row written during the
   copy stays behind in the old table.
3. **Create the carrier**, and the view under a name that is not yet
   `boxer.facts`.
4. **Copy the existing rows** with an `INSERT … SELECT` from `boxer.facts`
   that states your mapping in SQL, and compare counts. Nothing in this
   repository does the copy through the Go transform; the round trip checks
   the transform against the view, not this statement.
5. **Move the old table aside and put the view in its place** — or leave
   the name unused and set `BOXER_FACTS_READ_TABLE`:

   ```sql
   RENAME TABLE boxer.facts TO boxer.facts_before;
   ```

   Do not leave a table named `boxer.facts` beside the carrier: a binary
   without the transform would write into it, unseen.
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
   surface. The schema check judges the view by its columns; a column it
   lacks or returns under another type is reported at start and in
   `keelson('schema_tables')`, and fails the start under
   `BOXER_SCHEMA_STRICT`.

A query-run view installed before the switch is dropped by the first
start of the query-run service under the transform. Where that service
does not run, drop it by hand: `DROP TABLE IF EXISTS boxer.mv_queryruns`.

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

- **Removing kept conversation text.** The removal is refused and recorded
  on the trail as a failed ditch. To remove the text, write the statement
  for your carrier: `trail.DitchBodiesSQL` composes the one boxer runs
  against a facts table, and shows which section it empties and which rows
  it selects. Run by hand, it leaves no row on the trail.
- **Query-run capture.** The view that appends query runs is not
  installed: it inserts on the server, where no transform runs.
- **`Truncate` and `DropTable` of the facts store**, which are test
  helpers, are refused.
- **A capture to an Arrow IPC stream** holds the facts shape, not your
  table's; loading one into the carrier has no tool.

## Checking the result

- The first lines of a host's start name the facts backend as ClickHouse,
  and `keelson('schema_tables')` lists the read relation as `ok`.
- `SELECT count() FROM boxer.facts` (or your view) grows as the host
  writes, and `system.tables` shows no MergeTree named `boxer.facts`.
- Row ids written after the switch carry the leased stream's tag; an id
  with the process-local tag means a lease failed, which the log names.
