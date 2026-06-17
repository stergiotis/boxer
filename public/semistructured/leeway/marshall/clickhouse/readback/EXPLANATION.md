---
type: explanation
audience: leeway maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative. Sub-design for ADR-0066, open-question 1.

# Leeway DQL — non-scalar value mapping

The DQL read-back generator (ADR-0066) turns a `mappingplan.Plan` field into ClickHouse
that locates and extracts that field's value from `anchor.facts`.

The difficulty is an **impedance mismatch** between the DTO's shape and the storage's shape. A
DTO field means *one value, tagged with one membership*. Storage dissolves that two ways at
once: memberships are flattened across a section's attributes — and a value may carry several
(*aliasing*) — so a membership name does not index a value but a flattened *position* that must
be mapped back to an attribute through a cumulative sum; and array/set values are flattened
again, partitioned by their own `len` / `card` columns. Reading back one logical field
therefore means **inverting two independent flattenings in SQL** — membership→attribute, then
attribute→value-subarray — sharing one attribute index. Two consequences make this genuinely
hard rather than merely fiddly: the cheap "is this membership present" test (`has`) is
*necessarily* an over-approximation of "this membership identifies exactly one well-formed
attribute", which is why presence / projection / validator are three separate artefacts; and
the exact path is cumulative-sum arithmetic over 1-based arrays with aliasing and empty
attributes — easy to get subtly wrong, hence the round-trip oracle below rather than shipping
on inspection. (The `Plan`↔IR gap — a `Plan` not knowing physical column names — is the *easy*
part, solved by reusing the IR.)

For **scalar** fields the extraction reduces to `value[a]` for a located attribute `a`; the two
inversions above are what **array** and **set** fields add. This file is the timeless mechanics
of that mapping — the exact ClickHouse arithmetic, the invariants it relies on, and the
round-trip oracle that must prove it correct before the non-scalar path (ADR-0066 v1) is
committed. It is deliberately separate from the per-field SQL templating, which is plain string
assembly once the expressions here are fixed.

## Background

Within one ClickHouse row (one entity) and one tagged-value section `S`, the columns the IR
exposes for `S` are (each is a row-level `Array(...)`, i.e. an Arrow `List<X>`):

- **value subcolumn(s)** `V` — one per leeway sub-column (`val` role). A section may have
  several (`timeRange` → `beginIncl`, `endExcl`; `geoArea` → `polyLat`, `polyLng`, `h3`;
  `text` → `text`, `wordLength`, `wordBag`).
- **membership columns** `M_R` — one per present channel role `R` (`lv`, `lr`, `hr`, `lmr`,
  …), carrying the per-attribute memberships flattened across attributes.
- **support columns** — `LEN` (`len`, array element counts), `SETCARD` (`card`, set element
  counts), and `MC_R` (`<R>card`, e.g. `lvcard`, the per-attribute membership counts on
  channel `R`).

Two naming hazards, both load-bearing:

1. `MC_R` (`lvcard`, `lrcard`, …) is **membership** cardinality; bare `card` (`SETCARD`) is
   **set** cardinality. Different columns, different roles (`ColumnRoleLowCardVerbatimCardinality`
   vs `ColumnRoleCardinality`). Never conflate them.
2. Support columns are **per-section and shared across value subcolumns** (confirmed in the
   anchor DDL: `geoArea` has three `val` subcolumns but one `tv:geoArea:len:…` and one
   `tv:geoArea:card:…`). Membership columns are per-section too.

`cusumlen` / `cusumcard` exist as roles (`ColumnRoleCusumLength` / `ColumnRoleCusumCardinality`)
but are **not materialized** in the current schema, so prefix sums are computed in SQL. If they
are later materialized they replace the `arrayCumSum` / `arraySum(arraySlice(...))` calls below
verbatim.

### The double flattening

```
section S, one entity, A = 3 attributes
                    attr1        attr2      attr3
  value (array)   [ v11 v12  ][          ][ v31 ]      flat V = [v11,v12,v31]
  LEN             [   2      ][    0     ][  1  ]       len(LEN)=A,  len(V)=sum(LEN)
  lv (membership) [ m1a m1b  ][          ][ m3a ]       flat M_lv = [m1a,m1b,m3a]
  lvcard (MC_lv)  [   2      ][    0     ][  1  ]       len(MC_lv)=A, len(M_lv)=sum(MC_lv)
```

Membership flattening (level 1) and value flattening (level 2) are independent — different
support columns — but indexed by the **same** attribute index `a ∈ [1, A]`. Level 1 always
applies (scalar sections included); level 2 applies only to array/set value subcolumns.

## How it works

### Level 1 — membership identity → attribute index

A field binds to a membership identity `(role R, literal L)` (see ADR-0066 §Decision 1; verbatim
→ a quoted name, ref → a generation-time-resolved `uint64`). Its flattened position is
`p = indexOf(M_R, L)` (1-based, `0` if absent). The owning attribute is the first whose running
membership count reaches `p`:

```
a = arrayFirstIndex(c -> c >= p, arrayCumSum(MC_R))          -- 0 when p = 0
```

*Worked* (diagram above, `MC_lv = [2,0,1]` → `arrayCumSum = [2,2,3]`): `L = m3a` ⇒ `p = 3` ⇒
`a = 3`; `L = m1b` ⇒ `p = 2` ⇒ `a = 1` (first `c ≥ 2`); `L = m1a` ⇒ `p = 1` ⇒ `a = 1`. The empty
attr2 (`MC_lv = 0`) is correctly skipped.

**Aligned fast path.** When every attribute carries exactly one membership on `R`
(`MC_R ≡ [1,1,…]`), `arrayCumSum(MC_R) = [1,2,…,A]` so `a = p`. This is the common DTO case, and
the generator may emit `indexOf(M_R, L)` directly when it can prove alignment (Invariant I5,
§Invariants; detection is an open problem, §Trade-offs).

### Level 2 — attribute index → value

- **scalar subcolumn**: `V[a]`.
- **array subcolumn** (shared `LEN`): attribute `a`'s sub-array is
  `arraySlice(V, off(a)+1, LEN[a])` with `off(a) = arraySum(arraySlice(LEN, 1, a-1))`.
- **set subcolumn** (shared `SETCARD`): identical with `SETCARD` in place of `LEN` (set order
  is storage order; comparison is order-insensitive — §Trade-offs).

### Composition

The full extraction for field `f` (section `S`, value subcolumn `V`, channel role `R`):

```
m2v := LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(MC_R)               -- materialized position->attribute map
a   := LEEWAY_LU_ATTR_BY_TAG(M_R, L, m2v)                -- level 1, 0 if absent
out := LEEWAY_VALUE_BY_TAG_EQUAL(V, M_R, L, m2v)         (scalar:    V[a])
     | LEEWAY_LIST_BY_TAG_EQUAL(V, LEN, M_R, L, m2v)     (homogenous array)
     | LEEWAY_LIST_BY_TAG_EQUAL(V, SETCARD, M_R, L, m2v) (set)
```

**Multi-subcolumn sections.** All subcolumns of one section share one membership grain, so
locating `a` once and projecting each subcolumn at that `a` (e.g. `timeRange` → `beginIncl[a]`,
`endExcl[a]`) is the intended evaluation. The generator does **not** emit a `WITH` binding for
this: it re-embeds the byte-identical locate expression per subcolumn, and ClickHouse's
ActionsDAG deduplicates identical subexpressions within a stage — `EXPLAIN actions=1` (26.5)
shows one `indexOf` node, one materialized `m2v`, and one located-attribute node shared by all
of a section's slots. Identical emitted text is therefore load-bearing for performance.

### Miss handling

`indexOf` returns `0` on absence, and `LEEWAY_LU_ATTR_BY_TAG` propagates `0`. ClickHouse is
lenient where this relies on it — `array[0]` returns the element default and `arraySlice(arr,
0, …)` returns `[]` (both verified) — so a missing scalar reads as `''`/`0` and a missing list
as `[]` with no guard. Mandatory fields are gated by the presence prefilter (ADR-0066 (a)); for
Option fields the generator emits an `<= 1` validator term and projects the natural default.

## Reference ClickHouse implementation

The helper UDFs ship in [`lw_readback_udfs.sql`](./lw_readback_udfs.sql) (accessor `HelperUDFsSQL()`),
consolidated from pebble2impl's spinnaker `udfs_tag.sql` plus the anchor unflatten UDF — with
spinnaker's `BEGIN_INCL` bug fixed (it called an undefined `…_END`) and level-2 (value
array/set) extraction added. They are kind-independent (create once per database) and are
verified by a truth-table run through `clickhouse-local` (`lw_readback_udfs_test.go`). Naming follows
spinnaker's `LEEWAY_LU_*` ("Leeway LookUp") convention: "val idx" = attribute index, "memb idx"
= flattened membership position.

The locate primitive is the **materialized** position→attribute map (`MEMB_IDX_TO_VAL_IDX`),
built once per row and indexed — cheaper than a per-position `arrayFirstIndex`, and the form
proven multi-membership-correct by the truth-table:

```
LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(cardCol)        -- [2,0,1,3] -> [1,1,3,4,4,4]
LEEWAY_LU_ATTR_BY_TAG(idCol, lit, m2v)        -- attribute carrying membership `lit` (0 if absent)
                                              --   = m2v[indexOf(idCol, lit)]
```

Value extraction composes the locate with the level-2 unflatten (with
`m2v = LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(cardCol)`, CSE'd across a section's fields; `lenCol` = the
section's `len` for arrays or `card` for sets):

```
LEEWAY_VALUE_BY_TAG_EQUAL(valCol, idCol, lit, m2v)            -- scalar:    valCol[attr]
LEEWAY_LIST_BY_TAG_EQUAL(valCol, lenCol, idCol, lit, m2v)     -- array/set: the attr's sub-array
LEEWAY_LU_MEMBS_OF_VAL_IDX(idCol, cardCol, attr)             -- the attr's membership set (aliasing-aware)
```

*Worked* (a `u64Array` field on the low-card-ref channel, resolved id 2 — the form the round-trip
test generates and runs):

```sql
LEEWAY_LIST_BY_TAG_EQUAL(
    `tv:u64Array:value:val:u64h:g:0:0:0::data`,
    `tv:u64Array:len:len:u64:28o:0:0:0::data`,
    `tv:u64Array:lr:lr:u64:2q:0:0:0::data`,
    2,
    LEEWAY_LU_MEMB_IDX_TO_VAL_IDX(`tv:u64Array:lrcard:lrcard:u64:4gw:0:0:0::data`))
```

**Generated artefacts** (`lw_readback_generator.go`, `Generator.Generate`):

- *presence* — per physical column one `has(col, lit)` / `hasAll(col, [lits…])` term over
  mandatory fields (no false negatives): one array scan and one skip-index condition per
  column. Const fields on scalar **string** value columns additionally contribute the pinned
  value as a value-column term (`has(valCol, 'const')` — a second necessary condition; string
  columns only, since `has` does not coerce a string literal to a numeric array, unlike the
  validator's equality). `has`/`hasAll` are the index-eligible part: a `bloom_filter` skip
  index prunes granules for them but never for `countEqual`/`indexOf` (verified on 26.5,
  `EXPLAIN indexes=1`).
- *projection* — a **named** tuple: `CAST(tuple(<extract>, …), 'Tuple(<GoField> <chType>, …)')`.
  The `CAST` is required: `tuple(x AS name)` does **not** expose `name` for element access in
  ClickHouse (verified — `tupleElement` by name errors), so a downstream UDF cannot address
  slots by name without it. Slot types come from each value column's canonical type via the
  ddl/clickhouse type generator.
- *validator* — `countEqual(idCol, lit) = 1` per mandatory field (`<= 1` for Option; const adds
  a sub-array/value equality check). Exact but **index-blind**: embedded alone it forces a
  full scan. `LEEWAY_LU_VAL_IDX_TO_MEMB_IDX_BEGIN_INCL/END_EXCL` give the per-attribute
  membership ranges the exact path uses; mixed/parametrized channels add the aligned parameter
  match (§Trade-offs).
- *filter* — `presence AND validator`, the form to embed in WHERE: still the exact check, and
  the redundant-looking presence conjuncts are what carry skip-index pruning (the conjunction
  was verified to prune through the `has` terms). ClickHouse's automatic PREWHERE picks the
  filter up, reading the membership/cardinality columns before the wide value columns.

## Invariants

Let `A` be the attribute count of section `S` for an entity.

- **I1** — every per-attribute column has length `A`: `length(V_scalar) = length(LEN) =
  length(SETCARD) = length(MC_R) = A` for each present `R`.
- **I2** — every flattened column's length is the sum of its per-attribute counts:
  `length(M_R) = arraySum(MC_R)`, `length(V_array) = arraySum(LEN)`,
  `length(V_set) = arraySum(SETCARD)`.
- **I3** — support and membership columns are per-section; all homogenous-array value
  subcolumns in `S` share one `LEN` and are co-length per attribute; all set subcolumns share
  one `SETCARD`.
- **I4** — a membership literal `L` identifies at most one attribute for a single-valued field
  ⟺ `countEqual(M_R, L) ≤ 1`; mandatory ⟺ `= 1`. (A value carrying *other* memberships —
  aliasing — does not violate this; `countEqual` counts only `L`.)
- **I5** — fast path: `MC_R ≡ [1,…,1]` ⟺ each attribute carries exactly one membership on `R`
  ⟺ `a = p`.

A violation of I1–I3 is malformed ingestion, not a query concern; the driver already assumes it
(see `streamreadaccess/EXPLANATION.md`, "co-section topology"). I4 is enforced by the validator.

## Correctness — the round-trip oracle

This arithmetic is error-prone (1-based indexing, the `p = 0` guard, cumulative-sum off-by-one,
multi-membership flattening). It will **not** ship on inspection. The oracle is `marshallreflect`
reading the same Arrow batch: it is the canonical read-back, so the generated SQL must agree
with it cell for cell.

**Setup** (reuses the `anchor/card_anchor_integration2_test.go` pattern — HTTP client to
`localhost:8123`, skip-if-down, `//go:embed *.sql`):

1. Generate the kind's `anchor.facts` Arrow batch from the avalanche/cyber/drone data
   generators (the DML write path), insert it.
2. Exec the static helper UDFs + the generated `leeway_has_<K>` / `leeway_is_<K>` UDFs.
3. **Oracle**: run `marshallreflect`'s read of `(batch, Plan_K)` → per entity, the decoded field
   values, or a decode error.

**Assertions** (per row):

- **Validator agreement** — `leeway_is_K(row)` is true ⟺ `marshallreflect` decodes the row as
  `K` without error. This is the core invariant.
- **Projection agreement** — for rows the oracle accepts, each projected tuple slot equals the
  oracle's field value: byte-exact for strings/blobs, element-wise for arrays, order-insensitive
  for sets, with float tolerance.
- **Presence soundness** — `{ leeway_is_K } ⊆ { leeway_has_K }` (no false negatives). The
  false-positive rate of `leeway_has_K` is recorded, not asserted.

**Fixture matrix** — shape × channel × edges, each a small hand-authored `Plan` with expected
values:

| Shape | Anchor section(s) | Notes |
|---|---|---|
| scalar | `symbol`, `timeRange`, `geoPoint` | level 1 only; multi-subcolumn (timeRange/geoPoint) |
| homogenous array | `symbolArray`, `stringArray`, `u64Array`, `geoArea.polyLat/Lng`, `text.wordBag` | `LEN` path |
| set | `u64Set`, `u32Set`, `geoArea.h3` | `SETCARD` path |
| mixed-subtype section | `text` (scalar + array), `geoArea` (array + set) | per-subcolumn subtype dispatch |

Edge cases (each its own fixture): empty section (`A = 0`); empty array attribute (`LEN[a] = 0`);
aliased value (`MC_R[a] > 1`, one value two memberships); attribute with zero memberships on a
channel (`MC_R[a] = 0`); duplicate membership (`countEqual > 1` — validator must reject);
absent optional field; present-membership / absent-value (false-positive probe for presence).

**Two regression layers.** (1) Golden `*.out.sql` files for the generated artefacts — regresses
the *generator*. (2) The round-trip assertions above — regress *correctness*. v1 (non-scalar) is
gated on layer 2 green across the matrix; `clickhouse local` over the Arrow IPC file is the
CI-friendly variant when no server is present.

## Trade-offs

- **Mixed / parametrized channels (ADR-0008 Cut 2).** The parameter columns (`mrhp`, `mvhp`) are
  themselves flattened by their own `MC`, so an exact parameter match recurses level 1 on the
  parameter channel at the same `a`. The `MembershipResolver` already returns the parameter
  match (ADR-0066); the recursion is sketched only — no `Plan` exercises it until Cut 2.
- **Set semantics.** Sets are stored as arrays in storage order; the validator and the
  comparison treat them as order-insensitive multisets. Whether duplicates are possible within a
  set attribute is a write-path invariant to confirm in the matrix.
- **Performance.** Per-row `arrayCumSum(MC_R)` / `arraySum(arraySlice(...))` are recomputed per
  membership lookup. Materializing `cusumlen` / `cusumcard` turns both into an `O(1)` index.
  Measure before optimizing; the fast path (I5) already avoids the cumulative sum for the common
  single-membership case.
- **Index use.** The presence terms are the only granule-pruning handle, and they prune only if
  a skip index exists on the membership (and, for consts, value) columns. Nothing in leeway
  emits one today — `ddl/clickhouse` produces no `INDEX` clauses and `encodingaspects` has no
  index vocabulary — so on a plain MergeTree the presence terms cost one array scan per column
  and prune nothing. The schema-side design (an aspect mapping to
  `INDEX … TYPE bloom_filter(p) GRANULARITY g`) is open; see ADR-0066's 2026-06-09 update for
  the verified eligibility matrix.
- **Fast-path detection.** Proving `MC_R ≡ 1` at generation time (to emit bare `indexOf`) needs a
  schema signal — a section use-aspect asserting single-membership uniformity, or a per-channel
  invariant on the `Plan`. Open; until then the safe `leeway_attr_of_member` form is emitted
  unconditionally.

## Further reading

- [ADR-0066 — leeway dql ClickHouse read-back generator](../../../../../../doc/adr/0066-leeway-dql-clickhouse-readback-generator.md) — the decision this sub-design fills in (open-question 1).
- [ADR-0008 — leeway marshall extensions](../../../../../../doc/adr/0008-leeway-marshall-extensions.md) — membership channels; the Cut-2 parametrized/mixed channels.
- `streamreadaccess/EXPLANATION.md` — the column-major vs attribute-major layout and the cardinality-support reading the driver already does.
- `marshallreflect/unmarshal.go` — the read-back behaviour that is this mapping's oracle.
- `anchor/card_anchor_udf_unflatten_leeway_array.sql` — the existing unflatten UDF generalized as `leeway_unflatten`; `anchor/card_anchor_integration2_test.go` — the harness pattern.
- `common/lw_enums.go` — `ColumnRoleE` (`len`, `card`, `<role>card`, `cusum*`) and `MembershipSpecE`.
