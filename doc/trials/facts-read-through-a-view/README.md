---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** One run on one machine; not
> replicated and not reviewed. Do not cite as authoritative.

# Facts read through a view — what a read costs when the facts live in a deployment's own table

## 0 The claim, and how to cite it

**A time-bounded read of facts through a view reads as few rows as the same
read on the facts table when the carrier table is partitioned and ordered by
the very column the view returns as the facts timestamp; it reads every row
of the tenant when the carrier's key does not hold that column, and a whole
month when the view computes the timestamp from a column of another type.**
Measured once, on one machine, at 2.0 million facts in twelve monthly
partitions (ClickHouse 26.9, five repetitions per read, the query condition
cache cleared before each):

| The facts store's one-hour log read, against … | arm | rows the server read |
| --- | --- | --- |
| the facts table, `PARTITION BY month ORDER BY ts` | `table` | 16,370 |
| a view over a carrier `PARTITION BY month ORDER BY (tenant, ts)` | `view-keyed` | 12,012 |
| a view computing `ts` from an `Int64` of nanoseconds, carrier keyed by that `Int64` | `view-computed-ts` | 172,990 |
| a view over a carrier `ORDER BY tenant` only — the carrier design this trial includes as the mistake | `view-tenant-key-only` | 2,006,481 |

Each carrier holds the facts once more under a second tenant, which the
view leaves out; all four arms returned the same 232 rows.

**What this trial does not say.**

- **Not "a view adds N milliseconds."** Durations were recorded, on a
  machine busy with other work, and differ between `table` and `view-keyed`
  by a few milliseconds in either direction across reads. The claim is
  about rows read, which the plans in the run directory explain; no
  duration from this trial is a figure to quote.
- **Not "a read by row id is cheap."** A read by id alone read every row in
  every arm — 2,000,357 in arm `table`, the facts table itself — because no
  arm's key leads with the id (ADR-0397 keeps `ts` as the sorting key).
  Bounded by the row's day it read 16,376 rows in arm `table` and 12,012 in
  arm `view-keyed`.
- **Not "the 172,990 of `view-computed-ts` is what a view costs."** It is
  what a view costs when its timestamp is an expression: the partition key,
  written with the same expression, still selects the month, and the
  primary key is not used within it.
- **Not a statement about writes**, the insert transform's cost, or reads
  the facts store does not make.
- **Not replicated.** One machine, one run, one ClickHouse release, one
  row shape (log rows and a few heartbeats), two tenants of equal size.

**If you need a number**, take it from `cost.tsv` in the run directory the
[logbook](./logbook.md) names, which holds every repetition of every read
on every arm. **No figure from this trial travels without the pair of arms
it compares.**

## 1 Question and scope

[ADR-0402](../../adr/0402-facts-stored-in-a-deployments-own-table-a-write-transform-at-the-insert-seam-and-one-read-table-setting.md)
lets a deployment keep facts in a table of its own and point every read at
a view that returns the facts columns. Its consequences leave one thing
open: whether a time-bounded read still prunes by the carrier's key. That
is this trial's question, and here the domain question leads; the toolbelt
side is small — the reads run through the facts store's own reader and the
record-store executor, against the reference carrier of
`chstore/chstoretest`.

In scope: rows read, parts and marks selected, for time-bounded reads, reads
by row id and reads by kind. Out of scope: write cost, the transform,
concurrency, more than two tenants, other ClickHouse releases.

## 2 The workload

`FRV_ROWS` log rows (2,000,000 unless set), written through
`chstore.Store.WriteLogs` in blocks of 10,000, spread evenly over the twelve
months ending November 2026, one month per block; and one heartbeat row per
day, 365 in all, as a rare second kind. Ids are leased as the store leases
them.

Six reads, each answered identically by every arm (the harness fails
otherwise):

| read | what it is |
| --- | --- |
| `logrows-one-hour`, `logrows-one-day` | `chstore.Store.LogRows` with `Since` and `Until` |
| `point-by-id` | `count()` where the row id equals one row's |
| `point-by-id-within-its-day` | the same, bounded by that row's day |
| `kind-all-time` | `count()` of the rows carrying the heartbeat kind's membership |
| `kind-one-month` | the same within the latest month |

## 3 System under test and arms

| arm | stores the rows in | read through |
| --- | --- | --- |
| `table` | the facts table in the layout of ADR-0397 | the table |
| `view-keyed` | a carrier with every facts column renamed and a `tenant` column, `PARTITION BY toYYYYMM(ts) ORDER BY (tenant, ts)` | a view selecting one tenant under the facts columns' names |
| `view-tenant-key-only` | the same carrier `ORDER BY tenant`, unpartitioned | the same view |
| `view-computed-ts` | the keyed carrier with the timestamp as `Int64` nanoseconds, partitioned by the month of `fromUnixTimestamp64Nano` of it | a view returning `fromUnixTimestamp64Nano(…)` as the timestamp |

`view-tenant-key-only` measures a carrier whose key was chosen without the
facts' reads in mind; it is the mistake arm. Hypotheses a run confirms or
refutes: the view's filter and the reader's predicates reach the carrier's
keys (H1); an expression in place of the timestamp column loses the primary
key and keeps the partition key (H2).

## 4 Method

- **Environment.** A throwaway server from
  `scripts/ci/clickhouse_server.sh`; version, thread count, CPU and memory
  are captured into the run's `environment.md`.
- **Per run:** `tables.tsv` (rows, parts, partitions, bytes and keys per
  arm), `cost.tsv` (one line per read, arm and repetition, from
  `system.query_log`: rows and bytes read, parts and marks selected,
  duration), `plans.txt` (`EXPLAIN indexes = 1` of a one-day and a by-id
  read on each arm).
- **The query condition cache is cleared before every repetition.** It
  remembers which granules a predicate matched, so a repeated read is
  cheaper than the keys alone make it; the question here is the keys.
- **Idiom rule.** The `logrows-*` reads go through the facts store's
  reader; the others are SQL through the record-store executor. Nothing
  bypasses the view.
- **Reporting.** `cost.tsv` is the result; this page quotes from it only in
  §0. Every run appends a [logbook](./logbook.md) entry.

Run it with [`measure.sh`](./measure.sh).

## 5 Findings ledger

Per the [directory convention](../README.md). Pre-registered candidates: a
view's column expressions defeating key analysis; the reader's SQL naming
the timestamp in a form a view cannot push down.

## 6 Milestone cut

- **M0 — one release, two tenants.** Done by the first run. Gate: every arm
  answers every read identically; H1 and H2 decided from `plans.txt`.
- **M1 — a carrier with many tenants and a skewed one**, and a second
  ClickHouse release.

## 7 Open questions

1. Whether a carrier that must keep the timestamp in another type can
   regain the primary key with a key expression the view's column is
   written as; the first run tried only the partition key that way.
2. What a projection or a skip index on the row id would cost and give for
   the by-id read, in the facts table and in a carrier.

Related: [ADR-0402](../../adr/0402-facts-stored-in-a-deployments-own-table-a-write-transform-at-the-insert-seam-and-one-read-table-setting.md),
[ADR-0397](../../adr/0397-the-facts-tables-partition-key-row-id-and-engine-clause.md),
[when a SQL UDF can prune the primary key](../../explanation/clickhouse-udf-primary-key-pruning.md).
