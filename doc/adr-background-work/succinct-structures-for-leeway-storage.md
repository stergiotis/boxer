---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** An analysis, not a decision. Repo
> claims were checked against the tree on 2026-08-07 and carry file
> references. Literature claims (names, space bounds, who-uses-what) are from
> prior knowledge up to early 2026 and were **not** re-verified — treat them
> as pointers into the literature, not as citations. Measured numbers are
> quoted from [doc/trials/leeway-second-substrate/](../trials/leeway-second-substrate/README.md).

# Succinct data structures and leeway's packed-row / key-value storage (August 2026)

The question this note answers: leeway data is stored (a) **packed** — many
attributes per row, section lanes as parallel arrays, structure recovered by
arithmetic at read time — and (b) as **key-value state** — opaque blobs keyed
by `(appId, key)`, currently rows on `boxer.facts`. How does the body of work
on *succinct data structures* relate to that design, and what opportunities
and threats lie in applying it?

The short form: the relationship is closer to identity than to analogy.
Leeway's cardinality lanes *are* the degree sequences that succinct tree and
sequence structures are built from — leeway just stores the counts and omits
the o(n) index bits, so every read re-derives rank/select by scanning. That
makes the theory a precise vocabulary for what the read path currently pays,
and it points at a small number of well-fitting upgrades. It does not make
leeway a good host for the heavier succinct machinery: most of it either
cannot be expressed on the SQL read surface or duplicates coarse versions
ClickHouse already ships.

## 1 The two storage embodiments, restated structurally

### 1.1 Packed rows

A leeway table stores one entity (or event) per row —
`TableRowConfigMultiAttributesPerRow`
(`public/keelson/runtime/factsschema/factsschema.go`). Within a row, each
tagged-value section contributes parallel array lanes
(`public/semistructured/leeway/common/lw_enums.go`):

- `val` — one element per attribute for scalars, or a flattened element
  stream for array/set values, described by a `len`/`card` count lane;
- membership lanes (`lr`, `lmv`, `hr`, …) — flattened *across* attributes,
  so they are not positionally parallel to `val`;
- `<role>card` lanes (`lrcard`, `lmvcard`, …) — per-attribute membership
  counts, the ragged descriptor bridging the two flattenings;
- parameter lanes (`mvhp`, `mrhp`) — positionally parallel to their
  membership lane.

**Counts are stored; offsets are not.** The role enum reserves
`ColumnRoleCusumLength` / `ColumnRoleCusumCardinality`
(`lw_enums.go:43-44`) and the naming grammar and vdd registry know them, but
no generator emits them — the facts DDL
(`public/keelson/runtime/factsschema/ddl/runtime_facts_ddl.out.go`) carries
only count lanes. Consequently every read that crosses a flattening
recomputes prefix sums:

- `LW_RAGGED_STARTS(card)` = `arrayMap((h,c) -> h - c + 1, arrayCumSum(card), card)`
  (`public/semistructured/leeway/chpack/chpack.go`);
- `LW_RAGGED_PARENT_IDS(card)` materialises, per row per query, an array
  with one element per stream element;
- `pickLcrString` / `pickLcrNumeric` / `pickLcr`
  (`chstore/recentlogs.go`, `chstore/runsessions.go`,
  `queryrunfacts/readback.go`) compose `indexOf` over `arrayCumSum(lrcard)` —
  a predecessor query on the offset sequence, valid under the arity
  guarantees of the
  [component read contract (ADR-0146)](../adr/0146-leeway-marshall-component-read-contract.md);
- `composeLatestStateSql` (`chstore/chstore.go`) uses the same trick twice
  per state read.

The second-substrate trial found this is also where engine portability
breaks: hypothesis H2 held — DuckDB has no `list_cumsum` and DataFusion has
no higher-order array functions at all, so the offset-reconstruction idiom is
the ClickHouse-bound part of the read algebra.

### 1.2 Key-value state

`persist.StorageBackendI` is `Get/Set/Delete` over `(StorageRef, key) →
opaque []byte` (`public/keelson/runtime/persist/backend.go`). The shipped
backend writes each state value as a `boxer.facts` row: symbol-section
attributes carry `"state"`, the appId (mixed channel, id + parameter bytes),
and the key; a blobArray attribute carries the raw value; a delete is a *new*
row whose tombstone is a membership on the bool section. Latest-wins is
`ORDER BY ts DESC LIMIT 1`; workingsets use `HAVING argMax`-style
reconstruction.

Two structural observations:

- **The key lives inside array lanes the primary index cannot see.** The
  table is `MergeTree ORDER BY` the timestamp lane only; a point read is a
  membership-filtered scan (`has(...)` over `lr` lanes) bounded by nothing
  but time order. This is versioned KV storage with its keys stored out of
  reach of the storage engine's own access path.
- The accepted target,
  [ADR-0105](../adr/0105-keelson-adopts-generated-record-stores.md) D3a,
  moves state to a dedicated generated table (string key `"<appId>/<key>"`,
  z64 order lane, u8 lifecycle) — i.e. it fixes the access-path problem with
  a conventional ordered key, not with anything exotic.

### 1.3 What the trial already measured about this shape

From the second-substrate trial (committed protocol and ledger under
`doc/trials/leeway-second-substrate/`):

- The packed row wins **path-in-data** work on every engine tried; the
  exploded companion (one row per attribute, sorted `(path, doc)`;
  [ADR-0171](../adr/0171-leeway-sql-read-surface.md) §SD5) wins
  **retrieval** at every selectivity (1.4× to 5.0×) but costs 2.7–13.2× peak
  memory on regrouping queries and 69.8× on one intersection-shaped query.
- The sharpest cliffs were **expressibility**, not ratios: predicates that
  bind on one representation and not another (e.g. `starts_with` failing
  against BLOB-typed path lanes in Parquet).
- Effect sizes ranked: representation ≫ layout ≫ formulation/format ≫
  engine.

Any proposal below has to justify itself against those axes, not against a
blank slate.

## 2 The succinct lens

### 2.1 Definitions

For data whose information-theoretic minimum size is Z bits, a
**succinct** data structure occupies Z + o(Z) bits *and* answers its
operations without decompressing — typically O(1) or near-O(1) per
operation. ("Compact" relaxes to O(Z).) The distinction that matters here is
not versus *raw* storage but versus *block-compressed* storage: ZSTD-at-rest
also approaches Z, but every operation pays decompression; a succinct
structure is operate-in-place. The field's currency is therefore **which
operations you buy with the o(Z) index bits** — rank, select, predecessor,
tree navigation, prefix search, intersection.

### 2.2 The families relevant to leeway

- **Bitvectors with rank/select** (Jacobson): `rank(p)` = ones before p,
  `select(i)` = position of the i-th one; o(n) extra bits, O(1).
- **Elias–Fano** encoding of monotone sequences: n values up to m in
  n·(2 + ⌈log(m/n)⌉) bits with O(1) `select` (i → value) and
  O(log)-ish predecessor. The standard representation for *offset arrays*
  and *posting lists* (partitioned EF, quasi-succinct indices).
- **Succinct trees** (LOUDS, balanced parentheses): a degree sequence in
  unary is a bitvector; rank/select over it navigate an n-node ordinal tree
  in 2n + o(n) bits.
- **Succinct tries** (LOUDS tries, FST/SuRF): key sets with prefix
  navigation; lexicographic/DFS numbering turns *prefix predicates into
  contiguous id ranges*.
- **Semi-indexing** (Ottaviano–Grossi): keep the raw serialized document
  (JSON/CBOR) untouched; store a tiny succinct skeleton (structure bitvector
  + positions) beside it so field access needs no parse.
- **Roaring bitmaps** — the pragmatic industrial cousin (compressed, not
  strictly succinct, excellent intersection). Already present in this repo:
  `*roaring.Bitmap` is the canonical Go spelling of a `ScalarModifierSet`
  (`marshallreflect/shape.go`), and a note in `public/keelson/vdd/EXPLANATION.md`
  records that ClickHouse's `groupBitmap` aggregate state is
  roaring-compatible on storage. Neither fact is exploited today: sets land
  physically as plain `Array(UInt32)` + `card`, and `groupBitmap` appears
  nowhere in code.

### 2.3 Calibration: where succinctness won and lost industrially

Honest priors, because they cut both ways. Succinct and
succinct-adjacent structures won where workloads are *navigation, point
access, containment, and intersection over relatively static data*: posting
lists in search engines, key filters in LSM stores, bitmap indexes, pattern
matching on static text (FM-index in bioinformatics). They **lost the OLAP
scan half**: the column-store market converged on decode-fast lightweight
encodings (dictionary, delta, FOR, FSST/ALP-class) feeding SIMD scans — see
the column-store market survey note in this directory — and the notable
attempt to sell operate-on-compressed analytics wholesale (AMPLab's
"Succinct", mid-2010s) left no adoption trail.

Leeway's measured workload contains *both halves* — the trial's
retrieve-vs-aggregate axis — so the calibrated position is: succinct
techniques are candidates for the navigation half (membership lookup, path
prefix work, doc-id intersection, offset arithmetic) and are almost
certainly a loss on the aggregate half, where ClickHouse's existing
vectorised scan is the thing to beat.

## 3 The mapping: leeway mechanism ↔ succinct counterpart

| Leeway mechanism | What it is, structurally | Succinct counterpart | What is paid today |
| --- | --- | --- | --- |
| `card`/`len` count lanes | degree sequence of a two-level ragged forest, binary-coded | unary bitvector / Elias–Fano over cumulative offsets | `arrayCumSum` per row per query; idiom is CH-bound (trial H2) |
| `LW_RAGGED_STARTS` / `_RANGES` | `select(i)` → run start | select on a stored offset sequence | recomputed each call |
| `LW_RAGGED_PARENT_IDS` | `rank(p)` → owning attribute, for all p | rank | O(stream length) array materialised per row per query |
| `pickLcr*`, `LW_LU_VAL_IDX_TO_MEMB_IDX_*` | predecessor on offsets, composed with a membership scan | rank/select composition, O(1) | two linear `indexOf` scans per predicate per row |
| membership lanes + `has(...)` | containment query over per-row sets | inverted index / posting lists (EF, Roaring) | per-row array scan; no cross-row access path |
| exploded `(path, doc)` table (0171 §SD5) | materialised inverted index, granule-grained postings | partitioned-EF postings, skip-based intersection | granule pruning works; intersections via semi-join cost up to 69.8× peak memory |
| `lmv` path dictionary (`LowCardinality`) | string dictionary with arbitrary ids | succinct trie / lex-ordered dictionary — prefix = contiguous id range | prefix predicates are per-element string compares; the same predicate class failed to bind at all on BLOB-typed Parquet lanes (U4/U9) |
| membership ref ids (registry `uint64`s) | nominal ids, no order semantics | monotone dictionary / minimal perfect hash as a name↔id artifact | pages carry raw 64-bit literals (0171 §SD4 open) |
| state KV on `boxer.facts` (§1.2) | versioned KV, tombstoned, latest-wins | LSM view: static per-run structures, key filters | keys invisible to the primary index; point read = membership-filtered scan |
| opaque persist blobs | unparsed serialized values | semi-indexing (structure skeleton beside raw bytes) | opaque; typed payloads were declined in ADR-0151's alternatives |
| `ScalarModifierSet` (`*roaring.Bitmap`) | already a compressed set in Go | — (already succinct-adjacent) | decoded into plain `Array(u32)` + `card` at the storage boundary |

The left column is not being *compared* to the right column — it is the
right column with the index bits deleted. A leeway row is a two-level
ordinal forest (row → attributes → members/scalars) whose degree sequences
are the count lanes; LOUDS is the same information plus o(n) bits that make
navigation O(1). The read-back vocabulary
([ADR-0162](../adr/0162-leeway-co-ragged-function-pack.md) pack plus the
`LW_LU_*` family) is a hand-written rank/select library evaluated by
scanning, per row, at query time. That is the precise sense in which
succinct data structures "relate to this problem": they are the theory of
exactly the trade leeway currently takes implicitly — **zero index bits,
pay scan-time navigation** — and they chart the frontier of spending a few
bits to stop paying it.

Two grains must be kept apart, because the payoff differs by orders of
magnitude:

- **Row grain** (inside one row's lanes): arrays are short — the JSONBench
  corpus averages ~12 attributes per document. Succinct machinery per row
  is noise; vectorised recomputation is cheap and cache-friendly. The cost
  that *does* exist at this grain is portability (H2) and the O(stream)
  materialisations, not asymptotics.
- **Corpus grain** (across rows/parts: dictionaries, posting lists, key
  sets, offset columns): this is where the literature's structures actually
  operate, and where every opportunity below lives.

## 4 Opportunities

Ordered by fit; each carries the pain it addresses, the cost, a probe, and a
kill criterion. The ranking deliberately favours moves that stay *inside*
the existing SQL read surface (see threat T1).

### O1 — Emit the reserved offset lanes (`cusumcard`, `cusumlen`)

The degenerate succinct move: store the monotone offsets instead of (or
beside) the counts, in the lanes the role enum already reserves.

- **Addresses:** H2 — engines without `arrayCumSum`/higher-order functions
  can read a stored offset lane with plain relational operators, so the
  ragged algebra stops being ClickHouse-bound. Also removes the per-call
  `arrayCumSum` and the O(stream) `LW_RAGGED_PARENT_IDS` materialisation
  (an offset lane makes parent-of a predecessor query).
- **Cost:** one extra `u64` array lane per count lane. Within a row the
  sequence is strictly monotone, so Delta/T64+ZSTD should compress it to
  roughly the count lane's size (delta of offsets = counts — same entropy;
  this is a claim to measure, not assert). Redundant-emit is
  backward-compatible; emit-*instead* halves the pair's footprint but
  breaks every reader and is ADR-tier.
- **Probe:** a variant arm in the existing second-substrate harness: same
  corpus, offsets emitted, ragged-heavy queries (the U-set) on ClickHouse
  *and* the two file-engines. Note the canonical JSON arm runs at
  membership arity 1, where cumsum reconstruction is dead code — the probe
  only means something on a corpus that exercises `card > 1` or the
  array-section `len` lanes (trial README §7 Q6 is the same gap).
- **Kill:** no measured query-time or portability win at realistic arity,
  or a storage cost that fails to vanish under codecs.

### O2 — Range-structured path dictionary (prefix = id range)

Assign path ids in lexicographic (equivalently trie-DFS) order, so
`startsWith(path, p)` becomes `path_id BETWEEN lo AND hi`. The dictionary
itself can be a front-coded blob with select, a LOUDS trie, or — the 80%
version — a plain sorted table; the *numbering* is where the value is, not
the structure.

- **Addresses:** the U4/U9 predicate class (path prefix work), including
  the case where it currently cannot bind at all (BLOB-typed lanes in
  Parquet — an integer range binds everywhere); makes prefix predicates
  sargable on any engine; gives 0171 §SD4 (membership name↔id from SQL) a
  principled artifact instead of raw literals.
- **Friction, real:** leeway ref ids are registry-owned, stability-first
  (fibonacci-tagged); lexicographic ids are a *derived, ingest-mutable*
  numbering — a newly observed path lands mid-range. So this is an index
  artifact maintained like an index (per-snapshot or per-part numbering
  with a translation step, LSM-style), never a second identity scheme.
  That design question — who owns the numbering, when is it rebuilt — is a
  dialogue-before-code item.
- **Probe:** on the existing exploded table, compare prefix-class queries
  as `BETWEEN` over a lex-id column vs `startsWith` over the string lane,
  ClickHouse and DuckDB/Parquet both.
- **Kill:** if `LowCardinality`+`startsWith` (CH) and dictionary-encoded
  string predicates (Parquet readers) are already within ~1.5× and the
  BLOB-binding defect gets fixed at the type-mapping layer instead.

### O3 — Postings for the announced inverted-index arm (Roaring first)

The trial's stated next direction is an inverted-index arm. The succinct
literature's contribution is the posting-list representation: doc-id sets
per `(section, path)` with skip-based intersection. Two escalation stages:

1. **In-engine, zero new dependencies:** ClickHouse
   `AggregateFunction(groupBitmap, UInt64)` columns (roaring-compatible on
   storage per the vdd note) in a small materialized table keyed by path —
   `bitmapAnd`/`bitmapCardinality` for multi-predicate retrieval. This is a
   succinct-adjacent structure the engine already ships and the repo
   already half-adopted (roaring is a first-class Go type here).
2. **Application-held, only if (1) shows the shape pays:** partitioned
   Elias–Fano postings in Go for the embedded/no-server lane.

- **Addresses:** the retrieval shape (where multi-row already wins 1.4–5×)
  and specifically intersection *memory* — the 69.8× peak-memory outlier is
  a semi-join materialising id sets that a bitmap AND streams through in
  bounded space.
- **Cost:** a third representation to maintain and route to — the exact
  open question 0171 §SD5 already carries for the second one (batch vs
  materialized view; who routes).
- **Probe:** add a groupBitmap arm to the inverted-index trial next to
  tokenbf/text-index arms; measure retrieval latency *and peak memory*
  against the sorted-table semi-join baseline.
- **Kill:** granule pruning + native CH indexes within ~2× on latency and
  without the memory cliff.

### O4 — Semi-index over opaque payloads

For the persist blobs (§1.2) and any `AspectJson`/`AspectCbor`-tagged lane:
keep the value as raw bytes, store a small structural skeleton beside it,
and gain field projection without parsing and *without* the typed-payload
commitment that ADR-0151's alternatives declined. A genuine middle point
that the opaque-vs-typed dichotomy missed.

- **Honest sizing:** today's state values are small and read latest-wins —
  there is no measured pain. This becomes interesting only if large
  serialized payloads with partial-read access patterns appear. Park it
  with that named trigger (descope, don't gate).

### O5 — A static "leeway pack" for embedded readers

An mmap-able snapshot format: value lanes + EF offset lanes + path
dictionary + postings. The trial exposed the gap it would fill: DataFusion
has no persistent native format — its analogue cost 1.23 s and ~10× RAM per
session to rebuild from Parquet. A succinct pack is the natural "leeway
reader without a server" artifact (relevant to the embedded/composable lane
of the market survey, WASM/airgap interests, and data-product
distribution).

- **Honest sizing:** the largest and most research-grade item; a new format
  is a heavy commitment and Parquet-plus-O1/O2 columns may capture most of
  the value. Gate on a concrete embedded-reader need, not on elegance.

## 5 Threats

### T1 — The SQL read surface cannot see inside a succinct blob

The trial's sharpest lesson — cliffs are *expressibility*, not ratios —
cuts against succinct structures hardest. A rank/select bitvector packed
into a blob column is invisible to SQL: the chpack mechanism is SQL macros
(`CREATE FUNCTION … AS`), which can express array arithmetic but not
efficient bit-level operations over packed blobs; ClickHouse offers no
user-defined index extension point. So any structure that is not spellable
as *ordinary typed columns* (O1's offset arrays, O2's id ranges, O3's
groupBitmap states) either degrades into one or drags reads off the SQL
surface — directly against ADR-0171's direction of naming SQL *the* read
surface. This is the single strongest filter on the opportunity list, and
it is why O1–O3 look the way they do.

### T2 — Staticness vs an append-only, continuously-ingesting store

Succinct structures are build-once. The mitigation is the storage engine's
own shape — MergeTree parts are immutable, so per-part or per-snapshot
sidecars fit the same lifecycle as skip indexes — but ClickHouse exposes no
plugin point for custom part-local structures, so sidecars live in separate
tables or outside the engine, and someone owns rebuild-on-merge. Every
maintenance question 0171 §SD5 leaves open for the *second* representation
returns, compounded, for a third.

### T3 — Grain mismatch and constant factors

Per-row lanes are short; o(n) bounds hide constants that only amortise at
corpus grain. Rank/select is also branchy and cache-hostile compared to the
SIMD scans ClickHouse actually executes; on the aggregate half of the
workload the engine's existing execution model wins by default. Applying
succinct machinery at the wrong grain would add complexity with no
measurable return — the probes above exist to catch exactly that.

### T4 — The engine already ships the coarse version of each idea

MergeTree mark files are sampled offset arrays (a coarse select);
`LowCardinality` is the dictionary; tokenbf/ngrambf and the experimental
text index are the posting lists; granule pruning on the sorted companion
table is skip-based intersection at granule resolution. The baseline any
succinct proposal must beat is *those*, already configured well — not
nothing. Several plausible-sounding proposals (wavelet trees over the
section column, per-part key filters for the recordstore) collapse on
inspection into "declare the ClickHouse feature" and are therefore absent
from §4.

### T5 — Ecosystem thinness and verification burden

In Go, Roaring is mature; EF/rank-select/tries are thin or hand-rolled.
Hand-rolling is a few hundred lines of bit manipulation — exactly the code
class where the repo's review discipline ("never oracle a function with
itself") demands independent oracles and golden corpora, so the *testing*
cost, not the writing cost, dominates. The alternative — Rust crates over
the existing FFI boundary — puts a foreign runtime on the data-read path:
a Tier-1 surface decision with supply-chain and airgap consequences, not a
casual dependency bump.

### T6 — Representation multiplication

Packed + exploded already pose an unresolved routing/maintenance question.
Postings (O3) make three; a pack format (O5) four. Each copy is a
consistency liability and a query-routing decision nobody currently makes
automatically. The data-centricity stance of this repo (derivations should
be visible as data) helps audit the copies but does not pay for them.

## 6 Reading

Succinct data structures are the right *lens* on this storage design and,
mostly, the wrong *toolbox* for it. The lens value is real: it names
precisely what the packed format is (a two-level ragged forest stored as
degree sequences with the navigation bits omitted), why reads look the way
they do (rank/select re-derived by scanning, per row, per query), and why
the exploded table wins retrieval (it is a materialised inverted index).
The toolbox value is narrow because of T1/T4: whatever cannot be spelled as
ordinary typed columns is either inexpressible on the SQL surface or
already approximated by the engine.

What survives that filter, in order:

1. **O1** — emit the reserved offset lanes; smallest step, directly attacks
   the one measured portability defect (H2), probe-able in the existing
   harness. Needs an arity>1 corpus to mean anything (the same corpus gap
   the trial already records as its largest untested dimension).
2. **O3, stage 1** — a groupBitmap postings arm inside the already-planned
   inverted-index trial; zero new dependencies, targets the one measured
   memory cliff.
3. **O2** — lex-ordered path ids; real value, but the numbering-ownership
   question needs a design dialogue before any code.
4. **O4 / O5** — parked with named triggers (large partially-read payloads;
   a concrete embedded-reader need).

What would change this picture: an engine exposing pluggable part-local
index structures (T2/T4 soften); the read surface growing a sanctioned
non-SQL lane for embedded readers (T1 softens, O5 rises); or a corpus with
real membership arity showing the cumsum algebra live on hot paths (O1
rises from portability fix to performance fix).
