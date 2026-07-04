---
type: adr
status: accepted
date: 2026-07-03
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-04
---

# ADR-0101: leeway marshall — mixed-shape multi-sub-column sections (scalar + container)

## Context

A **mixed-shape section** is a tagged-value section declaring two or more value
sub-columns of which at least one is a container (homogenous array `…h` or set
`…m`) — including the all-container case. anchor declares two:

- `text` — `text` S (scalar) + `wordLength` U32h + `wordBag` Sh
  ([`card_anchor_schema.go:80-85`](../../public/semistructured/leeway/anchor/card_anchor_schema.go));
- `geoArea` — `polyLat` F32h + `polyLng` F32h + `h3` U64m (no scalar,
  array + set; `card_anchor_schema.go:160-168`).

Every layer of the leeway pipeline **except the marshall pair** already
implements this shape:

| Layer | Status | Evidence |
| --- | --- | --- |
| schema / DDL | supported | anchor `text` / `geoArea`; generated DDL carries per-subtype support columns |
| generated DML (write target) | supported | `InEntityTestTableSectionText.BeginAttribute(text string)` + `InAttr.AddToCoContainers(wordLength uint32, wordBag string)` (`card_anchor_dml.out.go:5139/:5298`) |
| generated RA (Arrow read target) | supported | per-sub-column accessors: `GetAttrValueText(…) string`, `GetAttrValueWordLength(…) iter.Seq[uint32]`, `GetAttrValueWordBag(…) iter.Seq[string]`, shared `AccelHomogenousArray`, plus a `GetAttrValueSingle` tuple form (`card_anchor_ra.out.go:984-996/:2792-2846`) |
| ClickHouse readback SQL (ADR-0066) | supported, untested | per-sub-column subtype dispatch — scalar → `LEEWAY_VALUE_BY_TAG_EQUAL`, array/set → `LEEWAY_LIST_BY_TAG_EQUAL` + shared LEN/CARD support column (`lw_readback_generator.go:237-312`); `readback/EXPLANATION.md` uses `text` as its worked example and lists the "mixed-subtype section" row, but no test maps a DTO onto one |
| **marshallgen** | **rejected** | write side `emit.go:531` `"non-scalar field in multi-sub-column section not supported"` (survey at `emit.go:519-534`); read side `emit.go:1051` `"section mixes scalar-section field shape with non-scalar-section field shape"` |
| **marshallreflect** | **panics** | `marshalMultiSubColumn` (`marshal.go:120-137`) passes every sub-column field positionally to `BeginAttribute`; `Validate[T]` passes the shape (`validate.go:104-105` requires only that `BeginAttribute` exists, arity unchecked), then `Marshal` panics `reflect: Call with too many input arguments` (verified against anchor `text`, 2026-07-03) |

Three sibling shapes marshal fine today: single scalar value, lone container
(anchor `u64Array`), all-scalar multi-sub-column (stage2 `geoPoint` /
`timeRange`). The gap is exactly the fourth combination.

### Was the cap deliberate? (finding)

**Deliberate-but-unexplained scoping.** Both refusal sites and the panic path
arrived fully formed in the bulk import `c1ee6eaa` (2026-05-24,
"feat(leeway): land marshallgen + marshallreflect from boxerstaging"), whose
message lists "multi-sub-column, rejection rules" among tested cases but gives
no rationale for excluding containers. The multi-sub-column model is
consistently documented as a *scalar tuple*: "multi-sub-column scalar
(u32Range) — emit BeginAttribute(arg1, arg2, …)" (`emit.go:519`),
"Multi-sub-column attributes carry one tuple per row" (`marshal.go:96`), flat
two-field framing in [ADR-0075](0075-leeway-typed-component-views.md), and
"u32Range / u32Set / u64Set stay scalar" in [ADR-0042](0042-keelson-leeway-codec-soa-generator.md).
Its motivating sections (`u32Range`, `timeRange`, `geoPoint`) are inherently
scalar co-arrays. Unlike genuinely deferred work in the same commit series
(D3 Cut-2 carried an explicit "not yet implemented — see ADR-0008 Cut-2"
parse error; the readback resolver wraps `common.ErrNotImplemented`), this cap
has **no** deferred-work marker and **no** written justification anywhere in
doc/, comments, or history.

One premise correction: [ADR-0008](0008-leeway-marshall-extensions.md) D2's
`partitionScalarsFirst` (`goplan/grouping.go:100-119`) does **not** anticipate
mixed sections. It stable-sorts *fields within one sub-column's field list* —
attribute ordering for sections where several memberships share the `value`
sub-column. In a multi-sub-column section every sub-column holds exactly one
field (`emit.go:526`), so the partition is a no-op there. What *does*
anticipate the shape is the DML/RA/readback generator row above.

### Wire semantics (already defined; nothing to invent)

The DML generator treats **every** section uniformly
(`lw_dml_generator.go:679-688/:1088-1158`):

- scalar sub-columns become `BeginAttribute(<scalars…>)` arguments, in schema
  declaration order within the scalar class;
- container sub-columns become `AddToContainer(v)` (exactly one container) or
  `AddToCoContainers(<containers…>)` (two or more) arguments, in schema
  declaration order within the container class;
- `BeginAttributeSingle(<scalars…>, <one element per container…>)` is a QoL
  wrapper (`BeginAttribute(...).AddToCoContainers(...)`), emitted only when a
  container is present.

Per attribute, each scalar sub-column carries exactly one value; each
`AddToCoContainers` call appends one element to *every* container sub-column,
so all containers advance in lockstep. Per-attribute container length is
recorded once per **subtype class** — `homogenousArraySupport` records the
arrays' shared length, `setSupport` the sets' shared cardinality (geoArea's
`handleNonScalarSupportColumns` appends the *first* array's and the *first*
set's counters) — so equal length within a class is a **wire invariant**, not
a convention; a producer that broke it would desynchronise the support column
that readback's LIST/CARD slicing relies on. Zero-length containers are
representable (`BeginAttribute` + zero `AddTo*` calls + `EndAttribute` records
a 0 in the support column). Multi-sub-column sections carry a single
membership and one attribute per row (existing rule, all four surfaces).

The three working shapes are degenerate cases of this one model (S scalar
sub-columns, C container sub-columns): single scalar = (1,0); lone container =
(0,1); all-scalar multi = (N,0). The gap is C ≥ 1 with S + C ≥ 2.

### Demand

The downstream consumer that surfaced this (hackathon_2026) committed a schema
with **12 mixed-shape sections**: facts11's ten canonical sections
(`u8`…`i64`, `time`, `float32`, `float64`: `value` array + `semantic` /
`models` / `params` scalar S columns), `string` (`short` Sh + `long` Sh +
three scalars) and `location` (five arrays + `models` / `params`). The
promoting commit (f781a04, "promote all values to homogenous arrays") states
the goal: *"natively support slices when marshalling."* Its generated DML
confirms the model above: `SectionU8.BeginAttribute(semantic, models, params)`
+ `InAttr.AddToContainer(value uint8)`;
`SectionLocation.BeginAttribute(models, params)` +
`AddToCoContainers(latitude, longitude, h3Resolution3, h3Resolution6,
h3Resolution15)`. Today the consumer works around the gap by avoidance: its
round-trip tests marshal only the scalar `provenance` section, a facts12
sibling schema downgrades every value to scalar (metadata demoted to
memberships, `location` split into scalar co-sections), and a 65-DTO
FollowTheMoney fleet only exercises `PlanFor[T]`, never `Marshal`.

## Design space (QOC)

**Question.** Should the marshall pair support mixed-shape multi-sub-column
sections, and at what scope?

**Options.**

- **O1** — Full co-container model: any S ≥ 0 scalars + C ≥ 1 containers
  (S + C ≥ 2), mirroring the DML's uniform model, across write + read + both
  front-ends.
- **O2** — Reject gracefully everywhere (plan-time error instead of the
  reflect panic); document that consumers must split sections so arrays live
  alone.
- **O3** — Narrow enablement: exactly one container plus at least one scalar
  (C = 1, S ≥ 1) — the facts11 canonical shape; defer co-containers.
- **O4** — No boxer change; consumers redesign schemas (the facts12 route:
  scalar values, metadata as memberships / co-sections).

**Criteria.**

- **C1** — pipeline consistency: does the marshall pair speak the same section
  model as schema/DML/RA/readback?
- **C2** — consumer fit: covers facts11's 12 sections (2 of which have C ≥ 2)
  and the FtM fleet without schema contortion.
- **C3** — implementation cost & regression risk against the byte-identity
  invariant (marshallgen `BuildEntities` ≡ `marshallreflect.Marshal`).
- **C4** — authoring-contract clarity (positional ordering, zip lengths,
  splice rules): no arbitrary boundaries users trip on.
- **C5** — failure-mode quality: unrepresentable DTOs fail at plan/validate
  time, not as reflect panics.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | +  | −− |
| C2 | ++ | −− | +  | −  |
| C3 | −  | ++ | −  | ++ |
| C4 | +  | +  | −  | −  |
| C5 | ++ | ++ | +  | −− |

O3 saves almost nothing over O1 — the zip loop over one container and over K
containers is the same driver, and it strands facts11's `string` / `location`
(C ≥ 2). O2/O4 keep the marshall pair the only layer that cannot express a
shape the rest of the pipeline (and a committed consumer schema) already
carries. O1 wins.

## Decision

We will extend the marshall pair's multi-sub-column model from "one scalar
tuple per row" to the DML's full attribute model — **a scalar tuple plus
zipped co-containers** — implemented once in the shared plan layer and driven
by both front-ends, preserving byte-identical wire output between
marshallgen's `<Kind>BuildEntities` / `<Kind>AddSections` and
`marshallreflect.Marshal` / `RowComposer`.

### D1 — Attribute model

A mixed-shape section's attribute is `(s₁…s_S, zip(c₁…c_C))`: one value per
scalar sub-column, plus N zipped elements across all container sub-columns
(shared N per attribute; N = 0 legal). One membership per section, one
attribute per row (zero for a spliced S = 0 row) — the existing
multi-sub-column rules, unchanged for S ≥ 1. The
codec always drives `BeginAttribute(<scalars…>)` + N ×
`AddToContainerP`/`AddToCoContainersP(<one element per container…>)`;
`BeginAttributeSingle` remains a human QoL surface the codec never calls.

### D2 — DTO grammar and authoring contract

The `lw:` grammar already parses the shape (`<membership>,<section>:<column>`,
`goplan.SplitLW`); no grammar change. A mixed-shape DTO declares one Go field
per sub-column — scalar sub-columns as `T`, container sub-columns as `[]T`
(element type = `TaggedField.GoType()`):

```go
Text       string   `lw:"prose,text:text"`
WordLength []uint32 `lw:"prose,text:wordLength"`
WordBag    []string `lw:"prose,text:wordBag"`
```

Contract (documented in `marshallreflect` package doc + `marshallgen`
EXPLANATION):

- **Per-class positional order.** Within each class (scalars; containers) the
  DTO's declaration order must match the schema's column declaration order —
  the same positional contract all-scalar multi-sub-column DTOs already carry,
  now stated per class. Classes may interleave freely in the DTO (the emit
  filters by class).
- **Zip length.** All container fields must have equal `len` per row. Runtime
  check with a clear error (precedent: the Cut-2 value/carrier
  length-agreement check, ADR-0008 update 2026-06-05); never a panic.
- **Presence / splice.** S ≥ 1: the attribute always emits — the scalar tuple
  is the presence signal (analogous to ADR-0008 SD8); containers may be empty
  (N = 0 on the wire). S = 0 (all-container, geoArea shape): the attribute is
  spliced (not emitted) when *every* container is empty — the lone-container
  splice rule generalised; emitted otherwise. Schema-`AspectOptional` scalar
  sub-columns (e.g. facts11 `params`) carry their zero value; `Option[T]`
  stays rejected in multi-sub-column sections.
- **Rejected in multi-sub-column sections** (now enforced at plan time, D3):
  `Option[T]`, `*roaring.Bitmap`, `,unit`, `,explode`, `,const`, carrier
  (mixed/parametrized) channels, more than one field per sub-column, more
  than one membership per section.

### D3 — Shared plan layer (goplan)

`SectionGroup` gains class views — `ScalarSubColumns()` /
`ContainerSubColumns()` (classification via `ClassifyBegin` on each
sub-column's single field) — used by every emit/drive/validate/read site, so
the two front-ends cannot drift. No reordering of `g.SubColumns` (reordering
would silently change existing all-scalar `BeginAttribute` argument order — a
wire/API break); the views filter, preserving declaration order within each
class, which is exactly the DML generator's own per-class rule.

The multi-sub-column structural rules move from marshallgen-emit-time into
`goplan.PlanBuilder.Finish` (shared by both front-ends): one field per
sub-column, single membership, plus the D2 rejections. `marshallgen` keeps its
emit-site errors only as internal-invariant backstops.
`marshallreflect.Validate[T]` inherits the plan-time rules for free and
additionally tightens the DML method contract: `BeginAttribute` arity = S
(today `wantArgs=-1`), and — when C ≥ 1 — `AddToContainerP` (C = 1) /
`AddToCoContainersP` (C ≥ 2) with arity C. This closes the
"Validate passes, Marshal panics" hole for good.

### D4 — Write drivers (byte-identity preserved by identical call sequence)

Both drivers emit, per row:

```
attr := sec.BeginAttribute(s₁, …, s_S)          // scalar class, declaration order
// zip-length guard over the container class (error, not panic)
for k := 0 .. N-1:
    attr.AddToCoContainersP(c₁[k], …, c_C[k])   // AddToContainerP when C == 1
attr.AddMembership<Channel>P(…)                  // single membership
attr.EndAttributeP()
```

- `marshallgen`: `writeSectionInterfaces` (`emit.go:509-596`) emits SecI
  `BeginAttribute(<scalar args>)` (empty arg list when S = 0) and AttrI
  `AddToContainerP(v T)` / `AddToCoContainersP(…)` from the class views;
  `writeMultiSubColumnDriver` (`emit.go:809-824`) grows the guard + zip loop
  (container-loop-then-membership, mirroring the lone-container driver
  `emit.go:899-925`). `<Kind>AddSections` (ADR-0100 SD6) inherits via
  `writeSectionDriver`.
- `marshallreflect`: `marshalMultiSubColumn` (`marshal.go:120-137`) splits by
  class views, passes scalars to `BeginAttribute`, checks zip lengths, loops
  `AddToCoContainersP` via `mustCall`. Fixed-byte `[N]byte` elements re-slice
  through the existing `reslicedIfFixedByte` on both paths.

### D5 — Read paths

- `marshallgen` read interfaces: the multi-sub-column branch of
  `writeSectionReadInterfaces` (`emit.go:1058-1062`) emits per-sub-column
  shapes — scalar `GetAttrValue<Col>(entityIdx, attrIdx) T`, container
  `GetAttrValue<Col>(entityIdx, attrIdx) iter.Seq[T]` — matching what the RA
  generator already produces. The `emit.go:1051` shape-mix rejection narrows
  to single-sub-column sections (where two field shapes would genuinely
  contend for one `GetAttrValueValue` signature).
- `<Kind>FillFromArrow` multi-sub-column match loops
  (`writeMultiSubMatchLoops`, `emit.go:~1531-1590`): container sub-columns
  accumulate `[]T` by draining the Seq per matched attribute; scalars as
  today.
- `marshallreflect.Unmarshal`: `unmarshalMultiSubColumn`
  (`unmarshal.go:451-509`) drains container sub-columns via the existing
  `collectIterSeq` (as the lone-container `consumeValue` path does at
  `unmarshal.go:386-398`) instead of scalar-assigning the Seq. The
  exactly-one-attribute-per-row rule stays for S ≥ 1; an S = 0 section
  admits zero occurrences (the spliced row decodes to nil slices,
  mirroring the lone-container tolerance). An N = 0 attribute reads back
  as a nil slice.

### D6 — ClickHouse readback

No generator change: `lw_readback_generator.go` already resolves subtype
per physical sub-column and shares the section's LEN/CARD support column
(`:237-312`). The gap is coverage only — add a mixed DTO (anchor `text`) to
`lw_readback_coverage_test.go` and the `clickhouse-local` round-trip
(`lw_readback_roundtrip_test.go`), exercising N = 0, N = 1, N > 1.

### D7 — RowComposer cardinality passes

A mixed-shape attribute classifies by its shared container length N:
N ≤ 1 → `AddSingleValueAttributes` pass, N > 1 → `AddMultiValueAttributes`
pass — the runtime-cardinality rule containers already follow (ADR-0008 D1),
replacing the blanket "multi-sub-column = single-value" comment
(`marshal.go:96`, `stack.go:96`). All-scalar multi-sub-column sections stay
single-value (unchanged). S = 0 sections with all containers empty are
spliced in both passes.

### Subsidiary decisions

- **SD1 — Set sub-columns write from `[]T`, not roaring.** The DML's
  co-container args are plain element values; set-ness lives in the schema +
  readback (CARD path). `*roaring.Bitmap` stays rejected in multi-sub-column
  sections — a bitmap iterates sorted with no stable index, so it cannot zip
  (same rationale as the carrier-channel roaring rejection). Consequence: a
  DTO field for a set sub-column classifies canonically as array (`U64h`);
  plans are schema-blind so write/read work against the set column, but
  plan-derived DDL (recordstore, ADR-0100) cannot yet express "set" from a
  `[]T` field — see Open questions.
- **SD2 — Wire bytes of the three existing shapes are unchanged.** The change
  is additive; every checked-in `.out.go` regenerates byte-stable except for
  the intended new mixed emit. Verified by regeneration + the existing
  gen-vs-reflect suites.
- **SD3 — Method naming follows the DML generator's count rule** —
  `AddToContainerP` when the section has exactly one container sub-column
  (even with scalars present, cf. facts11 `SectionU8`), `AddToCoContainersP`
  for two or more.

### Verification gates

1. `TestGenVsReflect_ByteEqualAndCrossDecode` extended with a mixed DTO over
   anchor `text` and an all-container DTO over `geoArea`: `RecordEqual` + IPC
   byte equality + cross-decode (gen-write → reflect-read and vice versa).
2. `marshallreflect_test` round-trip over anchor `text`: N = 0 / N = 1 /
   N > 1, zip-length-mismatch error, S = 0 all-empty splice.
3. `marshallgen` emit tests for the new SecI/AttrI/read-interface shapes;
   `codecdemo` gains a mixed DTO regenerated via
   `keelsoncodec --target=anchor` (documents the canonical DTO idiom).
4. Readback coverage + `clickhouse-local` round-trip incl. `text` (D6).
5. `Validate[T]` negative tests: roaring / Option / explode / unit / const /
   carrier channel / duplicate sub-column field / second membership / DML
   missing `AddToCoContainersP` / wrong `BeginAttribute` arity.
6. Existing suites regenerate/run byte-stable (SD2).

## Alternatives

- **O2 — graceful rejection + section splitting.** Keeps the marshall pair
  the odd layer out; forces facts11-style schemas to choose between losing
  slice values (facts12's scalar downgrade) and losing per-attribute scalar
  metadata (lone-container sections); co-section splits impose their own
  equal-attribute-count constraint (anchor deliberately keeps `geoPoint` /
  `geoArea` out of a co-section for exactly that reason,
  `card_anchor_schema.go:142-148`). Its one virtue — plan-time rejection —
  is subsumed by D3.
- **O3 — C = 1 only.** Same machinery as O1 minus the multi-container zip;
  strands facts11 `string` / `location` and anchor `text` / `geoArea`;
  arbitrary boundary (why is a second array different?) violating C4.
- **O4 — consumer schema redesign.** Entrenches the gap; the reflect panic
  remains for the next consumer; contradicts the pipeline's own read-side
  investment (RA + readback already paid for mixed sections).
- **Reordering `SubColumns` scalar-first in `ComputeGroups`** instead of
  class views. Rejected: silently permutes existing all-scalar
  `BeginAttribute` argument binding (wire/API break) and couples attribute
  ordering (D2 of ADR-0008) with argument layout.
- **Deriving zip length from the first container and truncating/padding the
  rest.** Rejected: silent data loss; the wire invariant wants equal lengths,
  so unequal input is a caller bug to surface.

## Consequences

### Positive

- The marshall pair speaks the DML's full section model; facts11's 12
  sections and the FtM DTO fleet marshal natively ("natively support slices
  when marshalling" lands).
- The `Validate[T]` / plan-time story becomes airtight for multi-sub-column
  sections — no reflect panics, contract failures name the field and the
  missing/mismatched method.
- The readback generator's already-built mixed-section path gains test
  coverage instead of remaining dead-on-arrival.

### Negative

- Two more positional/runtime contracts on DTO authors (per-class order; zip
  lengths) — checkable only at runtime, mitigated by clear errors and the
  documented idiom (gate 3's codecdemo example).
- The multi-sub-column emit/read paths grow real branching (S = 0, C = 1 vs
  C ≥ 2, splice) where they were one-line tuples; the byte-identity test
  matrix grows accordingly.

### Neutral

- Single-membership and one-attribute-per-row multi-sub-column rules are
  untouched; so is ADR-0008 D2 attribute ordering (a no-op here).
- Carrier channels remain incompatible with sub-columns (`build.go:415-417`),
  unchanged.
- `BeginAttributeSingle` stays codec-unused.

## Open questions

1. **Set-canonical DTO fields.** `[]T` classifies as array; `,ct=` may only
   relabel, not reshape, so a `[]uint64` field cannot declare the `U64m`
   canonical. Fine for writing against an existing schema (schema owns
   subtype), but plan-derived schema generation (recordstore, ADR-0100)
   cannot emit a set sub-column from a slice field. Defer until a consumer
   needs plan-derived mixed DDL with sets.
2. **`[]uint8` is `[]byte`.** The front-ends classify `[]uint8` as the
   scalar blob canonical (`Y`), so a u8-array sub-column (facts11's `u8`
   canonical section, or any lone `u8Array`) cannot be authored as
   `[]uint8` — and the `,ct=u8h` relabel is rejected because
   `resolveCanonicalOverride` compares `(goType, isSlice)` components,
   which cannot see that blob-`[]byte` and slice-of-`uint8` are the same
   Go type (verified against facts11 2026-07-04; the sibling `u16`…`i64` /
   float / time / string sections marshal fine). Fix candidate: compare
   the *rendered* Go types in the relabel check. Orthogonal to this ADR
   (it equally affects lone u8 containers); track as its own change.
2. **Optional scalar sub-columns.** `Option[T]` in the tuple is rejected;
   facts11 marks `params` `AspectOptional` and carries `""`. If a consumer
   needs wire-level absent-vs-empty on a tuple scalar, that is a schema
   (nullable sub-column) question first, a codec question second.
3. **Multi-membership mixed sections.** The single-membership rule predates
   this ADR; facts11's `AspectEmulatedMembershipVerbatim` scalar columns are
   value columns, not memberships, so no consumer currently needs it.
   Revisit only with a concrete section.

## Status

Accepted (2026-07-04). Implemented in the accompanying change set
(goplan + marshallgen + marshallreflect + tests + readback coverage);
all verification gates green, existing `.out.go` regenerate byte-stable,
and the facts11 canonical shape (S = 3, C = 1) smoke-verified against the
consumer's generated DML (2026-07-04).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0008](0008-leeway-marshall-extensions.md) — marshall extensions (D2 ordering, SD8 presence-signal precedent, Cut-2 carrier length-agreement precedent); superseded by ADR-0070–0073 but decisions stand.
- [ADR-0066](0066-leeway-dql-clickhouse-readback-generator.md) — readback generator this ADR adds mixed coverage to.
- [ADR-0074](0074-leeway-marshall-package-layout.md) — marshall/<target> layout (goplan / marshallgen / marshallreflect homes).
- [ADR-0100](0100-recordstore-generated-leeway-clickhouse-store.md) — `<Kind>AddSections` surface; plan-derived DDL (Open question 1).
- [`marshall/go/goplan/grouping.go`](../../public/semistructured/leeway/marshall/go/goplan/grouping.go) — `ComputeGroups` / `SectionGroup` / `ClassifyBegin` (D3 home).
- [`marshall/go/marshallgen/emit.go`](../../public/semistructured/leeway/marshall/go/marshallgen/emit.go) — write survey `:519-534`, driver `:809-824`, read interfaces `:1035-1081`, match loops `:~1531`.
- [`marshall/go/marshallreflect/marshal.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/marshal.go) / [`unmarshal.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/unmarshal.go) / [`validate.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/validate.go) — reflect codec sites.
- [`dml/lw_dml_generator.go`](../../public/semistructured/leeway/dml/lw_dml_generator.go) — `:679-688` container-method naming, `:1088-1158` BeginAttribute/Single emit (the wire model's source of truth).
- [`anchor/card_anchor_schema.go`](../../public/semistructured/leeway/anchor/card_anchor_schema.go) — `text` `:80-85`, `geoArea` `:160-168`.
- [`marshall/clickhouse/readback/EXPLANATION.md`](../../public/semistructured/leeway/marshall/clickhouse/readback/EXPLANATION.md) — per-sub-column model + "mixed-subtype section" coverage row.
- History: `c1ee6eaa` (2026-05-24 import, both refusal sites + panic path), `0a245799` (2026-05-26, D2 partition), `644a9013` (2026-05-30, grouping hoist), `5f52781f` (2026-06-09, ADR-0074 path move — no semantic change).
- Consumer (hackathon_2026): `src/go/public/semistructured/facts11/facts11_schema.go` (12 mixed sections), commit `f781a04` ("promote all values to homogenous arrays … natively support slices when marshalling"), `facts11_dml.out.go` (`SectionU8` / `SectionLocation` signatures), `factsftm/` (65 FtM DTOs, `PlanFor`-only today).
