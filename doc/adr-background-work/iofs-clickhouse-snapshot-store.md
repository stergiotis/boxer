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

# An `io/fs` ↔ ClickHouse bridge: a snapshot store for file trees (August 2026)

The question this note answers: Go's `io/fs` describes a read-only tree of
named byte streams; the repository's query layer is ClickHouse SQL. The two
sit badly together — a tree with path identity against flat, keyed sets; an
opaque stream against typed rows — yet for some operations a bridge would pay:
searching a corpus, comparing two states of a directory, asking a tree
questions that `find` answers slowly. What should that bridge look like, and
what does it give up?

The short form: the bridge is a **snapshot store**. A walk of an `fs.FS` is
written, once, as one snapshot — every entry's `Stat` into a metadata table,
and for files under a per-mount size threshold the bytes into a block table.
Nothing is ever updated or shared: blocks belong to exactly one file in exactly
one snapshot. That single restriction is what lets ClickHouse do the rest with
features it already has — append-only inserts, partitions, `TTL`, sorted keys,
block compression — and it is why there is no garbage collector, no reference
counting and no mutation anywhere in the design. The price is that storage
grows with the number of retained snapshots, undeduplicated; the scope section
keeps that bounded by being explicit about what the store is *not*.

## 1. Why a bridge, and why this shape

The mismatch is real but narrower than it first appears. Most of it reduces to
three things: paths versus keys (solved by a materialised path in a sorted
key), bytes versus rows (solved by ClickHouse's `FORMAT` codecs, or by
treating a file as a sequence of block rows), and a lazy seekable stream
versus a query that runs to completion (solved by fixed-size blocks, which
make `ReadAt` a key range). What remains — `io/fs` tolerating that a listing
and a later `Open` disagree — is no mismatch at all; `io/fs` already allows it.

The repository also owns most of the transport already:

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

So the design below is a generalisation, not a new transport: a mount is a
granted `fs.FS`; ingest rides the existing ArrowStream path; the table
functions are macros classified like `keelson()`.

Two shapes were considered and kept for other occasions rather than rejected:
a **live** provider that lists or serves an `fs.FS` per query (right for
bounded trees and for content that must not be copied — the `ref` mode below
points at it), and a **deduplicating** store with content-defined chunking
(right for backup-like snapshot series of large mutable files; parked because
sharing blocks between files is what forces garbage collection, and §11
records why). What follows is the third shape: the one that trades
deduplication for having no maintenance at all.

## 2. The model on one page

- A **mount** is a name for one `fs.FS` the runtime may read — in practice a
  capability handle, an embedded corpus, a zip, or a directory a user picked.
  It is also the unit of retention, partitioning and policy.
- A **snapshot** is one complete walk of a mount, identified by its start
  time `snap`. Rows of a snapshot are written once and never touched again.
  A snapshot is *complete* exactly when its root entry — the row for path
  `.` — exists; that row is inserted last, so it acts as the commit record.
- An **entry** is one row of `fs_meta`: the `Stat` of a file, directory or
  symlink in `io/fs` terms (mode bits verbatim, size, mtime, link target),
  plus derived columns for the tree (`name`, `dir`, `depth`, `ext`) and three
  things `io/fs` does not have: a content hash, a note of whether the content
  was stored, and any error the walker hit at that entry.
- A **block** is one row of `fs_data`: `block_size` bytes of one file, in
  order, keyed by the same `(mount, snap, path)` as its entry plus a sequence
  number. Blocks are unshared: no two entries ever point at the same block.
- **Retention** is a per-mount duration. Every row carries `expires_at`;
  ClickHouse's `TTL` reclaims the space and the table functions hide the rows
  the moment the instant passes, whichever happens first.
- **Content policy** is per mount: store no content (metadata only), store
  blocks for files up to a size threshold, or mark larger files `ref` so a
  reader fetches them from the live source through the existing content route.

Names are `io/fs` names: unrooted, slash-separated, no `.` or `..` elements,
root is `.`. The table refuses anything else.

## 3. The two tables

Both are plain `MergeTree` tables, append-only, partitioned by mount and day,
with a `TTL`. The full DDL comes first; the choices are explained underneath.

```sql
CREATE TABLE fs_meta
(
    mount        LowCardinality(String),   -- one granted fs.FS; also the partition / TTL domain
    snap         DateTime64(3),            -- snapshot id = walk start; complete iff its '.' row exists
    path         String,                   -- fs.ValidPath form, root '.'
    mode         UInt32,                   -- fs.FileMode bits, verbatim
    size         UInt64,
    mtime        DateTime64(9),
    link_target  String DEFAULT '',
    content_hash FixedString(32),          -- sha256 of the content; zero unless the content was read
    content      Enum8('none' = 0, 'blocks' = 1, 'ref' = 2),   -- stat only | in fs_data | fetch from the live source
    block_size   UInt32,                   -- this file's block size; 0 unless content = 'blocks'
    blocks       UInt32,
    text         Bool,                     -- blocks were cut at '\n' (line-wise queryable)
    kind         LowCardinality(String),   -- sniffed media type, '' if not sniffed
    err          String DEFAULT '',        -- walk / open error for this entry, recorded rather than swallowed
    expires_at   DateTime,                 -- toStartOfDay(snap) + 1 DAY + retention(mount): one value per partition
    name         String  MATERIALIZED splitByChar('/', path)[-1],
    dir          String  MATERIALIZED multiIf(path = '.', '', position(path, '/') = 0, '.',
                                             substring(path, 1, length(path) - length(name) - 1)),
    depth        UInt16  MATERIALIZED if(path = '.', 0, length(splitByChar('/', path))),
    is_dir       Bool    MATERIALIZED bitTest(mode, 31),    -- fs.ModeDir
    is_symlink   Bool    MATERIALIZED bitTest(mode, 27),    -- fs.ModeSymlink
    ext          LowCardinality(String) MATERIALIZED if(is_dir OR position(name, '.') = 0, '',
                                             concat('.', splitByChar('.', name)[-1])),
    CONSTRAINT valid_path CHECK path = '.' OR NOT hasAny(splitByChar('/', path), ['', '.', '..']),
    INDEX ix_dir dir TYPE bloom_filter GRANULARITY 4
)
ENGINE = MergeTree
PARTITION BY (mount, toYYYYMMDD(snap))
ORDER BY (mount, snap, path)
TTL expires_at
SETTINGS index_granularity = 1024, ttl_only_drop_parts = 1;

CREATE TABLE fs_data
(
    mount      LowCardinality(String),
    snap       DateTime64(3),
    path       String,                    -- same key as the entry: a file's blocks are one contiguous key range
    seq        UInt32,                    -- byte offset = seq * block_size
    line0      UInt32,                    -- text files: 1-based number of the first line in this block
    data       String CODEC(ZSTD(1)),     -- ≤ block_size bytes, immutable
    expires_at DateTime                   -- identical to the snapshot's entries
    -- text mounts, optional: INDEX ix_tok data TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 1
)
ENGINE = MergeTree
PARTITION BY (mount, toYYYYMMDD(snap))
ORDER BY (mount, snap, path, seq)
TTL expires_at
SETTINGS index_granularity = 1, ttl_only_drop_parts = 1;
```

**The key.** `(mount, snap, path)` makes every `io/fs` operation a point or a
range inside one snapshot: `Stat` is a point, a subtree is the range
`startsWith(path, 'a/')`, and a file's blocks are the range
`(mount, snap, path)` in `fs_data`, already in file order. `ReadDir` is the one
operation not on the key; the bloom filter on `dir` keeps it a granule skip
rather than a scan. If listings ever dominate, the key `(mount, snap, dir,
name)` flips that trade without changing any query text.

**No indirection.** Because blocks are unshared, the block key simply *is*
the entry key plus `seq`. There is no inode table, no recipe, no join on the
read path — that is the immediate dividend of the no-sharing rule.

**Derived columns.** The walker writes `path` and `mode`; `name`, `dir`,
`depth`, `is_dir`, `is_symlink` and `ext` are materialised at insert, so
listing, walking and globbing never touch `fs_data`. The root's `dir` is `''`
rather than `path.Dir(".") == "."` so that the root does not list itself. The
`CHECK` constraint is `fs.ValidPath` in SQL: every element must be non-empty
and neither `.` nor `..`, except the root itself.

**Partitions and `TTL`.** A partition is one mount on one day; a snapshot's
rows live in exactly one partition, and `expires_at` — the day's end plus the
mount's retention — is the same for every row in it. So a partition expires
all at once, `ttl_only_drop_parts = 1` drops whole parts instead of rewriting
them, and merges never cross snapshots. Partition count grows with mounts ×
retention days, not with snapshots, which is why the partition is a day and not
a snapshot.

**Block size and granularity.** With `index_granularity = 1` and a block size
between ClickHouse's minimum and maximum compressed-block sizes (64 KiB and
1 MiB by default), one block is one mark and one compressed block: `ReadAt`
reads exactly the block it needs, and nothing else. The default is 1 MiB;
256 KiB is the alternative when `ReadAt` granularity matters more than marks.

**Text files.** For files the walker classifies as text, blocks are cut at the
last newline before `block_size`, and `line0` records the first line number of
each block. That is what makes `grep`-shaped queries safe across block
boundaries and lets them report line numbers (§7).

## 4. Reaching the tables from SQL: `fs()` and `fsdata()`

Queries never name `fs_meta` or `fs_data`. Two table functions — macros in the
nanopass sense, expanded to parenthesised subqueries the way `keelson()` is —
bind a mount and a snapshot:

```
fs('m')               → (SELECT * FROM fs_meta WHERE mount = 'm' AND expires_at > now() AND snap = <latest>)
fs('m', snap)         → … AND snap = snap
fs('m', '*')          → … AND snap IN <complete snapshots>      -- history
fsdata('m'[, snap])   → the same over fs_data
<latest>              = (SELECT max(snap) FROM fs_meta WHERE mount = 'm' AND path = '.' AND expires_at > now())
```

Three things live in that expansion on purpose:

- **The completeness rule.** "Latest" means the newest snapshot whose root
  row exists. A walk that is still running, or one that crashed, is invisible.
- **The logical cutoff.** ClickHouse `TTL` reclaims space during merges; rows
  past their expiry stay visible until a merge runs — with whole-part drops,
  until the part's last row expires plus the scheduling delay. The predicate
  `expires_at > now()` uses the same column as the `TTL`, so what a query can
  see and what the disk still holds diverge only in disk usage, never in
  results, and entries and blocks disappear at the same instant regardless of
  which table's merge runs first. A `ROW POLICY … USING expires_at > now() TO
  ALL` is the server-side belt-and-braces for anyone reading the tables
  directly.
- **The capability check.** The mount argument is a handle; an ungranted
  mount is refused at expansion time, not at query time. The macro is
  classified as read, like `keelson()`.

A plain-ClickHouse fallback without any pass exists — parameterised views,
`fs(mount = 'm')` — which is the honest measure of how thin the macro is: it
adds positional sugar, the latest-complete rule, the cutoff and the capability
check, nothing the engine could not express.

## 5. Writing a snapshot

The walker is generic over `fs.FS`. One snapshot, in order:

1. Fix `snap := now()` and `expires_at := toStartOfDay(snap) + 1 DAY +
   retention(mount)`.
2. `fs.WalkDir` the mount. For every entry, emit an `fs_meta` row; if
   `WalkDir` or `Open` reports an error, record it in `err` and keep walking —
   the snapshot records what it could not read instead of failing.
3. For files that the mount's content policy says to store (regular file,
   size ≤ threshold), stream blocks into `fs_data` while hashing the same
   bytes into `content_hash`: text files cut at newlines with `line0`
   maintained, everything else at fixed `block_size`. Files above the
   threshold get `content = 'ref'`; metadata-only mounts get `'none'`.
4. Only after every other insert has been acknowledged, insert the root row
   `path = '.'`. The snapshot is now visible.

Failure at any step leaves rows without a root row: invisible to every query
and removed by `TTL`. A retry is a fresh `snap`; nothing is cleaned up by hand.
Two walkers on the same mount at the same time produce two snapshots. Ingest
uses the existing ArrowStream transport ([ADR-0094](../adr/0094-keelson-introspection-tables.md),
[ADR-0134](../adr/0134-adhoc-datasets.md)); the walker itself is the natural
tenant of the capability side, next to the existing
[fsbroker](../../public/keelson/runtime/fsbroker/watcher.go), since it is the
component that holds the `fs.FS`.

## 6. Reading: the `io/fs` adapter

A Go adapter turns one `(mount, snap)` back into an `fs.FS`. Opening the
adapter pins the snapshot, so the returned file system is immutable and
consistent across every call — stronger than `io/fs` requires — and
`(mount, snap)` is a ready-made ETag. It implements the optional interfaces
(`StatFS`, `ReadDirFS`, `ReadFileFS`, `GlobFS`, `ReadLinkFS`, `SubFS`) with
the queries below, and its `File` gets `io.ReaderAt` and `io.Seeker` from
`ReadAt`, so `http.FS` and anything else that seeks works unchanged.
`testing/fstest.TestFS` is the conformance test, run per snapshot.

| `io/fs` operation | SQL (inside one pinned snapshot) |
|---|---|
| `ValidPath` | not a query — checked in the adapter; the `CHECK` constraint rejects bad rows at ingest |
| `Open`, `Stat`, `Lstat` | `SELECT mode, size, mtime, content, block_size, blocks, text FROM fs('m') WHERE path = 'a/b.txt'` — zero rows is `ErrNotExist`; rows are what the walker `Lstat`-ed |
| `ReadDir` | `SELECT name, mode, size, mtime FROM fs('m') WHERE dir = 'a' ORDER BY name` — bytewise order, as `io/fs` sorts; paged `ReadDir(n)` adds `AND name > :last … LIMIT n` |
| `WalkDir` | `SELECT path, mode FROM fs('m') WHERE path = 'a' OR startsWith(path, 'a/') ORDER BY splitByChar('/', path)` — array order is pre-order depth-first, which is `WalkDir`'s order; `SkipDir` becomes `AND NOT startsWith(path, 'a/skip/')` |
| `Glob` | `SELECT path FROM fs('m') WHERE match(path, '^usr/[^/]*/bin/ed$')` — the adapter compiles `path.Match` syntax to RE2 (`*` → `[^/]*`, `?` → `[^/]`) |
| `ReadLink` | `SELECT link_target FROM fs('m') WHERE path = 'a/l' AND is_symlink` — following a link is `path.Join(dir, link_target)` in Go, then `Stat` again |
| `Sub` | best a second mount; ad hoc, `substring(path, 3)` over `startsWith(path, 'a/')` |
| `Read` (stream) | `SELECT data FROM fsdata('m') WHERE path = 'a/b.txt' ORDER BY seq` — one key range, already in file order |
| `ReadAt(o, n)` | `… AND seq BETWEEN intDiv(o, bs) AND intDiv(o + n - 1, bs) ORDER BY seq` — `bs` from the `Open` row; the adapter trims the first and last block |
| `ReadFile` | the stream, or in one cell: `SELECT arrayStringConcat(arrayMap(t -> t.2, arraySort(t -> t.1, groupArray((seq, data))))) FROM fsdata('m') WHERE path = 'a/b.txt'` |
| `Close` | nothing server-side; cancel the row stream |
| mutation | none; the store is read-only through the adapter by construction |
| errors | zero rows → `ErrNotExist`; invalid name → `ErrInvalid`; ungranted mount → `ErrPermission` at expansion; `content = 'none'` → `Stat` works, `Read` fails with a typed error; `content = 'ref'` → the adapter fetches from the live source |

## 7. Operations beyond `io/fs`

These are the reason to bridge at all. Each is ordinary SQL over `fs()` and
`fsdata()`; none needs a join to a second store. `s1`, `s2` stand for snapshot
ids (`fs('m', '2026-08-18 03:00:00')`).

**grep** — a pattern over every text file of a snapshot, with line numbers.
The `PREWHERE` prefilters whole blocks (and uses the token index, if
declared); the `ARRAY JOIN` turns the surviving blocks into lines; `line0`
restores numbering. Because text blocks end at newlines, a single-line match
can never straddle two blocks.

```sql
SELECT path, line0 + i - 1 AS lineno, line
FROM fsdata('m')
ARRAY JOIN splitByChar('\n', data) AS line, arrayEnumerate(splitByChar('\n', data)) AS i
PREWHERE match(data, 'TODO')
WHERE match(line, 'TODO')
ORDER BY path, lineno
```

**history** — how a mount grew, one row per retained snapshot; or one
path's versions.

```sql
SELECT snap, count() AS files, sum(size) AS bytes
FROM fs('m', '*') WHERE NOT is_dir GROUP BY snap ORDER BY snap;

SELECT snap, size, mtime, hex(content_hash) FROM fs('m', '*') WHERE path = 'a/b.txt' ORDER BY snap;
```

**diff** — added, removed and modified entries between two snapshots. The
empty string is never a valid path, so it is a safe marker for "missing on
this side" under ClickHouse's default-filling outer join.

```sql
SELECT if(n.path != '', n.path, o.path) AS path,
       multiIf(o.path = '', 'added', n.path = '', 'removed',
               n.content_hash != o.content_hash OR n.mtime != o.mtime, 'modified', 'same') AS change
FROM fs('m', s2) AS n
FULL OUTER JOIN fs('m', s1) AS o ON n.path = o.path
WHERE change != 'same'
ORDER BY path
```

**du** — every directory's recursive size, in one pass: each file is credited
to each of its ancestors.

```sql
SELECT anc, sum(size) AS bytes, count() AS files
FROM fs('m')
ARRAY JOIN arrayMap(k -> arrayStringConcat(arraySlice(splitByChar('/', path), 1, k), '/'), range(1, depth)) AS anc
WHERE NOT is_dir
GROUP BY anc ORDER BY bytes DESC
```

**what is where** — the usual questions `find` and `ls -lS` answer, now as
scans over a compressed, sorted column: largest files, newest files, files by
extension, and the entries the walker could not read.

```sql
SELECT path, size FROM fs('m') WHERE NOT is_dir ORDER BY size DESC LIMIT 20;
SELECT path, mtime FROM fs('m') WHERE mtime > now() - INTERVAL 1 DAY ORDER BY mtime DESC;
SELECT ext, count(), sum(size) FROM fs('m') WHERE NOT is_dir GROUP BY ext ORDER BY 3 DESC;
SELECT path, err FROM fs('m') WHERE err != '';
```

**identical content** — deduplication as a *question* rather than as storage:
which paths, in this snapshot or across the retained history, hold the same
bytes.

```sql
SELECT hex(content_hash), groupArray(path)
FROM fs('m') WHERE content != 'none'
GROUP BY content_hash HAVING count() > 1
```

**structured content** — a JSON or CSV file is a block column like any other,
so `JSONExtract*`, `splitByChar`, `extractAll` and friends apply to it
directly, and a whole mount of small JSON files is one query away from being
a table.

**joins** — entries carry `path`, `mtime`, `size`, `content_hash` and
`kind`, which is enough to join a mount against any other table in the
database by path or by hash: which source files a profile names, which
documents an ADR cites, which artefacts changed since the last run.

## 8. Properties

What the shape gives:

- **No mutations, ever.** Inserts only; merges are confined to one
  mount-day; there is no `ALTER … DELETE`, no mutation queue, and nothing that
  would complicate replication later.
- **Retention is declarative and exact.** `TTL` drops whole parts; the same
  `expires_at` hides rows at the instant they expire. No garbage collection,
  reference counts or sweeps — the direct consequence of unshared rows.
- **Crash-safe ingest with no cleanup.** An incomplete snapshot is invisible
  and expires by itself; a retry is a new snapshot.
- **Snapshot isolation for readers.** An adapter instance is one immutable
  tree; `(mount, snap)` is its ETag.
- **Time travel and diff for free.** Every retained snapshot is queryable;
  history, churn and diffs are joins and group-bys over the same rows.
- **Reads are key ranges.** No joins on the read path; one block is one
  mark is one compressed block.
- **Content is queryable, not opaque.** Regex and token search, line tables,
  JSON functions and skip indexes apply to blocks — the materialised lane that
  [ADR-0164 §SD7](../adr/0164-documentation-regex-search.md) deferred.
- **Metadata is cheap.** A sorted `path` column compresses well (~10–20×
  is typical for sorted path sets), so metadata-only snapshots of large trees
  cost tens of megabytes; `content_hash` gives deduplication as analytics
  without deduplication as storage.
- **It fits the house.** Existing transport, existing security class,
  existing capability model.

What it costs:

- **Storage is the sum of retained snapshots, undeduplicated.** A ~100 MB
  text corpus snapshotted daily for 90 days is ~9 GB raw and, at a
  ~3.5× text ratio, ~2.6 GB stored — fine. A ~50 GB source tree on the same
  schedule is ~4.5 TB — not fine, and that mount is metadata-only with `ref`
  content. The inline threshold, the cadence and the retention are the knobs,
  and they are per-mount policy rather than schema.
- **A full walk per snapshot.** Incremental ingest is excluded by the
  no-sharing premise (§11 names the variant that would relax it).
- **Latency in milliseconds.** Every adapter call is a round trip. The
  adapter serves batch work, templating, corpus search and exports — not a
  hot serving path, and never writes.
- **Retention granularity is partition granularity.** Per-day partitions put
  effective retention in `[R, R + 1 day]`.

Where it breaks under pressure:

- Many mounts × long retention drive the partition and part count; keep
  mounts × days in the low thousands, or coarsen the partition.
- Terabytes of content at `index_granularity = 1` make the marks files
  noticeable (~24 bytes per column per block — ~150 MB per TB at 1 MiB
  blocks); watch the mark cache.
- A regex that must match across block boundaries is only safe for
  line-aligned text and single-line patterns; a line longer than `block_size`
  falls back to a raw cut.
- One path's history across many snapshots is less index-friendly than any
  query inside a snapshot, because the key is snapshot-first; a bloom filter
  on `path` is the cheap fix if that query matters.
- An expired day lingers physically for up to `merge_with_ttl_timeout`
  (4 h by default); only disk usage notices.

## 9. Compression and the storage estimate

ClickHouse compresses per column, per compressed block, inside one part, and a
compressed block never spans parts. With one block per mark, every `data` row
is its own compressed block, so the `PARTITION BY` expression has no effect on
the compression ratio: it chooses which rows share a part, not which bytes
share a compression window. What sets the ratio is the content itself
(text ~3–5×, source ~4–6×, structured logs ~6–15×, already-compressed formats
~1.0× — ZSTD detects those quickly and adds negligible overhead), the codec
level (`ZSTD(3)` is ~5–10 % smaller than `ZSTD(1)` on text for ~2× the write
CPU), and the block size up to ~1 MiB. Nothing in the engine compresses across
blocks, so the redundancy between snapshots is invisible to compression;
storage is

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
WHERE table = 'fs_data' AND active
GROUP BY partition, column ORDER BY partition, column
```

## 10. Prior art, briefly

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

## 11. Alternatives considered and parked

- **Deduplicated blocks, content-defined chunking (FastCDC).** Buys
  cross-snapshot deduplication and chunk-set analytics (snapshot deltas as
  array algebra); costs a recipe per file, a hash-keyed block store with no
  read locality, a have-query on every ingest, and — decisively — a block's
  lifetime becomes the maximum over its references, which `TTL` cannot
  express. Getting retention back means reference counts, a tracing sweep,
  lease renewal on reference, or bounding deduplication to the retention
  window. All workable; none free. Parked until a mount is a snapshot series
  of large mutable files, which is the only case where it pays.
- **Whole-file content addressing (`ino = sha256`) with an inode table.**
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
  matters, a dictionary in front of `fs_meta` is the ClickHouse-native answer.
- **An in-engine VFS plug point, DuckDB-style.** Not available to an
  out-of-process host; the HTTP and FIFO seams the repository already uses
  are the ClickHouse-shaped equivalent.

## 12. Open decisions — the material for the next iterations

- Block size: 1 MiB (fewer marks) or 256 KiB (finer `ReadAt`).
- Partition unit: day, or per-mount choice between day and month.
- Retention and inline threshold defaults, and where per-mount policy is
  declared (an env var per [ADR-0009](../adr/0009-environment-variable-registry.md),
  a row in a table, or the mount grant itself).
- The text classification rule (media-type sniff, extension list, or both)
  and the fallback for lines longer than `block_size`.
- Whether `fs_data` carries a token/ngram skip index by default or per mount.
- Macro names and shape — `fs`/`fsdata` with optional snapshot, or one
  function with a relation selector; how a snapshot is named in SQL.
- `path` versus a 64-bit hash as the third key column of `fs_data`.
- Where the walker runs and how a mount becomes a grant.
- Whether a small manifest table (one row per snapshot: started, finished,
  counts, bytes) earns its place beside the root-row commit rule, or whether
  the root row and `GROUP BY snap` suffice.

## 13. To verify on a live server before the ADR

- One block is one compressed block for `block_size` in [64 KiB, 1 MiB]
  with `index_granularity = 1` (the flush-at-mark rule this design leans on).
- Whether regular merges still remove expired rows under
  `ttl_only_drop_parts = 1`, and the drop latency after a partition expires.
- `now()` accepted in a `ROW POLICY` condition.
- A `MATERIALIZED` column may depend on another `MATERIALIZED` column
  (`dir` uses `name`); if not, inline the expression.
- `FULL OUTER JOIN` default-filling semantics behind the diff idiom.
- Compression ratio and ingest throughput on one representative mount, per
  §9.
