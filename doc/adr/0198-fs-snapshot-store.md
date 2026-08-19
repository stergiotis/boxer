---
type: adr
status: proposed
date: 2026-08-19
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0198: the fs snapshot store — `io/fs` trees as facts-shaped ClickHouse tables

## Context

Go's `io/fs` describes a read-only tree of named byte streams; the repository's
query layer is ClickHouse SQL. Several operations want a bridge between the
two — searching a corpus, comparing two states of a directory, asking a tree
the questions `find` answers slowly — and three corpora are already bridged by
hand-written introspection providers (`adrcontent`, `helpsections`,
`adrsections`, ADR-0164), whose §SD7 defers a *materialised* lane for corpora
too large to scan live. The runtime already owns the transport such a bridge
needs (`keelson()` → `url('…/table/…','ArrowStream')`, ADR-0094; the trusted
pass-rewrite rule for reaching bytes from applet SQL, ADR-0132 §SD5), the
capability model (an `fs.FS` behind a handle the app never sees, ADR-0026),
the row shape (`boxer.facts`' leeway `TableDesc` and the generated record
store as the standing lane for a new kind, ADR-0105 §D5), and the identity
scheme (prefix-free tagged ids, ADR-0106).

The design space was worked through in
[the snapshot-store note](../adr-background-work/iofs-clickhouse-snapshot-store.md)
and its [compact page](../adr-background-work/iofs-clickhouse-snapshot-store-compact.md);
this ADR records the decisions that fell out and points at the notes for the
reasoning, the prior art and the rejected shapes. Two constraints shaped it
more than any other: the repository's data-centricity invariant (ADR-0148
Update 2026-07-30 — state lives in `boxer.facts` *and* is modelled as facts)
and the record that a writer on the shared facts table "cannot control the
indexes or retention of the table it writes" (ADR-0184, Consequences).

## Design space (QOC)

**Question.** Where do snapshot rows live, and in what shape?

**Options.**

- **O1** — Bespoke `CREATE TABLE`s with plain columns.
- **O2** — A generated record store with its own `TableDesc` (the
  `boxer.persiststate` precedent).
- **O3** — Facts-shaped tables: the `boxer.facts` `TableDesc` on separate
  physical tables owned by a generated store with its own engine clause.
- **O4** — Rows in `boxer.facts` itself.

**Criteria.**

- **C1** — Control of partitioning, key, `TTL` and indexes (the store's
  life-cycle needs).
- **C2** — Leeway tooling: shared read access and bus codecs, the vocabulary,
  components formulated later, `LW_GET` and play's surfaces.
- **C3** — Storage and scan cost at fleet scale (10¹⁰ entries).
- **C4** — Blast radius on `boxer.facts` (volume, retention, schema).
- **C5** — Implementation weight.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | ++ | ++ | −− |
| C2 | −− | +  | ++ | ++ |
| C3 | ++ | +  | +  | −  |
| C4 | ++ | ++ | ++ | −− |
| C5 | ++ | +  | +  | +  |

O3 is chosen: it keeps everything O1 has on C1 and C4, gains all of C2, and
pays on C3 with wide rows and attribute extraction — costs that are bounded by
the profile parameters and measured at M0 rather than assumed. O4 is rejected
for the reasons ADR-0184 and ADR-0105 §D3a already recorded; O1 is kept as the
fallback shape and the comparison arm for the M0 measurement.

## Decision

We will build a **snapshot store**: a walk of an `fs.FS` — a *mount*,
identified by a tagged id the application owns — is written once as one
snapshot into facts-shaped ClickHouse tables the store controls, never updated
or shared, retained by `TTL`, read back through a snapshot-pinned `io/fs`
adapter, queried through two table-function macros, and exposed to rclone
through one SFTP-over-stdio head.

```text
┌──────────────────────┐      ┌─────────────────────────────┐
│ fs.FS                │      │ any rclone remote           │
│ grant · embed · zip  │      │ rclone serve sftp --stdio   │
└──────────┬───────────┘      └───────────────┬─────────────┘
           │ io/fs                            │ pipe — pkg/sftp client as fs.FS
           ▼                                  ▼
┌──────────────────────────────────────────────────────────┐
│ walker (ingest): WalkDir · cut blocks · BLAKE3           │
│ DTO rows · batches across mounts · root row LAST         │
└────────────────────────────┬─────────────────────────────┘
                             │ Ingest → Arrow batches → InsertArrow
                             ▼
┌──────────────────────────────────────────────────────────┐
│ generated record stores (facts TableDesc · SharedRA)     │
│ entry store → fsmeta · block store → fsdata              │
│ policy kind → boxer.facts                                │
└────────────────────────────┬─────────────────────────────┘
                             ▼
┌──────────────────────────────────────────────────────────┐
│ ClickHouse      fsmeta ──MV──▶ fssnap          fsdata    │
│ ORDER BY (id, ts, naturalKey) · PARTITION BY expiry day  │
│ TTL expiresAt · MATERIALIZED name / dir / depth / ext    │
└──────────┬───────────────────────────────┬───────────────┘
           │ Scan<Kind>(ExtraPredicate)    │ fs() / fsdata() macros
           ▼                               ▼
┌──────────────────────┐      ┌───────────────────────────────┐
│ io/fs adapter        │      │ SQL: play · applets · any     │
│ snapshot-pinned      │      │ client — grep, history, diff, │
│ Go consumers, fstest │      │ du, audit, across mounts      │
└──────────┬───────────┘      └───────────────────────────────┘
           │
┌──────────▼───────────┐ pipe ┌───────────────────────────────┐
│ SFTP head (stdio)    │◀────▶│ rclone: mount · serve s3 /    │
│ boxer fs sftp-stdio  │      │ webdav / nfs / docker · union │
└──────────────────────┘      │ · hasher · sync · web GUI     │
                              └───────────────────────────────┘
```

The application's tagged id rides every row (`id:id:u64`); the store mints
nothing. Left column: the pipe both ways; right column: SQL.

### SD1 — Append-only, unshared, expiring

Rows are written once. Blocks belong to exactly one file in exactly one
snapshot; nothing is deduplicated or shared. Every row carries `expiresAt` and
the tables carry `TTL expiresAt`. Consequence, and the reason for the rule:
there is no garbage collector, no reference counting, no sweep and no
mutation anywhere in the store — retention is declarative and exact because
ownership is. A per-mount purge is a lightweight `DELETE … WHERE id = …` on
request, not a schedule. The deduplicating and content-defined-chunking shapes
are parked in the note (§12) with the reason: sharing makes a block's lifetime
the maximum over its references, which `TTL` cannot express.

### SD2 — Facts-shaped tables the store owns

Three tables per store — `boxer.fsmeta` (entries), `boxer.fsdata` (blocks),
`boxer.fssnap` (the snapshot index) — all with the `boxer.facts` `TableDesc`
(`factsschema.LoadRuntimeFactsMapping`), created and owned by generated
record stores (`recordstore/gen` with the facts `TableDesc`, `SharedRA` bound
to `factsschema/ra`, `gen.Input.DDL` supplying the clauses, ADR-0102), so the
physical columns, the read-access scaffolding and the bus codecs are those of
`boxer.facts` while partitioning, key, `TTL`, settings and skip indexes are
the store's. Tree columns (`name`, `dir`, `depth`, `ext`, `is_dir`,
`is_symlink`) are `MATERIALIZED` columns added by `ALTER` after `EnsureTable`,
which ClickHouse hides from `SELECT *`, so the stores' positional decode is
untouched; `fssnap` is filled by a materialised view copying every root row.
The `boxer.facts` `TableDesc` is **not** extended. The mount *policy record*
(name, store, retention class, inline threshold, text rule) is a kind in
`boxer.facts`, written through a facts-bound store — it is runtime state and
belongs there. `fsdata` may stay bespoke if the M0 insert-cost measurement
says so (SD11).

The logical schema — which design column lands in which leeway slot and
section — is the table in the compact page §3; the vocabulary is one registry
(`fsMode`, `fsSize`, `fsMtime`, `fsLinkTarget`, `fsContentHash`, `fsContent`,
`fsBlockSize`, `fsBlocks`, `fsText`, `fsKind`, `fsErr`, `fsSnapEntries`,
`fsSnapBytes`, `fsTtlClass`, `fsTextRule`, `fsInlineMax`, `fsData`,
`fsBlockHash`, `fsLine0`, the kind markers, the policy kind's memberships),
every attribute a scalar or `unit` shape so none of the generator's three
refusals applies.

### SD3 — Caller-owned identity

A mount is identified by a **tagged id the application supplies** — its own
tag, its own body, minted however it mints identity — carried verbatim in
`id:id:u64` on every row of the mount. The store claims no tag value, mints
no id, never inspects the body and never assumes a tag width; `LW_ID_BODY` /
`LW_ID_TAG_VALUE` are for display and for the application's own grouping. The
tag is therefore the application's degree of freedom (grouping, access), and
the prefix-free code is what makes one store safe for many owners. The facts
backbone `(id, ts, naturalKey)` *is* `(mount, snapshot, path)` — the
`sysmfacts` convention of container in `Id`, member in `NaturalKey` — so no
extraction and no routing plain is needed. Name → id resolution belongs to the
application or the runtime component registering the mount; the store accepts
ids. A store grant covers a store; visible mounts are an id set or "every id
under a tag".

### SD4 — Key, partitions, expiry

`ORDER BY (id, ts, naturalKey)`; `PARTITION BY toYYYYMMDD(expiresAt)` with
`expiresAt = toStartOfDay(snap) + 1 DAY + duration(retention class)`, so every
row of a partition expires at once and `ttl_only_drop_parts = 1` drops whole
parts; the partition count is the number of distinct expiry days, independent
of the mount count. `Stat` is a point, a subtree is `startsWith(naturalKey,
…)`, a file's blocks are one contiguous range; `ReadDir` rides a bloom filter
on the materialised `dir`. Because `TTL` reclaims space only at merge time,
the macros (SD7) carry the logical cutoff `expiresAt > now()` on the same
column, so results and disk usage can only diverge in disk usage.

### SD5 — Content, blocks, hashes

Content policy per mount: `none` (stat only), `blocks` for files up to an
inline threshold, `ref` for larger files (fetched from the live source through
the existing content route). Blocks are `block_size` bytes — 1 MiB by default,
256 KiB when `ReadAt` granularity matters — and in the corpus profile
`index_granularity = 1` makes one block one mark and one compressed block.
Text files are cut at the last newline before `block_size` with `line0` per
block, so `grep`-shaped queries are boundary-safe and numbered. The hash is
**BLAKE3** — the house standard (CODINGSTANDARDS § Packages to Use; CS009 bans
`crypto/sha256`): the file hash in `content_hash`, and in the corpus profile a
per-block `hash` — a BLAKE3 subtree chaining value for 1 KiB-aligned blocks,
a standalone digest for newline-cut text blocks — auditable in SQL with
`BLAKE3()`. Interop joins against external sha256 digests, if ever needed,
use `SHA256()` server-side.

### SD6 — The commit rule and the snapshot index

A snapshot is complete exactly when its root row (`naturalKey = '.'`) exists.
The walker inserts it last, carrying the snapshot's totals and the policy
actually applied; the materialised view copies it to `fssnap`. A failed or
running walk has no root row: invisible to every query and removed by `TTL`;
a retry is a new `snap`. Batches span mounts; a batch's root rows go in a
later insert than its other rows, so the rule holds per batch.

### SD7 — The SQL surface

Two table-function macros, nanopass rewrites into parenthesised subqueries,
classified read like `keelson()`:

```
fs(m)              → (SELECT <projection> FROM boxer.fsmeta WHERE id = m AND expiresAt > now() AND ts = <latest>)
fs(m, snap)        → … AND ts = snap          fs(m, '*') → … AND ts IN (complete snapshots)
fsdata(m[, snap])  → the same over boxer.fsdata
<projection>       = the entry kind's generated Projection, exposing the logical names (path, snap, size, mtime, …)
<latest>           = (SELECT max(ts) FROM boxer.fssnap WHERE id = m AND expiresAt > now())
```

The expansion carries the completeness rule, the logical cutoff and the
capability check (an ungranted mount is refused at expansion). The operations
beyond `io/fs` — grep with line numbers, history, diff, du, identical content,
audit, store-wide questions — are the SQL catalogue of the note §7 and the
compact page §7.

### SD8 — The `io/fs` adapter

A Go adapter turns `(store, id, snap)` into an immutable `fs.FS` (`StatFS`,
`ReadDirFS`, `ReadFileFS`, `GlobFS`, `ReadLinkFS`, `SubFS`; `File` with
`io.ReaderAt` and `io.Seeker`), reading **through the generated stores'
`Scan<Kind>(ScanOpts{ExtraPredicate, Limit})`** — trusted key predicates over
the physical columns — with ordering within a directory or block range done
in memory, and `fssnap` read with one query over the same `ExecutorI`.
`testing/fstest.TestFS` is its conformance test, per snapshot. Entries with
`content = 'none'` list but refuse `Read` with a typed error; `'ref'` entries
are served from the live source.

### SD9 — rclone: one native head, rclone for the rest

The store's only native transport beyond SQL is **SFTP over stdio**: rclone's
`sftp` backend runs our command in place of ssh and speaks SFTP over its
pipes — no socket, no port, no credential; possession of the pipe is the
authorisation; legal under the runtime's refusal to bind non-loopback
addresses before ADR-0082. The head is `pkg/sftp`'s `RequestServer` over the
adapter (`Fileread` → `ReadAt`, `Filelist` → `ReadDir`, writes refused) with
a mandatory per-handle cache of decoded blocks, presenting
`/<mount>/<snapshot>/<path>` with `latest` as a symlink. Everything else is
rclone's: VFS caching, `hasher` for rclone-vocabulary checksums, `union` as the
writable layer (a merged namespace with placement policies — no whiteouts, no
copy-up on write unless `:writeback`), `combine`/`alias`, `rclone serve
s3/webdav/nfs/docker/…` with rclone's users, keys and TLS as the
authenticated front door, `--metadata`, filters. S3 comes in two tiers:
`rclone serve s3` over the pipe now; a native S3 head on the HTTP mux only
when measurement says so. Ingress from any rclone remote: the walker spawns
`rclone serve sftp --stdio remote:path` and wraps the `pkg/sftp` client as an
`fs.FS`. The lessons this applies are the repository's own
([rclone-architecture-lessons §4–§6](../explanation/rclone-architecture-lessons.md)).

### SD10 — Profiles, and what scale changes

The schema is one; a store chooses a profile. *Corpus* (few mounts, large
files): `fsmeta` granularity 1024, `fsdata` granularity 1, 1 MiB blocks,
per-block hash, no projections. *Fleet* (very many small trees): default
granularities, no per-block hash (one-block files are the rule), a `by_path`
projection for store-wide questions. One mount or 10⁸ mounts: the same
tables, the same queries, a different profile row. Fleet estimates (to be
replaced by M0 numbers): ~10¹⁰ entries ≈ 100–200 GB before the facts-shape
overhead, ~60 MB primary index, ~16 k rows/s sustained for a weekly re-walk,
store-wide scans in seconds to tens of seconds.

### SD11 — Deferred to M0, by measurement

- *The block ordinal and the `fsdata` shape.* Three encodings — a suffix of
  `naturalKey` (`path ‖ '\0' ‖ be32(seq)`, no `TableDesc` change), a generic
  `rt:ordinal:u32` routing plain (one migration of `boxer.facts`), or leeway's
  value cardinality (one row per file, blocks as the N values of one
  `blobArray` attribute) — and whether `fsdata` is facts-shaped at all, or
  bespoke on the biggest table. Decided by the M0 insert-cost measurement and
  the generator checks.
- *Which hot attributes to materialise* beside the tree columns (`size`,
  `mtime`, `mode`), given the extraction cost on store-wide scans.
- *Macro spellings* (snapshot naming, name-as-sugar, "every mount of a store"
  / "every mount under a tag") and the policy kind's exact shape.
- *A native S3 head*: when, measured against tier 1.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `boxer.fsmeta`, `boxer.fsdata`, `boxer.fssnap` + `fssnap_mv` | new tables (facts `TableDesc`, own clauses), one MV | the provisioning step (`EnsureTable` + `ALTER`s + MV); `VerifySchema` at start |
| `boxer.facts` `TableDesc` | **unchanged** | nothing |
| `boxer.facts` rows | gains one kind: the mount policy record | the fs vocabulary; the facts-bound store that writes it |
| fs vocabulary (new package) | new registry: ~20 memberships and the kind markers, ids via `tagmint`/`namemint` like every vocabulary | the repo-wide disjointness check; `scripts/dev/generate.sh` |
| generated stores (new package) | new: entry store over `fsmeta`, block store over `fsdata`, policy kind facts-bound | gen-tests; the regeneration lanes |
| ingest library (new package) | new: walker, block cutting, hashing, commit protocol, batching, rclone-stdio source | the policy record; the integration lane |
| `io/fs` adapter (new package) | new | `fstest.TestFS` lane |
| `fs()` / `fsdata()` macros | new nanopass rewrites | `nanopass_analytics_security.go` allowlist, `play_dispatch_policy.go`, play's vocabulary tab |
| `boxer fs sftp-stdio` (CLI) | new subcommand; capability manifest entry for the store read (`ch.` family, ADR-0026) | the capslock baseline |
| dependencies | `pkg/sftp` (server and client) — new direct | govulncheck / osv-scanner posture |
| `extbin` | `rclone` registered as a resolvable program for the integration lane | nothing else |
| `keelson()` rewrite, `/table` endpoint, `sysmfacts`, `persiststate` | **unchanged** | nothing |

## Alternatives

- **Bespoke tables (O1).** Fastest and smallest; nothing leeway sees — no
  generated ingest or decode, no shared codec, no vocabulary, no components
  formulated later. Kept as the fallback shape and the measurement arm.
- **A generated store with its own `TableDesc` (O2).** Loses the shared read
  access and bus codec of the facts shape; right only if fs rows should never
  travel as facts.
- **Rows in `boxer.facts` (O4).** No control of retention or indexes, volume
  dominated by whoever writes most (ADR-0184), `ORDER BY ts` only; ADR-0105
  §D3a moved persist state out for less. Kept for what *is* runtime state: the
  policy record.
- **Extending the facts `TableDesc` with routing plains**, or a store-owned
  tag with an id generator as the mount registry. The caller's tagged id
  already leads the key; the store must not own identity.
- **Partitioning by mount.** Fatal for many-mount stores (partitions multiply
  by the mount count); expiry-day partitioning recovers whole-part expiry from
  a backbone plain.
- **sha256 as the file hash.** Not available — house standard BLAKE3, CS009.
- **Deduplicated or content-defined-chunked blocks**, whole-file content
  addressing, a live-only provider, `LowCardinality` on `data`, a row store
  for the metadata, an in-engine VFS plug point — each recorded with its
  reason in the note §12.
- **A network listener of our own for rclone** (S3/WebDAV head first).
  Blocked by the pre-ADR-0082 refusal; the pipe needs nothing.

## Consequences

### Positive

- No mutations, no garbage collection, crash-safe ingest with no cleanup,
  exact declarative retention, snapshot isolation for readers, time travel and
  diff for free, reads as key ranges, content queryable in SQL, integrity
  auditable in SQL.
- One row shape with `boxer.facts` — shared read access, codecs, vocabulary,
  components formulated later — while the store controls its life cycle: the
  combination ADR-0184's consequences named as missing.
- Identity-agnostic and multi-owner safe; scale changes parameters, not shape.
- ADR-0164 §SD7's materialised corpus lane exists.
- rclone (and through it nine protocols, auth, TLS, caching, checksums,
  writable views, a web GUI) reaches the store through one pipe head, and any
  rclone remote can be snapshotted.

### Negative

- Storage is the sum of retained snapshots, undeduplicated; bounded only by
  policy (inline threshold, cadence, retention class).
- Wide facts-shaped rows (~170 column streams) make insert and merge cost the
  number to measure at fleet scale; store-wide scans pay attribute extraction
  unless hot attributes are materialised by hand.
- A full walk per snapshot; millisecond latency per adapter call — not a hot
  serving path, never writes.
- A vocabulary and gen-tests enter the regeneration lanes; one new direct
  dependency (`pkg/sftp`).
- `union` is a merged namespace, not an overlay: no whiteouts, no copy-up on
  write — recorded so no one expects otherwise.

### Neutral

- Effective retention is `[R, R + 1 day]`; retention is a class, not a free
  duration.
- `latest` is the only mutable name rclone sees.
- Hashes in rclone's vocabulary come from `hasher`, never from the store.

## Migration — Tier 1

- **Breaks.** Nothing: new tables, a new kind on `boxer.facts` (additive, no
  `TableDesc` change), new packages, new macros.
- **Path.** Provisioning is idempotent — `EnsureTable`, then `ALTER TABLE …
  ADD COLUMN IF NOT EXISTS` / `ADD CONSTRAINT` / `ADD INDEX`, then the MV —
  and `VerifySchema` runs at start.
- **Regeneration.** The fs vocabulary's committed `(ordinal, name, id)` table
  and the generated stores regenerate from gen-tests; `scripts/dev/generate.sh`
  when the leeway aspect vocabulary moves.
- **Old shape.** None.

## Verification plan — Tier 1

- **Lane: default `go test`.** Walk an `fstest.MapFS` into an in-memory
  executor, read it back through the adapter, and assert `fstest.TestFS` plus
  field-for-field equality of every `Stat`; the commit rule (no root row ⇒
  invisible); text cutting and `line0`; BLAKE3 file and block hashes; the
  macro expansions as goldens.
- **Lane: `//go:build integration` (live ClickHouse).** Provisioning and
  `VerifySchema`; many rows per `(id, ts)` through `Scan`; the M0 checks of
  the note §14 (one block = one compressed block, `ttl_only_drop_parts`
  behaviour, the MV under batched multi-mount inserts, `MATERIALIZED` columns
  beside the store); the §7 operations as executed SQL; the TTL cutoff — an
  expired row invisible through `fs()` while still on disk; compression ratio
  and insert throughput recorded as dated numbers.
- **Lane: rclone (integration).** The real `rclone` binary, resolved through
  `extbin`: `lsd`/`ls`/`cat`/`copy --metadata` over the stdio head, a mount
  read, and an ingest from `rclone serve sftp --stdio` — both directions
  round-trip against a seeded store.
- **Gates.** CS009 (no sha256), the capability manifest lint, doclint,
  `go mod tidy --diff`.
- **What would fail.** A row visible after its `expiresAt`; a snapshot
  readable without its root row; a block read returning bytes whose BLAKE3
  differs from `hash`; a `Stat` that disagrees with the source `Lstat`; rclone
  listing or reading differently from the adapter.
- **Gap.** Fleet-scale numbers come from a synthetic mount, not a real fleet;
  the note's prior-art and rclone-behaviour claims are verified only where the
  lanes above touch them (`union` semantics are pinned by the rclone lane, not
  by prose).

## Status

Proposed — awaiting review.

The implementation plan for an implementing agent, with per-milestone
deliverables, acceptance criteria and stop points, is
[the plan page](../adr-background-work/iofs-clickhouse-snapshot-store-plan.md).
Milestones, if accepted:

- **M0 — verify and decide.** The live-server checks of the note §14 and the
  SD11 decisions, as a dated trial; no production code.
- **M1 — vocabulary, stores, provisioning.** The fs vocabulary; the entry and
  block stores over facts-shaped tables; the policy kind facts-bound;
  `EnsureTable` + `ALTER`s + MV; `VerifySchema`.
- **M2 — ingest.** The walker: rows, blocks, hashing, commit protocol,
  batching, policy record; default and integration lanes.
- **M3 — the `io/fs` adapter.** Over the generated `Scan`s; `fstest.TestFS`.
- **M4 — the SQL surface.** `fs()` / `fsdata()` macros, security class,
  dispatch policy, the operations catalogue as executed tests.
- **M5 — the rclone head.** `boxer fs sftp-stdio`, virtual tree, block
  cache; the rclone lane over the real binary.
- **M6 — rclone ingress.** The walker over `rclone serve sftp --stdio`.
- **M7 — deferred.** A native S3 head and `fsmeta` projections, by
  measurement against tier 1.

## References

- [The snapshot-store note](../adr-background-work/iofs-clickhouse-snapshot-store.md)
  and its [compact page](../adr-background-work/iofs-clickhouse-snapshot-store-compact.md)
  — reasoning, prior art, alternatives, the SQL catalogue, the verify list.
- ADR-0026 (capability subjects), ADR-0094 (introspection tables),
  ADR-0102 (table-clause seam), ADR-0105 (generated record stores),
  ADR-0106 (tagged identity), ADR-0132 (applet SQL security class),
  ADR-0134 (ad-hoc datasets), ADR-0145 (sealed data), ADR-0148 (the
  data-centricity invariant), ADR-0164 (doc corpora and the deferred
  materialised lane), ADR-0182 / ADR-0183 (leeway aspects and components),
  ADR-0184 (the sysmetrics tee and its consequences).
- [Facts-bound record stores](../explanation/facts-bound-record-stores.md),
  [the leeway SQL read surface](../explanation/leeway-sql-read-surface.md),
  [lessons from rclone's architecture](../explanation/rclone-architecture-lessons.md).
