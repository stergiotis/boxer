---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> A design note, not a decision — the material a later ADR will lean on. Repo
> claims were checked against the tree on 2026-08-19 and carry file references.
> Prior-art claims (who built what, thresholds from the literature) are from
> prior knowledge and were **not** re-verified. Numbers are estimates (marked
> with `~`) unless dated; nothing here has been measured on a live server yet.
> Revised four times the same day — many-mount stores, the hash algorithm,
> then leeway conformance and caller-owned identity, then rclone as ingress
> and egress (§15).
> The converged design without history or alternatives is in
> [the compact page](./iofs-clickhouse-snapshot-store-compact.md).

# An `io/fs` ↔ ClickHouse bridge: a snapshot store for file trees (August 2026)

The question this note answers: Go's `io/fs` describes a read-only tree of
named byte streams; the repository's query layer is ClickHouse SQL. The two
sit badly together — a tree with path identity against flat, keyed sets; an
opaque stream against typed rows — yet for some operations a bridge would pay:
searching a corpus, comparing two states of a directory, asking a tree
questions that `find` answers slowly. What should that bridge look like, and
what does it give up?

The short form: the bridge is a **snapshot store**. A walk of an `fs.FS` — a
*mount*, identified by a tagged id the application owns — is written, once, as
one snapshot into **facts-shaped tables**: the `boxer.facts` leeway row shape
on separate physical tables whose partitioning, key, retention and indexes the
store controls. Every entry's `Stat` goes into a metadata table, and for files
under a per-mount size threshold the bytes go into a block table. Nothing is
ever updated or shared: blocks belong to exactly one file in exactly one
snapshot. That single restriction is what lets ClickHouse do the rest with
features it already has — append-only inserts, partitions, `TTL`, sorted keys,
block compression — and it is why there is no garbage collector, no reference
counting and no mutation anywhere in the design. The same tables hold one mount
or a hundred million of them: partitioning is by expiry day, never by mount, so
scale changes a handful of profile parameters and not the shape. The price is
that storage grows with the number of retained snapshots, undeduplicated; the
scope section keeps that bounded by being explicit about what the store is
*not*.

## 1. Why a bridge, and why this shape

The mismatch is real but narrower than it first appears. Most of it reduces to
three things: paths versus keys (solved by a materialised path in a sorted
key), bytes versus rows (solved by ClickHouse's `FORMAT` codecs, or by
treating a file as a sequence of block rows), and a lazy seekable stream
versus a query that runs to completion (solved by fixed-size blocks, which
make `ReadAt` a key range). What remains — `io/fs` tolerating that a listing
and a later `Open` disagree — is no mismatch at all; `io/fs` already allows it.

The repository also owns most of the transport, the row shape and the identity
scheme already:

- `keelson('x')` is rewritten to `url('<base>/table/x', 'ArrowStream')` by
  [keelsonsql.go](../../public/keelson/runtime/introspect/keelsonsql/keelsonsql.go)
  and served by `GET /table/{name}` in
  [introspecthttp/server.go](../../public/keelson/runtime/introspect/introspecthttp/server.go)
  ([ADR-0094](../adr/0094-keelson-introspection-tables.md)); the security
  classifier treats that rewrite as introspection-read, not egress
  ([nanopass_analytics_security.go](../../public/db/clickhouse/dsl/nanopass/analysis/nanopass_analytics_security.go),
  [play_dispatch_policy.go](../../apps/play/play_dispatch_policy.go)).
- Applet SQL may not name `url()`, `file()`, `s3()` or `remote()` directly
  ([ADR-0132 §SD5](../adr/0132-sqlapplet-sql-defined-applets.md)); the only
  sanctioned way to reach bytes from SQL is a trusted, pass-generated rewrite.
- Three corpora are already bridged from embedded `fs.FS` values into
  introspection tables by hand-written providers — `adrcontent`,
  `helpsections`, `adrsections` ([ADR-0164](../adr/0164-documentation-regex-search.md)),
  whose §SD7 defers a *materialised* lane for corpora too large to scan live.
- A file system is the capability object of the runtime: the picker produces
  an `fs.handle.{uuid}` grant and the app never sees a path
  ([ADR-0026](../adr/0026-app-runtime-and-capability-subjects.md)).
- `boxer.facts` is a leeway table with a generated `TableDesc`
  ([runtime_facts_ddl.out.go](../../public/keelson/runtime/factsschema/ddl/runtime_facts_ddl.out.go)),
  and the standing policy for a new kind is a generated record store
  ([facts-bound record stores](../explanation/facts-bound-record-stores.md),
  [ADR-0105 §D5](../adr/0105-keelson-adopts-generated-record-stores.md));
  `boxer.persiststate` shows a generated store that owns its own table and
  engine clause ([persiststore/schema.go](../../public/keelson/runtime/persist/persiststore/schema.go)),
  and the store generator exposes the table clauses as a seam
  ([gen.go](../../public/storage/recordstore/gen/gen.go), [ADR-0102](../adr/0102-leeway-clickhouse-table-clause-seam.md)).
- Identity is a prefix-free tagged 64-bit id — a Fibonacci-coded tag over a
  body ([ident_fib.go](../../public/identity/identifier/ident_fib.go),
  [ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md)),
  with tags claimed through [tagmint](../../public/identity/tagmint/tagmint.go)
  and SQL helpers in [identsql](../../public/identity/identsql/identsql.go).

So the design below is a generalisation, not a new transport: a mount is a
tree the application identifies with an id it already owns; rows are facts
rows on tables the store controls; ingest rides the existing ArrowStream path;
the table functions are macros classified like `keelson()`.

Two shapes were considered and kept for other occasions rather than rejected:
a **live** provider that lists or serves an `fs.FS` per query (right for
bounded trees and for content that must not be copied — the `ref` mode below
points at it), and a **deduplicating** store with content-defined chunking
(right for backup-like snapshot series of large mutable files; parked because
sharing blocks between files is what forces garbage collection, and §12
records why). What follows is the third shape: the one that trades
deduplication for having no maintenance at all.

## 2. The model on one page

- A **store** is one set of facts-shaped tables — the `boxer.facts`
  `TableDesc` on separate physical tables — created and owned by a generated
  record store whose engine clause (partitioning, key, `TTL`, settings, skip
  indexes) the design chooses. A grant covers a store. Two profiles are
  described — one for a few mounts with large files, one for very many small
  trees — and they differ in parameters, not in schema.
- A **mount** is one tree inside a store: an `fs.FS` the runtime may read — a
  capability handle, an embedded corpus, a zip, a directory a user picked, or
  one of a hundred million per-tenant trees. It is identified by a
  **tagged id the application supplies** — its own tag, its own body, minted
  however it mints identity — and the store carries that id verbatim in
  `id:id:u64` on every row of the mount. The store never mints, never claims a
  tag, never reads the body: the tag stays the application's degree of
  freedom, and the prefix-free code is what lets many applications share one
  store without their ids colliding. A mount is the unit of snapshotting and
  the key prefix of every row.
- A **snapshot** is one complete walk of a mount, identified by its start
  time `snap`. Rows of a snapshot are written once and never touched again.
  A snapshot is *complete* exactly when its root entry — the row for path
  `.` — exists; that row is inserted last, carries the snapshot's totals and
  the policy that was applied, and acts as the commit record; a materialised
  view derives the store's snapshot index from it.
- An **entry** is one row of `fsmeta`: a facts row whose backbone is the key
  (`id` = mount, `ts` = snapshot, `naturalKey` = path, `expiresAt`) and whose
  tagged-value sections carry the `Stat` in `io/fs` terms (mode bits verbatim,
  size, mtime, link target) plus three things `io/fs` does not have: a content
  hash, a note of whether the content was stored, and any error the walker hit
  at that entry. Tree columns (`name`, `dir`, `depth`, `ext`) are materialised
  beside the row.
- A **block** is one row of `fsdata`: `block_size` bytes of one file, in
  order, keyed by the same `(id, ts, naturalKey)` as its entry plus an ordinal.
  Blocks are unshared: no two entries ever point at the same block.
- **Retention** is a small set of classes (`'7d'`, `'30d'`, `'90d'`, …); each
  mount has one, from its policy. Every row carries `expiresAt` — the day's
  end plus the class duration — and the tables are partitioned by the expiry
  day, so a partition expires all at once; ClickHouse's `TTL` reclaims the
  space and the table functions hide the rows the moment the instant passes,
  whichever happens first.
- **Content policy** is per mount — store no content (metadata only), store
  blocks for files up to a size threshold, or mark larger files `ref` so a
  reader fetches them from the live source through the existing content
  route — and lives in a **policy record**: a modelled kind in `boxer.facts`
  keyed by the mount's id, carrying the name the application calls the mount,
  the store, the retention class, the inline threshold and the text rule. The
  record is for discovery and defaults; every snapshot's root row records the
  policy actually applied, so the store is correct without it.

Names are `io/fs` names: unrooted, slash-separated, no `.` or `..` elements,
root is `.`. The table refuses anything else.

## 3. The tables

The physical schema is not written by hand: it is leeway's `TableDesc` for
`boxer.facts` — four plains and twenty-one typed sections, ~170 physical
columns — rendered onto separate tables by a generated record store, with
`SharedRA` binding the read-access scaffolding `boxer.facts` already has and
`gen.Input.DDL` supplying the clauses the design cares about. What this note
adds is the mapping of its logical columns onto that shape, the engine
clauses, and a few `ALTER`s. The bespoke `CREATE TABLE`s of the earlier drafts
remain in the history (`9420bb27`) as the fallback shape (§12).

**Logical schema of an entry row.**

| Logical column | Leeway slot | Physical |
|---|---|---|
| `mount` | `id:id:u64` — the application's tagged id | `UInt64` |
| `snap` | `ts:ts:z64` | `DateTime64(9)` |
| `path` | `id:naturalKey:y` | `String` |
| `expires_at` | `lc:expiresAt:z64` | `DateTime64(9)` |
| `mode`, `block_size`, `blocks` | `u32Array` — `fsMode`, `fsBlockSize`, `fsBlocks` | |
| `size`, `snap_entries`, `snap_bytes` | `u64Array` — `fsSize`, `fsSnapEntries`, `fsSnapBytes` (the latter two on the root row only) | |
| `mtime` | `timeArray` — `fsMtime` | |
| `link_target`, `err` | `stringArray` — `fsLinkTarget`, `fsErr` | |
| `content_hash` | `blobArray` — `fsContentHash` | |
| `content`, `kind`, the kind marker, applied policy | `symbol` — `fsContent` (`none` / `blocks` / `ref`), `fsKind`, `fsKindEntry`; `fsTtlClass`, `fsTextRule` on the root row | |
| `inline_max` (applied) | `u64Array` — `fsInlineMax`, root row only | |
| `text` | `bool` — `fsText` | |
| `name`, `dir`, `depth`, `ext`, `is_dir`, `is_symlink` | `MATERIALIZED` over `naturalKey` and the mode attribute, added by `ALTER` after `EnsureTable` | hidden from `SELECT *` |

Every attribute is a scalar or `unit` shape, so none of the generator's three
refusals applies, and every section already exists in the facts schema. A
block row (if `fsdata` is facts-shaped) is the same backbone with the ordinal
encoded per §13, and `fsData`, `fsBlockHash` (`blobArray`), `fsLine0`
(`u32Array`) and the marker `fsKindBlock`.

**Engine clauses** — the part the store owns, and the reason for separate
tables:

```sql
-- boxer.fsmeta
PARTITION BY toYYYYMMDD("lc:expiresAt:z64:4::0:")                    -- every row of a partition expires that day
ORDER BY ("id:id:u64:47::0:", "ts:ts:z64:47::0:", "id:naturalKey:y:4::0:")   -- (mount, snapshot, path): facts plains only
TTL "lc:expiresAt:z64:4::0:"
SETTINGS index_granularity = 1024, ttl_only_drop_parts = 1, allow_suspicious_low_cardinality_types = 1
-- after EnsureTable:
ALTER TABLE boxer.fsmeta
    ADD COLUMN name   String MATERIALIZED splitByChar('/', "id:naturalKey:y:4::0:")[-1],
    ADD COLUMN dir    String MATERIALIZED multiIf("id:naturalKey:y:4::0:" = '.', '', position("id:naturalKey:y:4::0:", '/') = 0, '.',
                                              substring("id:naturalKey:y:4::0:", 1, length("id:naturalKey:y:4::0:") - length(name) - 1)),
    ADD COLUMN depth  UInt16 MATERIALIZED if("id:naturalKey:y:4::0:" = '.', 0, length(splitByChar('/', "id:naturalKey:y:4::0:"))),
    ADD COLUMN ext    LowCardinality(String) MATERIALIZED if(position(name, '.') = 0, '', concat('.', splitByChar('.', name)[-1])),
    ADD CONSTRAINT valid_path CHECK "id:naturalKey:y:4::0:" = '.' OR NOT hasAny(splitByChar('/', "id:naturalKey:y:4::0:"), ['', '.', '..']),
    ADD INDEX ix_dir dir TYPE bloom_filter GRANULARITY 4;

-- boxer.fsdata (if facts-shaped): ORDER BY (id, ts, naturalKey[, ordinal]), same PARTITION BY / TTL,
--   SETTINGS index_granularity = 1 (corpus) | default (fleet)
-- boxer.fssnap: ORDER BY (id, ts); filled by
CREATE MATERIALIZED VIEW boxer.fssnap_mv TO boxer.fssnap AS
SELECT * FROM boxer.fsmeta WHERE "id:naturalKey:y:4::0:" = '.';
```

`is_dir` / `is_symlink` are derived from the mode attribute the same way, by
`bitTest` on the extracted value.

**The key.** `(id, ts, naturalKey)` *is* `(mount, snapshot, path)` with no
extraction and no added column: all rows of one mount share one id, so
ordering by the full tagged id groups by mount; ids of different
applications — different tags — interleave harmlessly and can never collide,
because the tag code is prefix-free. Every `io/fs` operation is then a point
or a range inside one snapshot: `Stat` is a point, a subtree is the range
`startsWith(naturalKey, 'a/')`, and a file's blocks are the range
`(id, ts, naturalKey)` in `fsdata`, already in file order. `ReadDir` is the
one operation not on the key; the bloom filter on the materialised `dir`
keeps it a granule skip rather than a scan. If listings ever dominate, an
`ORDER BY (id, ts, dir, name)` flips that trade without changing any query
text. This is the convention `sysmfacts` already follows — `Id` the container
(the series), `NaturalKey` the member, `Ts` the time
([sysmfacts/doc.go](../../public/keelson/runtime/sysmfacts/doc.go)) — with
the mount as the container and the path as the member.

**No indirection.** Because blocks are unshared, the block key simply *is*
the entry key plus an ordinal. There is no inode table, no recipe, no join on
the read path — that is the immediate dividend of the no-sharing rule.

**Derived columns.** The walker writes `naturalKey` and the mode attribute;
`name`, `dir`, `depth`, `ext`, `is_dir`, `is_symlink` are materialised at
insert, so listing, walking and globbing never touch `fsdata` and never pay
attribute extraction. They are added by `ALTER` because the generator does not
emit them (the known gap of the
[SQL read surface](../explanation/leeway-sql-read-surface.md)), and ClickHouse
hides `MATERIALIZED` columns from `SELECT *`, so the store's positional decode
is untouched. The root's `dir` is `''` rather than `path.Dir(".") == "."` so
that the root does not list itself. The `CHECK` constraint is `fs.ValidPath`
in SQL: every element must be non-empty and neither `.` nor `..`, except the
root itself.

**Partitions and `TTL`.** A partition is one expiry day; a snapshot's rows
live in exactly one partition, because `expiresAt` — the day's end plus the
mount's class duration — is the same for every row in it. So a partition
expires all at once, `ttl_only_drop_parts = 1` drops whole parts instead of
rewriting them, and merges never cross snapshots. The partition count is the
number of distinct expiry days — retention classes × ingest days, a few
thousand at most — and is independent of how many mounts the store holds; that
independence is the whole reason the mount is *not* in the partition key. It is
also derived from a plain the facts backbone already has, which is why the
retention class itself lives in the policy, not in the table. What a per-mount
partition would have bought is "drop this mount now", which becomes a rare
lightweight `DELETE … WHERE id = …` (a purge request) rather than a schema
feature.

**Block size and granularity.** With `index_granularity = 1` and a block size
between ClickHouse's minimum and maximum compressed-block sizes (64 KiB and
1 MiB by default), one block is one mark and one compressed block: `ReadAt`
reads exactly the block it needs, and nothing else. In the facts shape the
bytes sit in the `blobArray` value stream — an `Array(String)` element — and
the per-granule argument is unchanged. The default is 1 MiB; 256 KiB is the
alternative when `ReadAt` granularity matters more than marks. In a store of
small trees most files are one block far below the 64 KiB minimum; ClickHouse
then packs many blocks into one compressed block, which compresses *better*
(similar small files sit adjacent) and makes the one-mark-per-block setting
pure overhead — hence the fleet profile below.

**Text files.** For files the walker classifies as text, blocks are cut at the
last newline before `block_size`, and `line0` records the first line number of
each block. That is what makes `grep`-shaped queries safe across block
boundaries and lets them report line numbers (§7).

**Hashes.** `content_hash` plays two roles that pull in different directions:
*identity* — joining a mount against anything else by digest, inside or
outside the store — and *integrity* — knowing that the bytes read back are the
bytes written. The algorithm is **BLAKE3**, and that is not a choice this
note makes: [CODINGSTANDARDS § Packages to Use](../../CODINGSTANDARDS.md)
names `lukechampine.com/blake3` as the cryptographic hash, codelint CS009 bans
`crypto/sha256` ([gov_codelint_rule_cs009.go](../../public/gov/codelint/gov_codelint_rule_cs009.go)),
the dependency is already in the tree, and leeway's natural-key convention is
a domain-separated BLAKE3 digest. ClickHouse has `BLAKE3()` beside `SHA256()`,
so digests are checkable in SQL. The corpus profile adds a per-block `hash`,
a standalone digest of the block's bytes, so a single block can be verified on
read and the whole store audited in SQL (§7).

What BLAKE3's tree mode adds on top is structural rather than fast. Its
internal Merkle tree has 1 KiB leaves combined left-complete, so a block that
is a power-of-two multiple of 1 KiB and aligned to its own size is a complete
subtree with its own chaining value, and the file hash is the combination of
the block values. For such blocks `hash` can hold the chaining value; a file's
integrity can then be audited from its block values without reading data, a
rewritten block is caught by the root in `fsmeta`, and verified streaming
(Bao) at block granularity becomes possible on the content route. The caveat:
text blocks — cut at newlines, hence not 1 KiB-aligned — cannot be subtrees,
so for exactly the files the design most wants to query the tree property is
unavailable and `hash` is a standalone digest. Speed (~2–3× per core over
hardware sha256, and parallel within a file) is real but not decisive here:
hashing is a small share of ingest beside reading the source and inserting.
If a join against external sha256 digests (OCI images, Nix store paths, Go
module sums) is ever needed, compute it server-side with `SHA256()`; a Go
walker may not import sha256.

**Identity — what the store must not do.** Claim a tag value; mint ids;
inspect or depend on the body layout; hard-code a tag width. `id` is an
opaque, caller-owned, prefix-free 64-bit identity, and those four refusals are
what keep the store usable by any application with any identity scheme —
including one not yet written. `LW_ID_BODY(id)` and `LW_ID_TAG_VALUE(id)` are
for display and for the application's own grouping ("all my mounts" is "all
ids under my tag"); the store's SQL never names a tag.

**Policy record and snapshot index.** The policy record is a kind in
`boxer.facts`, keyed by the mount's id, written by whoever registers the mount
(the application, or the runtime component acting for it, such as the picker
grant). `fssnap` is never written directly: the materialised view copies every
root row into it — a facts-shaped row like any other, so "latest complete
snapshot of mount *m*" and "latest snapshot of *every* mount" are lookups in a
table with one row per snapshot rather than scans of `fsmeta`. The walker
puts the snapshot's totals and applied policy into the root row's attributes,
which are absent on every other row and cost nothing.

**Profiles.** The schema is one; a store chooses its parameters.

| Parameter | Corpus profile (few mounts, large files) | Fleet profile (very many small trees) |
|---|---|---|
| `fsmeta` `index_granularity` | 1024 | 8192 (default) — a mount's rows sit inside one granule either way |
| `fsdata` `index_granularity` | 1 (one block = one mark) | default — many small blocks per compressed block |
| `block_size` | 1 MiB | 1 MiB (rarely reached) |
| `fsdata` shape | facts-shaped or bespoke — open (§13) | the same question, with the insert-cost measurement as its trigger |
| hash | BLAKE3 (house standard); per-block `hash` — chaining value for aligned blocks, standalone digest otherwise | BLAKE3; no per-block `hash` — one-block files are the rule and the file hash covers them |
| projections on `fsmeta` | none | `by_path` (`ORDER BY (naturalKey, id, ts)`), optionally `by_hash` — for store-wide questions |
| partitioning | `toYYYYMMDD(expiresAt)` | the same; week or month for low-cadence stores |

## 4. Reaching the tables from SQL: `fs()` and `fsdata()`

Queries never name `boxer.fsmeta` or `boxer.fsdata`. Two table functions —
macros in the nanopass sense, expanded to parenthesised subqueries the way
`keelson()` is — bind a mount and a snapshot. The primary spelling takes the
mount's tagged id; a name is sugar, resolved through the policy record.

```
fs(<id>)              → (SELECT <projection> FROM boxer.fsmeta WHERE id = <id> AND expiresAt > now() AND ts = <latest>)
fs(<id>, snap)        → … AND ts = snap
fs(<id>, '*')         → … AND ts IN (SELECT ts FROM boxer.fssnap WHERE id = <id> AND expiresAt > now())   -- history
fsdata(<id>[, snap])  → the same over boxer.fsdata
<projection>          = the generated Projection of the entry kind — one plain column per attribute, under the
                        logical names of §3 (path, snap, size, mtime, …), plus naturalKey AS path, ts AS snap
<latest>              = (SELECT max(ts) FROM boxer.fssnap WHERE id = <id> AND expiresAt > now())
```

Because the projection exposes the logical names, every operation in §6 and
§7 is written against `path`, `size`, `mtime`, `data`, `seq` — and keeps its
text whatever the physical encoding of the ordinal turns out to be. Three
things live in the expansion on purpose:

- **The completeness rule.** "Latest" means the newest snapshot whose root
  row exists — which is exactly the set of rows in `fssnap`. A walk that is
  still running, or one that crashed, is invisible.
- **The logical cutoff.** ClickHouse `TTL` reclaims space during merges; rows
  past their expiry stay visible until a merge runs — with whole-part drops,
  until the part's last row expires plus the scheduling delay. The predicate
  `expiresAt > now()` uses the same column as the `TTL`, so what a query can
  see and what the disk still holds diverge only in disk usage, never in
  results, and entries, blocks and index rows disappear at the same instant
  regardless of which table's merge runs first. A `ROW POLICY … USING
  expiresAt > now() TO ALL` is the server-side belt-and-braces for anyone
  reading the tables directly.
- **The capability check.** A grant covers a store; which mounts a caller may
  name is a predicate over ids — a set, or "every id under this tag"
  (`LW_ID_HAS_TAG`) — checked at expansion time, not at query time. The
  identity model and the capability model are the same thing seen twice. The
  macro is classified as read, like `keelson()`.

A plain-ClickHouse fallback without any pass exists — the kind's generated
Presence / Projection / Validator SQL, or `LW_GET` over the raw table — which
is the honest measure of how thin the macro is: it adds positional sugar, the
latest-complete rule, the cutoff and the capability check, nothing the engine
could not express.

## 5. Writing a snapshot

The walker is generic over `fs.FS` and identity-agnostic: the caller hands it
the mount's tagged id and the policy to apply. One snapshot, in order:

1. Fix `snap := now()` and `expires_at := toStartOfDay(snap) + 1 DAY +
   duration(ttl_class)`.
2. `fs.WalkDir` the mount. For every entry, write an entry row through the
   generated store's `Ingest` — a DTO per row, the DML emitting Arrow, which
   is the ArrowStream transport already in use; if `WalkDir` or `Open`
   reports an error, record it in `fsErr` and keep walking — the snapshot
   records what it could not read instead of failing.
3. For files that the policy says to store (regular file, size ≤
   `inline_max`), stream blocks into `fsdata` while hashing the same bytes
   into `content_hash` — and, where the profile has it, each block into
   `hash`: text files cut at newlines with `line0` maintained, everything else
   at fixed `block_size`. Files above the threshold get `content = 'ref'`;
   metadata-only mounts get `'none'`.
4. Only after every other insert has been acknowledged, insert the root row
   `path = '.'` with the snapshot's totals and the applied policy. The
   materialised view copies it to `fssnap`; the snapshot is now visible.

Inserts are batched, and in a store of many small mounts they are batched
*across* mounts: ClickHouse wants thousands of rows per insert, and one insert
per tree is the classic way to exhaust its part budget. A batch touches at most
as many partitions as it has distinct expiry days, and the root rows of a
batch go in a later insert than the batch's other rows, so the commit rule
holds per batch exactly as it holds per mount.

Failure at any step leaves rows without a root row: invisible to every query
and removed by `TTL`. A retry is a fresh `snap`; nothing is cleaned up by hand.
Two walkers on the same mount at the same time produce two snapshots. The
walker itself is the natural tenant of the capability side, next to the
existing [fsbroker](../../public/keelson/runtime/fsbroker/watcher.go), since
it is the component that holds the `fs.FS`; the id it is handed is whatever
the granting side already uses to name the tree.

## 6. Reading: the `io/fs` adapter

A Go adapter turns one `(store, id, snap)` back into an `fs.FS`. Opening the
adapter pins the snapshot, so the returned file system is immutable and
consistent across every call — stronger than `io/fs` requires — and
`(id, snap)` is a ready-made ETag. It implements the optional interfaces
(`StatFS`, `ReadDirFS`, `ReadFileFS`, `GlobFS`, `ReadLinkFS`, `SubFS`) with
the queries below — through the macro's projection, or through the generated
`Scan` and decode — and its `File` gets `io.ReaderAt` and `io.Seeker` from
`ReadAt`, so `http.FS` and anything else that seeks works unchanged.
`testing/fstest.TestFS` is the conformance test, run per snapshot.

| `io/fs` operation | SQL (inside one pinned snapshot; `m` stands for the mount's id) |
|---|---|
| `ValidPath` | not a query — checked in the adapter; the `CHECK` constraint rejects bad rows at ingest |
| `Open`, `Stat`, `Lstat` | `SELECT mode, size, mtime, content, block_size, blocks, text FROM fs(m) WHERE path = 'a/b.txt'` — zero rows is `ErrNotExist`; rows are what the walker `Lstat`-ed |
| `ReadDir` | `SELECT name, mode, size, mtime FROM fs(m) WHERE dir = 'a' ORDER BY name` — bytewise order, as `io/fs` sorts; paged `ReadDir(n)` adds `AND name > :last … LIMIT n` |
| `WalkDir` | `SELECT path, mode FROM fs(m) WHERE path = 'a' OR startsWith(path, 'a/') ORDER BY splitByChar('/', path)` — array order is pre-order depth-first, which is `WalkDir`'s order; `SkipDir` becomes `AND NOT startsWith(path, 'a/skip/')` |
| `Glob` | `SELECT path FROM fs(m) WHERE match(path, '^usr/[^/]*/bin/ed$')` — the adapter compiles `path.Match` syntax to RE2 (`*` → `[^/]*`, `?` → `[^/]`) |
| `ReadLink` | `SELECT link_target FROM fs(m) WHERE path = 'a/l' AND is_symlink` — following a link is `path.Join(dir, link_target)` in Go, then `Stat` again |
| `Sub` | best a second mount; ad hoc, `substring(path, 3)` over `startsWith(path, 'a/')` |
| `Read` (stream) | `SELECT data, hash FROM fsdata(m) WHERE path = 'a/b.txt' ORDER BY seq` — one key range, already in file order; with `hash` present the adapter verifies each block as it arrives |
| `ReadAt(o, n)` | `… AND seq BETWEEN intDiv(o, bs) AND intDiv(o + n - 1, bs) ORDER BY seq` — `bs` from the `Open` row; the adapter trims the first and last block |
| `ReadFile` | the stream, or in one cell: `SELECT arrayStringConcat(arrayMap(t -> t.2, arraySort(t -> t.1, groupArray((seq, data))))) FROM fsdata(m) WHERE path = 'a/b.txt'` |
| `Close` | nothing server-side; cancel the row stream |
| mutation | none; the store is read-only through the adapter by construction |
| errors | zero rows → `ErrNotExist`; invalid name → `ErrInvalid`; ungranted mount → `ErrPermission` at expansion; `content = 'none'` → `Stat` works, `Read` fails with a typed error; `content = 'ref'` → the adapter fetches from the live source |

## 7. Operations beyond `io/fs`

These are the reason to bridge at all. Each is ordinary SQL over `fs()` and
`fsdata()`; none needs a join to a second store. `m` is a mount's id; `s1`,
`s2` stand for snapshot ids (`fs(m, '2026-08-18 03:00:00')`).

**grep** — a pattern over every text file of a snapshot, with line numbers.
The `PREWHERE` prefilters whole blocks (and uses the token index, if
declared); the `ARRAY JOIN` turns the surviving blocks into lines; `line0`
restores numbering. Because text blocks end at newlines, a single-line match
can never straddle two blocks.

```sql
SELECT path, line0 + i - 1 AS lineno, line
FROM fsdata(m)
ARRAY JOIN splitByChar('\n', data) AS line, arrayEnumerate(splitByChar('\n', data)) AS i
PREWHERE match(data, 'TODO')
WHERE match(line, 'TODO')
ORDER BY path, lineno
```

**history** — how a mount grew, one row per retained snapshot; or one
path's versions.

```sql
SELECT snap, snap_entries, snap_bytes FROM fs(m, '*') WHERE path = '.' ORDER BY snap;

SELECT snap, size, mtime, hex(content_hash) FROM fs(m, '*') WHERE path = 'a/b.txt' ORDER BY snap;
```

**diff** — added, removed and modified entries between two snapshots. The
empty string is never a valid path, so it is a safe marker for "missing on
this side" under ClickHouse's default-filling outer join.

```sql
SELECT if(n.path != '', n.path, o.path) AS path,
       multiIf(o.path = '', 'added', n.path = '', 'removed',
               n.content_hash != o.content_hash OR n.mtime != o.mtime, 'modified', 'same') AS change
FROM fs(m, s2) AS n
FULL OUTER JOIN fs(m, s1) AS o ON n.path = o.path
WHERE change != 'same'
ORDER BY path
```

**du** — every directory's recursive size, in one pass: each file is credited
to each of its ancestors.

```sql
SELECT anc, sum(size) AS bytes, count() AS files
FROM fs(m)
ARRAY JOIN arrayMap(k -> arrayStringConcat(arraySlice(splitByChar('/', path), 1, k), '/'), range(1, depth)) AS anc
WHERE NOT is_dir
GROUP BY anc ORDER BY bytes DESC
```

**what is where** — the usual questions `find` and `ls -lS` answer, now as
scans over a compressed, sorted column: largest files, newest files, files by
extension, and the entries the walker could not read.

```sql
SELECT path, size FROM fs(m) WHERE NOT is_dir ORDER BY size DESC LIMIT 20;
SELECT path, mtime FROM fs(m) WHERE mtime > now() - INTERVAL 1 DAY ORDER BY mtime DESC;
SELECT ext, count(), sum(size) FROM fs(m) WHERE NOT is_dir GROUP BY ext ORDER BY 3 DESC;
SELECT path, err FROM fs(m) WHERE err != '';
```

**identical content** — deduplication as a *question* rather than as storage:
which paths, in this snapshot or across the retained history, hold the same
bytes.

```sql
SELECT hex(content_hash), groupArray(path)
FROM fs(m) WHERE content != 'none'
GROUP BY content_hash HAVING count() > 1
```

**audit** — the store checking itself: every stored block against its digest,
in SQL. For standalone digests (text blocks, one-block files) this is the
whole check; aligned subtree values are checked by the adapter, or by
recomputing file roots from `hash` alone without touching `data`.

```sql
SELECT count() AS bad FROM fsdata(m) WHERE BLAKE3(data) != hash
```

**across mounts** — in a store of many trees the interesting questions run
across them: every mount's latest snapshot, which trees carry a given path,
which trees changed it this week, all the trees under one application's tag.
The first is a lookup in `fssnap`; the others are store-wide scans under the
store's grant, which is what the fleet profile's `by_path` projection exists
for. A spelling for "every mount of a store" (`fs('*')` below) is an open item
(§13).

```sql
SELECT id, max(snap) AS latest FROM boxer.fssnap WHERE expiresAt > now() GROUP BY id;

SELECT id, snap, size, mtime FROM fs('*') WHERE path = 'etc/config.yaml' AND snap > now() - INTERVAL 7 DAY;

SELECT LW_ID_TAG_VALUE(id) AS tag, count() AS mounts FROM boxer.fssnap WHERE expiresAt > now() GROUP BY tag;
```

**structured content** — a JSON or CSV file is a block column like any other,
so `JSONExtract*`, `splitByChar`, `extractAll` and friends apply to it
directly, and a whole mount of small JSON files is one query away from being
a table.

**joins** — entries are facts rows: they carry the application's own id, a
path, `mtime`, `size`, `content_hash` and `kind`, which is enough to join a
mount against any other table in the database by id, by path or by hash —
which source files a profile names, which documents an ADR cites, which
artefacts changed since the last run — and for another domain to formulate a
component over them later, since the memberships come from the shared
vocabulary registry.

## 8. Properties

What the shape gives:

- **No mutations, ever.** Inserts only; merges are confined to one expiry
  day; there is no `ALTER … DELETE` in the life cycle (a per-mount purge is
  the one exception, and it is a request, not a schedule), no mutation queue,
  and nothing that would complicate replication later.
- **Retention is declarative and exact.** `TTL` drops whole parts; the same
  `expiresAt` hides rows at the instant they expire. No garbage collection,
  reference counts or sweeps — the direct consequence of unshared rows.
- **Crash-safe ingest with no cleanup.** An incomplete snapshot is invisible
  and expires by itself; a retry is a new snapshot.
- **Snapshot isolation for readers.** An adapter instance is one immutable
  tree; `(id, snap)` is its ETag.
- **Time travel and diff for free.** Every retained snapshot is queryable;
  history, churn and diffs are joins and group-bys over the same rows, and the
  snapshot index is derived, not maintained.
- **Reads are key ranges.** No joins on the read path; in the corpus profile
  one block is one mark is one compressed block.
- **Content is queryable, not opaque.** Regex and token search, line tables,
  JSON functions and skip indexes apply to blocks — the materialised lane that
  [ADR-0164 §SD7](../adr/0164-documentation-regex-search.md) deferred.
- **Integrity is auditable.** Per-block digests verify on read and in SQL;
  for aligned blocks, a file audits from its block values without reading
  data.
- **One row shape with `boxer.facts`.** Shared read access, shared bus
  codecs, rows that can travel as facts or be copied between tables, and
  components formulated later over one vocabulary — while the store, not the
  shared table, controls partitioning, key, retention and indexes: the
  combination ADR-0184's consequences said was missing.
- **Identity-agnostic.** A mount is whatever tagged id the application hands
  over; the tag is the application's free dimension for grouping and for
  access; prefix-free codes make one store safe for many owners.
- **Metadata is cheap.** A sorted `path` column compresses well (~10–20×
  is typical for sorted path sets), so metadata-only snapshots of large trees
  cost tens of megabytes; `content_hash` gives deduplication as analytics
  without deduplication as storage.
- **Scale changes parameters, not shape.** One mount or 10⁸ mounts: the same
  tables, the same queries, a different profile row.
- **It fits the house.** Existing transport, existing security class,
  existing capability model, the house hash, the generated-store lane, the
  identity scheme.

What it costs:

- **Storage is the sum of retained snapshots, undeduplicated.** A ~100 MB
  text corpus snapshotted daily for 90 days is ~9 GB raw and, at a
  ~3.5× text ratio, ~2.6 GB stored — fine. A ~50 GB source tree on the same
  schedule is ~4.5 TB — not fine, and that mount is metadata-only with `ref`
  content. The inline threshold, the cadence and the retention class are the
  knobs, and they are policy rather than schema.
- **Wide rows.** A facts-shaped row carries ~170 physical columns, ~150 of
  them empty arrays on an fs row. Storage is unaffected (empty arrays
  compress to nothing) and reads touch only the columns they name, but
  insert and merge cost scales with the number of column streams — at fleet
  scale the number to measure before committing, and the trigger for the
  `fsdata` shape decision.
- **Store-wide scans pay attribute extraction.** Reading an attribute out of
  a section is array work per row; the read-surface trial measured ~3×
  against plain columns and 7–14× against a `MATERIALIZED` column, which is
  why the tree columns are materialised and why hot attributes (`size`,
  `mtime`, `mode`) may be too. Per-mount reads do not notice.
- **A vocabulary and a gen-test enter the regeneration lanes.** ~20
  membership names under a registry, a store generated from a gen-test, and
  `scripts/dev/generate.sh` whenever the leeway aspect vocabulary moves.
- **A full walk per snapshot.** Incremental ingest is excluded by the
  no-sharing premise (§12 names the variant that would relax it).
- **Latency in milliseconds.** Every adapter call is a round trip. The
  adapter serves batch work, templating, corpus search and exports — not a
  hot serving path, and never writes.
- **Retention granularity is partition granularity.** Per-day partitions put
  effective retention in `[R, R + 1 day]`, and retention is a class, not a
  free duration per mount.

Where it breaks under pressure:

- Many retention classes × long retention drive the partition count; keep
  classes few, or coarsen the partition for low-cadence stores. The mount
  count no longer matters here.
- Terabytes of content at `index_granularity = 1` make the marks files
  noticeable (~24 bytes per column per block — ~150 MB per TB at 1 MiB
  blocks); watch the mark cache, or use the fleet profile's granularity.
- A regex that must match across block boundaries is only safe for
  line-aligned text and single-line patterns; a line longer than `block_size`
  falls back to a raw cut.
- One path's history across many snapshots is less index-friendly than any
  query inside a snapshot, because the key is snapshot-first; a bloom filter
  on `naturalKey` is the cheap fix if that query matters.
- Store-wide questions are scans: ~10–30 s per 10¹⁰ rows on one node before
  extraction cost, acceptable for analytics and not for interaction, which is
  what the `by_path` projection and the materialised hot attributes are for.
- An expired day lingers physically for up to `merge_with_ttl_timeout`
  (4 h by default); only disk usage notices.

**At fleet scale** — ~10⁸ mounts of ~100 entries each, ~10¹⁰ entries in one
store; all estimates, to be replaced by measurements: metadata ≈ 100–200 GB
on disk before the facts-shape overhead (paths and hashes dominate; zero
hashes and empty sections compress away), the primary index ≈ 60 MB in RAM at
granularity 8192, a weekly re-walk of everything ≈ 1.4 × 10⁹ rows/day ≈
16 k rows/s sustained across ~170 column streams — within one node, given
batching, and the figure to measure first; any per-mount operation reads one
or two granules and stays in the millisecond range; a store-wide scan is
seconds to tens of seconds. What makes this ordinary is that it *is* the
ordinary ClickHouse workload — a key-prefixed, time-partitioned, append-only
log — once the mount is out of the partition key.

## 9. Compression and the storage estimate

ClickHouse compresses per column, per compressed block, inside one part, and a
compressed block never spans parts. With one block per mark, every `data` row
is its own compressed block, so the `PARTITION BY` expression has no effect on
the compression ratio: it chooses which rows share a part, not which bytes
share a compression window. What sets the ratio is the content itself
(text ~3–5×, source ~4–6×, structured logs ~6–15×, already-compressed formats
~1.0× — ZSTD detects those quickly and adds negligible overhead), the codec
level (`ZSTD(3)` is ~5–10 % smaller than `ZSTD(1)` on text for ~2× the write
CPU), and the block size up to ~1 MiB. In the small-file regime of the fleet
profile, many blocks share one compressed block and adjacency (same mount,
sorted paths) helps. Nothing in the engine compresses across blocks, so the
redundancy between snapshots is invisible to compression; storage is

    Σ snapshots × (content bytes ÷ per-block ratio) + Σ snapshots × entries × ~20 B.

The ranges above are experience with ZSTD on those content classes, not
measurements on this corpus. Before the ADR, one ingest of a representative
mount at two codec levels and two block sizes, read back with the query below,
replaces them with numbers.

```sql
SELECT partition, column,
       formatReadableSize(sum(column_data_uncompressed_bytes)) AS raw,
       formatReadableSize(sum(column_data_compressed_bytes))   AS stored,
       round(sum(column_data_uncompressed_bytes) / sum(column_data_compressed_bytes), 2) AS ratio
FROM system.parts_columns
WHERE table = 'fsdata' AND active
GROUP BY partition, column ORDER BY partition, column
```

## 10. rclone: ingress and egress

rclone has no backend plug-in mechanism, so "an rclone adapter" means our side
speaks a protocol an existing rclone backend consumes — the lesson the
repository already drew from rclone itself
([rclone-architecture-lessons §5–§6](../explanation/rclone-architecture-lessons.md)):
implement the consumer's protocol once, on your side, and let a pipe be the
transport. Applied here it yields one native head, one dependency, and no
networking of our own:

    any rclone remote ─► rclone serve sftp --stdio remote:path ─pipe─► walker (pkg/sftp client as fs.FS) ─► store
    store ─► boxer fs sftp-stdio --store X ─pipe─► rclone  :sftp,ssh="…"  ─► mount | serve s3/webdav/nfs/docker | hasher | union | sync …

**Why SFTP over stdio, and why first.** rclone's `sftp` backend accepts an
external command in place of ssh (`ssh = "<cmd>"`, run with `-s sftp`
appended) and speaks SFTP over that command's stdin and stdout. So
`rclone mount ":sftp,ssh=\"boxer fs sftp-stdio --store corpus\":/<mount>/latest" /mnt/x`
spawns our head and needs no socket, no port and no credential: possession of
the pipe is the authorisation, the runtime session the process runs in is the
grant, and it is legal today under the runtime's refusal to bind non-loopback
addresses before ADR-0082
([introspecthttp/server.go](../../public/keelson/runtime/introspect/introspecthttp/server.go)).
SFTP is also the most faithful protocol for what the store holds —
`stat`/`lstat`/`readlink`, modes, symlinks — and `pkg/sftp`'s `RequestServer`
maps onto the adapter of §6 almost one to one: `Fileread` returns an
`io.ReaderAt` (our `ReadAt`), `Filelist` returns a `ListerAt` with offset
paging (our `ReadDir`), `Filewrite` and `Filecmd` return permission denied.
One detail is mandatory rather than optional: rclone reads SFTP in 32–256 KiB
pipelined chunks, so the head keeps a per-handle cache of decoded blocks (plus
readahead), or every chunk becomes a query.

**The virtual tree.**

    /<mount>/<snapshot>/<path>          mount = the tagged id in hex (a policy-record name may alias it)
    /<mount>/latest → <newest snapshot>  a symlink

Time travel is `cd ../2026-08-18T03-00-00Z/`. Snapshot directories never
change, so rclone's caches can be told so (`--read-only --dir-cache-time 1000h
--vfs-cache-mode full`); only `latest` moves. Entries with `content = 'none'`
are listed — metadata is the point — and refuse `open` with a clear error;
`'ref'` entries are served from the live source.

| SFTP request | Store query |
|---|---|
| `readdir` | `ReadDir` (bloom on `dir`), paged by `ORDER BY name` |
| `stat` / `lstat` | the entry row: size, mtime, mode |
| `readlink` | `link_target` |
| `read(offset, len)` | `ReadAt` = `seq BETWEEN`, served from the per-handle block cache |
| list mounts, list snapshots | `fssnap` under the grant's id set or tag |

**Everything else is rclone's.** Once the store is an rclone remote, rclone's
own machinery improves it for consumers — none of it ours to build:

| rclone | What a consumer of the store gets |
|---|---|
| VFS cache — `--vfs-cache-mode full`, `--vfs-read-chunk-size`, `--dir-cache-time` | our round trips hidden behind local chunk and listing caches; immutability makes long cache lifetimes safe |
| `hasher` overlay | md5/sha1/sha256 computed once and persisted for a remote that has none — rclone cannot speak blake3, so `rclone check` and `sync --checksum` come from here |
| `union` | a writable layer beside an immutable snapshot (below) |
| `combine`, `alias` | one tree over several mounts or stores; short names |
| `rclone serve s3 / webdav / http / ftp / sftp / nfs / dlna / docker`, `rclone nfsmount` | re-export to every protocol **with rclone's users, keys and TLS** — the authenticated, TLS-terminated front door while our head stays pipe-only; read-only Docker volumes from snapshots; NFS without FUSE |
| `rclone rcd --rc-web-gui` | a web file browser |
| `--metadata` | modes and mtimes travel on `rclone copy` and land back on disk |
| filters, `--transfers` / `--checkers`, retries, bandwidth limits | selection and robustness |

**S3 in two tiers.** Tier 1, zero code:
`rclone serve s3 --auth-key K,S :sftp,ssh="boxer fs sftp-stdio --store corpus":`
— rclone implements S3 (experimental, gofakes3-based), SigV4 keys and TLS
over our pipe. Tier 2, when measurement says so: a native S3 head on the
runtime's HTTP mux — recursive listing as one paged key-range walk (S3's UTF-8
key order is our bytewise `naturalKey` order), large ranged reads, and
non-rclone clients (DuckDB `read_text('s3://…')`, ClickHouse `s3()`,
aws-cli); symlinks as `.rclonelink` objects, modes and mtime as `x-amz-meta-*`,
empty directories as `dir/` markers. Loopback-only until ADR-0082; rclone can
go anonymous against it.

**Ingress: any rclone remote.** `rclone serve sftp --stdio remote:path` exists
(rclone documents it for `authorized_keys command=`); the walker spawns it and
wraps the `pkg/sftp` *client* as an `fs.FS` — `ReadDir`, `Lstat`, `Open`,
`ReadLink` — keeping its single `io/fs` code path. Every rclone backend
becomes a source, with rclone's auth, retries and bandwidth; rclone's filter
language (`--filter-from`, `--max-age`, …) is the source-side content policy;
a `crypt` remote ingests as plaintext (rclone decrypts) or, pointed at the
ciphertext remote, as a sealed archive — a policy knob. Limits: SFTP mtime is
1 s, symlinks exist only where the source has them (local trees still go
through the fsbroker grant directly), and per-file round trips make a
10⁶-file ingest a matter of hours — fine for snapshots.

**`union`: a writable layer, and what it is not.** `union` merges upstreams
under placement policies; it is mergerfs-shaped, not overlayfs-shaped. For a
working copy:

    [work]
    type = union
    upstreams = /scratch/work store:<mount>/2026-08-18T03-00-00Z:ro
    search_policy = ff      create_policy = ff      action_policy = epff

New files and directories go to scratch; scratch shadows the snapshot on read;
the store never sees a write. What it cannot do: delete or rename a snapshot
file (no whiteouts — deleting a scratch copy makes the snapshot's version
reappear) or modify a snapshot-only file (no copy-up on write) — unless the
scratch upstream carries `:writeback`, which copies a file into scratch **in
full on first read** and from then on edits the copy; that suits small-file
corpora and defeats cheap partial reads of large blocks. Pin the lower layer to
a snapshot directory, never `latest`. The intended loop: working copy = union,
commit = ingest the union as a new snapshot, history = §7's `diff`. For a true
working copy with deletes and renames, mount the snapshot read-only and put
overlayfs on it (Linux), or `rclone copy` the subtree; for read-only container
inputs, `rclone serve docker`.

**Rules.** The store does no networking of its own until ADR-0082 — rclone is
the front. Hashes in rclone's vocabulary come from `hasher`, not from us.
Writable views come from `union`; the store stays append-only. The SFTP head
caches blocks per handle. `latest` is the only mutable name. An integration
lane drives the real `rclone` binary — resolved through `extbin`, not invoked
ad hoc — against a seeded store in both directions. Cost: one new direct
dependency (`pkg/sftp`, as server and client) and a few hundred lines over the
adapter of §6.

## 11. Prior art, briefly

The shape is old. Unix file systems are an inode table plus data blocks;
NTFS's master file table, btrfs and ZFS are literally tables. Every operating
system ships a materialised file-metadata index for search (`locate`,
Spotlight, Windows Search, Android's MediaStore), HPC sites index billions of
entries in SQL (LANL's GUFI, CEA's Robinhood), and every recent SQL engine has
grown a "files as rows" surface: osquery's `file` table, SQLite's `fsdir()` and
`sqlar`, Postgres's `pg_ls_dir`, Snowflake directory tables, BigQuery object
tables, Spark's `_metadata` column, DuckDB's `glob()` / `read_text()`,
ClickHouse's own `file()` virtual columns. Content *in* database rows is the
contested half: the 2006 Microsoft study *To BLOB or Not to BLOB* put the
crossover near 256 KB–1 MB per object, SQLite's `sqlar` and MongoDB's GridFS
sit on either side of it in reputation, and the systems that aged well
(Venti, Git, IPFS, Perkeep, restic, Dropbox, CephFS) keep content immutable
and content-addressed with metadata in a separate store. The famous failure,
WinFS, failed at making a *writable, POSIX* tree relational — the part `io/fs`
excludes by contract, which is why a read-only `fs.FS` over tables is in the
same class as `zip.Reader` or `embed.FS`, not in WinFS's.

DuckDB is the closest comparison and made the opposite storage choice: an
in-engine virtual file system (any Python `fsspec` file system can be
registered), content read in place per query, nothing persisted; when its
authors did design persisted file metadata — DuckLake — they put it in a
transactional row store and inlined only small data. The lesson taken here is
that whether to persist is decided by the source's listing cost, not by the
store's row or column layout; and that content-in-rows is a corpus feature,
not a blob service.

## 12. Alternatives considered and parked

- **Bespoke tables** (the first three drafts of this note; DDL in the history
  at `9420bb27`). Fastest and smallest — no empty sections, no extraction on
  scans — and nothing leeway sees: no generated ingest or decode, no shared
  codec, no vocabulary, no components formulated later. Kept as the fallback
  shape, and as the comparison arm for the insert-cost measurement.
- **A generated store with its own `TableDesc`** (the `boxer.persiststate`
  precedent). Plains are real columns, SQL stays plain, the store carries its
  own RA/DML scaffolding; it loses the shared read access and bus codec of the
  facts shape. The right choice only if fs rows should never travel as facts.
- **Rows in `boxer.facts` itself.** The components skill's main scenario, and
  wrong for this store for reasons the repository already recorded: the
  shared table's engine is `ORDER BY ts` with retention left to the operator,
  ADR-0184's consequences say the writer "cannot control the indexes or
  retention of the table it writes" and that volume becomes whoever writes
  most — at ~86 k rows/day/host, with 22 M/day/host rejected as not worth it
  — and ADR-0105 D3a moved persist state out for less. A fleet store is
  10⁹–10¹⁰ rows per cycle with its own key and lifecycle. Kept for what *is*
  runtime state: the policy record.
- **Extending the facts `TableDesc` with routing plains** (`mount`,
  `ordinal`), or a store-owned tag with an id generator as the mount
  registry. Rejected: the caller's tagged id already leads the key with no
  extraction, so nothing forces a migration of `boxer.facts`; and the store
  must not own identity — the tag is the application's degree of freedom.
  The block ordinal is the sole remaining reason an extension would ever be
  considered (§13).
- **Partitioning by mount (the first draft).** `PARTITION BY (mount, day)`
  reads naturally for a handful of corpora and is fatal for a store of many
  trees: partitions multiply by the mount count, and ClickHouse wants them in
  the low thousands. What it bought — merges confined per mount, "drop this
  mount now" — is not worth a second partitioning rule; partitioning by expiry
  day recovers whole-part expiry from a backbone plain, and a purge is a
  lightweight delete.
- **sha256 as the file hash.** Considered for interoperability with external
  digests; not available — the house standard is BLAKE3 and CS009 bans the
  import. Interop joins, if ever needed, use `SHA256()` server-side.
- **Deduplicated blocks, content-defined chunking (FastCDC).** Buys
  cross-snapshot deduplication and chunk-set analytics (snapshot deltas as
  array algebra); costs a recipe per file, a hash-keyed block store with no
  read locality, a have-query on every ingest, and — decisively — a block's
  lifetime becomes the maximum over its references, which `TTL` cannot
  express. Getting retention back means reference counts, a tracing sweep,
  lease renewal on reference, or bounding deduplication to the retention
  window. All workable; none free. Parked until a mount is a snapshot series
  of large mutable files, which is the only case where it pays.
- **Whole-file content addressing (`ino = hash`) with an inode table.**
  Simpler than CDC but has the same sharing problem one level up: two paths
  with the same content share rows, and `TTL` on shared rows is wrong in the
  same way. That observation is what moved the design to "unshared".
- **A live provider only.** Lists or serves the `fs.FS` per query, persists
  nothing; no retention, no history, no content search beyond what a scan
  can do live. Complementary, not competing: it is the `ref` path and the
  right answer for bounded trees that must not be copied.
- **Per-part dictionaries (`LowCardinality` on `data`) to fold identical
  blocks.** The only in-engine mechanism that would deduplicate within a
  part; wrong tool — built for thousands of distinct values, and identical
  blocks across days sit in different partitions anyway.
- **A row store for the metadata** (`EmbeddedRocksDB`, a dictionary, or a
  different engine altogether). Right for microsecond point lookups; wrong
  for the scans that are the point of the bridge. If `Stat` latency ever
  matters, a dictionary in front of `fsmeta` is the ClickHouse-native answer.
- **An in-engine VFS plug point, DuckDB-style.** Not available to an
  out-of-process host; the HTTP and FIFO seams the repository already uses
  are the ClickHouse-shaped equivalent.

## 13. Open decisions — the material for the next iterations

- **The block ordinal and the `fsdata` shape.** Three encodings — the
  ordinal as a suffix of `naturalKey` (`path ‖ '\0' ‖ be32(seq)`, no
  extension, `ReadAt` a `BETWEEN` on bounds the adapter builds, `seq` and
  `path` recovered by `MATERIALIZED` columns); a generic `rt:ordinal:u32`
  routing plain (cleaner SQL, one migration of `boxer.facts`); or leeway's
  value cardinality — one row per file, the blocks as the N values of one
  `blobArray` attribute (the most leeway-native shape, right for the fleet
  profile's small files, wrong for the corpus profile's large ones). And
  whether `fsdata` is facts-shaped at all, or stays bespoke on the biggest
  table — decided by the insert-cost measurement.
- Block size for the corpus profile: 1 MiB (fewer marks) or 256 KiB (finer
  `ReadAt`).
- Partition unit per store: day, or week / month for low-cadence stores.
- The set of retention classes, and the defaults for `inline_max` and
  `text_rule` in the policy record; the shape of the policy kind itself.
- The text classification rule (media-type sniff, extension list, or both)
  and the fallback for lines longer than `block_size`.
- Whether `fsdata` carries a token/ngram skip index by default or per mount.
- Macro shape — `fs`/`fsdata` taking an id, a name resolved through the
  policy record as sugar, how a snapshot is named in SQL, and a spelling for
  "every mount of a store" / "every mount under a tag".
- Which hot attributes to materialise beside the tree columns (`size`,
  `mtime`, `mode`), given the extraction cost on store-wide scans.
- Whether the fleet profile carries the `by_path` projection by default.
- Where the walker runs; how a store grant is expressed and how mount
  visibility inside it is declared (an id set, a tag, or a row policy).
- A native S3 head (§10 tier 2): when — recursive-listing scale, bulk
  throughput, non-rclone clients — decided by measuring tier 1
  (`rclone serve s3` over the stdio head) first.

## 14. To verify on a live server before the ADR

- One block is one compressed block for `block_size` in [64 KiB, 1 MiB]
  with `index_granularity = 1` — in the `blobArray` value stream of a
  facts-shaped table as well as in a plain `String` column.
- Whether regular merges still remove expired rows under
  `ttl_only_drop_parts = 1`, and the drop latency after a partition expires.
- `now()` accepted in a `ROW POLICY` condition.
- A `MATERIALIZED` column may depend on another `MATERIALIZED` column
  (`dir` uses `name`); if not, inline the expression.
- `FULL OUTER JOIN` default-filling semantics behind the diff idiom.
- A generated store with `SharedRA` over a non-facts table name *and*
  `EnsureTable` with `gen.Input.DDL` (the facts-bound precedent is externally
  provisioned); `VerifySchema` with the `ALTER`-added `MATERIALIZED` columns,
  constraint and skip index present.
- Many rows per `(id, ts)` — one per path — through the generated store's
  `Scan` and decode (no uniqueness or latest-wins assumption; `sysmfacts` has
  one row per `(id, ts)` with items as arrays).
- `PARTITION BY toYYYYMMDD(<DateTime64 plain>)` and `TTL` on the same plain.
- The materialised view into `fssnap` under batched, multi-mount inserts
  (one insert block, many root rows).
- Projections together with `TTL`, and with the lightweight `DELETE` used
  for a purge (`lightweight_mutation_projection_mode`).
- Insert throughput of a facts-shaped `fsdata` against a bespoke one at
  1 MiB blocks, and of facts-shaped `fsmeta` at many small mounts per batch.
- For the per-block hash: that `lukechampine.com/blake3` exposes subtree
  chaining values at block offsets (or Bao with 1 MiB chunk groups); and the
  throughput of `BLAKE3()` in ClickHouse for a store-wide audit.
- Compression ratio and ingest throughput on one representative mount, per
  §9.
- rclone (§10): the `--sftp-ssh` command contract (rclone appends `-s sftp`;
  the head must accept it); `rclone serve sftp --stdio` with filters and over
  a `crypt` remote (mtime and symlink fidelity); `rclone serve s3` listing
  without delimiter over the SFTP remote at 10⁵–10⁶ entries; `hasher` over
  the stdio remote; the `union` operation table against the real binary
  (`:writeback`, deletes, renames).

## 15. Revisions

- 2026-08-19 — first draft: bespoke tables partitioned by `(mount, day)`,
  mounts keyed by a `LowCardinality(String)` name, snapshot index left as an
  open question.
- 2026-08-19, later the same day — revised for stores with very many mounts:
  store / mount split with a registry and numeric `mount_id`; partitioning by
  `(ttl_class, day)` for every store; `fs_snap` derived from root rows by a
  materialised view; corpus and fleet profiles; the fleet-scale estimates in
  §8. Trigger: the question "what if there are 10⁸ small trees?", which the
  first draft answered with 10¹⁰ partitions.
- 2026-08-19, third pass — the hash algorithm as a store parameter; per-block
  digests in the corpus profile; BLAKE3 assessed: its tree mode composes only
  over aligned blocks, which newline-cut text blocks are not. (The sha256
  default this pass chose was wrong for the repository — corrected in the
  fourth pass.)
- 2026-08-19, fourth pass — leeway conformance and caller-owned identity:
  the tables become facts-shaped (the `boxer.facts` `TableDesc` on separate
  tables owned by a generated store, engine clauses through the ADR-0102
  seam); a mount is the application's tagged id in `id:id:u64`, so the facts
  backbone `(id, ts, naturalKey)` *is* `(mount, snapshot, path)` and no
  `TableDesc` extension is needed; the registry becomes a policy kind in
  `boxer.facts`; the hash default corrected to BLAKE3 (house standard, CS009
  bans `crypto/sha256`, dependency already present); partitioning by expiry
  day replaces the retention-class key; the block ordinal and the `fsdata`
  shape recorded as the open decision; bespoke / own-`TableDesc` / in-facts
  recorded as alternatives with their reasons.
- 2026-08-19, fifth pass — rclone as ingress and egress (§10): one native
  head (SFTP over stdio) and rclone for every other protocol, credential and
  TLS; S3 in two tiers; `union` as the writable view with its limits; ingest
  from any rclone remote through `rclone serve sftp --stdio`.
