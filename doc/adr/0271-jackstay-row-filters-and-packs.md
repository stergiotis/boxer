---
type: adr
status: proposed
date: 2026-10-02
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0271: jackstay — per-table row filters, and packs that carry a sync between hosts

## Context

[ADR-0259](./0259-jackstay-guided-clickhouse-to-clickhouse-sync.md) syncs
whole tables, or a hash sample of them, from one server to another. The tool
relays each chunk and needs both servers reachable at the same time. Two cases
fall outside that:

- **A subset by content.** An operator wants one tenant's rows, a recent
  window, or the rows of one source system: a `WHERE` per table, not a
  fraction of keys.
- **No route between the servers.** The target sits on a host the source
  cannot reach, and neither can a machine that reaches both. The rows have to
  travel as files and be ingested over there.

ADR-0259 planned the second case as milestone M6, a pack-file target: "a tar
holding a manifest and one `Native` stream per chunk … reuses the relay
unchanged, with a file standing in for the target server". This ADR settles
its shape and supersedes that sketch.

## Decision

We will add a **row filter** to each plan table, applied the same way on
both sides, and a **pack**: a directory that an `export` writes from a source
server. A later plan can name the pack as its source in place of a server.

### SD1 — A row filter defines a slice, the same on both sides

The operator gives a filter per source table as a ClickHouse boolean
expression. It is stored in `Selection.Filters`, keyed by the source table,
because a re-check rebuilds every plan table from the selection. Each plan
table records the filter it was planned with (`PlanTable.Filter`).

`DigestSpec.Filter` carries the filter into every query on **both** sides:
digests, chunk lists, row pairs, the relayed `SELECT` and the target's
`DELETE`. The unit of comparison and copying is the **slice**, the rows that
satisfy the filter. A sync makes the target's slice equal to the source's
slice and never reads, counts or clears a target row outside it. Several
plans with disjoint filters can therefore share one target table.

What this implies for ADR-0259 §SD5:

- **Ownership** is decided on the target's slice. A run owns the slice when
  it was empty, or under `replace`.
- **Clearing** under a filter is always a lightweight
  `DELETE … WHERE <filter> AND <chunk> [AND <leaves>]`. `DROP PARTITION`
  and `TRUNCATE` would also remove rows outside the slice, so they are not
  used. A target outside the MergeTree family cannot delete a subset of rows,
  so a filtered sync into one is refused.
- **Resume.** The journal's start entry records the filter, and a run
  resumed under a different filter is refused, as a mode change is.
- **Carry-over.** A diff is not carried across a filter change, because its
  digests counted other rows. The chunk layout is carried, since bounds that
  were sampled over the whole table are still correct for a slice.

**Validation.** A filter is parsed with the nanopass parser and refused when:

- it is not a single expression on its own text. Every query wraps the
  filter in parentheses and joins it to a chunk or leaf predicate with
  `AND`, so a filter whose parentheses close early (`1) OR (1`) would lift
  that predicate off the target's `DELETE`. Parentheses must balance outside
  literals, the parsed expression must span the whole text, and comments are
  refused;
- it references a column outside the copy column list (the target must be
  able to evaluate it). A lambda's parameters count as names only inside
  its body;
- it calls a function whose value depends on when or where it runs
  (`now`, `today`, `rand*`, `generateUUID*`, server and connection
  identity, …) or that changes the row count (`arrayJoin`);
- it reads a table.

A filter that compares against a date or time literal, or calls a date
function without a timezone argument, gets a note: either is read in each
server's timezone, and the two servers may differ.

The same validation runs on a filter a plan file carries and on one a pack
manifest carries, so neither reaches a server unchecked. A pack's filter may
also name the sorting key's columns, stored or MATERIALIZED, since a sampled
export's predicate hashes the key.

### SD2 — A pack is a directory: a manifest and one compressed Native file per chunk

`jackstay export --pack DIR` takes the selection, filters and optional sample
of a structure step. It runs against the source alone and writes:

- `manifest.json`, which is versioned. It holds the source's server identity,
  the exported databases and, per table, the source's `TableInfo` (columns,
  keys, `create_table_query`), the effective filter, the copy columns, the
  chunk layout and the chunks. Each chunk entry records its file, size,
  SHA-256, content encoding, and the leaf digests (ADR-0259 §SD4) of the rows
  the file holds.
- One file per chunk: the source's `SELECT … FORMAT Native` response body,
  stored exactly as the server sent it. With `zstd` that is the compressed
  body, and nothing is decoded on the way to disk.

A sample is folded into the effective filter at export time
(`<filter> AND <sample predicate>`). A pack then holds a slice like any other.

**Verification and resume.** For each chunk, the export reads the source
chunk's leaf digests, streams the chunk to a temporary file while hashing it,
and reads the digests again. If they are equal, it renames the file into
place and rewrites the manifest atomically. Otherwise it retries a bounded
number of times. The manifest is the export's journal: a re-run skips a
recorded chunk while the source digest still matches, and replaces it
otherwise. A re-run with a different selection, filter, sample or chunk
layout is refused unless it is asked to restart. Once every table is done,
the manifest is marked complete.

The premise is the same one the relay's retry already rests on: a source
whose digest did not move between the two reads served the rows those
digests describe. Nothing decodes the stream to check it.

**A directory, not a tar.** A directory lets each chunk be written and
renamed on its own, so an interrupted export resumes chunk by chunk. A tar
could only be appended to and would have to be scanned on import. The
operator moves the directory however they like (`tar`, `rsync`, a disk).

### SD3 — The pack stands in for the source server

The engine's source side becomes a sealed interface, `SourceI`, with two
implementations: a ClickHouse server (`ServerSource`) and a pack
(`OpenPack`). A plan's source `Endpoint` names either a URL or a pack
directory. Every step reads the source only through `SourceI`, so on the
second host `structure → apply-ddl → diff → sync` run unchanged against a
pack:

- **structure** takes the source inventory from the manifest. The pack's
  `create_table_query` proposes the DDL, and the pack's filter becomes the
  plan table's filter. A different filter given at this step is refused,
  because the pack holds no other rows.
- **diff** compares the target's slice against the leaf digests in the
  manifest. No row pairs can be read from a pack, so differing leaves stay
  unresolved unless one side is empty.
- **sync** relays each chunk file into the target as its `INSERT … FORMAT
  Native` body, declaring the file's content encoding. Before reading a file
  it checks the file's SHA-256 against the manifest, so a damaged transfer
  fails before any row lands. Verification is ADR-0259's arithmetic against
  the manifest's digests. The journal lives beside the plan on the second
  host, as before.

A pack is read only when its manifest is marked complete. An incomplete pack
would let `replace` clear target chunks the export never reached.

The pack's layout is fixed. Every read checks that the plan's chunk layout,
copy columns and filter equal the manifest's, and refuses the read
otherwise.

### SD4 — What a pack cannot serve, and the light cut

A pack holds whole chunks, and it can only be read as written. So the
following are refused on a pack source, with the reason given:

- `repair`, which copies a subset of leaves;
- `sample` at import (sample at export instead);
- `--final`, which needs the source's merged rows.

A merge-semantics table whose rows collapse on the target during import
fails verification. On a server source, the FINAL comparison would accept it
(ADR-0259 §SD5); a pack has no FINAL side to compare.

**Front ends.** The CLI gains `export`, a `--pack` source on `structure`,
and `--filter` on `structure` and `export`. The wizard gains the filter
field on its Structure page. Export and import pages in the wizard are
deferred: the wizard writes files only through the fs broker, whose
interface carries whole files, not multi-gigabyte streams.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| jackstay engine API | source parameters of the workflow functions, `DiffTable`, `SyncTable` and `RunSync*` change from `QueryI` / `ClientI` to `SourceI`; added `ServerSource`, `OpenPack`, `Export` | the CLI, the wizard and the package's tests |
| plan JSON | added `Selection.Filters`, `PlanTable.Filter`, `Endpoint.Pack`; format version unchanged, because the fields are optional and an older plan reads as unfiltered | none |
| journal JSON | added `filter` on start entries | none |
| pack manifest JSON | new, versioned | none outside the package |
| CLI | added `jackstay export`; `--pack` and `--filter` on `structure` | none |

## Alternatives

- **Filter on the source only.** The target would be compared whole against a
  filtered source. Every target row outside the filter would read as extra,
  and `replace` would delete it. A filter would then also be a delete
  instruction for rows the operator never looked at.
- **Removing the target's rows outside the filter under `replace`.** This
  gives the target "equals the filtered source" semantics. It was rejected
  for the same reason: it deletes rows the plan never compared, and it rules
  out disjoint filtered plans into one table.
- **A dedicated `import` that loads a pack without the plan steps.** Less
  code would move, but the diff and the structure step's DDL would not work
  against a pack, and the importer would need a journal of its own.
- **`BACKUP` / `RESTORE` or `INTO OUTFILE`.** These have the costs ADR-0259
  gives (backup disks on both servers, no subset). `INTO OUTFILE` also
  writes on the server's host, not the operator's.
- **Decoding the stream at export to digest the rows written.** This would
  give byte-level proof that the file holds what the digest says, at the
  cost of a decode the relay was designed never to need. The before/after
  digest is the relay's own premise.
- **A tar pack.** See §SD2.

## Consequences

### Positive

- One flag gives a content subset, and the diff, the sync and the
  verification all see that subset.
- Disjoint filtered plans can fill one target table without clearing each
  other's rows.
- A sync can cross an air gap. On the second host it keeps structure
  proposals, a diff against what the pack holds, digest verification and
  resume.

### Negative

- A filtered clear is a lightweight `DELETE` where an unfiltered one could
  drop a partition. It is slower, and its space returns only when the parts
  merge.
- An export holds an extra copy of the selected data, compressed, on the
  exporting machine's disk.
- Importing from a pack has no leaf-level repair. A differing table is
  re-synced under `replace`, which re-copies every chunk of the slice.
- The validator's list of nondeterministic functions is a list. A function
  missing from it passes, and the run's digests then disagree.

### Neutral

- The sampled-key predicate of ADR-0259 §SD5 is unchanged. A sample at
  export is that predicate, frozen into the pack's filter.

## Migration — Tier 1

A plan or journal written before this change has no filter fields and reads
as unfiltered. Callers of the engine wrap their source client in
`ServerSource`.

## Verification plan — Tier 1

- **Default lane.** Filter validation, the composition of `Filter` into every
  query, the filtered `clear` (never `DROP PARTITION` or `TRUNCATE`), the
  manifest round trip, and the pack's refusals of mismatched layouts are
  unit-tested without a server. The scripted fake client drives a filtered
  sync's ownership and resume.
- **Integration lane.** On a seeded server: a filtered sync must leave a
  target row outside the filter untouched under `replace`. An
  export → structure → apply-ddl → sync from the pack → diff must report
  every chunk identical. A pack file with a corrupted byte must fail before
  insert.

## Status

Proposed 2026-10-02.

### M1 — Row filters: validation, both sides, clear, journal, CLI and wizard field

### M2 — `SourceI`: the source side behind one interface

### M3 — Export to a pack, with resume

### M4 — A pack as the source of structure, diff and sync

Deferred:

- **Export and import pages in the wizard**, which need streamed writes
  through the fs broker.
- **Repair from a pack** at chunk granularity: clear a differing chunk's
  slice and load its whole file.
- **A pack as a target** for a later incremental export: diffing a source
  against an earlier pack and exporting only the chunks that moved.

## References

- [ADR-0259](./0259-jackstay-guided-clickhouse-to-clickhouse-sync.md) — jackstay; §SD4 digests, §SD5 sync and ownership, M6 superseded by this ADR's §SD2–§SD3.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) — the fs broker the wizard's files go through.
