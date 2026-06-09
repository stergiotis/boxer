---
type: adr
status: accepted
date: 2026-06-03
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-08
---

# ADR-0066: leeway dql — ClickHouse read-back generator from `mappingplan`

## Context

Leeway stores many orthogonal document kinds in one sparse, columnar
ClickHouse table (the "single unified event bus" pattern demonstrated by
`public/semistructured/leeway/anchor/`, where drone, avalanche, and cyber
events share `anchor.facts`). Each tagged-value section is a struct-of-arrays:
a `value` column plus membership columns (`lr`/`hr`/`lv`/…) and support
columns (`*card`, `len`), all named by the human-readable naming convention —
e.g. `` `tv:symbol:lr:lr:u64:2q:0:0:0::data` `` encodes scope, section, role,
canonical type, encoding hints, aspects, table-row-config, and groups.

Three generation surfaces already derive from a schema: DDL (`CREATE TABLE`,
`ddl/clickhouse`), the Arrow write path (`dml`, ADR-0042), and Go read-access
accessors (`readaccess`). The **query** surface does not exist: reading one
document kind back out is hand-written CH SQL today (`anchor/card_anchor_dql_query*.sql`),
and `dql/lw_dql_ir.go` (`InformationRetrieval`) is an unused metadata stub.

A document kind is already captured precisely by a `mappingplan.Plan`
(ADR-0008): plain columns plus `lw:`-tagged fields, each with a section, a
membership name, a channel, and a value shape. `marshallgen` / `marshallreflect`
turn a `Plan` into wire codecs. This ADR adds the SQL **read-back** analog of
`marshallreflect`'s unmarshal: given a `Plan`, generate the ClickHouse needed to
find, project, and validate rows of that kind in the shared table.

Two properties of the substrate shape the whole design:

1. **A `Plan` is logical; physical column names are not derivable from it.** A
   `Plan` knows Go type, section, membership name, and one of four simple
   channels. It does **not** know canonical type, encoding hints, value/use
   aspects, co-section/streaming groups, or the full `MembershipSpec` — exactly
   the attributes the naming convention bakes into a column name. The physical
   schema is authored separately (`TableManipulator` → `TableDesc` → IR;
   see `anchor/card_anchor_schema.go`) and is strictly richer. Faithful SQL
   therefore needs the IR, not just the `Plan`. (Concretely: `anchor`'s `symbol`
   section carries `MixedLowCardRefHighCardParameters`, a spec the `Plan` parser
   cannot even express.)

2. **The layout is doubly flattened, and values alias.** Within a section, the
   `value` array holds one element per attribute, but membership arrays are
   flattened across attributes with per-attribute counts in `<role>card`; and
   array/set value columns are themselves flattened with `len` / `card`
   support. A single value may carry several memberships (aliasing — e.g. `19.99`
   tagged `/price` and `/min_price`). Consequently a cheap "is this membership
   present" test can over-approximate, while pinning a value to a specific
   membership requires cumulative-sum arithmetic over the support columns. This
   gap between cheap-and-conservative and exact is the reason the generator
   emits a *ladder* of artefacts rather than one query.

## Design space (QOC)

**Question.** How do we mechanically derive faithful ClickHouse read-back SQL
for a specific document kind defined as a `mappingplan.Plan`?

**Options.**

- **O1** — Status quo: hand-write CH SQL per kind against the physical columns.
- **O2** — Generate from the physical IR / schema alone (no `Plan`): per-section
  projections driven only by `IntermediateTableRepresentation`.
- **O3** — Generate from a `Plan` ↔ IR join: the `Plan` supplies the kind's
  field set and read shape; the IR (via `InformationRetrieval` + the naming
  convention) supplies the physical column names and types.

**Criteria.**

- **C1** — Document-kind binding: can it produce a per-kind presence / projection
  / validator, not just generic per-section SQL?
- **C2** — Physical-name fidelity: does it reproduce exact naming-convention
  column names without hand re-derivation?
- **C3** — Reuse of existing infra (IR, `InformationRetrieval`, naming
  convention, write-side membership registry).
- **C4** — Testability: is there a mechanical correctness oracle?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 (hand-written) | O2 (IR-only) | O3 (Plan ↔ IR) |
|----|-------------------|--------------|----------------|
| C1 | −−                | −−           | ++             |
| C2 | +                 | ++           | ++             |
| C3 | ++                | ++           | +              |
| C4 | −                 | +            | ++             |

O2 reproduces physical names but cannot express "which memberships constitute
kind K" — that knowledge lives only in the `Plan`. O3 is the only option that
binds to a document kind (C1) while keeping name fidelity (C2), and it gains a
mechanical oracle (C4): the generated projection/validator must agree, row for
row, with `marshallreflect` reading the same Arrow batch — data and read path
both already exist in `anchor`.

## Decision

We will build a focused **DQL read-back generator** in
`public/semistructured/leeway/dql/`, deriving from a `mappingplan.Plan` joined
with the physical IR, that emits **three** ClickHouse artefacts per document
kind:

- **(a) a presence-prefilter UDF** — necessary-but-not-sufficient; false
  positives allowed, **no false negatives**;
- **(b) a named-tuple projection** — extracts every `Plan` field into one
  self-describing, typed SQL column;
- **(c) an exact validator UDF** (plus an inline-predicate accessor) — precise
  conformance, no false positives or negatives.

All membership ids are resolved to **literals at generation time** through a
`MembershipResolver` interface — the emitted SQL carries constants and never
calls `dictGet`. This is not the `lwq` query language (ADR-0022): it is a
mechanical, per-kind codegen sibling of `ddl`/`dml`/`readaccess`, with no
grammar, parser, or planner.

### Inputs and the Plan ↔ IR join

The generator takes a `mappingplan.Plan` and a `common.IntermediateTableRepresentation`
(built from the kind's physical `TableDesc`, the same path DDL uses) plus a
`MembershipResolver`. `dql.InformationRetrieval` already materializes, per
physical column, a `ColumnRecord{Name, Role, CanonicalType, EncodingHints, …}`;
this is the lookup table. For each `Plan` field the generator:

1. maps `(LWSection, LWColumn)` to the section's `value` column and, via the
   field's channel, the membership identity column(s) and matching `*card` /
   `len` support columns — all by `(section, role)` against `InformationRetrieval`;
2. resolves the membership identity to SQL literals (below);
3. templates the field into each artefact.

Fields span multiple sections; each resolves against its own section's columns.
Plain columns (`id`, `naturalKey`, …) are top-level scalars and project directly.

### Decision 1 — membership resolution interface (all channels, compile-time ids)

A `Plan` field binds to a membership by `(name, channel)`. At query time that
logical pair becomes one or more *physical* column predicates, keyed by
`common.ColumnRoleE`:

| Channel (`MembershipSpecE`)                    | identity role | identity literal | parameters role |
|------------------------------------------------|---------------|------------------|-----------------|
| `LowCardRef` / `HighCardRef`                   | `lr` / `hr`   | `uint64` id      | —               |
| `LowCardVerbatim` / `HighCardVerbatim`         | `lv` / `hv`   | `'name'`         | —               |
| `LowCardRefParametrized` / `HighCardRefParametrized` | `lp` / `hp` | `uint64` id | (high-card params) |
| `MixedLowCardRefHighCardParameters`            | `lmr`         | `uint64` id      | `mrhp`          |
| `MixedLowCardVerbatimHighCardParameters`       | `lmv`         | `'name'`         | `mvhp`          |

Verbatim channels carry the membership *name* on the wire, so it is matched
directly. Ref channels carry a `uint64` id resolved from the same name→id
registry the write path uses (`LookupI.LookupMembership`); the generator calls
it once per field and bakes the result in as a literal. The interface is shaped
for all eight channels from the start, even though the `Plan` front-end emits
only the four simple ones until ADR-0008 Cut 2 lands:

```go
// SQLLiteral renders a generation-time-resolved value as a ClickHouse literal:
// an escaped quoted string for verbatim names, a decimal for ref ids, hex for
// parameter blobs. (String escaping reuses db/clickhouse/dsl/marshalling.)
type SQLLiteral interface{ AppendSQL(b *strings.Builder) }

// ColumnMatch is one physical predicate identifying a membership: the column of
// role Role must contain Literal — has(col, Literal) for presence,
// indexOf(col, Literal) to locate the attribute.
type ColumnMatch struct {
	Role    common.ColumnRoleE
	Literal SQLLiteral
}

// ResolvedMembership is a logical membership fully resolved for SQL emission:
// the per-column matches that together pin it down. Simple channels yield one
// match (the identity); mixed channels yield two (identity + high-card params).
type ResolvedMembership struct {
	Spec    common.MembershipSpecE
	Matches []ColumnMatch
}

// MembershipParameters is the high-card-parameters payload for the parametrized
// and mixed channels; empty for the simple channels. Its wire shape follows
// ADR-0008 Cut 2.
type MembershipParameters struct { /* params blob; empty until Cut 2 */ }

// MembershipResolver turns a logical (name, channel[, params]) into a
// ResolvedMembership with every id already a literal — emitted SQL carries
// constants and never calls dictGet. Resolution is total: an unresolvable ref
// name is a generation error, not a runtime miss. The default implementation
// wraps the write-side LookupI for ref channels and echoes the name for
// verbatim channels.
type MembershipResolver interface {
	Resolve(name string, spec common.MembershipSpecE, params MembershipParameters) (ResolvedMembership, error)
}
```

Presence (a) uses the identity match; the exact validator (c) uses all matches
(identity + parameters) plus attribute alignment.

### Decisions 3–5 — the three artefacts

Worked on `anchor`'s `symbol` section: `value` =
`` `tv:symbol:value:val:s:m:0:24:0::data` `` (`Array(LowCardinality(String))`),
verbatim id = `` `tv:symbol:lv:lv:y:m:0:0:0::data` ``, ref id =
`` `tv:symbol:lr:lr:u64:2q:0:0:0::data` ``; plain `` `id:id:u64:2k:0:0:` ``.
DTO fields: `attackType` (verbatim), `severity` (`LowCardRef`, id resolved to
`8456` at generation time).

**Mandatory fields (decision 4).** Plain columns, and tagged fields that are
neither `option.Option[T]` (optional) nor `const`. A `const` field
(`lw:"m,sec,const=X"`) contributes a *value-equality* check to the validator;
optional fields are skipped by (a) and (c) and rendered as nullable slots in (b).

**(a) Presence-prefilter UDF (decision 5).** Conjunction of `has(identityCol, literal)`
over mandatory fields. No false negatives (an absent identity ⇒ the field is
genuinely absent); false positives arise from flattening/aliasing or an absent
value behind a present membership.

```sql
CREATE FUNCTION leeway_has_MyDTO AS (lv_symbol, lr_symbol) ->
    has(lv_symbol, 'attackType') AND has(lr_symbol, 8456);
-- WHERE leeway_has_MyDTO(`tv:symbol:lv:lv:y:m:0:0:0::data`, `tv:symbol:lr:lr:u64:2q:0:0:0::data`)
```

**(b) Named-tuple projection (decision 3).** Locate each field by `indexOf` and
pack all `Plan` fields into one named, typed tuple — safe to run *after* (a),
which guarantees `indexOf > 0`. The generator knows each slot's canonical type
from the IR, so it can emit a fully-typed `Tuple(...)` that a UDF consumes by
slot name:

```sql
CAST(tuple(
    `id:id:u64:2k:0:0:`,
    `tv:symbol:value:val:s:m:0:24:0::data`[indexOf(`tv:symbol:lv:lv:y:m:0:0:0::data`, 'attackType')],
    `tv:symbol:value:val:s:m:0:24:0::data`[indexOf(`tv:symbol:lr:lr:u64:2q:0:0:0::data`, 8456)]
  ) AS x,
  'Tuple(id UInt64, attackType String, severity String)') AS myDTO
```

(Correct under the aligned case — each attribute one membership on the channel,
all `card`=1. Aliasing needs the cumulative-`card` remap of decision 2.)

**(c) Exact validator UDF + inline accessor (decision 5).** Precise conformance:
each mandatory membership present with correct multiplicity, aligned to a real
attribute, value present and (for `const`) equal to the literal; parameters
matched for mixed channels. The generator also exposes the same predicate as an
inline string for embedding in a hand-written `WHERE`.

```sql
CREATE FUNCTION leeway_is_MyDTO AS (lv, lvcard, value /*…*/) ->
    countEqual(lv, 'attackType') = 1 AND /* attribute alignment via cumulative lvcard, value presence/type */ …;
```

The exact predicate is **sketched, not solved** here — its body is the substance
of decision 2.

### Decision 2 — non-scalar values and the flatten inversions (needs deeper design + tests)

Extracting field *f* in section *S* at attribute index *a* composes up to two
flatten inversions. Notation: `V` = value column, `MC` = the channel's
per-attribute membership-count column (`<role>card`), `LEN` = the array `len`
support column; ClickHouse arrays are 1-based and `indexOf` returns 0 on miss.

**Level 1 — membership identity → attribute index.** A membership identity sits
at a *flattened* position `p = indexOf(idCol, literal)`. The owning attribute is

```
a = arrayFirstIndex(x -> x >= p, arrayCumSum(MC))
```

In the aligned case (`MC` all 1) this collapses to `a = p`, which is why (b) can
use `indexOf` directly.

**Level 2 — attribute index → value (array/set sections).** A scalar section
yields `V[a]`. An array section's `V` is itself flattened; attribute *a*'s
sub-array is

```
arraySlice(V, arraySum(arraySlice(LEN, 1, a-1)) + 1, LEN[a])     -- ≡ leeway_unflatten(V, LEN)[a]
```

where `leeway_unflatten` generalizes the existing `ANCHOR_UNFLATTEN_LEEWAY_ARRAY`.
Set sections use the set-cardinality support column (`card`) in place of `LEN`,
modulo set semantics.

`cusumlen` / `cusumcard` exist as column *roles* but are **not materialized** in
the current schema, so v1 computes prefix sums in SQL (`arrayCumSum` /
`arraySum(arraySlice(...))`). Materializing them later is a drop-in optimization
that replaces the in-SQL cumulative sums.

This arithmetic is error-prone (off-by-one, 1-based indexing, the miss case,
multi-membership). It will **not** be committed on inspection. The correctness
oracle is a round-trip golden test: ingest `anchor`'s avalanche/cyber/drone Arrow
batches into ClickHouse, run the generated projection and validator, and compare
cell-by-cell against `marshallreflect` reading the same batches — the validator
must accept exactly the rows `marshallreflect` decodes without error. The
non-scalar path (v1) is gated on this harness passing. The detailed sub-design —
the two flatten inversions, the exact helper-UDF bodies, the invariants, and the
test matrix — is drafted in [`dql/EXPLANATION.md`](../../public/semistructured/leeway/marshall/clickhouse/readback/EXPLANATION.md).

### Scope and phasing

- **v0** — scalar fields; all eight channels resolved to literals via
  `MembershipResolver`; the three artefacts; the round-trip harness for scalars.
- **v1** — non-scalar (homogenous array, set) values via the decision-2 mapping,
  gated on the test harness.
- **Deferred** — co-section / streaming-group joins; tree/JSON entity
  reconstruction (that is `lwq`, ADR-0022); the parametrized/mixed channels on
  the *write/Plan* side (ADR-0008 Cut 2) — the resolver already accommodates
  them, but no `Plan` exercises them yet; materialized `cusum*` columns.

## Alternatives

- **Hand-written CH SQL per kind (O1).** Rejected as the mechanism: no
  document-kind binding, and every kind re-derives physical names by hand.
  Retained as the fallback for queries beyond the three artefacts' shapes.
- **IR-only generator (O2).** Rejected: the IR cannot express which memberships
  constitute a kind, so it cannot emit a per-kind presence or validator.
- **Extend the `lw:` tag grammar to carry physical attributes.** Rejected:
  couples the DTO to physical tuning and touches the `marshall*` subsystem; the
  Plan ↔ IR join keeps the logical/physical split the codebase already enforces.
- **Runtime `dictGet` for ref-channel ids.** Rejected per decision 1: ids are
  resolved at generation time, keeping emitted SQL self-contained and portable.
- **Build `lwq` (ADR-0022) instead.** Different scope: a FLWOR language with a
  grammar and planner, in `public/db/leeway/lwq/`. This generator is the small,
  mechanical read-back path; it does not block or substitute for `lwq`.

## Consequences

### Positive

- Reading one document kind back out of the shared table becomes codegen, not
  hand-rolled SQL, and stays in lockstep with the DTO `Plan`.
- The presence UDF is a cheap, index-friendly prefilter; expensive exact checks
  run only on candidates it admits.
- A mechanical correctness oracle (round-trip vs `marshallreflect`) makes the
  flatten arithmetic testable rather than eyeballed.
- The resolver interface is complete across all eight channels, so ADR-0008
  Cut 2 needs no rework on the query side.

### Negative

- The flatten inversions (decision 2) are subtle and carry real correctness
  risk; the non-scalar path is deliberately gated behind tests, delaying it.
- Ref-id resolution snapshots the registry at generation time: regenerating
  against a different name→id registry changes the emitted literals. Acceptable
  for a generated artefact, but it must be regenerated when the registry moves.
- The presence UDF's false positives mean it is only ever a prefilter; consumers
  must compose it with the validator for exactness.

### Neutral

- The package lives in `semistructured/leeway/dql/`, extending the existing stub
  rather than under `public/db/leeway/`, marking it as protocol-coupled codegen
  rather than a query engine.
- `leeway_unflatten` generalizes the anchor-local `ANCHOR_UNFLATTEN_LEEWAY_ARRAY`;
  the anchor queries can later adopt the generated form.

## Status

Accepted — 2026-06-08 (reviewed by p@stergiotis). The scalar read-back path has landed in [`leeway/dql`](../../public/semistructured/leeway/marshall/clickhouse/readback/) with round-trip tests; the non-scalar open questions below are post-acceptance refinements.

Open questions:

1. **Validator body for non-scalar (decision 2).** Drafted in
   [`dql/EXPLANATION.md`](../../public/semistructured/leeway/marshall/clickhouse/readback/EXPLANATION.md);
   the fast-path detection (when `MC_R ≡ 1` lets the generator emit bare `indexOf`)
   and the mixed/parametrized parameter recursion remain open within it.
2. **Named-tuple emission form.** `tuple(expr AS slot, …)` vs the explicit
   `CAST(tuple(…) AS Tuple(slot T, …))` shown above; the latter is robust and
   uses the IR's canonical types, but the slot-type spelling for every canonical
   type needs an emission table (reuse `ddl/clickhouse`'s `GenerateType`).
3. **UDF naming + namespacing.** `leeway_has_<Kind>` / `leeway_is_<Kind>` collision
   policy across kinds and schemas; whether to suffix with a schema/table token.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only.

## Updates

### 2026-06-04 — implemented in `public/semistructured/leeway/dql/`

The core shape (Plan ↔ IR join, three artefacts, generation-time id resolution) stands; implementation revealed refinements:

- **Prior art reused.** The jagged-array UDFs come from pebble2impl's spinnaker (`internal/spinnaker/sql/udfs_tag.sql`), consolidated into `dql/lw_dql_udfs.sql`: the `LEEWAY_LU_*` ("Leeway LookUp") family + `LEEWAY_VALUE_BY_TAG_EQUAL`, the anchor unflatten UDF as `LEEWAY_UNFLATTEN`, plus the new level-2 `LEEWAY_LIST_BY_TAG_EQUAL`. The locate is the *materialized* position→attribute map `LEEWAY_LU_MEMB_IDX_TO_VAL_IDX` (multi-membership-correct), not the per-position `arrayFirstIndex` first sketched. A latent spinnaker bug — `BEGIN_INCL` calling an undefined `…_END` — was found (via `clickhouse-local`) and fixed.
- **Resolver + generator landed** as `lw_dql_resolver.go` (`MembershipResolver`/`LookupResolver`, all eight channel→role mappings) and `lw_dql_generator.go` (`Generator.Generate`).
- **Open-question 2 resolved.** The named-tuple projection must be `CAST(tuple(…), 'Tuple(<slot> <type>, …)')` — ClickHouse's `tuple(x AS name)` does *not* expose `name` for element access. Slot types are rendered from each value column's canonical type via the ddl/clickhouse type generator.
- **Decision 2 (non-scalar)** is implemented for homogenous arrays and sets (the flatten inversions in `dql/EXPLANATION.md`).
- **Verification.** `clickhouse-local` (no server) is the oracle: a UDF truth-table (`lw_dql_udfs_test.go`) and a real-data round-trip (`lw_dql_roundtrip_test.go`) that marshals DTOs through anchor's write path to an Arrow file, runs the generated artefacts over it, and asserts the read-back equals the originals with presence = validator = 1.
- **Still open:** the mixed/parametrized channels on the Plan front-end (ADR-0008 Cut 2); aligned-fast-path detection (I5); named-tuple slot types across the full canonical-type range.

### 2026-06-09 — relocated to `marshall/clickhouse/readback` (ADR-0074)

[ADR-0074](0074-leeway-marshall-package-layout.md) re-homed the leeway marshall packages onto a target-namespaced layout. This generator moved from `leeway/dql/` (package `dql`) to `leeway/marshall/clickhouse/readback/` (package `readback`), reframed as the ClickHouse-SQL **marshall target** beside the Go target (`marshall/go/…`). The design is unchanged — same `Generator` / `Artefacts` / `InformationRetrieval` / `MembershipResolver`, the three artefacts, and the helper UDFs; the `lw_dql_*` files became `lw_readback_*`. The Plan IR it consumes stays in `mappingplan`; the Go-DTO construction machinery it does not use now lives in `marshall/go/goplan`.

### 2026-06-09 — index-aware filter artefacts

An index-use analysis of the generated SQL, verified on ClickHouse 26.5 via `clickhouse-local`
(`EXPLAIN indexes=1`, 1M rows / 123 granules): `has`/`hasAll` over Array columns prune granules
through a `bloom_filter` skip index; `indexOf` and `countEqual` never use one. A WHERE of
`presence AND validator` prunes via the presence conjuncts even though the validator is
index-blind — the validator alone forces a full scan. Two supporting facts: lambda UDFs are
inlined before index analysis, so wrapping artefacts in `leeway_has_<K>` keeps pruning intact;
and the analyzer's ActionsDAG deduplicates byte-identical inlined subexpressions within a
stage, so the per-(section, membership) locate work is computed once across a kind's
projection slots without any `WITH` binding (the sketched `WITH` emission is dropped from the
sub-design).

Three generator changes followed:

- **`Artefacts.Filter`** — the pre-ANDed `Presence AND Validator`, now the documented WHERE
  embed. The contract is explicit: Validator alone is exact but unprunable; the
  redundant-looking Presence conjuncts are the index carriers.
- **Presence grouping.** Presence literals are grouped per physical column — `has(col, lit)`
  for one literal, `hasAll(col, [lits…])` for several — one array scan and one skip-index
  condition per column instead of one `has` per mandatory field.
- **Const value-side presence.** A const field also contributes its pinned value as a presence
  term on the **value** column (`has(valCol, 'const')`, a necessary condition — pruning-relevant
  for selective kind discriminators), guarded to scalar string-typed value columns: `has` does
  not coerce a string literal to a numeric array (`NO_COMMON_TYPE`), unlike the validator's
  equality.

Still open on the schema side: nothing emits the skip indexes these terms would prune with —
`ddl/clickhouse` produces no `INDEX` clauses and `encodingaspects` has no index vocabulary. An
encoding/section aspect mapping to `INDEX … TYPE bloom_filter(p) GRANULARITY g` on the
membership (and const-bearing value) columns is the candidate design; a `set(N)` index
additionally serves `countEqual`/`indexOf` while per-granule distinct membership-array values
stay ≤ N (verified), which fits homogeneous-ingest tables.

## References

- [ADR-0008 — leeway marshall extensions](./0008-leeway-marshall-extensions.md) — the `Plan`, the `lw:` tag grammar, membership channels (D3), the Cut-2 parametrized/mixed channels the resolver anticipates.
- [ADR-0042 — keelson leeway codec SoA generator](./0042-keelson-leeway-codec-soa-generator.md) — the write/read codegen this generator parallels on the query side.
- [ADR-0022 — leeway lwq FLWOR query language](./0022-leeway-lwq-flwor-query-language.md) — the query *language*; this ADR is the mechanical per-kind read-back, deliberately not that.
- [ADR-0018 — leeway card-JSON canonical format](./0018-leeway-card-json-canonical-format.md) — attribute-centric layout and multi-membership aliasing.
- [ADR-0010 — leeway CBOR RPC codec](./0010-leeway-cbor-rpc-codec.md) — the single-entity transport; deferred, but the same flatten/shred concerns recur.
- `public/semistructured/leeway/marshall/clickhouse/readback/lw_readback_ir.go` — `InformationRetrieval`, the metadata layer this generator consumes.
- [`readback/EXPLANATION.md`](../../public/semistructured/leeway/marshall/clickhouse/readback/EXPLANATION.md) — the decision-2 sub-design: the non-scalar flatten inversions, helper-UDF bodies, invariants, and the round-trip oracle.
- `public/semistructured/leeway/anchor/` — `card_anchor_schema.go` (physical schema), `card_anchor_dql_query*.sql` (hand-written queries this generalizes), and the avalanche/cyber/drone datasets used as the correctness oracle.
- `public/semistructured/leeway/marshall/go/marshallreflect/` — `unmarshal.go`, the read-back behaviour the generated SQL must agree with.
- `public/semistructured/leeway/common/lw_enums.go` — `ColumnRoleE`, `MembershipSpecE`, the support-column role taxonomy (`*card`, `len`, `cusum*`).
