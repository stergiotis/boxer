---
type: explanation
audience: designer weighing ADR-0259's diff and sync mechanisms against published synchronisation and reconciliation techniques
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-09-25 to feed
> [ADR-0259](../adr/0259-jackstay-guided-clickhouse-to-clickhouse-sync.md)
> (accepted 2026-09-25); nothing here is a decision. The survey is clean-room: it rests
> on published papers, manual pages and product documentation, and no
> third-party source code was read. Each claim about another system is tagged
> either `[verified: URL]` (the page was fetched on the compile date and says
> so) or `[from general knowledge]` (not re-read for this page; check it
> before building on it). The ClickHouse SQL sketches in §5 and §8 were not
> run. No measurement was taken.

# Synchronising tables between two ClickHouse servers — prior art

ADR-0259 compares two tables chunk by chunk with `count()` and
`sumWithOverflow(cityHash64(…))` over key and row, drills one level into a
chunk that differs, copies differing chunks as a relayed `Native` stream,
verifies each chunk after the copy, and resumes from a local journal. A
pending change moves the row hash to `cityHash64(formatRow('RowBinary', …))`.

The question this page answers is which parts of that shape have precedents,
and what the precedents suggest changing. Most of the literature assumes a
setting ours does not share, so each section ends with what carries over.
Our setting, for reference:

- two ClickHouse servers of close versions, holding ephemeral data;
- sorted MergeTree tables, compared as multisets of rows, not as byte files;
- the diff runs inside the servers as aggregate queries, so the costly
  resource is **server scan time**, not bytes on the wire — a digest
  exchange is kilobytes;
- one-way, and an operator reviews the plan before anything is written.

The last two points change the weighting of nearly every technique below.
Most of the surveyed work minimises communication. Here communication is
already small, and the limit is the number of scans.

## 1 rsync — fixed blocks, rolling weak checksum, one round

**Mechanism.** The receiver splits its old file into fixed-size blocks and
sends, per block, a 32-bit weak checksum and a 128-bit MD4 strong checksum.
The sender computes the weak checksum at *every* byte offset of its new file.
The checksum is rolling, so sliding the window one byte costs O(1). A weak
hit is confirmed by the strong hash. The sender then emits block references
and literal bytes. The whole exchange is one round trip
[verified: https://rsync.samba.org/tech_report/node2.html]
[verified: https://rsync.samba.org/tech_report/node3.html]. The rolling
checksum is modelled on Adler-32
[verified: https://rsync.samba.org/tech_report/node3.html]. Tridgell's PhD
thesis (ANU, 1999) analyses block-size choice; the transfer cost is minimised
near a block size proportional to the square root of the file size
[from general knowledge].

**Assumptions.** One side can read the other's entire new content locally, at
every offset, so rolling is cheap. The data is a byte sequence in which an
insertion shifts every later offset.

**What transfers.** Two things. First, a cheap filter confirmed by a strong
check: our `count()` plus a 64-bit sum is the filter, and there is no strong
confirmation. ADR-0259 already accepts this for ephemeral data. Second,
rsync's reason to exist is the **boundary-shift problem**: offset-defined
blocks go stale after an insert. Our chunks are defined by *key values* (a
partition id, or a range of the first sorting-key column), not by offsets.
An insert into one key range does not move the bounds of the others. The
rolling search itself does not transfer. It needs one side to scan every
offset of data it holds, and a SQL aggregate has no equivalent of "try every
alignment". Our data also has no alignment to try, because sort order fixes
it.

## 2 Content-defined chunking, and its analogue for sorted keys

**Mechanism.** LBFS (Muthitacharoen, Chen, Mazières, SOSP 2001) computes a
Rabin fingerprint over a sliding 48-byte window. It cuts a chunk wherever the
low 13 bits of the fingerprint equal a chosen constant, which gives about
8 KiB on average. Chunks are named by their SHA-1 [from general knowledge].
Because a boundary depends only on nearby bytes, an insert disturbs at most
the chunks around it. Boundaries downstream re-synchronise. This is how CDC
resists boundary shift [from general knowledge]. FastCDC (Xia et al., USENIX
ATC 2016) replaces Rabin with a Gear hash, skips cut points below a minimum
size, and "normalizes" the chunk-size distribution into a narrower band
[verified: https://www.usenix.org/conference/atc16/technical-sessions/presentation/xia].
restic uses Rabin-based CDC, and borg and casync use a buzhash rolling hash.
All three deduplicate content-addressed chunks across snapshots
[from general knowledge].

**The sorted-rows analogue already exists: prolly trees.** Dolt stores tables
as prolly trees. These are B-trees whose node boundaries are chosen by
hashing entries rather than by insertion order. Dolt hashes **only the key**,
so a value update never moves a boundary. It makes the split probability
depend on the current chunk size, via a CDF of a target distribution, to
suppress very small and very large chunks
[verified: https://www.dolthub.com/blog/2022-06-27-prolly-chunker/]. The
resulting tree is history-independent: the same key–value set gives the same
boundaries and the same root whatever the insertion order. Diff prunes
equal subtrees by hash
[verified: https://www.dolthub.com/blog/2024-03-03-prolly-trees/].

**Key-hash boundaries for jackstay.** The prolly rule transposes to
ClickHouse. Over the sorted first sorting-key column (or the whole key), a
row starts a new chunk when `cityHash64(key) % M = 0`, for an average chunk
of M rows. Properties:

- *Stable under inserts.* A new key moves a boundary only if it is itself a
  boundary key, which happens with probability 1/M. Otherwise it changes
  the digest of exactly one chunk. A deleted boundary key merges two chunks.
- *Identical on both sides without coordination.* Each server can compute
  its boundaries independently, and the two lists agree wherever the data
  agree. Quantile bounds agree only because the plan copies one side's bounds
  to the other.
- *Cost.* Finding the boundary keys is a scan of the key columns:
  `SELECT key FROM t WHERE cityHash64(key) % M = 0 ORDER BY key`. Quantiles
  also scan, approximately. Neither can use the primary index to skip data.
- *Skew.* When the chunking column has few distinct values (for example a
  date as the first key column), both schemes produce chunks as large as
  the largest value's run. A boundary rule over the full key tuple avoids
  that, but a range predicate on a tuple prunes less well than one on the
  first column [from general knowledge].
- *Variance.* A fixed modulus gives geometric chunk sizes. Dolt's
  size-dependent probability, or a FastCDC-style minimum, narrows the spread.
  Either would need a window or state per row, which SQL expresses poorly. A
  post-pass in the tool (merge chunks below a floor) is simpler.

Compared with ADR-0259's rules:

| Chunk rule | Stable across re-plans | Both sides agree without exchange | Pruned copy | Cost to derive |
| --- | --- | --- | --- | --- |
| partition (`_partition_id`) | yes | yes | yes | metadata only |
| quantile ranges at plan time | no: new quantiles each plan | only by exchanging bounds | yes (first key column) | one approximate scan |
| key-hash boundaries (`% M = 0`) | yes, up to 1/M local change | yes | yes (range between boundary keys) | one key-column scan |
| hash bucket (`cityHash64(key) % B = b`) | yes | yes | no: every bucket is a full scan | none |

Within a single diff–sync session, quantile ranges and key-hash ranges are
equally correct: the plan fixes the bounds and both sides use them. The
difference shows only **across sessions**. That matters if digests are to be
remembered and compared later (§3). A plan that simply **persists and reuses
its previous bounds** gets most of the same stability without a new
boundary rule. Chunks then drift in size as data arrives, and they can be
re-split by key-hash when a chunk grows past a ceiling.

A cheaper source of candidate boundaries is the primary index itself. The
`mergeTreeIndex` table function exposes, per part and mark, the primary-key
values at granule starts
[verified: https://clickhouse.com/docs/sql-reference/table-functions/mergeTreeIndex].
Taking every k-th mark's key value gives roughly equal ranges without
scanning the data. Those values depend on the part layout, which differs
between the two servers, so the bounds would be taken from the source and
copied, as quantiles are.

## 3 Unison — archive, update detection, reconciliation, propagation

**Mechanism.** Unison is a two-replica, state-based synchroniser. It works in
three phases: update detection, reconciliation, propagation
[verified: https://www.cis.upenn.edu/~bcpierce/papers/unisonspec.pdf].
Between runs it keeps an **archive** of the last synchronised state. Without
it, a file present on one side and absent on the other is ambiguous (created
here, or deleted there?). The archive also defines "changed": a path is
changed in a replica if it differs from the archive
[verified: https://www.cis.upenn.edu/~bcpierce/papers/unisonspec.pdf §5]. The
update detector gives a safe over-estimate of dirty paths: a path it does not
mark is guaranteed unchanged since the archive
[verified: https://www.cis.upenn.edu/~bcpierce/papers/unisonspec.pdf].
In practice, Unison's fast check trusts modification time and inode to skip
re-hashing unchanged files [from general knowledge]. A path changed on both
sides to different results is a conflict, and it is left for the user. The
user interface lets the user review and override the reconciler's
recommendations before replicas change
[verified: https://www.cis.upenn.edu/~bcpierce/papers/unisonspec.pdf §3].

**Assumptions.** Two replicas, both writable. The archive is kept by the
synchroniser. A cheap, conservative change signal exists (mtime).

**What transfers.** Reconciliation and conflicts mostly do not, because we
are one-way. Three ideas do:

1. *The archive.* After a verified sync, the journal could keep each chunk's
   bounds and digests as the "last synced state". On the next run, a source
   chunk whose digest equals the archived one is unchanged at the source.
   If the target is assumed untouched, the target need not be re-scanned for
   that chunk. The assumption can be checked cheaply (next point).
2. *A conservative fast check.* ClickHouse has an mtime analogue: the set of
   active parts. `system.parts` gives per part its name, rows, modification
   time and `hash_of_all_files` ("sipHash128 of compressed files")
   [verified: https://clickhouse.com/docs/operations/system-tables/parts].
   If a partition's active part set (names plus hashes) is unchanged since
   the archive, its content is unchanged, with no data scan. A merge renames
   parts without changing content, so this signal over-reports dirt. That is
   the same safe direction as Unison's detector. It applies to each side
   *against its own past*, never across the two sides (§7).
3. *Review before propagation.* ADR-0259's plan-then-sync split is Unison's
   shape: recommendations the operator can read and edit, then propagation.
   Unison's formal "never overwrite a change the user made" property has a
   one-way counterpart. `replace` must not clear rows on the target that the
   operator did not see listed as "extra" or "changed" in the plan.

## 4 Merkle trees and anti-entropy

**Mechanism.** Dynamo (DeCandia et al., SOSP 2007) keeps one Merkle tree per
key range held by a node. Replicas compare roots, then descend only into
children that differ [from general knowledge]. Cassandra's repair builds
trees during a "validation compaction" that reads and hashes every row. The
tree has a fixed depth of 15 (32K leaves) per range, so a leaf covers many
partitions. With one damaged partition in a million, about 30 partitions are
streamed, the leaf's population
[verified: https://docs.datastax.com/en/cassandra-oss/3.x/cassandra/operations/opsRepairNodesManualRepair.html]
[verified: https://axonops.com/docs/data-platforms/cassandra/architecture/distributed-data/repair/].
Riak's active anti-entropy keeps persistent on-disk hash trees that are
updated on each write, so a comparison does not rebuild them
[from general knowledge].

**Assumptions.** Replicas share a partitioner, so leaf ranges coincide by
construction. Descent costs network round trips, and each level is cheap
once the tree exists.

**Depth/fanout trade-off.** A tree of fanout f and depth L over n rows has
leaves of n/f^L rows. Deeper trees localise differences more tightly and
cost more round trips. Wider nodes cost more digest bytes per round.
Cassandra fixes depth and accepts overstreaming. data-diff and pg_comparator
(§6) make it a parameter.

**What transfers — and one fact that changes the calculus.** Our digests are
`count` and a wrapping `sum`, which are **additive**. The digest of a union
of disjoint chunks is the sum of their digests. A Merkle node hashes its
children, so the leaves are needed to rebuild it. An additive digest tree
can be *derived* from its finest level by addition, in the tool, at no
server cost. So the choice is not "one level versus a tree". It is how fine
the single scan's leaves are. One `GROUP BY` returning 2^12–2^16 leaf
digests per side is a few hundred kilobytes to a megabyte, and it yields
every coarser level for free. The price of fine leaves is the cost of
grouping on the server, not the transfer.

The ADR's drill-down re-queries each differing chunk. That is one more scan,
pruned to the chunk, per level. It becomes worth it only when the leaves of
one scan are still too coarse to act on.

## 5 Set reconciliation — polynomials, IBLTs, rateless IBLTs

**Mechanism.**

- *Characteristic polynomials.* Minsky, Trachtenberg and Zippel (IEEE Trans.
  Inf. Theory 2003) encode a set as the characteristic polynomial evaluated
  at sample points. With d differences, about d evaluations suffice, and
  interpolation recovers the difference. Communication is nearly optimal,
  and decoding costs cubic time in d
  [verified: https://ipsit.bu.edu/documents/ieee-it3-web.pdf] (title and
  claim; the cubic decoding cost is [from general knowledge]).
- *IBLT / Difference Digest.* Eppstein, Goodrich, Uyeda and Varghese
  (SIGCOMM 2011) hash each element into k cells of a table. Each cell keeps
  a count, an XOR of element ids and an XOR of their hashes. Subtracting two
  tables cell-wise leaves only the difference. "Pure" cells (count ±1 and
  a matching hash check) are peeled iteratively. Communication is
  proportional to d, in one round
  [verified: http://conferences.sigcomm.org/sigcomm/2011/papers/sigcomm/p218.pdf].
  The table must be sized for d in advance. The paper's strata estimator
  estimates d first [from general knowledge].
- *Rateless IBLT.* Yang, Gilad and Alizadeh (SIGCOMM 2024) encode the set
  into an unbounded stream of coded symbols. The receiver consumes a prefix
  until decoding succeeds, so d need not be known. They report 3–4× lower
  communication than fixed-size schemes at similar computation
  [verified: https://arxiv.org/abs/2402.02668].
- A 2026 preprint targets exactly database replication verification. It
  observes that an IBLT's count array already measures d before decoding,
  and it uses this to size a second round. It reports production use between
  Oracle and MySQL, computing sketches outside the databases
  [verified: https://arxiv.org/abs/2608.26537].

**Assumptions.** Elements are fixed-width ids. Sets, not multisets (a
multiplicity difference of two or more breaks XOR cells). Communication is
the scarce resource.

**Can ClickHouse build an IBLT server-side?** It appears feasible with
ordinary aggregates. The sketch below, with k = 3 and m cells per hash
partition, was not run:

```sql
SELECT cell,
       sum(1)                         AS cnt,     -- or count()
       groupBitXor(h)                 AS id_xor,
       groupBitXor(cityHash64(h, 1))  AS chk_xor
FROM (
  SELECT cityHash64(formatRow('RowBinary', c1, c2, c3)) AS h
  FROM db.t WHERE <chunk predicate>
)
ARRAY JOIN [h % m, m + intHash64(h) % m, 2*m + cityHash64(h, 2) % m] AS cell
GROUP BY cell
```

Each side returns m·k rows. The tool subtracts counts, XORs the ids and
peels. Using `sumWithOverflow` instead of XOR for the id field gives a
multiset-tolerant variant: a pure cell then holds count·id, which is
decodable when the count divides it. Recent work studies such linear
multiset sketches [from general knowledge; not read].

What peeling returns is a set of **row hashes**, not keys or rows. Turning
them into rows needs one more pass:
`WHERE cityHash64(formatRow(…)) IN (…)`. That pass is a full scan of the
chunk, because the hash is not indexed. For a single fixed-width key column,
the key itself could be the IBLT id, and the fetch would then be a primary-
key lookup.

**What transfers.** The IBLT's advantage — communication proportional to d —
buys us little. Our digest exchange is already small. Server scans dominate,
and an IBLT costs one scan per side, the same as a fine-leaved digest
`GROUP BY` (§4). What it adds is row-level identification in one round,
without a drill-down. That helps in one case: few, scattered differences in
a large chunk that cannot be range-pruned, where a drill-down would take
several scans. Its costs are sizing (d unknown; the rateless variant fixes
that but is harder to express in SQL), multiset handling, and the
hash-to-row fetch.

## 6 Database table comparison tools — the closest analogues

**Percona pt-table-checksum / pt-table-sync.** pt-table-checksum walks an
index, preferably the primary key, in "nibbles". Chunk size starts at 1000
rows, and it adapts so that each checksum query takes about `--chunk-time`
= 0.5 s, tracked by an exponentially decaying average. Oversized chunks
(beyond `--chunk-size-limit` = 2.0 × target, estimated with EXPLAIN) are
skipped. Per chunk it stores `COUNT(*)` and a BIT_XOR-combined CRC32 of
`CONCAT_WS('#', cols…)` (MD5, FNV1A_64 and MurmurHash are options).
`--float-precision` rounds FLOAT/DOUBLE, to avoid version-dependent float
text [verified: https://docs.percona.com/percona-toolkit/pt-table-checksum.html].
The query runs on the source, and it is replicated as a statement, so each
replica computes the same checksum over its own rows
[verified: https://manpages.ubuntu.com/manpages/xenial/man1/pt-table-checksum.1p.html].
pt-table-sync offers several algorithms. *Chunk* divides the range of a
numeric/temporal first index column into chunks of about `--chunk-size`
rows. *Nibble* uses `LIMIT` bounds. *GroupBy* and *Stream* are full-table
fallbacks. Within a differing chunk it compares per-row checksums before
fetching rows. It is one-way; bidirectional is experimental
[verified: https://docs.percona.com/percona-toolkit/pt-table-sync.html].

*Transfers.* Time-targeted adaptive chunk sizing is a direct answer to
"quantiles go stale". Size chunks by measured scan time on the source, not
by row count. The BIT_XOR choice is the duplicate-cancelling weakness
ADR-0259 already rejects. Float rounding is a reminder that text-based row
hashing is version-fragile; RowBinary avoids that (§8).

**pg_comparator (Fabien Coelho, DBKDA 2011).** Each tuple gets a key
checksum `kcs = checksum(key)` and a tuple checksum
`tcs = checksum(key || cols)`, 8 bytes by default. A hierarchy of summary
tables is built by **masking the key checksum**: level p+1 is
`SELECT kcs & mask(p+1), XOR(tcs) … GROUP BY kcs & mask(p+1)`. Each level
folds about 2^factor groups (the default factor gives 128 per level), up to
a single root row. The two sides' hierarchies are then merged top-down. Four
outcomes: equal; `kcs` present on both but `tcs` different (descend); `kcs`
only in one table (insert); only in the other (delete). Sum is the default
aggregate, and XOR is available with a signed/unsigned caveat across
engines. The false-negative bound is about k·⌈log n / log f⌉·2^-c
[verified: https://manpages.debian.org/testing/postgresql-comparator/pg_comparator.1.en.html].
The paper describes a checksum algorithm and a two-level variant
[verified: https://www.coelho.net/sw/pg_comparator/].

*Transfers.* This is the closest published match to jackstay. Its levels are
**hash buckets of the key, not key ranges**, so both sides agree on them
without exchanging bounds. It builds all levels from one pass over the
checksum table, and it uses additive or XOR folds, exactly the additivity
point of §4. What it gives up is range pruning: a differing bucket is not a
contiguous key range. pg_comparator materialises the checksum table, so it
does not care. For jackstay, copying a bucket is a scan of the chunk with a
hash predicate. Batching all differing buckets into one
`WHERE cityHash64(key) % B IN (…)` pass makes that a single scan.

**Datafold data-diff / reladiff.** data-diff (archived May 2024) and its
continuation reladiff diff across databases by querying `min(key)` and
`max(key)`, then splitting the range into `--bisection-factor` equal-width
segments (32 by default in current docs, 10 in the technical explanation's
example). It checksums each segment on both sides as a sum of a truncated
MD5 of the concatenated columns. It recurses into mismatching segments, and
below `--bisection-threshold` rows (16384 by default) it downloads the
segment and compares rows locally. Segments run in parallel threads
[verified: https://github.com/datafold/data-diff/blob/master/docs/technical-explanation.md]
[verified: https://data-diff.readthedocs.io/en/stable/python-api.html]
[verified: https://reladiff.readthedocs.io/].

*Transfers.* A bisection with a configurable factor and a threshold below
which rows are compared directly is the "drill down, stop at depth" of
ADR-0259, generalised. The threshold is the missing piece: once a chunk is
small, stop hashing and hand the rows (or their per-row hashes) to the
tool. Equal-width key segments are fragile on skewed keys, and ADR-0259's
quantiles are already better there. Text concatenation for hashing is the
cross-engine necessity that we, being ClickHouse-to-ClickHouse, avoid.

## 7 ClickHouse-native options

- **Replication.** Replicated MergeTree replicas fetch whole parts from each
  other, coordinated through Keeper. Part checksums are verified, so replicas
  hold byte-identical parts [from general knowledge]. `ALTER TABLE … FETCH
  PARTITION` pulls a partition from another replica's Keeper path
  [from general knowledge]. Both need a shared Keeper namespace, which two
  independent servers do not have.
- **`BACKUP` / `RESTORE` with `base_backup`.** An incremental backup
  references unchanged parts of a base backup instead of copying them, and
  restoring needs the base present [verified: https://clickhouse.com/docs/operations/backup].
  This is part-granular change detection against a server's own history, the
  Unison fast check of §3 done by the server. It does not express samples,
  missing-only syncs or column subsets, as ADR-0259 notes.
- **`clickhouse-copier`.** It was moved out of the server bundle to a
  separate repository marked obsolete (24.2 changelog)
  [verified: https://clickhouse.com/docs/resources/changelogs/cloud/release-notes/24_02]
  [verified: https://github.com/ClickHouse/copier]. It is not a basis.
- **`remote()` / `remoteSecure()`.** `INSERT … SELECT FROM remote(…)` on the
  target moves data server to server [from general knowledge]. The same
  function can run the *diff*: the target could compute both sides' chunk
  digests in one query. That only moves the scans, it does not remove them.
- **Part hashes.** `system.parts` exposes `hash_of_all_files`,
  `hash_of_uncompressed_files` and `uncompressed_hash_of_compressed_files`
  (all sipHash128)
  [verified: https://clickhouse.com/docs/operations/system-tables/parts].
  They reflect the part's physical layout: block boundaries, compression,
  merge history. Two servers with the same rows will in general have
  different parts, because inserts arrive in different blocks and merges run
  on different schedules. Part hashes therefore cannot compare *across*
  servers. There is one exception: parts that were copied byte-for-byte
  (`ATTACH` of a copied part directory, replication, restore) and not merged
  since. A Native re-insert, which is what jackstay does, produces new parts,
  so the exception does not arise. Part hashes remain a good *same-side,
  over-time* change signal (§3).
- **Engines with merge semantics.** For ReplacingMergeTree, Collapsing,
  Summing and AggregatingMergeTree, the rows visible without `FINAL` depend
  on how far merges have progressed. Source and target can hold the same
  logical content with different row multisets, and a row digest would
  report a difference that is not one [from general knowledge]. No surveyed
  tool addresses this, because it is specific to engines that merge rows.

## 8 `formatRow('RowBinary', …)` as the row hash input

**What the docs say.** `formatRow(format, x, y, …)` "converts arbitrary
expressions into a string via given format". It is variadic, and "only
row-based formats are supported"
[verified: https://clickhouse.com/docs/sql-reference/functions/type-conversion-functions].
The page does not show RowBinary specifically, and it does not mention `*`.
`formatRowNoNewline` trims the trailing newline, which RowBinary does not
emit in the first place [verified: same page;
the RowBinary no-newline point is from general knowledge].

**RowBinary encoding** [verified: https://clickhouse.com/docs/interfaces/formats/RowBinary]:

- integers and floats: little-endian fixed width; Decimal as a little-endian
  integer of its width; DateTime64 as Int64 ticks. There is no text rendering,
  so no float formatting drift and no dependence on the session time zone.
- String: LEB128 length plus bytes; FixedString: padded bytes.
- Nullable: a one-byte null flag, then the value if not null. **This is the
  main gain over `cityHash64(col, …)`**, whose docs state "Hash of NULL is
  NULL" and advise `cityHash64(tuple(*))` so that NULL-bearing rows are not
  skipped [verified: https://clickhouse.com/docs/sql-reference/functions/hash-functions].
- LowCardinality: "does not affect the wire format", so a LowCardinality and
  a plain column with equal values hash equally.
- Array/Map: LEB128 count, then elements. A Map is an array of pairs, so two
  maps with equal content in a different order hash differently. A Native
  copy preserves order, so this does not cause false differences after
  jackstay.
- Tuple: elements back to back, no names.
- Variant: a discriminant byte by alphabetically sorted type index, then the
  value. It is stable if both sides declare the same Variant.
- Dynamic: self-describing, with the type spec first. JSON: a path count,
  then path–value pairs. Settings such as
  `output_format_binary_encode_types_in_binary_format` and
  `output_format_binary_write_json_as_string` change these bytes.

**Pitfalls for jackstay.**

1. *`*` is not a safe column list.* An issue shows `formatRow('TSV', *)`
   failing with "Unknown identifier: *" in an ALTER context
   [verified: https://github.com/ClickHouse/ClickHouse/issues/34056]. In a
   SELECT, `*` excludes MATERIALIZED and ALIAS columns unless settings say
   otherwise
   [verified: https://github.com/ClickHouse/ClickHouse/issues/54964].
   More importantly, under the `narrower` and `extend` verdicts the two
   sides have different column sets, and `*` expands to different lists. The
   hash input should be the **explicit, ordered list of copied columns**,
   generated from the plan and identical on both sides.
2. *Pin the format settings.* Pass the `output_format_binary_*` and
   `output_format_*` settings that affect RowBinary in the digest query's
   `SETTINGS`, so that a server or profile default cannot change the bytes.
3. *Float bit patterns.* NaN payloads and ±0 differ bytewise while comparing
   equal. After a Native copy the bits are preserved, so this is a false
   difference only for data produced independently on each side.
4. *JSON and Dynamic.* The docs do not say whether the path order in
   RowBinary JSON is canonical. It was checked once, on clickhouse-local
   26.8, on the compile date. Two `CAST(… AS JSON)` literals, equal apart from
   key order, serialized with their paths in different orders. Logically
   equal JSON values can therefore hash differently. The failure is a false
   "changed", never a missed difference. Whether a part's internal layout
   (typed, dynamic and shared paths) adds further variation was not tested.
   With `output_format_binary_write_json_as_string = 1`, the same two
   literals gave equal bytes, paths in sorted order (same build, checked
   later the same day). ADR-0259 pins that setting.
5. *Enum* is encoded as its integer, so a label renamed on one side hashes
   equal [from general knowledge]. The structure verdict's type equality
   already catches this.
6. *Cost.* `formatRow` materialises a String per row before hashing, which
   is more work than hashing columns directly. An informal check on one
   machine, outside any trial, found it slower than `cityHash64(tuple(c1, …))`
   over the same columns. `tuple` also avoids the NULL problem: the tuple of a
   NULL-bearing row hashes to a value, where `cityHash64(1, NULL)` is NULL
   (checked on the same build). A trial under `doc/trials/` should settle the
   choice.

**`cityHash64` versus `sipHash128` over tuples.** The docs warn that hashes
"may be equal for the same input values of different argument types"
(integer widths, named versus unnamed tuples, Map versus Array(Tuple)), and
that ClickHouse's CityHash is v1.0.2, not current upstream
[verified: https://clickhouse.com/docs/sql-reference/functions/hash-functions].
The type-width insensitivity is harmless here, because the structure step
requires equal types. A 128-bit hash (`sipHash128`) would cut collisions
per row. The chunk digest is still a 64-bit wrapping sum, though, so a
UInt128 sum (`sumWithOverflow` keeps the input type
[verified: https://clickhouse.com/docs/sql-reference/aggregate-functions/reference/sumwithoverflow])
is what would widen the chunk-level guarantee. `formatRow` removes the
per-type hashing rules altogether: the digest becomes a hash of a
documented byte encoding, which is easier to reason about across versions
than per-type hash combining [from general knowledge].

## 9 Implications for ADR-0259

Ranked by expected value for the effort. **Adopt** means take it as is,
**adapt** means take the idea in a changed form, **reject** means leave it
out, with the reason. None of this is decided.

1. **Adopt: fine leaves in one scan, and derive the tree locally
   (supersedes "drill down one level").** `count` and a wrapping sum are
   additive, so every coarser level of a digest tree is a sum of finer
   ones. One `GROUP BY` per side at 2^12–2^16 leaves per chunk costs one
   scan and on the order of a megabyte of transfer. It gives a full
   multi-level view without the per-level re-scan the ADR's drill-down
   needs. pg_comparator (one pass, masked folds) and data-diff (factor plus
   threshold) are the precedents. Keep a data-diff-style **threshold**: a
   leaf below N rows is resolved by fetching per-row `(key hash, row hash)`
   pairs, not by further drilling.
2. **Adopt: an explicit copied-column list in the row hash, with pinned
   format settings.** `formatRow('RowBinary', c1, …, cn)` fixes the NULL
   problem that `cityHash64(col, …)` has and removes text-format drift. `*`
   is wrong under `extend`/`narrower` and fails in some contexts. Test JSON
   and Dynamic columns before trusting them, and measure the per-row cost
   against `cityHash64(tuple(…))`.
3. **Adapt: a Unison-style archive of last-synced digests, gated by a part-
   set fast check.** After a verified chunk, the journal already knows the
   chunk's bounds and digests. Keep them. On re-sync, compare each side's
   `system.parts` active set (names plus `hash_of_all_files`) against the
   archive: an unchanged part set means an unchanged partition, with no scan.
   Only dirty partitions are re-digested. This needs stable chunk bounds
   across runs (item 4). It is a same-side test only; part hashes never
   compare the two servers.
4. **Adapt: persist chunk bounds across plans; use key-hash boundaries only
   where re-deriving is needed.** Quantile ranges are correct within a
   session. Their weakness is that each new plan picks new bounds, which
   defeats item 3. Reusing the previous plan's bounds, and splitting a grown
   chunk at key-hash boundaries (`cityHash64(key) % M = 0`, the prolly-tree
   rule), gives insert-stable chunks without replacing quantiles
   everywhere. Prefer partitions whenever the table has them. Consider
   `mergeTreeIndex` marks as a scan-free source of initial bounds, and
   pt-table-checksum's time-targeted sizing as the size criterion.
5. **Adapt: batch hash-bucket copies.** SD4 rule 3 says a hash-bucket chunk
   costs a full scan *per bucket*. For the diff, one `GROUP BY` computes
   every bucket at once. For the sync, all differing buckets can be copied
   in one pass with `WHERE cityHash64(key) % B IN (…)`, and cleared on the
   target with the same predicate. Say so in the ADR, so that rule 3 does
   not read as B scans.
6. **Add a caveat: merge-semantics engines.** For Replacing, Collapsing,
   Summing and AggregatingMergeTree tables, row digests without `FINAL` can
   differ between logically equal tables. Either digest over `FINAL`
   (slower), or record in the plan that such tables report "changed" until
   both sides are fully merged.
7. **Reject for now: IBLT and rateless IBLT.** A server-side IBLT looks
   feasible with `ARRAY JOIN` and `groupBitXor`/`sumWithOverflow`. It saves
   communication, which is not our constraint. It costs the same one scan as
   item 1's fine leaves, needs sizing (or the harder rateless encoding), is
   fragile on multisets, and returns row hashes, which need another full
   scan to turn into rows. Revisit only if measurement shows chunks with few
   scattered differences that neither range pruning nor item 1's threshold
   resolves in one or two scans. Even then, the single-fixed-width-key case
   (key as IBLT id, fetch by primary key) is the one worth building.
8. **Reject: rsync's rolling search and byte-level CDC.** Key-defined chunks
   do not have the boundary-shift problem these techniques solve. The
   transferable part of CDC is the key-hash boundary rule in item 4.
9. **Keep as stated: `remote()` deferred, part-level cross-server diff not
   pursued.** Part hashes differ across servers after any re-insert, and
   replication-style part fetch needs a shared Keeper. `remote()` could
   move the diff's scans to one server as well as the copy, which is worth a
   line in the deferred mode's note.
