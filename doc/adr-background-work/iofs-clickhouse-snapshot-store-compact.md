---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
> The design only — no history, motivation or alternatives. Those live in
> [the full note](./iofs-clickhouse-snapshot-store.md), together with the
> verification list; this page is what that note converged on (2026-08-19).

# Snapshot store for file trees — compact design (August 2026)

A walk of an `fs.FS` is written once as a **snapshot** into **facts-shaped
ClickHouse tables** — the `boxer.facts` leeway row shape on separate tables
whose partitioning, key, retention and indexes the store controls. Rows are
never updated or shared; retention is `TTL`; a mount is identified by a tagged
id the application owns. Every `io/fs` operation is a key-range query; grep,
history, diff and du are SQL over the same rows.

## 1. Concepts

| Term | Meaning |
|---|---|
| **store** | one set of facts-shaped tables (`fsmeta`, `fsdata`, `fssnap`), created and owned by a generated record store with its own engine clause; the unit of capability (a grant covers a store); a *profile* fixes its parameters |
| **mount** | one tree in a store, identified by a **tagged id the application supplies**; the store carries it verbatim in `id:id:u64` on every row; the unit of snapshotting and the key prefix |
| **snapshot** | one complete walk of a mount, identified by its start time `snap`; complete iff its root row (`path = '.'`) exists — inserted last, it carries the totals and the applied policy and is the commit record |
| **entry** | one facts row of `fsmeta`: backbone = key, sections = the `Stat` plus content hash, content mode, error |
| **block** | one row of `fsdata`: `block_size` bytes of one file, keyed by the entry's key plus an ordinal; **unshared** — no two entries share a block |
| **retention** | a class per mount (`'7d'`, `'30d'`, `'90d'`, …); every row carries `expiresAt = day end + class`; tables are partitioned by expiry day |
| **policy** | per mount: retention class, inline threshold (`inline_max`), text rule; held in a **policy kind in `boxer.facts`** keyed by the mount's id; recorded on each snapshot's root row |
| **content mode** | `none` (stat only) \| `blocks` (stored, size ≤ `inline_max`) \| `ref` (fetched from the live source) |

Names are `io/fs` names: unrooted, `/`-separated, no `.`/`..` elements, root `.`.

## 2. Identity rules

- A mount's id is a prefix-free tagged 64-bit id minted by the **application** under **its** tag. The tag is the application's degree of freedom (grouping, access).
- The store **never** claims a tag, mints an id, inspects the body, or assumes a tag width. `LW_ID_BODY` / `LW_ID_TAG_VALUE` are for display and for the application's own grouping.
- Ids of different applications interleave in one store and cannot collide (prefix-free code).
- Name → id resolution belongs to the application (or the runtime component registering the mount); the store accepts ids.

## 3. Tables

Physical schema: the `boxer.facts` `TableDesc` rendered by a generated record store (`SharedRA` binds the existing read access; `gen.Input.DDL` supplies the clauses). Logical schema of an **entry row**:

| Logical | Leeway slot | Physical |
|---|---|---|
| `mount` | `id:id:u64` — the application's tagged id | `UInt64` |
| `snap` | `ts:ts:z64` | `DateTime64(9)` |
| `path` | `id:naturalKey:y` | `String` |
| `expires_at` | `lc:expiresAt:z64` | `DateTime64(9)` |
| `mode`, `block_size`, `blocks` | `u32Array` — `fsMode`, `fsBlockSize`, `fsBlocks` | |
| `size`; `snap_entries`, `snap_bytes` (root row) | `u64Array` — `fsSize`, `fsSnapEntries`, `fsSnapBytes` | |
| `mtime` | `timeArray` — `fsMtime` | |
| `link_target`, `err` | `stringArray` — `fsLinkTarget`, `fsErr` | |
| `content_hash` | `blobArray` — `fsContentHash` (BLAKE3) | |
| `content`, `kind`, marker; applied policy (root row) | `symbol` — `fsContent`, `fsKind`, `fsKindEntry`; `fsTtlClass`, `fsTextRule` | |
| `inline_max` (root row) | `u64Array` — `fsInlineMax` | |
| `text` | `bool` — `fsText` | |
| `name`, `dir`, `depth`, `ext`, `is_dir`, `is_symlink` | `MATERIALIZED` over `naturalKey` / `fsMode`, added by `ALTER` | hidden from `SELECT *` |

**Block row** (`fsdata`): same backbone, ordinal encoded per §9; attributes `fsData`, `fsBlockHash` (`blobArray`), `fsLine0` (`u32Array`), marker `fsKindBlock`.

**Engine clauses and post-`EnsureTable` `ALTER`s**

```sql
-- boxer.fsmeta
PARTITION BY toYYYYMMDD("lc:expiresAt:z64:4::0:")                              -- one expiry day per partition
ORDER BY ("id:id:u64:47::0:", "ts:ts:z64:47::0:", "id:naturalKey:y:4::0:")     -- (mount, snapshot, path)
TTL "lc:expiresAt:z64:4::0:"
SETTINGS index_granularity = 1024, ttl_only_drop_parts = 1, allow_suspicious_low_cardinality_types = 1;

ALTER TABLE boxer.fsmeta
    ADD COLUMN name  String MATERIALIZED splitByChar('/', "id:naturalKey:y:4::0:")[-1],
    ADD COLUMN dir   String MATERIALIZED multiIf("id:naturalKey:y:4::0:" = '.', '', position("id:naturalKey:y:4::0:", '/') = 0, '.',
                                             substring("id:naturalKey:y:4::0:", 1, length("id:naturalKey:y:4::0:") - length(name) - 1)),
    ADD COLUMN depth UInt16 MATERIALIZED if("id:naturalKey:y:4::0:" = '.', 0, length(splitByChar('/', "id:naturalKey:y:4::0:"))),
    ADD COLUMN ext   LowCardinality(String) MATERIALIZED if(position(name, '.') = 0, '', concat('.', splitByChar('.', name)[-1])),
    ADD CONSTRAINT valid_path CHECK "id:naturalKey:y:4::0:" = '.' OR NOT hasAny(splitByChar('/', "id:naturalKey:y:4::0:"), ['', '.', '..']),
    ADD INDEX ix_dir dir TYPE bloom_filter GRANULARITY 4;
-- is_dir / is_symlink: bitTest on the extracted mode attribute, same way

-- boxer.fsdata: ORDER BY (id, ts, naturalKey[, ordinal]); same PARTITION BY / TTL;
--               SETTINGS index_granularity = 1 (corpus) | default (fleet)
-- boxer.fssnap: ORDER BY (id, ts); filled by
CREATE MATERIALIZED VIEW boxer.fssnap_mv TO boxer.fssnap AS
SELECT * FROM boxer.fsmeta WHERE "id:naturalKey:y:4::0:" = '.';
```

Why these clauses: the key makes `Stat` a point, a subtree a `startsWith` range, a file's blocks one contiguous range; `ReadDir` rides the bloom on `dir`. Partitioning by expiry day makes every partition expire at once (`ttl_only_drop_parts` drops whole parts) with a partition count independent of the mount count. One block = one mark = one compressed block in the corpus profile (`block_size` ∈ [64 KiB, 1 MiB]). Text files are cut at the last newline before `block_size`, with `line0` per block.

**Profiles**

| Parameter | Corpus (few mounts, large files) | Fleet (very many small trees) |
|---|---|---|
| `fsmeta` `index_granularity` | 1024 | 8192 |
| `fsdata` `index_granularity` | 1 | default |
| `block_size` | 1 MiB (256 KiB if `ReadAt` granularity matters) | 1 MiB (rarely reached) |
| per-block `hash` | yes — BLAKE3 chaining value for 1 KiB-aligned blocks, standalone digest for newline-cut text blocks | none |
| projections on `fsmeta` | none | `by_path` (`ORDER BY (naturalKey, id, ts)`), optionally `by_hash` |
| partitioning | expiry day | expiry day; week/month for low cadence |

Hash: BLAKE3 everywhere (house standard); `SHA256()` only server-side if an interop join ever needs it.

## 4. SQL surface: `fs()` and `fsdata()`

Macros (nanopass rewrites, classified read like `keelson()`); the primary argument is the mount's id, a name is sugar resolved through the policy kind.

```
fs(m)              → (SELECT <projection> FROM boxer.fsmeta WHERE id = m AND expiresAt > now() AND ts = <latest>)
fs(m, snap)        → … AND ts = snap
fs(m, '*')         → … AND ts IN (SELECT ts FROM boxer.fssnap WHERE id = m AND expiresAt > now())
fsdata(m[, snap])  → the same over boxer.fsdata
<projection>       = the entry kind's generated Projection, exposing the logical names (path, snap, size, mtime, …)
<latest>           = (SELECT max(ts) FROM boxer.fssnap WHERE id = m AND expiresAt > now())
```

The expansion carries three things: the completeness rule (latest = newest root row), the logical cutoff (`expiresAt > now()`, same column as the `TTL`), and the capability check (a grant covers a store; visible mounts are an id set or "all ids under a tag").

## 5. Writing a snapshot

1. Caller hands the walker the mount's id and its policy. `snap := now()`; `expires_at := toStartOfDay(snap) + 1 DAY + duration(class)`.
2. `fs.WalkDir`; one entry row per node through the generated `Ingest` (DML → Arrow → ArrowStream); walk/open errors go into `err`, the walk continues.
3. Files with `size ≤ inline_max`: blocks into `fsdata` (text: newline-cut + `line0`; else fixed `block_size`), hashing the same bytes into `content_hash` (and, corpus profile, each block into `hash`). Larger files: `content = 'ref'`; metadata-only mounts: `'none'`.
4. After every other insert is acknowledged: the root row `path = '.'` with totals and applied policy → the MV copies it to `fssnap` → the snapshot is visible.
5. Batch across mounts (thousands of rows per insert); root rows of a batch go in a later insert than the batch's other rows.

A failed or running walk has no root row: invisible, expires by `TTL`; a retry is a new `snap`.

## 6. Reading: the `io/fs` adapter

An adapter pins `(store, id, snap)` and is an immutable `fs.FS` (`StatFS`, `ReadDirFS`, `ReadFileFS`, `GlobFS`, `ReadLinkFS`, `SubFS`; `File` has `ReaderAt`/`Seeker`); `testing/fstest.TestFS` per snapshot. `m` = mount id.

| Operation | SQL |
|---|---|
| `Open` / `Stat` / `Lstat` | `SELECT mode, size, mtime, content, block_size, blocks, text FROM fs(m) WHERE path = 'a/b.txt'` — zero rows ⇒ `ErrNotExist` |
| `ReadDir` | `SELECT name, mode, size, mtime FROM fs(m) WHERE dir = 'a' ORDER BY name` (paged: `AND name > :last … LIMIT n`) |
| `WalkDir` | `SELECT path, mode FROM fs(m) WHERE path = 'a' OR startsWith(path, 'a/') ORDER BY splitByChar('/', path)` — pre-order DFS; `SkipDir` ⇒ `AND NOT startsWith(path, 'a/skip/')` |
| `Glob` | `SELECT path FROM fs(m) WHERE match(path, '^usr/[^/]*/bin/ed$')` — `path.Match` compiled to RE2 |
| `ReadLink` | `SELECT link_target FROM fs(m) WHERE path = 'a/l' AND is_symlink` |
| `Read` | `SELECT data, hash FROM fsdata(m) WHERE path = 'a/b.txt' ORDER BY seq` — verify each block on arrival if `hash` present |
| `ReadAt(o, n)` | `… AND seq BETWEEN intDiv(o, bs) AND intDiv(o + n - 1, bs) ORDER BY seq`, trim first/last block |
| `ReadFile` | the stream, or `SELECT arrayStringConcat(arrayMap(t -> t.2, arraySort(t -> t.1, groupArray((seq, data))))) FROM fsdata(m) WHERE path = 'a/b.txt'` |
| errors | zero rows → `ErrNotExist`; bad name → `ErrInvalid`; ungranted mount → `ErrPermission` at expansion; `content = 'none'` → `Read` fails typed; `'ref'` → adapter fetches from the live source |

## 7. Operations beyond `io/fs`

```sql
-- grep with line numbers (text blocks end at newlines, so a single-line match never straddles blocks)
SELECT path, line0 + i - 1 AS lineno, line
FROM fsdata(m) ARRAY JOIN splitByChar('\n', data) AS line, arrayEnumerate(splitByChar('\n', data)) AS i
PREWHERE match(data, 'TODO') WHERE match(line, 'TODO') ORDER BY path, lineno;

-- history: a mount over time; one path's versions
SELECT snap, snap_entries, snap_bytes FROM fs(m, '*') WHERE path = '.' ORDER BY snap;
SELECT snap, size, mtime, hex(content_hash) FROM fs(m, '*') WHERE path = 'a/b.txt' ORDER BY snap;

-- diff between two snapshots ('' is never a valid path ⇒ safe "missing side" marker)
SELECT if(n.path != '', n.path, o.path) AS path,
       multiIf(o.path = '', 'added', n.path = '', 'removed',
               n.content_hash != o.content_hash OR n.mtime != o.mtime, 'modified', 'same') AS change
FROM fs(m, s2) AS n FULL OUTER JOIN fs(m, s1) AS o ON n.path = o.path
WHERE change != 'same' ORDER BY path;

-- du: every directory's recursive size in one pass
SELECT anc, sum(size) AS bytes, count() AS files
FROM fs(m) ARRAY JOIN arrayMap(k -> arrayStringConcat(arraySlice(splitByChar('/', path), 1, k), '/'), range(1, depth)) AS anc
WHERE NOT is_dir GROUP BY anc ORDER BY bytes DESC;

-- what is where
SELECT path, size FROM fs(m) WHERE NOT is_dir ORDER BY size DESC LIMIT 20;
SELECT path, mtime FROM fs(m) WHERE mtime > now() - INTERVAL 1 DAY ORDER BY mtime DESC;
SELECT ext, count(), sum(size) FROM fs(m) WHERE NOT is_dir GROUP BY ext ORDER BY 3 DESC;
SELECT path, err FROM fs(m) WHERE err != '';

-- identical content (dedup as a question, not as storage)
SELECT hex(content_hash), groupArray(path) FROM fs(m) WHERE content != 'none' GROUP BY content_hash HAVING count() > 1;

-- audit: every block against its digest
SELECT count() AS bad FROM fsdata(m) WHERE BLAKE3(data) != hash;

-- across mounts
SELECT id, max(snap) AS latest FROM boxer.fssnap WHERE expiresAt > now() GROUP BY id;
SELECT id, snap, size, mtime FROM fs('*') WHERE path = 'etc/config.yaml' AND snap > now() - INTERVAL 7 DAY;   -- spelling open
SELECT LW_ID_TAG_VALUE(id) AS tag, count() AS mounts FROM boxer.fssnap WHERE expiresAt > now() GROUP BY tag;
```

Structured content (JSON/CSV files) is a block column: `JSONExtract*`, `splitByChar`, `extractAll` apply directly. Entries are facts rows, so they join with any other table by id, path or hash, and other domains can formulate components over them later.

## 8. Guarantees and limits

- No mutations (inserts only; a per-mount purge is a rare lightweight `DELETE` on request). No garbage collection, reference counts or sweeps — unshared rows make `TTL` exact; the macro's cutoff makes expiry exact in results too.
- Crash-safe ingest without cleanup; snapshot isolation for readers; `(id, snap)` is an ETag.
- Every retained snapshot is queryable: history, diff, churn are joins and group-bys; the snapshot index is derived, not maintained.
- Reads are key ranges; content is queryable (regex, tokens, lines, JSON); integrity is auditable in SQL.
- One row shape with `boxer.facts` (shared read access, codecs, vocabulary) while the store controls partitioning, key, retention and indexes; identity-agnostic; scale changes parameters, not shape.
- Costs: storage = Σ retained snapshots, undeduplicated (bound it with `inline_max`, cadence, retention class); wide facts-shaped rows (~170 column streams) make insert/merge cost the number to measure at fleet scale; store-wide scans pay attribute extraction unless hot attributes are materialised; a full walk per snapshot; millisecond latency per adapter call (batch/templating/search/export, not a hot serving path); effective retention in `[R, R + 1 day]`.

## 9. Open decisions

- **Block ordinal and `fsdata` shape**: ordinal as a `naturalKey` suffix (`path ‖ '\0' ‖ be32(seq)`, no `TableDesc` change) vs a generic `rt:ordinal:u32` routing plain (one migration of `boxer.facts`) vs leeway value cardinality (one row per file, blocks as N values — fleet-friendly, wrong for large files); and whether `fsdata` is facts-shaped or bespoke — decided by the insert-cost measurement.
- Corpus block size (1 MiB vs 256 KiB); partition unit per store (day vs week/month); the retention-class set; `inline_max` / text-rule defaults; the policy kind's shape.
- Text classification rule and the fallback for lines longer than `block_size`; token/ngram skip index on `fsdata` by default or per mount.
- Macro spellings: snapshot naming, name-as-sugar, "every mount of a store" / "every mount under a tag".
- Which hot attributes to materialise besides the tree columns; whether the fleet profile carries `by_path` by default.
- Where the walker runs; how a store grant and mount visibility (id set / tag / row policy) are declared.

The verification list (generator, decode, materialised views, compression, insert throughput, BLAKE3 subtree API) is in the full note, §13.
