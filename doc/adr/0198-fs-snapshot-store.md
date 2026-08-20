---
type: adr
status: proposed
date: 2026-08-19
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Not reviewed by a second reader.
> M0–M6 are built and committed ahead of that review (see the dated `## Updates`
> below), so this is a decision whose consequences can be inspected rather than
> only argued; what has not happened is the review that would flip it to
> accepted.

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
untouched — but *not* from `DESCRIBE TABLE`, which is what the emitted
`VerifySchema` compares, so that verb fails on the provisioned table until it
is taught to skip them (`## Updates`, 2026-08-19); `fssnap` is filled by a materialised view copying every root row.
The `boxer.facts` `TableDesc` is **not** extended. The mount *policy record*
(name, store, retention class, inline threshold, text rule) is a kind in
`boxer.facts`, written through a facts-bound store — it is runtime state and
belongs there. `fsdata` may stay bespoke if the M0 insert-cost measurement
says so (SD11).

The logical schema — which design column lands in which leeway slot and
section — is the table in the compact page §3; the vocabulary is one registry
(`ladingMode`, `ladingSize`, `ladingMtime`, `ladingLinkTarget`,
`ladingContentHash`, `ladingContent`, `ladingBlockSize`, `ladingBlocks`,
`ladingText`, `ladingNodeKind`, `ladingErr`, `ladingSnapEntries`,
`ladingSnapBytes`, `ladingTtlClass`, `ladingTextRule`, `ladingInlineMax`,
`ladingData`, `ladingBlockHash`, `ladingLine0`, the four kind markers, the
policy kind's memberships), every attribute a scalar or `unit` shape so none
of the generator's three refusals applies — except `ladingText`, which is
plain `bool`: that section has no `unit` cardinality (M1).

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

*Answered 2026-08-19 — see the `## Updates` entry for the decisions and
the evidence. The list below is what was deferred, and why.*

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
| `boxer.facts` rows | gains one kind: the mount policy record | `ladingvocab`; `ladingpolicy`, the facts-bound store that writes it |
| `public/fs/lading/ladingvocab` | new registry: 28 memberships and four kind markers under tag value 2178315, ids via `tagmint`/`namemint` like every vocabulary | the repo-wide disjointness check; `scripts/dev/generate.sh` |
| `public/fs/lading{,/ladingschema,/ladingmeta,/ladingdata,/ladingpolicy}` | new: the schema, the entry store over `fsmeta`, the block store over `fsdata`, the policy kind facts-bound, and `Provision` / `Verify` | gen-tests; the regeneration lanes; `props harvest` |
| `public/fs/lading/ladingingest` | new: walker, block cutting, hashing, commit protocol, batching | the policy record; the integration lane |
| `public/fs/lading/ladingremote` | new: an `fs.FS` over SFTP, and the `rclone serve sftp --stdio` source — its own package so the walker stays source-agnostic (M6) | the rclone lane |
| `public/fs/lading/ladingadapter` | new | `fstest.TestFS` lane |
| `fs()` / `fsdata()` macros | new nanopass rewrites | `nanopass_analytics_security.go` allowlist, `play_dispatch_policy.go`, play's vocabulary tab |
| `boxer fs sftp-stdio` (CLI) | new subcommand (`public/app/commands/ladingfs`); the capability subject is still undecided, so visibility comes from `--mount` / `--all-mounts` | the capslock baseline (unchanged — the check passes as is) |
| dependencies | `pkg/sftp` v1.13.11 — new direct (M5); links `x/crypto/ssh` for a constructor nothing calls | govulncheck / osv-scanner posture |
| `extbin` | `rclone` declared (`extbin.Rclone`, `BOXER_RCLONE`) for the integration lane | nothing else |
| `recordstore/gen`'s emitted `VerifySchema` | skips `MATERIALIZED` / `ALIAS` columns, so a store may own derived columns its decode never sees | every generated store in the tree regenerates |
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
- **Regeneration.** `ladingvocab`'s committed `(ordinal, name, id)` table
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
- **M1 — vocabulary, stores, provisioning.** `ladingvocab`; the entry and
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

## Updates

### 2026-08-19 — M0 ran: the eleven checks, and what they corrected

The [M0 trial](../trials/fs-snapshot-store-m0/) ran every check of the plan
page against ClickHouse 26.7.3.19, rclone v1.74.3 and boxer `deb4be09`. Its
[logbook](../trials/fs-snapshot-store-m0/logbook.md) carries the per-check
verdicts and the evidence; this entry records only what the ADR got wrong
and what SD11 now decides. No production code exists yet.

**What held.** One block is one compressed block at `index_granularity = 1`,
in the facts `blobArray` value stream as much as in a plain `String`, at
1 MiB and at 256 KiB — a point read of a 1 MiB block costs exactly 1.00 MiB
of compressed reads. `TTL` takes the `DateTime64` lifecycle plain bare,
beside `PARTITION BY toYYYYMMDD(…)` on the same plain. `now()` is legal in a
`ROW POLICY`. The materialised view copies exactly the root rows of a
5 000-row, 500-mount batch. `FULL OUTER JOIN` fills the missing side with
`''`. `MATERIALIZED` tree columns may be added by `ALTER`, may depend on one
another, are hidden from `SELECT *`, and a bloom filter over `dir` pruned 197
granules to 12. `recordstore/gen` produced a store over the facts `TableDesc`
on `boxer.fsmeta` with `SharedRA`, our clauses and a working `EnsureTable`;
it took 2 000 rows under one `(id, ts)` and read them back through `Scan`
with a key-range `ExtraPredicate`, none mis-decoded.

**Corrections.**

- **SD2 overstated the `MATERIALIZED` claim.** The columns are indeed hidden
  from `SELECT *`, so the positional *decode* is untouched — but the emitted
  `VerifySchema` compares `DESCRIBE TABLE`, which lists them, and therefore
  fails on the provisioned table ("189 columns, expects 185"). Provisioning
  as designed and verifying as generated cannot both hold. `DESCRIBE
  TABLE`'s `default_type` marks exactly the materialised columns, so
  teaching `VerifySchema` to skip `MATERIALIZED` and `ALIAS` would align the
  two — a change to a shared generator, and therefore a decision to take
  before M1 rather than inside it.
- **SD2's "every attribute a scalar or `unit` shape" is not reachable for
  `fsText`.** The `bool` section has no `Single` cardinality: a DTO tagged
  `lw:"fsText,bool,unit"` generates code calling `BeginAttributeSingle` and
  `GetAttrValueSingle`, which that section does not have, and the package
  does not compile. `lw:"fsText,bool"` does — the field is `ladingText` since
  M1. The `unit` shape exists on the
  `*Array` and `*Set` sections only — not on `bool`, `symbol`, `foreignKey`,
  `u32Range` or `indices`.
- **The walker cannot use the generated `Ingest<Kind>` verb.** It refuses two
  rows sharing a key (`ErrDuplicateIngestKey`), and every row of a mount
  shares the mount's id; it also opens each entity with an empty envelope, so
  a DTO's `naturalKey` and `expiresAt` never reach the row. `Begin(id, ts,
  Envelope{NaturalKey, ExpiresAt}).Add<Kind>(row).Commit()` carries both.
  The plan page's M1 and M2 wording is wrong on this point.
- **`recordstore/gen` refuses the facts `TableDesc` under a foreign table
  name** unless the caller copies it and sets `DictionaryEntry.Name`. One
  line, no effect on the physical columns — but it is the first thing M1
  will hit. `gen.Input.Database` also refuses anything outside
  `[a-z][a-z0-9]*`; `boxer` passes.
- **The compact page's `ext` expression is wrong at both edges** — the root
  row gets `.`, and `.gitignore` is read as all extension. The M1 spelling is
  `if(position(substring(name, 2), '.') = 0, '', concat('.',
  splitByChar('.', name)[-1]))`.
- **`ttl_only_drop_parts = 1` binds a constraint SD4 left implicit:
  retention classes must be whole days.** A partially expired part keeps its
  expired rows through every background merge — only `OPTIMIZE … FINAL`
  removes them. Because `expiresAt = toStartOfDay(snap) + 1 DAY +
  duration(class)`, a whole-day class puts every row of a partition at the
  same instant and the part expires as a unit; a sub-day class would not, and
  the rows would sit on disk until a manual merge. The macros' logical cutoff
  already keeps them out of results; this is about disk. Drop latency is
  bounded by `merge_with_ttl_timeout`, 14 400 s by default.
- **SD1's purge and SD10's fleet projection are mutually exclusive at the
  server's defaults.** With a projection present, the lightweight `DELETE`
  throws under `lightweight_mutation_projection_mode = THROW`, and the
  setting must be set on the *table* — passing it on the statement leaves
  `getSetting()` reporting `rebuild` while the guard still fires. Without a
  projection the purge took 58 ms.
- **The diff idiom depends on `join_use_nulls = 0`.** True by default and
  true here, but it is a setting a caller can change, and under `1` the idiom
  stops classifying rather than failing. M4 pins it in the expansion.

**SD11, decided.**

- *Block ordinal:* the `naturalKey` suffix, `path ‖ 0x00 ‖ be32(seq)`. It was
  the only encoding measured, and it needs no `boxer.facts` migration; the
  key range over a file's blocks stays contiguous and `Scan` reads it with an
  ordinary prefix predicate. The routing-plain and value-cardinality shapes
  stay rejected on the grounds already recorded, now with no cost argument
  left to prefer them.
- *`fsdata` stays facts-shaped.* The facts shape costs roughly 2× on insert
  wall-clock at 1 MiB blocks (the host was too loaded to pin the factor
  closer — see the trial's finding T1), and essentially nothing on storage:
  the 184 columns beside the block data are 6.1 MiB uncompressed and 39 KiB
  compressed against 31.3 MiB → 7.7 MiB of block data, a ratio of 159, and
  marks cost 178 B per block row against 12 B bespoke. A 2× insert cost on a
  once-per-snapshot write buys the shared read access, codecs and vocabulary
  that were O3's whole point; an order of magnitude would not have.
- *Hot attributes to materialise:* the tree columns (`name`, `dir`, `depth`,
  `ext`) and nothing else yet. `size`, `mtime` and `mode` were not measured
  under extraction pressure — a 2 000-row scan is not that measurement — so
  materialising them stays a later decision with a named trigger: a
  store-wide query whose cost is dominated by attribute extraction.
- *The fleet profile's `by_path` projection is not default.* It blocks the
  per-mount purge unless the table carries
  `lightweight_mutation_projection_mode = 'rebuild'`, which is a trade a
  store should make deliberately.
- *Macro spellings* stay open. No check touched them.

**Per-block hashes (SD5), with a second route.** `lukechampine.com/blake3`
exposes no subtree chaining value: the primitives are in `guts`, and the
function that composes them over an aligned block group is unexported in
`bao`. Reimplementing it is ~25 lines. The supported alternative is
`bao`'s chunk-group encoding — `group = 10` is 1 MiB groups, and a 4 MiB file
gets a 200-byte outboard tree whose root *is* `blake3.Sum256` of the file,
against which `VerifyChunk` accepts a block and rejects a corrupted one. That
chains every block to `content_hash`, which a per-block digest column does
not; it costs about 2× a digest column's bytes at 1 MiB groups. Both are
open to M2; the digest column remains the default because SQL can audit it
(`BLAKE3(data) != hash`), and `BLAKE3()` in ClickHouse agrees with Go and
runs at 2.5–2.8 GiB/s including the read.

**rclone.** All three contracts hold. One wrinkle for M5: rclone's `sftp`
backend runs the `ssh=` command twice — once with `-s sftp`, once with `echo
${ShellId}%ComSpec%` to probe the shell — and `shell_type=unix` on the remote
suppresses the probe. `rclone serve s3` over the stdio head lists buckets,
lists a bucket recursively without a delimiter and reads bytes back
unchanged, with each of the remote's top-level directories becoming a bucket
— so under the `/<mount>/<snapshot>/<path>` tree a mount is a bucket and the
snapshot is the first key segment.

**Still open after M0.** Whether `VerifySchema` learns to ignore
`MATERIALIZED` columns (a shared-generator change); the insert ratio to
better than a factor of two; macro spellings; and everything the checks did
not touch — the text-cutting rule, the retention-class set, `inline_max`
defaults, where the walker runs, and how a store grant is declared.

### 2026-08-19 — M1 shipped: the vocabulary, the four kinds, provisioning

What exists now, under `public/fs/lading/`:

- **`ladingvocab`** — 28 memberships and four kind markers under tag value
  2178315, the seventh of the width-32 class; the committed assignment table
  and the id-landing, round-trip and cross-vocabulary disjointness guards.
- **`ladingschema`** — what the three tables are: the renamed facts
  `TableDesc`, the engine clauses, the two profiles, and the ALTERs,
  constraint, skip index and view that a CREATE TABLE cannot express.
- **`ladingmeta`** — the entry store over `boxer.fsmeta`, two kinds;
  **`ladingdata`** — the block store over `boxer.fsdata`; **`ladingpolicy`** —
  the mount policy, facts-bound through `storegen`. 27 000 lines of generated
  code, reproducible byte for byte.
- **`lading`** — `Provision` and `Verify`.

**Where it lives, and what it is called.** Not under `public/keelson/`: the
store's only tie to keelson is the row shape it borrows (`factsschema`) and
the executor it writes through, which `public/gov/capmapfacts` and
`public/gov/datacatalog` already reach the same way. It sits under
`public/fs/` beside the tree watcher, as **`lading`** — a bill of lading is
issued once for one voyage, lists exactly what was loaded, and is never
amended, only superseded by the next one, which is this store's snapshot
contract verbatim. The packages and the memberships carry that name
(`ladingMode`, not `fsMode`), because a membership named `fsMode` beside a
field named `Mode` reads as something `io/fs` defines and it is not. §SD2
above carries the settled spellings; the M0 entry below still quotes the `fs*`
ones, which is what that probe actually ran under.

**The ClickHouse tables keep their `fs` names** — `boxer.fsmeta`,
`boxer.fsdata`, `boxer.fssnap` — and so do the macros, `fs()` and `fsdata()`.
The two names answer different questions: `lading` is which subsystem owns
them, `fs` is what they hold, and it is the table names a query author sees.

**The root row is two components, not one.** §SD6 said the root row carries
the totals and the applied policy; it is modelled as a `ladingEntry` *and* a
`ladingSnapshot` on one row, rather than as an entry with extra optional
fields. The root is a node like any other and has to `Stat`; the commit record
is a separate thing that happens to ride it. That is what leeway components
are for, and it makes "complete" a property of the row's archetype rather than
of which fields happen to be set. It also means the two kinds share the
`symbol` and `u64Array` sections, which the registry-resolved id snapshot
(`FixedIdsWrapper`) is what permits.

**`VerifySchema` learned to skip `MATERIALIZED` and `ALIAS` columns**, which
is the open decision M0 left. The change is in `recordstore/gen`'s emitter, so
every generated store in the tree regenerated; the emitted diff is the filter
and its comment, and nothing else moved. Under it, provisioning as designed
and verifying as generated both hold — the M0 contradiction is closed. A
`DEFAULT` column is deliberately still caught: it is stored, it is in
`SELECT *`, and it would shift the positional decode.

**The snapshot index is a table with a view into it**, not a view over
`fsmeta`. "The newest complete snapshot of this mount" is a query the `fs()`
expansion will run on every read, and answering it from the entry table means
scanning every path of every snapshot to find root rows.

**Verified on a live server** (integration lane): provisioning is idempotent
and `Verify` passes after the ALTERs; the tree columns are present and hidden
from `SELECT *`; the bloom index on `dir` is there; a five-node tree writes as
five rows under one `(id, ts)` and reads back through `Scan` with a
`startsWith` predicate; the snapshot is invisible until the root row lands and
appears exactly once after, carrying its commit record; a file's blocks are
one contiguous key range under the `path ‖ 0x00 ‖ be32(seq)` suffix and a
sibling path with a longer name does not fall inside it. The per-mount purge
runs as the test's own cleanup.

**Not done here, and deliberately.** No walker, no adapter, no macros — M2 to
M4. The mount policy store is generated but nothing writes to it yet; the
profile a store runs under reaches only `index_granularity`, so changing it on
a live table is a migration rather than a restart, which no milestone yet
handles.

### 2026-08-20 — M2 shipped: the walker

`public/fs/lading/ladingingest`: `Snapshot(ctx, fsys, mount, policy, stores)`
walks an `fs.FS` once and writes it as one snapshot; `RecordPolicy` writes a
mount's declared policy to `boxer.facts`. Both lanes green — the default one
through `clickhouse-local` over the fixture tree §SD6 asks for (files,
directories, symlinks, an unreadable entry, a text file spanning blocks, a
binary file, an empty directory, a file over the threshold), the integration
one against a live server.

**The commit rule is the whole protocol, and it removed work rather than
adding it.** The walker holds the root row back, flushes everything else,
then writes it alone. There is no rollback path, no cleanup path and no
partial-snapshot detector anywhere in the package, because an interrupted walk
leaves rows that no query can reach and `TTL` removes unaided. A cancelled
context is tested for exactly that: rows written, no root row, no snapshot.

**Corrections.**

- **§SD5's per-block hash is a standalone digest, not a subtree chaining
  value.** The two cannot both hold: the column exists so `BLAKE3(data) !=
  hash` audits the store in SQL, and a subtree chaining value is not
  `BLAKE3(block)` — the audit would report every block bad. M0 established
  that Go's `blake3.Sum256` and ClickHouse's `BLAKE3()` agree, which is what
  makes the standalone digest auditable; the chaining-value variant is dropped
  for text and aligned blocks alike. Bao's chunk-group tree stays available as
  the other route (M0), and it is a different feature: it chains to
  `content_hash`, where a digest column does not.
- **Retention classes are whole days, enforced rather than documented.**
  `TtlClass` is a count of days and `Policy` refuses a zero class, so the
  expiry a walk computes always lands on midnight UTC and a partition expires
  as a unit. §SD4's formula is unchanged; what M0 found is that a sub-day
  class would silently keep expired rows on disk, so the type no longer
  expresses one.
- **`text` on an entry row is a guarantee, not a hint.** A file whose rule
  says text but which carries a line longer than one block is stored
  `text = false`, cut at fixed offsets. The alternative — cutting that line
  mid-way and leaving the flag set — makes the flag mean "usually" and loses
  exactly the matches that span the cut, silently. So where `text` is set,
  every boundary between two blocks falls immediately after a newline, and the
  §7 grep idiom is exact rather than nearly exact. An empty file is
  `text = false`: there are no blocks to describe.
- **`content = 'ref'` rows carry a content hash.** §SD5 left it implicit and
  §7's *identical content* query assumed it. The walker streams a file over
  the threshold through BLAKE3 without holding it, so change detection and
  the identical-content question cover the whole mount rather than its small
  half; only the bytes are absent.

**Two knobs that were open (compact page §10) are now decided in code, not in
prose.** The text rule is content-based, not extension-based — `TextSniff`
calls a file text when its first 8 KiB decode as UTF-8 and carry no NUL,
because an extension says what a file is meant to be and the cut has to follow
what its bytes are. And the fallback for an over-long line is the
`text = false` downgrade above, rather than a mid-line cut.

**`InlineMax` is also the walker's memory bound per file.** A stored file is
held whole while it is cut, because whether it is text is a property of the
whole file and re-cutting a stream is not free; a `ref` file is streamed one
block at a time. That makes the threshold one number rather than two.

**Found on the way, and relevant to M4.** `toDateTime64(n, 9)` reads a plain
number as *seconds* whatever the scale says, so a nanosecond timestamp
saturates to the year 2262 and every predicate over it matches nothing,
silently. The conversion is `fromUnixTimestamp64Nano`. The `fs()` expansion
will pin a snapshot by exactly such a literal.

**Not done here.** Nothing reads the blocks back as a file yet — that is M3's
adapter. The rclone-stdio source is M6, not M2 as the plan sketched it: it
needs the `pkg/sftp` client, which is a dependency decision still to take.

### 2026-08-20 — M3 shipped: the `io/fs` adapter

`public/fs/lading/ladingadapter`: `Open(stores, mount, snap)` is one snapshot
as an `fs.FS` — `StatFS`, `ReadDirFS`, `ReadFileFS`, `GlobFS`, `ReadLinkFS`,
`SubFS`, with a `File` that is also an `io.ReaderAt`, an `io.Seeker` and an
`fs.ReadDirFile`. `Snapshots` / `Latest` / `OpenLatest` read the index.
`fstest.TestFS` passes in both lanes against a snapshot the M2 walker wrote.

**Immutability is what makes the caches free.** The view is pinned at Open and
every query carries `id = mount AND ts = snap`, so nothing it has read can
change: entry lookups, negative lookups and directory listings are cached with
no invalidation, no TTL and no size bound, because there is no event that could
make a hit wrong. `TestAPinnedViewDoesNotShift` writes a whole second snapshot
underneath an open view — a file added, one removed, one rewritten — and reads
the same bytes and the same listing before and after.

**Byte offsets map to block ordinals only for fixed-cut files, and that falls
out of M2's text guarantee.** A text file's blocks end where its newlines were,
which is exactly what makes a line-oriented query over them exact — and exactly
what makes their lengths unknowable without reading them. So `ReadAt` on a
non-text file fetches only the blocks its range touches, and a text file is
materialised whole on first read. Materialising is bounded by the mount's
inline threshold, because a file has blocks at all only if it was under it: the
same number bounds the walker's memory per file and the reader's. A caller
cannot tell the two paths apart, which is what the tests check.

**A chunked read is one query per block range, not per chunk.** Counted rather
than reasoned about — a per-chunk implementation returns identical bytes, so
nothing but a counter distinguishes them. This is the property §SD9's head
depends on, and it is checked now rather than at M5.

**Symlinks: the store records, the adapter resolves.** The walker never follows
a link, and `Lstat` / `ReadLink` serve exactly what it recorded. But `io/fs`
says an FS implementing `ReadLinkFS` resolves links on `Open` and `Stat`, so
this one does — inside the snapshot, against the link's own directory for a
relative target, with a depth limit. The snapshot holds the graph; the adapter
interprets it the way `io/fs` callers expect. An absolute target is taken as
rooted at the snapshot, which is the only root a snapshot has.

**`Glob` matches in Go, not in SQL.** §SD8 sketched `match(path, …)`; pushing a
glob down means re-implementing `path.Match` in RE2 and having every edge case
agree, and a divergence there is invisible to a caller and unlikely to be
caught by a test. A pattern with no meta characters is one point lookup; the
rest walks. Callers who want a regular expression get one directly from the SQL
surface (M4), where it is the caller's own regexp rather than a translation of
their glob.

**Two things `fstest.TestFS` cannot judge, and why the conformance fixture
drops them.** It opens and reads everything it can reach, so a `ref` entry
(size, mtime and hash, no bytes) and a broken symlink both make it report an
error — correctly, by its own contract. Both are ordinary in a real snapshot,
so they keep their own tests: reading a `ref` entry without a fetcher fails
with a typed `ErrReferenced` rather than returning zero bytes, and a broken
link `Lstat`s and `ReadLink`s but fails to resolve like any missing path.

**Found on the way.** A block's natural key carries a literal NUL between the
path and the ordinal, and an executor that hands SQL to a process as an
argument cannot carry one at all — `exec` fails with EINVAL before ClickHouse
sees anything. String literals are escaped for it (`\0`), which is two ordinary
characters on the wire and the byte on arrival. This is a property of the
ordinal encoding SD11 chose, so it belongs to anything that builds a predicate
over `fsdata`: the M4 macros will need the same care.

**Not done here.** The `ref` fetch hook is an interface with no implementation
— nothing in the tree yet knows how to reach a mount's live source, and
inventing one before there is a caller would be guessing. `Snapshots` reads
every complete snapshot of a mount rather than paging; a mount with a long
retention and a daily cadence is hundreds of rows, which is fine, and a mount
with an hourly cadence over 90 days is thousands, which is the point at which
it wants a bound.

### 2026-08-20 — M4 shipped: the SQL surface

`public/fs/lading/ladingsql`: the nanopass rewrites, the security
classification, play's dispatch entry, expansion goldens, and §7's operations
catalogue run as SQL against a live server. 22 tests green.

**Three relations, not two.** §SD7 named `fs()` and `fsdata()`; `fssnap()`
joins them, and it is not a convenience. §7's history query asks a snapshot for
its totals, and those are the *commit record's* — a different component on a
different row grain. Every entry row has a path; only the root row has totals;
a projection carrying both would report a default for every ordinary node. So
the snapshot index gets its own relation, reading the index rather than every
path of every snapshot.

**What rides in every expansion**, none of it visible in the query an author
writes: the completeness rule (the newest snapshot is resolved from `fssnap`,
which holds only committed root rows), the logical cutoff on the same column
the `TTL` names, and the capability check.

**The mount id is spelled three ways** — bare, quoted decimal, quoted hex — and
all three expand identically. Name-as-sugar is still not implemented; a name
argument is refused with a message that says so rather than being read as an id.

**A snapshot argument is a string or a number, and the two take different
conversions.** A string is a datetime literal; a number is Unix nanoseconds and
goes through `fromUnixTimestamp64Nano`, never `toDateTime64` — that reads a
plain number as *seconds* whatever the scale says, so nanoseconds saturate to
the year 2262 and the predicate matches nothing with no error anywhere.

**Corrections to §7's catalogue, both found by running it.**

- **`PREWHERE` does not survive the macro.** The expansion is a parenthesised
  subquery, and ClickHouse allows `PREWHERE` only against a table or a table
  function — the grep query in §7 fails outright with it. `WHERE` alone is
  correct; what is lost is the pre-filter, and a caller who needs it reads the
  physical table. This is the price of the subquery shape §SD7 chose, and it is
  the first thing that shape has actually cost.
- **The history query needs `fssnap()`**, per the relation above.

**`is_dir` and `is_symlink` come off the stored node kind**, not off the mode
bits. The mode is Go's own `fs.FileMode` encoding, and reading it in SQL would
put that encoding in every query; the node kind is a LowCardinality symbol a
query groups by directly. Which is what §SD2's `ladingNodeKind` is for — M1
recorded it as redundant with the mode, and this is where it pays.

**The capability check is a seam, not a wiring.** `MountVisibilityI` is
consulted once per macro call at expansion, and a nil visibility refuses every
mount — a capability check that defaults to open is not one. Three
implementations ship: `VisibleAll` (a caller that has already decided, named so
the call site says it out loud), `VisibleSet`, and `VisibleUnderTag` — the
"every id under a tag" shape §SD3 names, which is what lets one store serve many
owners without an id set to maintain. **What is NOT done is binding it to the
capability broker**: that needs a capability subject, which the plan lists as a
stop point, so it is a decision rather than an implementation gap.

**Classification and routing.** `fs`, `fsdata` and `fssnap` join the
table-position local-read allowlist beside `keelson` and `docsearch` — they
expand to a SELECT over a local MergeTree table and their arguments cannot name
a remote. Play's resolver counts a lading reference as *server-side*: the macro
is a table function, so the plain-table walk skips it, and without the addition
a statement joining `keelson('env')` to `fs(m)` would be routed to the
introspection plane where those tables do not exist.

**Also corrected:** an expired row is invisible through the macros while still
on disk, which is what the cutoff is for — and demonstrating it needs
`SYSTEM STOP TTL MERGES`, because a wholly expired partition is dropped by the
engine as soon as it is written. That is M0's finding seen from the other side.

### 2026-08-20 — M5 shipped: the rclone head

`public/fs/lading/ladingsftp` and `boxer fs sftp-stdio`. 13 tests green: nine
against `pkg/sftp` over an in-memory socket pair, four driving the real rclone
binary — resolved through `extbin`, which now declares it — against the real
head, over a real pipe, against a real server.

`github.com/pkg/sftp` v1.13.11 is a new direct dependency, audited before
adding: BSD-2-Clause (the licence gate's `CategoryNotice`), clean on
`govulncheck` and `osv-scanner`, four releases since 2021 with the last five
weeks old. The one cost is that importing it links `golang.org/x/crypto/ssh` —
`client.go` imports it for a single convenience constructor this package never
calls, and Go links per package. ~1.55 MB, and nothing in the tree linked the
SSH stack before. The transport itself carries no SSH: it is a pipe.

**What rclone actually does, against what §SD9 expected.**

- **`--metadata` restores mtimes, not modes.** rclone's `sftp` backend
  documents *no* system metadata at all — modification times ride
  `--sftp-set-modtime` (on by default) and there is no `mode` key for
  `--metadata` to carry. The head reports the mode correctly over the wire, as
  the `pkg/sftp` tests show; nothing on the rclone path asks for it. So a copy
  out of the store restores content and mtime, and the destination takes the
  local backend's default permissions. Worth knowing before treating an rclone
  copy as a faithful restore.
- **`latest` presents to rclone as a directory, not as a link.** M0 check 11b
  pinned the `.rclonelink` mechanism, but that is the *local* backend's;
  reading a remote's symlinks over `sftp`, rclone follows them. Which is the
  better outcome — `rclone mount …/latest` works with no flag — and an SFTP
  client that `Lstat`s still sees the link, so both readings are available.
- **A server must resolve its own symlinks for path operations.** The head
  first refused everything under `/mount/latest/`, on the theory that a client
  reads the link and re-issues against the target. That is not how path
  resolution works anywhere: rclone lists `latest` and then addresses its
  children through that name. Paths through the link now resolve, and listing
  the link lists what it points at — while `Lstat` and `Readlink` still report
  a link, which is where a client learns it is one.

**One store, one goroutine.** The SFTP request server answers packets
concurrently and a generated record store is single-goroutine (ADR-0100), so
every path into the store takes the head's lock — including the reads a client
makes through a handle *after* the handler that opened it returned, which is
why the returned reader is wrapped rather than handed over bare. The head is
therefore serial against the store, which is the right trade for a surface the
ADR already calls batch-shaped.

**Views are cached and never invalidated**, one adapter per (mount, snapshot),
for the same reason the adapter's own caches are: a pinned snapshot cannot
change. The per-handle block cache M3 built is what makes rclone's 32 KiB
chunked reads cost one block query per block rather than one per chunk.

**An invisible mount is absent, not forbidden.** A tree that answered
"permission denied" for one name and "no such file" for another would let a
client enumerate what it cannot read. The CLI requires `--mount <id>`
(repeatable) or an explicit `--all-mounts`: possession of the pipe is the
authorisation for the *store* (§SD9), but which mounts inside it are visible is
still a decision, and defaulting it to all would make it one nobody took.

**House-style corrections applied across M2–M5.** The gov gate could not run
for most of this session — a concurrent Go 1.27 bump left the tree unbuildable
by the gate's own launcher — so six violations accumulated and were fixed in
one pass once it ran: `Stores` was a type alias (CS008, an error: it is now
[lading.Stores] at the call sites), two `fmt.Errorf` calls became `eb.Build()`
(CS001), `SourceFetcher` became `SourceFetcherI` (CS005), `TtlClass` and
`TextRule` became `TtlClassE` / `TextRuleE` (CS006), and their values took the
type prefix — `TtlClass7d`, `TextRuleSniff` (CS007). Worth recording because
the cause is procedural rather than technical: a gate that cannot run does not
stop anything.

**Not done.** The capability check remains the seam M4 built — a
`MountVisibilityI` the caller supplies — with no binding to the capability
broker, because that needs a capability subject and the subject is a decision
rather than an implementation gap. The head is also read-only in the strong
sense: every `Filewrite` and every `Filecmd` returns permission denied, which
is checked from rclone's side too (`rclone copy` *into* the store fails and
leaves nothing behind).

### 2026-08-20 — M6 shipped: rclone ingress

`public/fs/lading/ladingremote`: an `fs.FS` over an SFTP connection, and
`Serve(ctx, remote, …)` which spawns `rclone serve sftp --stdio <remote>` and
returns its tree as one. Seven integration tests green against the real binary.

	src, err := ladingremote.Serve(ctx, "s3:bucket/prefix")
	defer src.Close()
	res, err := ladingingest.Snapshot(ctx, src, mount, policy, stores)

Anything rclone can reach is now snapshottable, and the walker learned nothing
about any of it.

**Its own package, not the walker's.** The plan put this inside
`ladingingest`. The walker's input is an `fs.FS` and nothing else — that is
what makes one walker serve a grant, an embed, a zip and a remote alike — and
folding a transport into it would make every consumer link `pkg/sftp` and,
through it, `x/crypto/ssh`: the 1.55 MB M5 measured, for a dependency most of
them have no use for.

**Filters run at the source.** `WithFilters("--exclude", "*.bin")` passes
rclone's own filter language to the serving side, so what is excluded never
reaches this process — the difference between filtering and not storing. A
mount's content policy for a remote is rclone's language rather than anything
this store invents.

**What a remote costs, measured against the same directory walked directly.**

- **Modification times arrive at whole seconds.** SFTP's attribute width. A
  snapshot of a remote records seconds where a local walk records nanoseconds.
- **Modes do not survive: `rclone serve sftp` reports 0644 for every regular
  file**, whatever the source's permissions are. This is the ingress mirror of
  M5's finding that `--metadata` carries no mode on egress — rclone does not
  carry modes in either direction. The test asserts the normalisation rather
  than skipping the field, so a future rclone that starts carrying them is a
  failure someone reads.
- **A directory's size is not carried**, which is correct: it is the local
  filesystem's own bookkeeping. `Result.Bytes` therefore differs between the
  two paths while every file's size, content and block count agree.

**Symlinks survive, which §SD9 assumed they would not.** Without `--links`,
`rclone serve sftp` does not show a symlink at all — it is absent from the
listing and simply not in the snapshot. *With* `--links`, the node arrives over
the wire **as a symlink**, target and all, and the walker records it as one;
the adapter then resolves it inside the snapshot like any other. rclone's own
`ls` renders such a node as a small regular file whose bytes are the target,
but that is its client-side `.rclonelink` convention showing through, a
different layer from what SFTP carries. So symlink fidelity through rclone
ingress is a flag away rather than unavailable — the M0 check 11b note about
`.rclonelink` described the client side, not this one.

**Round-trip acceptance.** The same directory snapshotted twice — once through
`os.DirFS`, once through rclone — agrees node for node on kind, file size,
content and mtime-to-the-second, and `fstest.TestFS` passes over the snapshot
that came in through rclone. The two divergences above are asserted, not
tolerated.

**Process hygiene.** `Serve` returns a `Remote` whose `Close` shuts the pipe
and waits: a dropped one leaves an rclone running until its stdin closes.
rclone's stderr is captured into a bounded ring and folded into the error, so a
spawn that fails — a bad remote, a missing credential, a filter that hid
everything — says why instead of surfacing as an EOF.

### 2026-08-20 — the how-to

[How to snapshot a file tree and query it](../howto/lading-snapshot-store.md),
linked from the AGENTS.md router. It covers all three ways in — Go, SQL and
rclone — which is why it waited until M5 and M6 existed rather than shipping
with M4's SQL surface and needing a rewrite one milestone later.

Its last section is the collected limits, each of them measured during a
milestone and most of them somebody else's rather than the store's: no
`PREWHERE` through a macro, no file modes through rclone in either direction,
whole-second mtimes over SFTP, symlinks needing `--links` on ingress, `text`
as a guarantee a very long line forfeits, block-ordinal arithmetic only for
non-text files, no deduplication, and not a hot serving path. Collected there
so a reader meets them before they cost an afternoon.

### 2026-08-20 — a review pass over M0–M6, and the seventeen things it found

A multi-agent review of the whole range, at the recall end of the effort scale.
Nothing it found was visible to `go build`, `go vet`, `go test`, `-race` or
`go mod tidy --diff`, which is itself the finding about the test suite: every
defect below sat behind a lane that was green.

**Two read paths disagreed about expiry, and the wrong one was the one rclone
sees.** §SD4's cutoff is a logical one — `ttl_only_drop_parts = 1` leaves a
partly expired part on disk until an explicit OPTIMIZE FINAL, so every read has
to carry `expiresAt > now()`. The macros did; the `io/fs` adapter's entry and
block scans did not. An expired snapshot was therefore gone from `fs()` and
from `Snapshots()` while `rclone ls` still walked it and read its bytes — which
this ADR's own acceptance list names as a failure ("a row visible after its
`expiresAt`"). The cutoff now has one spelling, `ladingschema.NotExpired`,
which every read path ANDs.

**A snapshot was addressable before it was complete.** `ladingingest.Snapshot`
returns its `Result` even when the walk failed, so the instant of a walk that
never committed is a real address. `listSnapshots` hid it and nothing stopped a
client naming it directly. §SD6 makes "has a root row" the rule; the head now
applies it at `view`, from the same snapshot list the listing uses. The
converse hole closed with it: a walk whose root could not be stat'd used to
commit a root row anyway — `latest` moved onto a snapshot the adapter then
refused to list, superseding a good one — and now writes no snapshot at all.

**`is_dir` could answer differently in SQL and in Go for the same row.** The
SQL surface derives it from the stored `NodeKind`, the adapter from `Mode`. The
root row hard-coded `NodeKind` to `dir` while filling `Mode` only when the stat
had succeeded, so the two diverged exactly when something had gone wrong. The
root row is now derived from its stat like every other row, and carries a size.

**A directory whose ReadDir failed got two rows.** `fs.WalkDir` reports that
failure by calling back a *second* time for the same directory, immediately
after the first; the walker wrote a full second row rather than amending the
first. The tables are plain MergeTree, so both rows read back — one of them a
mode-0 stub — and `LIMIT 1` picked between them by tie-break. One directory row
is now held back exactly one callback, which is the window in which the
amendment can arrive.

**Every text file over 8 KiB with non-ASCII in it had a coin-flip chance of
being called binary.** `isText` trims a rune the sniff window cut in half, and
bounded the trimming against `len(content)` — the whole file — instead of
against the window. For anything past 8192 bytes the guard fired on the first
trimmed byte. With it went the newline cutting that the entire line-oriented
SQL surface rests on: `Text = false`, `Line0 = 0`, fixed-offset blocks. Every
`isText` fixture was pure ASCII, where byte 8192 is always a rune boundary.

**A damaged snapshot could read as a short file with no error.** `content`
checked that the block ordinals it found were contiguous from zero, never that
it found as many as the row claims — so a snapshot missing its *trailing*
blocks reassembled truncated and returned a clean EOF. `readFixed` failed
loudly on the same damage, which meant the two paths disagreed about whether a
damaged snapshot was readable, and the silent one was the default under
`TextRuleSniff`.

**`FS.Sub` was not a boundary.** Symlink resolution walks full snapshot paths,
and re-applied neither the prefix nor a containment check, so a link inside a
subtree could name anything in the snapshot and be followed there. The escape
*above* the snapshot was already refused; this was the same refusal one level
in, and `Sub` is the natural primitive for handing a subtree to a consumer that
should not have the rest.

**`*File` promised `io.ReaderAt` and could not keep it.** Parallel `ReadAt` —
which the interface explicitly allows, and which `archive/zip`,
`io.NewSectionReader` and an SFTP request server all do — raced the handle's
block cache. The head had noticed and wrapped every handle in a
`lockedReaderAt`; the fix belonged at the type that made the promise, so the
handle now carries its own lock and the head's wrapper keeps only the job that
is genuinely the head's, serialising against the shared store.

**The SQL surface was declared everywhere and wired nowhere.** The three macros
were on the security classifier's local-read allowlist and routed server-side
by play's dispatch, but `ExpandPass` was registered in no pass pipeline — so a
statement following the how-to was classified as a local read, sent to the
server, and answered with *"unknown table function `fs`"*. It is now a
`passreg` **Factory**, not an Entry: expanding a macro is an authorisation
decision (which mounts may be read), and a factory declines when no visibility
is bound rather than inventing a default. play binds `VisibleAll{}` and says
why in one comment — it already routes `boxer.fsmeta` to the same server as an
ordinary table, so gating the macro more tightly than the table it reads would
refuse the convenient spelling of a query the inconvenient spelling answers.

**`Provision`'s `Profile` argument reached one of the three tables.** The
generated `EnsureTable` runs DDL rendered at code-generation time, so `fsmeta`
and `fsdata` were frozen at whatever profile the gen-test passed —
`ProfileFleet` silently produced a store at the corpus granularities, one mark
per block row, which is exactly the cost that profile exists to avoid.
Provisioning now composes all three `CREATE TABLE`s through the path `fssnap`
already used, from the same `TableDesc` the store decodes.

**`Verify` passed on a half-provisioned store**, and the M1 update's
`VerifySchema` change is what removed the accidental guard: it checks the
columns of the decode, which is the half `Provision` does *not* add. A store
with no tree columns, no `fssnap` and no view decoded every row correctly and
died on the first ReadDir — over a pipe rclone had already mounted, since
`sftp-stdio` runs `Verify` precisely so that it creates nothing. `Verify` now
also checks the materialised columns, `fssnap` and the view.

**And `VerifySchema` itself got a better fix than the one M1 gave it.** Skipping
`MATERIALIZED` and `ALIAS` rows of `DESCRIBE TABLE` missed `EPHEMERAL`, and
rested on an assumption nothing pinned: `asterisk_include_materialized_columns`
and its alias twin decide what `SELECT *` returns, and under them a derived
column *is* in the decode — where the skip-switch would have blessed the
mis-decode, because it reasoned about column kinds rather than about the
projection. It now describes the projection itself, `DESCRIBE (SELECT * FROM
t)`, which is what the positional decode consumes by construction: no kind
vocabulary to keep current, and it tracks the settings. Verified against
`clickhouse-local` in both directions.

**Smaller, and worth the line each:** synthetic tree levels reported an mtime
of 2042 (pkg/sftp marshals `uint32(ModTime().Unix())`, and the zero
`time.Time`'s low 32 bits land there) — they now report the epoch, the
conventional "not known"; `Readlink` handed absolute targets back verbatim,
which the adapter roots at the snapshot and a client roots at the SFTP tree, so
`rclone copy --links` wrote a dangling link where the adapter would have copied
the file; `Glob` turned every `Stat` error into "no matches", including a dead
server; the commit record's byte total counted the pre-read stat rather than
the restated size, disagreeing with `SUM(size)` for exactly the files the
restatement exists for; the walker's flush bounded rows and not bytes, so two
shipped profiles at 1 MiB blocks could hold ~4 GiB before the first flush;
`quoteLiteral` and `unquoteLiteral` had drifted apart in two private copies and
are now one pair; a dispatcher's refusal named `fs(…)` for statements that
wrote `fsdata(…)`; and 65 error messages carried `"lading…: "` prefixes that
CODINGSTANDARDS bans, because `eh` already records the call site.

**Two test defects, both of the kind that keeps a lane green.** A table test
over four cases shared one mount with no snapshot pin and relied on a `DELETE`
between iterations, so a case could read another's row depending on Go's map
ordering. And an assertion checked `err.Error()` for a phrase no code path
emits — it would have passed with the diagnostic it exists to protect deleted.
The second one, rewritten to assert what it meant, immediately failed: the
remote's name and rclone's stderr ride the error as *structured fields*, which
`Error()` does not render. That is correct house style, so the test now reads
the CBOR payload — but it is worth knowing that the string a caller prints does
not carry the reason.

**Not fixed, and deliberately.** M7 (the native S3 head) stays deferred, and
`MountVisibilityI` still has no binding to the capability broker — that needs a
capability subject, which is a decision rather than a detail. The factory
registration above is what makes the absence honest: no subject, no binding, no
expansion.

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
