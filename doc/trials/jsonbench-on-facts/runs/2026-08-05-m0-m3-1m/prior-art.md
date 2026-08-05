---
type: reference
audience: package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Prior art: the same benchmark, done differently

A sibling experiment outside this repository had already recreated JSONBench
on leeway before this trial ran. Reading it corrects three of this run's
findings and supplies two techniques the trial should have used. Recorded here
because the corrections are load-bearing, not because the comparison is
interesting on its own.

## The two constructions

| | Sibling experiment | This trial |
| --- | --- | --- |
| Target table | the canonical leeway JSON mapping (`mapping.NewJsonMapping`) | the boxer.facts DDL |
| Membership channel | verbatim — `lmv` is `Array(LowCardinality(String))` | Ref + params — `mrhp` is `Array(String)` |
| Path → value | `value[indexOf(lmv, '/path')]` | two-level `arrayCumSum` reconstruction |
| Sort key | the five extracted backbone expressions | `ts`, the live store's default |
| Second variant | `MATERIALIZED` columns for the backbone | data-skipping bloom filters |
| Ingest | a CLI shredder → Arrow IPC file → `INSERT … FORMAT Arrow`, 6-way parallel | in-process shredder → Arrow batches → `InsertArrow` |
| Result capture | `log_comment` JSON → `system.query_log` → dashboard | `timings.tsv` in the run dir |

Both run 3 tries per query with a page-cache drop between queries.

## What this corrects

**1. "No JSON→leeway shredder exists" — overstated.** One exists, as a CLI
subcommand outside this repository, and it emits exactly the canonical
mapping. What is true is narrower and still worth carrying: *boxer* has none —
its `mapping` package exports schema constructors only — so a boxer-side
consumer must write one, as this trial did. Restated in the logbook.

**2. The two-level reconstruction was mostly self-inflicted.** In the
canonical mapping every attribute carries exactly one membership, so `lmv`
co-indexes 1:1 with `value` and a plain `indexOf` resolves a path. This
trial's encoding broke that co-indexing twice over: the kind tag rides
`lr` (contributing zero `lmr` entries) and array indices ride a *second*
`lmr` membership. Both were choices, not constraints.

What is *not* self-inflicted is the other half. facts' string and integer
sections are array-valued (`stringArray`, `i64Array`), so their value columns
are flattened across attributes and need the `len` cumulative sum regardless.
The canonical mapping uses scalar sections and has no such indirection. So the
finding survives for facts, at half its claimed size.

**3. "No accelerated SQL read path exists" (filed S1) — wrong.** The sibling
experiment's second variant is precisely that path:

```sql
ALTER TABLE … ADD COLUMN commit_collection LowCardinality(String)
  MATERIALIZED value[indexOf(lmv, '/commit/collection')];
ALTER TABLE … MATERIALIZE COLUMN commit_collection;
```

Path resolution moves from per-row-per-query to once per part at merge time.
Measured on this run's own arm B data, Q1:

| Formulation | Time | Peak memory | Added storage |
| --- | --- | --- | --- |
| Reconstruction (arm B as run) | 0.073 s | 194 MB | — |
| `MATERIALIZED` column | **0.008 s** | **4.7 MB** | 535 KiB |
| *(arm A, for reference)* | 0.005 s | 2.5 MB | — |

**1.6× arm A's time and 1.9× its memory, for 0.36 % more storage** — against
13.8× and 76× for the same query as this trial ran it. The tax this trial
reported is very largely an artefact of how it asked, not of what it stored.

## The technique this trial missed entirely

The sibling DDL sorts by the extracted expressions rather than by `ts`:

```sql
ORDER BY (
  value[indexOf(lmv,'/kind')]              AS kind,
  value[indexOf(lmv,'/commit/operation')]  AS commit_operation,
  value[indexOf(lmv,'/commit/collection')] AS commit_collection,
  … AS did,
  fromUnixTimestamp64Micro(…)              AS time_us)
```

This does two things at once: it reproduces the reference entry's clustered
index — so the primary key prunes, which §4's hypothesis correctly predicted
it would not under `ORDER BY ts` — and it binds `kind`, `commit_collection`,
`did` as names the queries then use directly. That is arm D, already designed;
it should be run against the same corpus rather than re-invented.

## Where this trial's construction is the stronger one

The sibling reference arm is *not* upstream's pinned DDL: it drops
`max_dynamic_paths = 0`, uses `CODEC(ZSTD(3))` where upstream uses `ZSTD(1)`,
and omits the four serialization settings. Its leeway-vs-reference ratios are
therefore internally consistent but not comparable with the published
JSONBench numbers. This trial's arm A is the pinned DDL byte for byte
([`../../upstream/PIN.md`](../../upstream/PIN.md)) and passed the M0 ordering
gate against the published results, so its denominator is the real one.

Two caveats on the sibling files as committed, noted so a later reader does
not trust them blindly: the non-materialized Q4 filters `WHERE did = 'commit'`
where it means `kind = 'commit'`, and the physical column names in the query
files have drifted from the naming convention the DDL generator now emits, so
the queries as committed will not bind against a freshly generated table.

## Harness techniques worth adopting

- **`clickhouse format --oneline -n`** normalises a readable multi-statement
  file into one-statement-per-line before the runner loop. This trial's
  `measure.sh` requires one statement per line and so
  [`queries-facts.sql`](../../queries-facts.sql) is committed as unreadable
  2,000-character lines. Adopting this would let the queries be formatted.
- **`log_comment` carrying a JSON tag** (`runid`, `tech`, `query`) makes
  `system.query_log` the result store, queryable after the fact. That is
  materially what M5 ("results as facts, read back through an applet") asks
  for, and it is a smaller step from here than building a fresh facts writer.
