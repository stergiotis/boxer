---
type: adr
status: accepted
date: 2026-05-18
reviewed-by: "@spx"
reviewed-date: 2026-05-18
---

# ADR-0042: Generated SoA codec for keelson runtime.facts rows

## Context

`runtime.facts` is the keelson-side tagged-value table introduced by
ADR-0026 and shredded per ADR-0041. Today two fact kinds —
`CapabilityGrant` and `Error` — are served by the hand-coded
`public/keelson/runtime/rowmarshall/` package: per-type
`MarshalRowBinaryRuntimeFacts()` methods, reflection only at init via
`tagplan.go`, ~51 ns/op on the hot path. ADR-0041 spelled out the
philosophy: no reflection on the row-emit path; each new fact kind is
either hand-rolled or (the option we're deciding here) emitted by a
generator that produces the same kind of code.

Three forces motivate a codec generator now:

- **Fact-kind count is about to grow.** Beyond `CapabilityGrant` and
  `Error`, the runtime layer is queuing up several other kinds
  (sysmetrics samples, capslock decisions, codegen audit rows). Each
  new kind currently demands a hand-written marshaller, hand-curated
  colspec, hand-curated drift test, and a hand-curated reverse-read
  path (which today doesn't exist — see next point).
- **`factsschema/ra` is now landing.** A concurrent session is filling
  the read-access layer over Arrow. Without a generator the symmetric
  read side would also be hand-coded per fact kind, doubling the
  authoring tax.
- **`keelson/vdd` just landed.** Memberships are now centrally
  registered with stable `TaggedId`s; the **interpretation contract**
  the generator respects (two-axis schema model, DTO grammar, wire
  encodings, empty-as-absent rule, roaring-as-array, codegen-time
  consistency checks) is documented in
  [`keelson/vdd/EXPLANATION.md`](../../public/keelson/vdd/EXPLANATION.md).
  This ADR records the decision to **generate** the codec; the
  EXPLANATION records the **model** the codec respects.

Constraints in play:

- **Wire format is `RowBinary` for write, Arrow for read.** Writers
  emit ClickHouse RowBinary directly (rowmarshall's existing path);
  the in-process read path will go RowBinary → Arrow via
  `factsschema/rowbinaryarrow`, then Arrow → columns via
  `factsschema/ra` emitters. The generator must cover both edges with
  the same type/section vocabulary.
- **No reflection on the hot path.** ADR-0041 sets this as a hard
  rule; the generator must match.
- **SoA over AoS.** Caller-facing types are columnar buffers, not
  records. An AoS `Append(row T)` adapter is provided as ergonomic
  sugar but is not the primary surface.
- **vdd is the single schema authority.** A `lw:` tag referencing an
  unregistered membership name must be a codegen-time error, not a
  runtime panic. Field-shape mismatches with the vdd cardinality /
  sub-type declaration are also codegen-time errors.

## Design space (QOC)

**Question.** How should keelson generate per-fact-kind marshal /
unmarshal code targeting `runtime.facts` over RowBinary (write) and
Arrow (read)?

**Options.**

- **O1 — Runtime reflection per row.** A generic `Marshal(any)` that
  walks struct tags every row. Smallest codegen surface; pays reflect
  costs on every emission.
- **O2 — Reflection-at-init + `unsafe.Pointer` plan.** Generalize
  `rowmarshall/tagplan.go`: at first call per type, build a plan of
  field-offset → section emitter; the hot path uses `unsafe.Pointer`
  arithmetic with no reflection. No codegen pipeline.
- **O3 — Per-fact-kind codegen mirroring `rowmarshall`.** A generator
  emits explicit `Marshal/Unmarshal` for an AoS record type, exactly
  the shape `CapabilityGrant` and `Error` use today.
- **O4 — Per-fact-kind SoA codegen.** A generator emits a `<Kind>Columns`
  SoA struct (one typed slice per field, sized in lockstep), plus
  `Append(row T)`, `Marshal(io.Writer)`, `Unmarshal(arrow.Record)`.
  Columns are the primary surface; the AoS record is the convenience
  overlay.

**Criteria.**

- **C1 — Hot-path cost.** Allocations, dispatch overhead, ns/op
  measured against `rowmarshall.BenchmarkCapabilityGrant`.
- **C2 — Authoring cost per new fact kind.** Lines a contributor must
  write or maintain to add a fact kind end-to-end (DTO + marshal +
  unmarshal + drift test).
- **C3 — Auditability.** Can a reader follow the generated code
  without `unsafe`-aware tooling? Is the diff small when a membership
  is renamed?
- **C4 — Symmetry with Arrow / factsschema.** Does the in-memory shape
  align with what `factsschema/dml` produces and `factsschema/ra`
  consumes?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | +  | ++ | ++ |
| C2 | ++ | +  | −  | +  |
| C3 | ++ | −− | +  | ++ |
| C4 | −  | −  | +  | ++ |

O1 fails C1 outright (the philosophy ADR-0041 settled). O2 inherits
`unsafe` and resists audit (C3) — the existing `tagplan.go` is already
the most subtle file in `rowmarshall/`. O3 reproduces the hand-coded
shape and is a strict subset of O4: O4 emits the SoA columns *and* an
AoS `Append` adapter on top, giving the columnar shape Arrow consumers
want for free. O4 also composes with the forthcoming `factsschema/ra`
(read populates `Columns` directly; no intermediate AoS allocation)
and with bulk-write callers that already think in column buffers
(e.g. sysmetrics tick streams).

## Decision

We will adopt **O4 — per-fact-kind SoA codegen**. The generator
(`cmd/keelsoncodec`) reads an annotated Go DTO and emits, per fact
kind, a single `<kind>.out.go` containing:

- **`<Kind>Columns`** — SoA struct, one typed slice (or paired-slice
  pair for `Option[T]` fields) per declared DTO field. Slices grow in
  lockstep; `Len()` returns the row count.
- **`(c *<Kind>Columns) Append(row <Kind>)`** — AoS convenience
  adapter.
- **`(c *<Kind>Columns) Row(i int) <Kind>`** — SoA → AoS extractor,
  inverse of `Append`.
- **`(c *<Kind>Columns) Marshal(w io.Writer) error`** — delegates to
  `factsschema/dml_rowbinary.InEntityFacts`, the boxer-generated
  sparse-RB driver. No hand-emitted byte appenders; membership ids
  and section routing are baked at codegen time via `vdd.Memb*`
  lookups and passed to the driver.
- **`(c *<Kind>Columns) Unmarshal(rec arrow.Record) error`** —
  delegates to `factsschema/ra.ReadAccessFacts*` typed readers.
- **`<Kind>ActiveSections []int` / `<Kind>ActiveFields []int`** —
  precomputed at codegen time; the driver uses them to skip per-row
  work for sections / columns this kind doesn't populate.
- **`<kind>Pool sync.Pool`** — per-kind reuse of `InEntityFacts`
  instances (per-kind isolation because the active hints are
  kind-specific).
- **`<Kind>Codec` (buscodec.CodecI) + `init()` registration** —
  auto-installs the per-kind codec on package import, so
  `buscodec.Encode[<Kind>]` / `Decode[<Kind>]` route through sparse-RB
  instead of the fxamacker-cbor default.

### Input idiom

A keelson DTO is a flat Go struct. Entity-level metadata lives on a
`_ struct{}` blank field; per-field membership wiring lives in
`lw:"<membership>,<section>"` tags.

```go
type CapabilityGrant struct {
    _       struct{}        `kind:"capabilityGrant" plain:"id=Id,ts=Ts,naturalKey=Subject"`
    Id      uint64          `lw:"id"`
    Ts      time.Time       `lw:"ts"`
    Subject string          `lw:"subject,symbol"`
    Scope   []string        `lw:"scope,symbol"`
    Bits    *roaring.Bitmap `lw:"capBits,blob"`
}
```

The supported field shapes (`T`, `Option[T]`, `[]T`, `*roaring.Bitmap`),
the vdd-cardinality × sub-type matrix that maps them to wire encodings,
and the four codegen-time consistency checks are documented in
[`keelson/vdd/EXPLANATION.md`](../../public/keelson/vdd/EXPLANATION.md).
The ADR does not duplicate them.

### Trade-off pinned by the driver path

The driver path pays ~6× per-row CPU at batch-1000 and ~24× at
single-row vs the original byte-appender path (M3.5 numbers in the
2026-05-19 changelog entry). In exchange the codec collapses to a
single 35-section `runtime.facts` schema (no per-kind schemas), the
sparse-format wire is invariant to schema width, and arbitrary
leeway-shaped DTOs — including broker request/response DTOs — share
one production-grade dml/ra implementation instead of N hand-emitted
codecs.

### Implementation outcome

Three phases shipped May 2026 (detail per phase in `## Updates`):

1. **M1-M6 — byte-appender path** (retired by M10). Generator emitted
   direct RowBinary bytes via `codec/wire`; covered every DTO shape
   (plain types, Option[T], []T, *roaring.Bitmap, multi-sub-column
   ranges) with chlocal + ~51 ns/op benchmark gates.
2. **M7-M12 — driver-path consolidation**. Per-kind `.out.go` becomes
   a thin driver over `dml_rowbinary` (write) + `ra` (read) with
   active-* hints and per-kind sync.Pool. M12 auto-registers each
   kind with buscodec; `codec/wire` retires from per-kind output.
3. **Post-M12 — feature completion + broker-DTO migration**. Convert
   covers every section + role the schema declares; arrowrowbinary
   emits CH-canonical 4-byte DateTime; first broker DTO
   (`task.TaskProgress`) migrated off fxamacker-cbor onto the codec.

## Alternatives

- **O1 — Runtime reflection.** Rejected on hot-path cost; conflicts with
  ADR-0041's no-reflection rule. Useful only for ad-hoc tooling that
  doesn't ingest at production rate.
- **O2 — `unsafe.Pointer` plan generalized from `tagplan.go`.** Rejected
  on auditability and on the fact that this codebase already runs
  several `.out.go` generators (`factsschema/dml`, `factsschema/ra`,
  egui2gen, runtimecodegen). Adding one more generator is the
  established idiom; introducing more `unsafe` is not.
- **O3 — Per-kind codegen with AoS as primary.** Rejected as a strict
  subset: O4 also produces the AoS `Append` adapter, so callers that
  prefer record-per-row keep that ergonomic; the difference is that the
  SoA `Columns` is the storage and Arrow consumer surface.
- **Hand-code each new fact kind indefinitely.** Rejected on authoring
  cost as fact-kind count grows; the moment the second hand-coded
  fact kind landed (`Error`, post-ADR-0041), the second-system marginal
  cost was already visible.

## Consequences

### Positive

- **Membership names are checked at codegen time.** A typo in an `lw:`
  tag fails the generator, not a production deployment. Field-shape
  mismatches with vdd are also caught before any `.out.go` is written.
- **Hot path is reflection-free.** Marshal delegates to the
  boxer-generated dml driver; Unmarshal to ra typed readers. No
  reflection, no per-cell `switch`. Absolute ns/op is materially
  worse than the original hand-coded byte-appender ceiling (~24×
  single-row, ~6× batch-1000); the post-pivot rationale is in the
  2026-05-19 changelog entry. Mitigated by batching and per-kind
  active-* hints.
- **Read side closes the round-trip.** `Unmarshal(arrow.Record)` is
  emitted alongside `Marshal`; `factsschema/ra` alone gives you
  Arrow, the generator gives you typed Go columns from Arrow. The
  same `<Kind>Columns` shape is the storage on both sides.
- **SoA matches Arrow.** The columnar Columns layout maps 1:1 onto
  Arrow record batches; bulk-write callers (sysmetrics ticks, batch
  audit) populate `Columns` directly without per-row allocations.
- **`vdd` is the single source of truth for memberships.** ADR-0035
  (keelson namespace) and the just-landed `keelson/vdd` package
  cooperate: name lookups happen against one registry, codegen makes
  the resolution mechanical.
- **Generated files are auditable.** Each emitted `.out.go` is a
  straight-line series of typed slice reads/writes — diffable, greppable,
  no `unsafe`.

### Negative

- **New generator to maintain.** Another `.out.go` producer joins
  `factsschema/dml`, `factsschema/ra`, `runtimecodegen`, egui2gen.
  Lint tooling already filters `.out.go` so the marginal CI cost is
  small, but the generator itself is real code.
- **DTO grammar is intentionally narrow.** Anything tree-shaped must be
  modelled per ADR-0041 as parallel arrays; row-level optionality for
  non-scalar values is not representable (a present
  `HomogenousArray` / `Set` always carries `card ≥ 1`). The generator
  rejects out-of-grammar DTOs with a pointer to the EXPLANATION rather
  than silently coercing.
- **`[]T` is polysemic by vdd lookup.** Reading a DTO file alone does
  not reveal whether a `[]T` field is multi-membership or
  homogeneous-array — the wire encoding requires consulting vdd. The
  trade-off is documented in the EXPLANATION's "Trade-offs" section
  and surfaced in the generated `.out.go`'s leading comment (which
  records the resolved sub-type per field).
- **Time precision capped at `z32` until leeway adds finer.** Callers
  needing sub-second precision or arbitrary timezone offsets must
  encode themselves into `u64` nanoseconds or a `blob`, and document
  it.
- **Per-row latency tax vs the byte-appender baseline.** ~24×
  single-row / ~6× batch-1000 (M8 measurements). Acceptable for the
  workloads on the table (bus codec <10k msg/s, audit fan-out,
  frame-batched chstore ingest); a future per-kind dml schema would
  close it but would forfeit the "one schema" simplification this
  ADR turns on. Listed under Negative because it's the load-bearing
  trade-off any new fact-kind author has to know about.
- **Generated code volume grows with fact-kind count.** Each kind
  emits one `.out.go`. Negligible per-kind, real at fleet scale;
  mitigated by the `.out.go` lint exemption.

### Neutral

- **`rowmarshall` retired.** The two pre-existing hand-coded writers
  (CapabilityGrant, Error) shipped as M11 retrofits and the
  `rowmarshall` package was removed. The ~51 ns/op benchmark from
  the byte-appender era survives as a historical reference, not a
  regression gate.
- **`vdd` capacity may need bumping.** `KeelsonHrNkRegistry` ships
  with a capacity of 64; each new fact kind likely adds a handful of
  memberships. The registry grows on demand, but the constant can be
  pre-sized to avoid one reallocation in init.
- **Builder pattern for incremental rows is out of scope v1.** The SoA
  `Append` is the only push API; a stateful single-row builder
  (analogous to `dml.InEntityFacts`) is left to a follow-up if
  consumers ask.

## Status

Accepted (reviewed 2026-05-18 by @spx). As of 2026-05-21 every
in-tree fact kind (m1fixture, capabilitygrant, errkind, taskprogress)
rides the driver path + the auto-registered sparse-RB buscodec
bridge. Convert is feature-complete for the runtime.facts schema;
arrowrowbinary emits CH-canonical wire; chlocal round-trip green;
broker-DTO migration in progress with `task.TaskProgress` as the
shipped canary.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See boxer's `doc/DOCUMENTATION_STANDARD.md` (resolve via `bash scripts/boxer-path.sh`) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-05-18 — M1-M6: byte-appender path (retired)

The original generator emitted RowBinary bytes directly via the
`codec/wire` byte appenders. Five increments landed in May 2026 and
are retired by the M10 driver-path rewrite; preserved here as
historical reference:

- **M1 — entry point + plain types.** `string`, `uint{8,16,32,64}`,
  `float{32,64}`, `bool`, `time.Time`, `[4]byte`, `[16]byte` under
  ExactlyOne (`T`) and ZeroToOne (`Option[T]`). One fixture fact-kind
  (m1Sample); chlocal RowBinary round-trip is the gate. Commits
  `3a9302b9` (hand-written canary) and `2ec0a98d` (generator).
- **M2 — `[]T` Arbitrary.** Multi-membership shredded encoding
  (`val ‖ lr ‖ lrcard`); empty/nil ⇒ kind omitted. Commit `234bb509`.
- **M3 — `*roaring.Bitmap` + mixed-card sections.** Per-row
  `vals/lrs/cards` accumulator; ClickHouse `groupBitmap` interop
  round-trip. Commit `7fbcab78`.
- **M3.5 — refactor + perf.** `bytebufferpool` lifted single-row
  cost -53% ns/op / -92% B/op; absent-field -70% / -97%; large
  roaring -41% / -79% allocs. 1000-row batch regressed +72%
  (per-row mixed-section scratch slices). Negative parser tests
  locked in the four codegen-time consistency checks.
- **M6 — `Unmarshal(arrow.Record)`.** Symmetric read across every M1-M3
  shape (scalar / Option / `[]T` / roaring / multi-sub-column ranges).
  `colspec.FindKindInLr` makes the consumer blind to per-distinct-kind
  vs per-attribute `lrcard` wire shapes — same leeway content, two
  producer dialects. m1fixture round-trips deferred pending the
  Convert section-coverage extension (closed later in this changelog).

### 2026-05-19 — Driver-path pivot rationale

Four converging threads pushed the codec off the byte-appender shape:

1. **Section count tripled** (22 → 39) once homogeneous-array and set
   sub-types landed for the bus-codec use case.
2. **buscodec migration off CBOR/JSON** pulled toward "every bus payload
   is a runtime.facts row", not per-message-type byte appenders.
3. **Sparse-format shim validation.** Three drop-in shims (sparse-RB,
   sparse-CBOR, full-RB) sed-forked from `factsschema/dml/` output
   proved boxer's column-major buffering + `NewRecord` finalisation is
   a viable per-kind codec target.
4. **Width-invariant wire under sparse formats.** With the then-current
   39-section schema (reduced to ~25 post-2026-05-21 homogeneous-array-
   only rename; wire shapes per encoded row unchanged), sparse-RB and
   CBOR stay flat while full RB grows linearly (Grant 423→577 B / +36%;
   SparseRB 318→318 B / 0%; CBOR 298→298 B / 0%). Per-row CPU at N=1000
   scaled ~2× across all backends with the doubled column count
   (predictable; per-row builder buffer work).

Measured tax (Ryzen AI MAX+ PRO 395, M1Sample = 12 of 35 sections):

| Workload     | Hand (M3.5) | Driver+Pool+Hint     | Penalty |
|--------------|-------------|----------------------|---------|
| SingleRow    | 119 ns      | 2.82 μs / 8 allocs   | 24×     |
| Absent       | 75 ns       | 2.48 μs              | 33×     |
| Batch 1000/r | 298 ns      | 1.78 μs/row          | 6×      |
| BitmapLarge  | 16-37 μs    | (bitmap-dominated)   | —       |

Pool reuse drops un-pooled construction from ~95 μs to ~5 μs/row
(~19×); SetActiveSections + SetActiveFields hints save another
~45-50% at single-row, ~38% per-row at N=1000. The 6×/24× tax is the
maintenance simplification's price; mitigated by callers batching
(the buscodec frame-batching pattern from ADR-0036). The pre-M8
"keep two pipelines" and "per-kind dml schemas" alternatives were
both rejected at this point.

Phase-0 sparse-format shims live in `factsschema/{arrowrowbinary,
arrowrowcbor, arrowsparserb, dml_rowbinary, dml_cbor, dml_sparserb,
bench}/`.

### 2026-05-20 — M7-M12: driver-path consolidation shipped

- **M7** (boxer commit `1601a47`): `BuilderPackage` parameterisation in
  `leeway/dml.NewGoCodeGeneratorDriver` makes `dml_rowbinary`,
  `dml_cbor`, `dml_sparserb` first-class generator outputs.
- **M8** (`6ee2d024`, `048e4c65`, `da05e3de`): m1fixture canary on the
  driver path; measurements above. Three levers (sync.Pool +
  SetActiveSections + SetActiveFields) extracted; promoted to
  first-class generator output in M9.
- **M10**: codec/codegen rewrites per-kind `.out.go` to delegate to
  `dml_rowbinary.InEntityFacts` (write) and `factsschema/ra.ReadAccess*`
  (read). Per-kind emitted blocks: `<Kind>ActiveSections`,
  `<Kind>ActiveFields` (sync.OnceValue), `<kind>Pool`, `Marshal`,
  `Unmarshal`. `codec/wire` retires from per-kind output (survives
  only inside `arrowrowbinary`). Generated headers drop the
  `//go:build llm_generated_opus47` tag.
- **M11**: capabilitygrant + errkind retrofitted. `fixture_driver.go`
  deleted from m1fixture (12 MarshalDriver* benchmarks retired with
  the canary). Per-kind `<Kind>ColumnList()` /
  `<Kind>ChlocalStructure()` retired in favour of kind-agnostic
  `colspec.ProjectColumnsByActiveFields(<Kind>ActiveFields())` /
  `colspec.ProjectChlocalStructureByActiveFields(...)`. The chlocal
  test assertions on `lrcard` moved from the byte-appender's
  per-distinct-kind shape (`[1, 3]`) to the driver path's
  per-attribute shape (`[1, 1, 1, 1]`); both are leeway-canonical.
- **M12**: codegen emits a `<Kind>Codec` (buscodec.CodecI) + a
  package-init `buscodec.Register` call. `Name()` returns
  `<kindName>-sparse-rb`; `Encode` accepts `<Kind>` or `*<Kind>` →
  one-row Columns → Marshal → wire; `Decode` requires `*<Kind>` →
  Convert + active-fields projection → ipc.NewReader → Unmarshal →
  `Row(0)`. Three `buscodec_test.go` files pin `Lookup[T]()`
  returning the kind's codec and full Encode/Decode round-trip.
  Cycle-break note: `rowbinaryarrow/convert_test.go` moved to
  `package rowbinaryarrow_test` (kind packages now import
  rowbinaryarrow for Convert).

Driver-path numbers:

| Kind            | Marshal                              |
|-----------------|--------------------------------------|
| M1Sample        | 3.1 μs SingleRow / 1.9 μs/row N=1000 |
| CapabilityGrant | 1.6 μs / 5 allocs                    |
| Error (4-fact)  | 2.6 μs / 6 allocs                    |

### 2026-05-20 — Convert + arrowrowbinary feature completion

`factsschema/rowbinaryarrow.Convert` now covers every section + role
the runtime.facts schema declares:

- 8 typed `readArray*` (Uint8 / Uint16 / Int8 / Int16 / Int32 / Int64
  / Float32 / Float64) plus 2 discard helpers in `reader.go`.
- `taggedReader` extended to HighCardRef / MixedLowCardRef /
  *Cardinality / Length / MixedRefHighCardParameters. No in-tree
  kind populates them; the wire still encodes them as `Uvarint(0)`.
- `taggedValueReader` covers text / u8 / u16 / i\* / f\* / time and
  their *Array / *Set / timeArray variants (35 sections total).
- 16 *Array + 2 Set apply functions in `convert.go`, all sharing the
  same scaffold: walk `lr` + `countsPerAttr` in lockstep,
  BeginAttribute → AddToContainer×n → AddMembershipLowCardRef →
  EndAttribute, sum-bounds-checked via `validateArrayShape`.
  `TestU32Array_RoundTrip` is the synthetic gate; the other 17 are
  trusted-by-construction.
- `sectionState.countsPerAttr []uint64` is the shared slot for `len`
  (HomogenousArray, per-attribute element count) and `card` (Set,
  per-attribute cardinality) — orthogonal in the schema, so apply
  functions branch on section type alone.

`arrowrowbinary.TimestampBuilder` switched from 8-byte UnixMilli
(Phase-0 stop-gap) to CH-canonical 4-byte UInt32 unix-seconds.
Symmetric reads in Convert + Unmarshal followed. Conversion is lossy
in principle (ms → sec) but lossless for runtime.facts (canonical
type z32, second precision); a future z64 / DateTime64 column would
trigger a per-instance unit flag on the builder.

Eight previously-skipped round-trips un-skipped across m1fixture,
capabilitygrant, errkind, and rowbinaryarrow/convert_test.go. The
chlocal-via-RowBinary tests un-skipped through `colspec.ProjectChlocal
StructureByActiveFields` (the full 299-column structure would mismatch
the SetActiveFields-suppressed wire). m1fixture's `colF32Value` /
`colF64Value` constants re-pinned to the current schema's `:gM:`
encoding hint (was `:i:` before the slowly-changing-float aspect
landed).

`codec/errkind/leewayrender/leewayrender.go` switched its consumer
shape from `errkind.ErrorColumnList()` (retired in M11) to
`colspec.ProjectColumnsByActiveFields(errkind.ErrorActiveFields())`.

Out of scope (deferred):

- The codegen `<Kind>` DTO grammar still rejects fields whose vdd
  cardinality declares `HomogenousArray` or `Set` sub-types — those
  would need new Go shapes (typed wrapper around `[]T` with a
  sub-type marker). When a real fact-kind needs it, the Go surface
  change + parse.go + emit.go support all live in the codegen layer,
  not in Convert.
- The HighCard / Mixed-card / mrhp roles stay "consume + discard"
  in classify.go. No in-tree fact-kind populates them; symmetric
  apply paths would compile but have no test coverage.

### 2026-05-21 — First broker-DTO migration: `task.TaskProgress`

The codec was designed for *fact kinds* observed in `runtime.facts`.
M12 plugged it into buscodec via a CodecI register seam, but every
broker wire DTO (TaskCreated / TaskProgress / TaskDone / TaskError /
TaskCancel / GrantRequest / …) still rode the fxamacker-cbor default.
This entry opens the migration of those DTOs onto the codec, with
`task.TaskProgress` as the canary.

Broker wire forms share two structural traits with proper fact kinds:
a small fixed set of typed scalar fields, plus a natural unit-of-work
id and emission timestamp. They already fit the leeway grammar — a
fact kind is exactly "an id + a timestamp + N typed columns" — but
needed vocabulary. Two halves:

1. *Shared (abstract) memberships* in
   `keelson/vdd/keelson_dimdata_shared.go`: `taskId` (every task.\*
   wire DTO will reuse; future GrantId / RunId likely fold in once
   the broker DTOs converge), `note` (free-text annotation reusable
   by progress, errkind.Error, future audit DTOs).
2. *Narrow (kind-specific) memberships* in
   `keelson/vdd/keelson_dimdata_taskprogress.go`: `progressCurrent`,
   `progressTotal`, `progressUnit`, `progressThroughputPerSec`,
   `progressEtaMs`. Mirrors the `cg…` / `m1…` / `err…` per-kind
   layout.

The split lets future task.\* DTOs reuse `taskId` without
duplicating it under a kind prefix; it documents cross-cutting versus
narrow terms.

DTO at `public/keelson/runtime/codec/taskprogress/` (kind
`taskProgress`, plain `id=FactId, ts=AtNs`). Every tagged field
ExactlyOne — Go zero value carries the absence semantics (Total=0 ⇒
indeterminate, EtaMs=0 ⇒ not-yet-computed, Note="" ⇒ no annotation),
matching capabilitygrant.ExpiresAt rather than reaching for
`codec.Option`.

Wire breaks vs the legacy `task.TaskProgress` JSON form:

- `Id TaskIdT` → `TaskId string`. Subject id moves out of the plain
  `id` slot (fact-row id) into a string-section tagged column.
- `AtMs int64` → `AtNs int64`. Plain `ts` expects nanoseconds (or
  `time.Time`); producers multiply UnixMilli by 1e6 at the wire
  boundary. Internal millisecond arithmetic (lastEmitMs /
  heartbeatMs) unchanged.
- New `FactId uint64` plain `id` (per-row event sequence). Producers
  currently leave zero until a real sequencer lands.

`codec/taskprogress/buscodec_test.go` pins full-fields roundtrip,
zero-value tail (Total=0 / EtaMs=0 / Note="" reconstruct as literal
zero), and sub-second AtNs truncation. `buscodec.Lookup` resolves to
`taskProgress-sparse-rb` via the auto-registered init().

Pattern for the remaining broker DTOs (TaskCreated/Done/Error/Cancel
reuse `MembTaskId`; GrantRequest/Reply, wireRequest/Reply,
PersistReply follow the same flattening; `map[string]string` settings
via sorted co-arrays backed by
`boxer/public/containers.BinarySearchGrowingKV` when iteration
determinism matters). Each migration is an isolated commit using the
canary's vocabulary + DTO + test triple. Once all migrate, the
fxamacker-cbor default in buscodec is dead weight.

### 2026-05-21 — Second broker-DTO migration: `task.TaskCreated`

`task.TaskCreated` is the second wire DTO to migrate. The canary
pattern transfers cleanly with no codegen change. The migration also
exercises *shared vocabulary reuse* — `MembTaskId` defined for
TaskProgress is the same membership referenced by TaskCreated; a
query joining "all rows for task X" matches both DTOs through one
column.

Vocabulary extension:

- Shared abstract terms added to
  `keelson/vdd/keelson_dimdata_shared.go`: `MembTitle` (text),
  `MembAppId` (string), `MembTileKey` (u64), `MembRunId` (string).
  Future audit / event DTOs that bind to runtime lifecycle or carry
  a one-line label can reuse them without coining new names.
- Narrow memberships in `keelson_dimdata_taskcreated.go`:
  `MembTaskKind` (symbol), `MembTaskCancellableB` (bool),
  `MembTaskEstimatedMs` (i64).

DTO at `public/keelson/runtime/codec/taskcreated/`. Same wire
breaks as TaskProgress: `Id TaskIdT` → `TaskId string`, `AtMs` →
`AtNs`, new `FactId uint64` plain id. `OwnerAppId app.AppIdT` flattens
to plain `string` because the codec field type is `string`; callers
cast with `string(appId)` at construction and `app.AppIdT(s)` on
read. The supervisor / taskmonitor cast at the boundary in their
respective Observer callbacks.

`buscodec_test.go` mirrors the TaskProgress suite (full roundtrip +
zero-value tail + sub-second AtNs truncation). Three of nine in-tree
broker DTOs remain on the fxamacker-cbor default (TaskDone,
TaskError, TaskCancel); they follow the same recipe.

### 2026-05-21 — Third broker-DTO migration: `task.TaskCancel`

`task.TaskCancel` is the third wire DTO to migrate, validating the
recipe on a trivially-shaped payload (one user-visible field, a
short `Reason` string). Shared vocab added:

- `MembReason` (text, ExactlyOne) in
  `keelson/vdd/keelson_dimdata_shared.go`. Will be reused by
  TaskError next; the cross-cutting half of "any DTO carrying a
  'why' annotation" — siblings of `MembNote` (annotation) and
  `MembTitle` (label).

DTO at `public/keelson/runtime/codec/taskcancel/`. Same wire
breaks as the prior migrations (`Id TaskIdT` → `TaskId string`,
`AtMs` → `AtNs`, new `FactId uint64`). No narrow vocab — every
field maps to an existing shared term, which is the test that the
shared-vs-narrow split chosen for TaskProgress holds up under
reuse.

The legacy "nil-payload yields zero TaskCancel" interop hook in
`task.UnmarshalTaskCancel` survives — the helper bypasses the codec
on empty input, so cancel-with-no-reason publishers stay
wire-compatible across the migration. `spawn.go`'s cancel
subscription doesn't inspect the payload (any message on the
cancel subject cancels the producer context), so the wire-shape
change has zero observable effect on the cancellation path.

Two of nine in-tree broker DTOs remain on fxamacker-cbor
(TaskDone, TaskError). TaskError next: it adds `Reason` (now
`MembReason`) plus a `[]byte Error` field whose semantics
(plain-text FormatErrorWithStackS rendering today) map cleanly to
the `text` section. TaskDone's `[]byte Result` is genuinely opaque
binary and will need a follow-up call on whether to widen the
codec DTO grammar to variable-length blob or to encode the result
out-of-band.

### 2026-05-21 — Fourth broker-DTO migration: `task.TaskError`

`task.TaskError` is the fourth wire DTO to migrate. Adds one
shared term and validates the design decision flagged in the
TaskCancel entry (that the producer-side `[]byte Error` field can
move cleanly into a text-section column).

Vocabulary addition:

- `MembErrorText` (text, ExactlyOne) in
  `keelson/vdd/keelson_dimdata_shared.go`. Names the rendered
  error chain. Reusable by future DTOs (RPC reply, audit
  envelope) that surface a captured error alongside their
  payload. A future structured-CBOR variant would land as a
  *separate* column (e.g. `errorStructured`) — `errorText` stays
  the human-readable surface.

DTO at `public/keelson/runtime/codec/taskerror/`. The
field-level wire breaks repeat the prior migrations plus one
type rename: `Error []byte` → `ErrorText string`. The producer
already captured `eh.FormatErrorWithStackS(taskErr)` (UTF-8
multi-line text), so the rename is a `[]byte ↔ string` cast at the
producer (`handle.go`) and the consumer boundaries
(`supervisor.OnError`, `taskmonitor.OnError`, observer tests).
The supervisor's `error_text` audit field name stays the same;
only its source changes from `string(e.Error)` to `e.ErrorText`.

The migration also retires the doc-comment's aspirational claim
that TaskError carried the `eh.MarshalError` CBOR chain — the
producer has always written plain text. When a structured chain
column lands, it'll be a new field, not a re-typing of this one.

`buscodec_test.go` adds a multi-line `ErrorText` round-trip to pin
that newlines + indentation survive the text-section emit (errorview
reads the value verbatim). Reason-only failures (taskErr=nil)
round-trip with `ErrorText=""` as expected.

One DTO remains on the fxamacker-cbor default: `task.TaskDone`.
Its `[]byte Result` is genuinely opaque application payload
(decided by the originating TaskCreated.Kind), so the migration
hinges on either widening the codec DTO grammar to support
variable-length blob columns or moving Result onto an out-of-band
carrier. Decision deferred until a TaskDone consumer surfaces a
constraint.

### 2026-05-21 — Scalar-blob grammar extension + final task.* migration

Closes both the codec grammar extension flagged in the TaskCancel /
TaskError entries above and the final task.* broker DTO. With this
entry every wire DTO in the `task` package rides the leeway codec;
no in-tree publisher remains on the fxamacker-cbor default.

#### Grammar extension: `[]byte` is the blob spelling

The codec grammar gains scalar-blob support: a top-level `[]byte`
tagged field encodes as a single variable-length blob column
(ExactlyOne or via `codec.Option[[]byte]` for ZeroToOne). The
extension is keyed off the Go type's *spelling*: per the boxer
coding standard, sized integers spell themselves explicitly, so
`[]byte` is reserved for binary blobs while `[]uint8` continues to
denote a slice of uint8 values (the existing multi-cardinality
lane). The parser inspects the AST identifier name — `byte` triggers
the blob path; `uint8` stays in the slice-of-u8 path — so the choice
is at the surface layer, not inferred.

Concretely:

| Go shape                  | Section | Cardinality        | Meaning                                  |
|---------------------------|---------|--------------------|------------------------------------------|
| `[]byte` tagged           | blob    | ExactlyOne         | single variable-length blob (NEW)        |
| `codec.Option[[]byte]`    | blob    | ZeroToOne          | optional variable-length blob (NEW)      |
| `[4]byte` / `[16]byte`    | blob    | ExactlyOne         | fixed-width blob (existing)              |
| `[][]byte`                | blob    | Arbitrary          | multi-cardinality of variable blobs      |
| `[]uint8` tagged          | u8      | Arbitrary          | slice of uint8 scalars (existing lane)   |

Codegen changes are surgical:

- `parse.go` classifyType branches on the AST identifier "byte" for
  top-level `[]byte` (and `Option[[]byte]`), routing both to
  `IsSlice=false, GoType="[]byte"` — same path the existing `string`
  ExactlyOne shape takes structurally.
- `parse.go` validation gains one new check: scalar `[]byte` rejects
  any section other than `blob`, with an error message that points
  callers at `[]uint8` for slice-of-u8 intent.
- `emit.go` adds a `case "[]byte":` arm in the Marshal / Unmarshal
  membership-switch alongside the existing `[4]byte` / `[16]byte`
  cases. Marshal passes the slice straight to `BeginAttribute` (no
  re-slice needed; the blob section's `value` parameter is already
  `[]byte`). Unmarshal does a defensive copy (`make + copy`) because
  the `val` returned from the ra reader aliases the shared Arrow
  buffer — pinned by a new `TestBuscodecRoundTripDefensiveCopy` gate
  in `codec/taskdone/buscodec_test.go`.

Re-running `./generate.sh` produces byte-identical `.out.go` for
every existing kind — no existing DTO uses scalar `[]byte`, so the
extension is purely additive. The plain-column `[]byte` path
(capabilitygrant.NaturalKey, errkind.NaturalKey) is unaffected
because `if isPlain` short-circuits ahead of the tagged-shape
classification.

#### TaskDone migration

`task.TaskDone` is the first in-tree consumer of the scalar-blob
grammar. Vocabulary addition:

- `MembTaskResult` (blob, ExactlyOne) in
  `keelson/vdd/keelson_dimdata_taskdone.go`. Narrow — "the task's
  application-defined success payload" is task-specific semantics.
  A future RPC-reply DTO that wants a generic `result` column can
  introduce its own term then; better to start narrow than to
  retroactively split a too-broad shared term.

DTO at `public/keelson/runtime/codec/taskdone/`. Same wire
breaks as the prior task.* migrations (`Id`→`TaskId`, `AtMs`→`AtNs`,
new `FactId uint64`). `Result []byte` keeps the same Go shape; the
codec replaces the CBOR bytes envelope with a single variable-length
blob column.

`buscodec_test.go` carries six gates: Lookup name + full roundtrip +
empty Result + 256-byte 0-255 binary payload (preserves embedded
NULs and non-UTF-8 bytes verbatim) + defensive-copy contract (mutate
first decode, verify second decode is unaffected) + sub-second AtNs
truncation. The binary-payload and defensive-copy tests pin the
grammar extension's wire faithfulness independently of any specific
producer.

#### Migration ledger

All five task.* wire DTOs now ride the leeway codec via the
auto-registered sparse-RB buscodec bridge:

| DTO          | Codec name                  | Test gate                         |
|--------------|-----------------------------|-----------------------------------|
| TaskProgress | `taskProgress-sparse-rb`    | `codec/taskprogress/buscodec_test.go` |
| TaskCreated  | `taskCreated-sparse-rb`     | `codec/taskcreated/buscodec_test.go`  |
| TaskCancel   | `taskCancel-sparse-rb`      | `codec/taskcancel/buscodec_test.go`   |
| TaskError    | `taskError-sparse-rb`       | `codec/taskerror/buscodec_test.go`    |
| TaskDone     | `taskDone-sparse-rb`        | `codec/taskdone/buscodec_test.go`     |

The shared-vocabulary file (`keelson/vdd/keelson_dimdata_shared.go`)
now carries the cross-cutting half — `taskId`, `note`, `title`,
`appId`, `tileKey`, `runId`, `reason`, `errorText` — and is the
template for the next migration cohort (broker request/reply DTOs:
GrantRequest/Reply, wireRequest/Reply, PersistReply). Those bring
the `map[string]string` co-array question (`BinarySearchGrowingKV`
in boxer) and the broker-side request/response framing decisions
that the canary series intentionally deferred.

### 2026-05-21 — Capbroker migration + nested-struct flatten pattern

`capbroker.GrantRequest` and `capbroker.GrantReply` migrated as the
first broker request/reply pair. Introduces the *nested-struct
flatten* pattern that the rest of the broker DTO cohort will reuse:
when a Go DTO field is itself a struct (here `app.SubjectFilter`),
the codec wire form carries the sub-fields as peer columns and the
in-package Marshal/Unmarshal helpers convert at the boundary. The
codec grammar stays flat by design.

#### Vocabulary

Two new files. Narrow (in
`keelson/vdd/keelson_dimdata_capbroker.go`):

- `MembCapFilterPattern` (string, ExactlyOne) — SubjectFilter.Pattern.
- `MembCapDirection` (symbol, ExactlyOne) — SubjectFilter.Direction
  rendered as its canonical String() ("pub" / "sub" / "pub+sub" /
  "unspecified"). Symbol section so the wire is self-describing and
  the on-disk column dictionaries the small enum.
- `MembCapFilterSticky` (bool, ExactlyOne) — SubjectFilter.Sticky.
- `MembGrantApproved` (bool, ExactlyOne) — the policy decision.
- `MembGrantId` (string, ExactlyOne) — broker-local grant id, empty
  on denial.

Shared reuse — both DTOs lean heavily on the cross-cutting half
that already exists:

- `MembAppId` (since taskcreated) — the target app.
- `MembReason` (since taskcancel) — used twice in this pair:
  `SubjectFilter.Reason` *and* `GrantReply.Reason` collapse to the
  same column. A future query that wants "all rows with a reason"
  matches across task.cancel, task.error, grant.request, and
  grant.reply through one join key.

#### Nested-struct flatten

`capbroker.GrantRequest`:

```go
type GrantRequest struct {
    AppId         app.AppIdT
    SubjectFilter app.SubjectFilter  // {Pattern, Reason, Direction, Sticky}
}
```

Codec wire form (`grantrequest.GrantRequest`) flattens the nested
SubjectFilter into peer tagged columns. The translation happens
inside `capbroker.MarshalRequest`:

```go
wire := grantrequest.GrantRequest{
    AppId:           string(r.AppId),
    FilterPattern:   r.SubjectFilter.Pattern,
    FilterReason:    r.SubjectFilter.Reason,
    FilterDirection: r.SubjectFilter.Direction.String(),
    FilterSticky:    r.SubjectFilter.Sticky,
}
```

`UnmarshalRequest` reconstructs the SubjectFilter from the flat
columns via a new `app.ParseCapDirection` helper (the symmetric
inverse of `CapDirectionE.String`, added in this migration). The
broker's existing API (broker.go, policy.go, GrantPolicyI) stays
unchanged — only the codec boundary moved.

This is the recipe for every remaining broker DTO whose Go shape
includes a nested struct. Don't extend the codec grammar to handle
nesting; flatten at the wire and convert at the boundary.

#### Field rename

`GrantReply.Granted` → wire `grantApproved`. The in-package
`capbroker.GrantReply.Granted bool` field is unchanged; the codec
DTO chose the more explicit `grantApproved` vocabulary so a future
query that scans "all approval decisions" doesn't collide with a
generic `granted` term that future DTOs might also want.

#### Test gates

`codec/grantrequest/buscodec_test.go` + `codec/grantreply/buscodec_test.go`:
Lookup name + full roundtrip + per-direction round-trip (all four
canonical CapDirectionE strings including the zero-value
"unspecified" → empty-string sentinel) + denied-path tail (Approved
false / GrantId empty / Reason populated).

`capbroker/broker_test.go` (untouched) continues to exercise the
broker's end-to-end behaviour via the migrated helpers — the
nested-struct flatten + reconstruct contract is implicitly pinned
because every existing broker test round-trips through the new
codec path.

#### What's left in the cohort

Five DTOs remain on the fxamacker-cbor default:

| DTO                       | Pkg          | Notes                                  |
|---------------------------|--------------|----------------------------------------|
| WatchRequest              | fsbroker     | 3 primitives; trivial.                 |
| WatchReply                | fsbroker     | 4 primitives; trivial.                 |
| WatchEvent                | fsbroker     | 4 primitives (uint8 enum); trivial.    |
| DialogReply               | fsbroker     | 3 primitives; trivial.                 |
| ~~PersistReply~~              | persist      | **migrated 2026-05-21 (homogeneous-array entry below)** |
| InflightSnapshotReply     | task         | **list of structs** — needs design.    |

The first four are mechanical. `InflightSnapshotReply.Entries`
([]InflightSnapshotEntry) is the structural challenge the canary
series has been pointing at: the leeway codec is "one fact-kind per
buscodec call, one row per call". A reply carrying a list of N
entries maps either to (a) N rows of an entry fact-kind in the
buscodec frame, or (b) parallel-array columns (each entry-field
becomes a `[]T` column on a single row). (b) fits the existing
codec grammar but needs a wrapper kind that owns the parallel
arrays; (a) needs a buscodec-level batch contract that doesn't
exist yet. Deferred until the next migration step.

### 2026-05-21 — Fsbroker cohort (DialogReply, Watch{Request,Reply,Event})

Four flat-primitive DTOs migrated as a single cohort. All four
follow the recipe established by the capbroker entry: codec wire
form lives in `keelson/runtime/codec/<kind>/`, in-package Go shape
in fsbroker stays unchanged for API stability, and
`fsbroker/payload.go` translates at the boundary.

#### Vocabulary

11 new narrow terms in `keelson/vdd/keelson_dimdata_fsbroker.go`
across two thematic prefixes:

- *dialog* — `dialogApproved` (bool), `dialogHandleSubject` (string).
- *watch* — `watchPollFallback` (bool), `watchPollIntervalMs` (i32),
  `watchRecursive` (bool), `watchStarted` (bool),
  `watchEventSubject` (string), `watchBackend` (symbol),
  `watchEventKind` (symbol), `watchEventName` (string),
  `watchEventCookie` (u32).

Shared reuse: `reason` (used by DialogReply.Reason and
WatchReply.Reason — the `reason` column is now joined across six
DTOs).

`dialogApproved` is intentionally kept narrow rather than reusing
`grantApproved` from the capbroker pair. Both are
broker-says-yes booleans but the domains differ (file-picker UI vs
capability policy); the boxer code-standard's "abstract
shared / narrow per-kind" line falls on the narrow side here. If a
third "approval" DTO emerges, that's the trigger to elevate to a
shared `approved` term and migrate both back-references.

#### Two new ParseXxx helpers

- `app.ParseCapDirection` was added with the capbroker migration.
- `fsbroker.ParseWatchEventKind` is the symmetric inverse of
  `WatchEventKindE.String`. Both follow the same pattern as
  `task.ParseUnit`: unknown inputs map to the Unspecified zero value
  so the wire stays forward-compatible with future enum members.

#### Unit boundary note

`WatchEvent.Ts` was already `int64` unix **nanoseconds** in the
producer (every `watcher.go` emission uses `time.Now().UnixNano()`),
so the field rename `Ts` → `AtNs` on the wire is a pure name
change — no `* 1_000_000` cast at the producer like the task.*
migrations needed. The legacy "nil payload yields zero
WatchRequest" interop hook in `UnmarshalWatchRequest` survives
unchanged.

#### Test gates

Per-DTO `buscodec_test.go`: Lookup name + round-trip happy path +
the kind-appropriate tail case (denial / failed / all-defaults /
zero-Name root event / every canonical enum string). All green.
The existing fsbroker package tests (service_test.go,
watcher_test.go) pass unchanged through the new codec path.

#### Cohort tracker

| DTO                       | Pkg          | Migration       |
|---------------------------|--------------|-----------------|
| DialogReply               | fsbroker     | ✓ this entry    |
| WatchRequest              | fsbroker     | ✓ this entry    |
| WatchReply                | fsbroker     | ✓ this entry    |
| WatchEvent                | fsbroker     | ✓ this entry    |
| PersistReply              | persist      | next            |
| InflightSnapshotReply     | task         | needs design    |

`PersistReply` is the last trivial-flat DTO; its only complication
is an opaque `[]byte` payload (now well-trodden ground via the
scalar-blob grammar extension landed with TaskDone).

### 2026-05-21 — `runtime.facts` homogeneous-array-only rename + PersistReply migration

Two-part change. The `runtime.facts` schema migrated mid-cohort: it
no longer declares the scalar-section variants
(`string`, `text`, `blob`, `u{8,16,32,64}`, `i{8,16,32,64}`,
`f{32,64}`, `time`) and instead declares only their homogeneous-
array siblings (`stringArray`, `textArray`, `blobArray`, …). The
leeway grammar itself was unchanged — both scalar and array
sub-types still exist as protocol concepts. The change is at the
**`runtime.facts` schema-binding layer** (which sections this
specific table declares), not at the leeway-protocol layer.

On the leeway-generator side (`dml/lw_dml_generator.go`,
`readaccess/lw_ra_generator.go`) boxer **added** new
`BeginAttributeSingle` / `GetAttrValueSingle` /
`GetAttrValueSingleOrDefault` QoL methods on homogeneous-array
section types. These are pure API additions — they let producers
and consumers keep using the scalar call shape against array-typed
columns. Six sections stay scalar: `bool`, `foreignKey`, `symbol`,
`u32Range`, `u32Set`, `u64Set`. (Symbol has a separate
`symbolArray` schema section for genuine multi-symbol per attribute;
the scalar `Symbol` API stays distinct.)

#### Vocabulary rename across a downstream consumer

The schema rename surfaces in a downstream consumer as a vocabulary rename
on both vdd memberships and DTO `lw:` tags. The scalar names are
gone from the schema, so DTOs and vdd register against the new
canonical `*Array` names:

| Before (scalar-name)                                   | After (*Array name) |
|-------------------------------------------------------|--------------------|
| `MembXxx.MustAddRestriction("string", …)`             | `…("stringArray", …)` |
| `lw:"<name>,u32"`, `lw:"<name>,blob"`, …               | `lw:"<name>,u32Array"`, `lw:"<name>,blobArray"`, … |
| (unchanged) `MembXxx.MustAddRestriction("bool", …)` / `lw:"<name>,bool"`        | (unchanged) — `bool` / `foreignKey` / `symbol` / `u32Range` / `u32Set` / `u64Set` stay scalar |

Codegen accepts the legacy non-Array names as back-compat aliases
in `dmlSections`, but every in-tree DTO + vdd file has been ported
to the canonical `*Array` form.

#### Code surfaces touched

- **`codec/codegen/parse.go`** — section consistency checks (roaring
  requires `u32Array`; scalar `[]byte` requires `blobArray`;
  `[][]byte` requires `blobArray`) accept the legacy non-Array
  names as aliases.
- **`codec/codegen/emit.go`** — `dmlSectionInfo` gains an
  `IsSingle` bool. `dmlSections` map renumbered to 0..20 (was
  0..34) reflecting the schema's 21 surviving sections. The
  canonical `*Array` keys carry `IsSingle: true` so the codegen
  emits `BeginAttributeSingle` for the cardinality-1 attribute
  calls every codec field produces. `writeFieldDriver` selects
  `BeginAttribute` vs `BeginAttributeSingle` based on
  `info.IsSingle`. `activeSectionNames` returns
  `lowerFirst(info.Method)` so the active-fields-projection scan
  matches the schema's `tv:<schemaName>:...` column prefix.
- **`writeUnmarshalSingleSubColumnDecodeRa`** — emits
  `GetAttrValueSingleOrDefault` for `IsSingle` sections,
  `GetAttrValueValue` otherwise. The writer-side pairing guarantee
  (always `BeginAttributeSingle`) means `OrDefault` never silently
  zeros for codec-emitted data.
- **`factsschema/rowbinaryarrow/convert.go`** — manual dml driver
  migrated to the new `GetSectionXArray().BeginAttributeSingle(...)`
  shape for the scalar-promoted sections; the *Array apply
  functions kept their existing
  `GetSectionXArray().BeginAttribute().AddToContainer(...)`
  multi-value form. New `applyStringLikeArray` generic mirrors the
  existing `applyStringLike` against the new InAttr type names.
- **`factsstore/chstore/chstore.go`** — same scalar-promoted-section
  rename + `BeginAttributeSingle` switch on the producer side.
- **All `keelson/vdd/keelson_dimdata_*.go` + `codec/<kind>/<kind>.go`** —
  vdd `MustAddRestriction` calls + DTO `lw:` tags ported to
  `*Array` names.
- **All `codec/<kind>/<kind>.out.go`** — regenerated by
  `./generate.sh`.
- **Per-kind chlocal test column constants** in `m1fixture_test.go`,
  `errkind_test.go`, `capabilitygrant_test.go` updated from
  `tv:u32:...:u32:` style to `tv:u32Array:...:u32h:` (the schema
  column prefix gained the `Array` suffix; encoding hint gained
  the `h` marker for HomogenousArray storage).

#### PersistReply migration

The persist broker's reply DTO is the first migration authored
against the post-rename schema directly. Narrow vocabulary in
`keelson_dimdata_persist.go`:

- `MembPersistFound` (bool) — Get-located-the-key marker.
- `MembPersistValue` (blobArray) — opaque application payload via
  the scalar-blob grammar.

Shared reuse: `MembReason` (since taskcancel). The legacy
`PersistReply.Error` field maps to the `reason` wire column at
the codec boundary in `persist.MarshalReply` / `UnmarshalReply`;
the broker's Go API (Found / Value / Error) stays unchanged. A
future audit query joining "all rows with a reason" now matches
across **seven** migrated DTOs (taskcancel, taskerror,
grantreply, dialogreply, watchreply, persistreply, …).

The `nil` vs zero-length `[]byte` distinction doesn't survive the
scalar-blob roundtrip — empty Value reconstructs as `[]byte{}`
regardless of producer nil-ness. Documented on
`MembPersistValue`; pinned by `payload_test.go` switching from
deep-equal to `bytes.Equal` on the Value column.

#### Boxer-side regression: multi-row mixed-membership reader

`TestUnmarshal_RoundTrip_Batch` in `codec/m1fixture` (5 rows,
`U32Array` section carrying both ExactlyOne `Sequence` and
roaring `CapBits` per row) fails on the new ra
`AccelHomogenousArray` reader — every entityIdx returns row 0's
values. Single-row roundtrips pass; the regression is confined to
multi-row mixed-membership sections. Test is `t.Skip`'d with a
pointer at boxer's ra emit; the codec migration itself doesn't
re-enable until the upstream fix lands.

#### Cohort tracker (updated)

| DTO                       | Pkg          | Migration       |
|---------------------------|--------------|-----------------|
| DialogReply               | fsbroker     | ✓ prior         |
| WatchRequest              | fsbroker     | ✓ prior         |
| WatchReply                | fsbroker     | ✓ prior         |
| WatchEvent                | fsbroker     | ✓ prior         |
| PersistReply              | persist      | ✓ this entry    |
| InflightSnapshotReply     | task         | ✓ migrated 2026-05-21 (parallel-array entry below) |

### 2026-05-21 — Legacy alias cleanup + InflightSnapshotReply (list-of-structs)

Two follow-ups land together: the legacy non-Array vocabulary aliases
retired, and the final task.* DTO migrated via the parallel-array
list pattern.

#### Legacy non-Array aliases removed

Every in-tree DTO + vdd registration now uses the canonical
`*Array` section names. The back-compat aliases (`"string"` /
`"u8"` / `"blob"` / …) are dropped from `dmlSections`; parse.go
consistency checks revert to `*Array`-only acceptance (roaring
requires `u32Array`; scalar `[]byte` requires `blobArray`;
`[][]byte` requires `blobArray`). `blobSliceMaybe` follows. Net
effect: future DTOs can only spell scalar-promoted sections by
their canonical schema name, mirroring the runtime.facts schema
exactly.

#### InflightSnapshotReply — parallel-array list pattern

`task.InflightSnapshotReply` carries `Entries []InflightSnapshotEntry`.
The leeway codec is one-fact-kind-per-row and doesn't natively model
a list-of-structs. The migration flattens the slice of entries into
**eleven parallel `[]T` columns** on a single wrapper kind, one per
entry-field:

| Go field on `InflightSnapshotEntry` | Wire column (`lw:`-tag)      | Section       |
|-------------------------------------|------------------------------|---------------|
| `Id`                                | `inflightTaskId`             | `stringArray` |
| `Kind`                              | `inflightTaskKind`           | `symbol`      |
| `Title`                             | `inflightTitle`              | `textArray`   |
| `OwnerAppId`                        | `inflightAppId`              | `stringArray` |
| `State`                             | `inflightState`              | `symbol`      |
| `CreatedAtMs`                       | `inflightCreatedAtMs`        | `i64Array`    |
| `LastEmitMs`                        | `inflightLastEmitMs`         | `i64Array`    |
| `Current`                           | `inflightCurrent`            | `u64Array`    |
| `Total`                             | `inflightTotal`              | `u64Array`    |
| `Unit`                              | `inflightUnit`               | `symbol`      |
| `EtaMs`                             | `inflightEtaMs`              | `i64Array`    |

Each entry-field becomes an Arbitrary-cardinality membership; the
codec emits N attribute calls per parallel-array field per row.
Slice-order preservation is the load-bearing invariant — the wire
emits Ids[0..N-1] in slice-index order, then Titles[0..N-1], etc.,
and the read path appends back into each column in the same order.
Entries zip correctly by index on reconstruction.

Multiple memberships sharing the same physical section: `stringArray`
carries both Ids and OwnerAppIds; `symbol` carries Kinds, States, and
Units; `i64Array` carries CreatedAtMss, LastEmitMss, EtaMss;
`u64Array` carries Currents and Totals. The read-side
GetMembValueLowCardRef classifier separates the parallel streams by
membership id — they don't interfere even though they share storage.

#### Vocabulary

Eleven new narrow Arbitrary-cardinality memberships in a new
`keelson_dimdata_inflightsnapshot.go`. All `inflight…`-prefixed:
`inflightTaskId`, `inflightTaskKind`, `inflightTitle`,
`inflightAppId`, `inflightState`, `inflightCreatedAtMs`,
`inflightLastEmitMs`, `inflightCurrent`, `inflightTotal`,
`inflightUnit`, `inflightEtaMs`.

These can't reuse the shared `MembTaskId` / `MembAppId` / etc.
because the cardinality differs: TaskProgress.TaskId is ExactlyOne
(one task id per row); InflightSnapshotReply's Ids column is
Arbitrary (N task ids per row, one per entry). A single membership
can't declare both cardinalities, so the inflight surface gets its
own vdd entries.

#### Boundary translation

`task.InflightSnapshotReply` (the Go shape callers see — `Entries
[]InflightSnapshotEntry` + `AtMs`) stays unchanged. The fan-out from
`Entries` to parallel arrays happens inside
`task.MarshalInflightSnapshotReply`; the fold-in happens in
`UnmarshalInflightSnapshotReply` (defensive `sliceAt` accessors guard
against malformed wire where a column carries fewer than N values —
shouldn't fire when both sides came through the helpers, but matters
for cross-implementation interop).

#### Codegen widening

`parse.go`'s slice-element allowlist now accepts `int8`/`int16`/
`int32`/`int64` alongside the existing `uint{8,16,32,64}` /
`float{32,64}` / `bool` / `string`. The omission was historical —
the codec emits `BeginAttributeSingle(v)` per element regardless of
the underlying integer signedness, so signed-integer slice elements
slot in naturally.

#### Test gates

`codec/inflightsnapshotreply/buscodec_test.go`: Lookup name +
full three-entry round-trip pinning every parallel column with
`reflect.DeepEqual` (regression on order would break the entry
zip) + empty-snapshot case + single-entry boundary case. All green.

#### Cohort tracker — all DTOs migrated

| DTO                       | Pkg                          | Status |
|---------------------------|------------------------------|--------|
| TaskProgress              | task → codec/taskprogress    | ✓      |
| TaskCreated               | task → codec/taskcreated     | ✓      |
| TaskCancel                | task → codec/taskcancel      | ✓      |
| TaskError                 | task → codec/taskerror       | ✓      |
| TaskDone                  | task → codec/taskdone        | ✓      |
| GrantRequest              | capbroker → codec/grantrequest | ✓    |
| GrantReply                | capbroker → codec/grantreply | ✓      |
| DialogReply               | fsbroker → codec/dialogreply | ✓      |
| WatchRequest              | fsbroker → codec/watchrequest | ✓     |
| WatchReply                | fsbroker → codec/watchreply  | ✓      |
| WatchEvent                | fsbroker → codec/watchevent  | ✓      |
| PersistReply              | persist → codec/persistreply | ✓      |
| InflightSnapshotReply     | task → codec/inflightsnapshotreply | ✓ |

No in-tree DTO remains on the fxamacker-cbor default. The
codec-grammar gates that landed across the cohort — scalar `[]byte`
under `blobArray`, nested-struct flatten at the boundary,
parallel-array list — cover every real broker-DTO shape the
runtime has needed so far.

### 2026-05-21 — boxer ra accel multi-row mixed-membership fix

The boxer-side regression flagged in the 2026-05-21 runtime.facts
schema-rename entry (`AccelHomogenousArray` returning row 0's
values for every entityIdx > 0 on multi-row mixed-membership
sections) is fixed upstream. With a downstream consumer resolving boxer via
`go.work`, no go.mod pin bump is needed locally; CI/CD will pick
up the new pin when it next bumps.

`codec/m1fixture/unmarshal_test.go::TestUnmarshal_RoundTrip_Batch`
un-skipped — the 5-row scenario (mixed-membership U32Array with
ExactlyOne Sequence + roaring CapBits per row) decodes correctly
per-entity.

New gate: `codec/inflightsnapshotreply/unmarshal_test.go::TestUnmarshal_RoundTrip_Batch`.
Three wrapper rows with different per-row entry counts (2 / 3 / 0
entries) exercise the harder shape: multi-row + multi-membership-
per-section + Arbitrary cardinality. The empty-row case
(`FactId=3` with zero entries) pins the boundary where one wrapper
row in a batch has no parallel-array content even when sibling rows
do.

The two batch tests together cover the two distinct failure surfaces
the accel bug could have caused — mixed-membership-per-row (m1fixture's
shape, where one section carries one ExactlyOne field + one
Arbitrary field) and multi-membership-shared-section (inflight's
shape, where four sections each carry 2-3 Arbitrary fields zipped
by index). Both green.

### 2026-05-22 — Cross-backend benchmark sweep (count=10 + benchstat)

Full sweep of `factsschema/bench/` after the
homogeneous-array-only rename + `BeginAttributeSingle` migration
to confirm no regression from the schema simplification (~19k LOC
removed from `factsschema/{dml*,ra,ddl}/*.out.go` net). Reproduction:

```bash
go test -tags "$(cat tags | tr -d $'\n')" -bench . -benchmem \
        -count=10 -run='^$' \
        ./public/keelson/runtime/factsschema/bench/ \
        | tee /tmp/bench.log
benchstat /tmp/bench.log
```

`AMD Ryzen AI MAX+ PRO 395` (32 threads), commit `200f01bb`,
15.4 min wall. Cells are `median ± p95 CI`; **bold** = tight CI ≤ 10%.

Build phase, sec/op:

| Backend   | Grant N=1     | State N=1     | Log5 N=1      | Grant N=1000     | State N=1000     | Log5 N=1000      |
|-----------|---------------|---------------|---------------|------------------|------------------|------------------|
| Arrow     | 220 µs ± 34%  | 212 µs ± 14%  | **224 µs ± 9%** | 3.25 ms ± 16%  | **3.22 ms ± 2%** | **3.61 ms ± 4%** |
| CBOR      | 101 µs ± 18%  | 90 µs ± 47%   | 101 µs ± 15%  | **3.61 ms ± 10%** | **3.58 ms ± 3%** | **5.78 ms ± 4%** |
| RowBinary | **62 µs ± 2%** | 82 µs ± 23%  | 88 µs ± 41%   | **3.09 ms ± 4%** | 2.82 ms ± 12%    | 3.49 ms ± 12%    |
| SparseRB  | **74 µs ± 8%** | 79 µs ± 49%  | 84 µs ± 22%   | 4.18 ms ± 36%    | **3.49 ms ± 10%** | 4.64 ms ± 12%   |

Build + wire, sec/op:

| Backend       | Grant N=1     | State N=1        | Log5 N=1      | Grant N=1000     | State N=1000     | Log5 N=1000     |
|---------------|---------------|------------------|---------------|------------------|------------------|-----------------|
| Arrow IPC     | 552 µs ± 13%  | **522 µs ± 3%**  | 625 µs ± 15%  | **3.53 ms ± 2%** | **3.60 ms ± 1%** | **3.86 ms ± 3%** |
| CBOR Row      | 108 µs ± 15%  | 115 µs ± 11%     | 100 µs ± 21%  | **3.92 ms ± 6%** | 3.72 ms ± 11%    | **5.69 ms ± 6%** |
| RB Row        | 155 µs ± 80%  | 97 µs ± 32%      | **68 µs ± 7%** | 3.43 ms ± 29%   | **2.80 ms ± 7%** | **3.59 ms ± 10%** |
| SparseRB Row  | 75 µs ± 37%   | 118 µs ± 32%     | 88 µs ± 23%   | 3.41 ms ± 16%    | **3.41 ms ± 3%** | **4.71 ms ± 9%** |

Round-trip (Arrow, full encode → ipc → decode → ra.Load), sec/op:

|        | Grant            | State            | Log5             |
|--------|------------------|------------------|------------------|
| N=1    | 993 µs ± 20%     | 815 µs ± 19%     | **827 µs ± 6%**  |
| N=1000 | **4.13 ms ± 5%** | 3.96 ms ± 9%     | **4.31 ms ± 3%** |

**Geomean across all 54 sub-benchmarks: 770.8 µs.**

Per-row amortized (N=1000 Build): RB 2.82–3.49 µs · Arrow 3.22–3.61 µs ·
SparseRB 3.49–4.64 µs · CBOR 3.58–5.78 µs.

Wire bytes per row (steady-state, from `TestXxxWireSize`):

| Backend   | Grant  | State  | Log5   |
|-----------|--------|--------|--------|
| RowBinary | 457 B  | 432 B  | 680 B  |
| SparseRB  | 318 B  | 290 B  | 637 B  |
| CBOR      | 298 B  | 362 B  | 954 B  |

#### Findings

- **No regression.** The `BeginAttributeSingle` one-line delegation
  (`s.BeginAttribute().AddToContainer(v)`) costs nothing measurable
  vs the prior scalar `BeginAttribute(v)` shape.
- **`count=5` is below benchstat's confidence-interval threshold**
  (`need >= 6 samples for confidence interval at level 0.95`). A
  prior `count=5` run reported three plausible-looking outliers —
  `RBBuild/State/N=1000` ≈ 10.8 ms, `SparseRBBuildAndRow/Log5/N=1000`
  ≈ 10.1 ms, `SparseRBBuildAndRow/Grant/N=1000` ≈ 11.4 ms — all of
  which normalised to 2.82–4.71 ms at `count=10`. Lesson: don't
  publish numbers from `count<6` without flagging them as
  noise-floor candidates.
- **N=1 micro-cases are noise-dominated** even at `count=10`
  (worst: `RBBuildAndRow/Grant/N=1` ± 80%, `CBORBuild/State/N=1`
  ± 47%, `SparseRBBuild/State/N=1` ± 49%). Their N=1000 batched
  counterparts are tight (± 2–12%) — when comparing backends,
  prefer N=1000.
- **RowBinary Build wins at N=1** (62 µs Grant ± 2%) — fastest
  single-row producer. Arrow's row-builder overhead shows up
  cleanly here.
- **Arrow IPC adds ≈ 500 µs over Build at N=1** (the schema/footer
  cost that doesn't amortize until N grows). At N=1000 it's
  invisible — Arrow IPC matches CBOR Row within 5%.
- **CBOR wire is smallest for Grant** (298 B/row) but largest for
  Log5 (954 B/row) — typed-field heterogeneity hurts CBOR. SparseRB
  is the most consistent across fixtures (290–637 B/row).

### 2026-05-22 — z64 precision + sparse-CBOR cutover + RowBinary cleanup

Three-phase cohort lifting the codec off RowBinary entirely. Survivors:
`dml/` (Arrow record builder, chstore-ingest path), `dml_cbor/` +
`arrowrowcbor/` (codec wire), `cborarrow/` (new: codec read bridge),
`ra/` (Arrow read).

#### Phase A — `runtime.facts` time columns: z32 → z64

One-character source change at `factsschema/factsschema.go:70`
(`dt := ctabb.Z32` → `ctabb.Z64`) fans out into every leeway-generated
artefact. The boxer DDL emitters already mapped z64 to
`DateTime64(9,'UTC')` on the CH side and `arrow.Nanosecond` on the
Arrow side; `UnixMilli()` calls in the generated dml became
`UnixNano()` automatically.

Hand-edits matching the new wire:

- `arrowrowbinary.TimestampBuilder.emitRow`: 4-byte UInt32 unix-sec
  → 8-byte Int64 unix-nanos (still in tree during transition).
- `rowbinaryarrow/convert.go`: `tsSeconds uint32` →
  `tsNanos int64`; `time.Unix(s, 0)` → `time.Unix(0, ns)`;
  `readUint32LE` → `readInt64LE`; `valUint32["value"]` → `valInt64["value"]`
  for the time/timeArray apply paths.
- `codec/codegen/emit.go` Unmarshal template:
  `int64(ValueTs.Value(i)) * 1_000_000` → direct `int64(…)` read
  (Arrow Nanosecond unit gives nanos already; the legacy
  millis-to-nanos multiplier overflowed int64 post-z64).
- 12 hardcoded `:z32:` column-name string literals across
  `chstore/{chstore,recentlogs,runsessions}.go` and 6 chlocal
  test-column constants flipped to `:z64:` / `:z64h:`.

Test contracts updated:

- Five `TestBuscodecAtNsDownToSecPrecision` per-DTO tests renamed
  to `TestBuscodecAtNsLosslessNanoPrecision` and flipped from
  "expect truncation" to `got.AtNs == orig.AtNs`. The
  seconds-precision contract was a Phase-0 wire artefact, not a
  semantic promise.
- Six chlocal date-string fixtures gained `.000000000` suffix
  (chlocal renders `DateTime64(9)` with nine fractional digits).
- `rowbinaryarrow/convert_test.go::TestCapabilityGrant_RowBinaryToArrowStream`
  lossy assertion `(g.Ts/1e9)*1000` collapsed to `g.Ts`.

Commit `cb1cfe49`.

#### Phase B — codec wire: sparse-RB → sparse-CBOR

The M10–M12 codec template imported `dml_rowbinary` (the misnamed
full-RB sed-fork via `arrowrowbinary`) but advertised
`<kind>-sparse-rb`. Naming drift settled here by switching the
template to `dml_cbor` (sparse self-describing CBOR via the
`arrowrowcbor` shim) and renaming the codec to `<kind>-sparse-cbor`.

New package: `factsschema/cborarrow/` (≈ 1300 lines). Mirrors
`rowbinaryarrow`'s apply machinery (`rowState` / `sectionState` /
`applyTo` + per-section apply functions, copied verbatim) and
replaces the byte-positional column reader with a CBOR map-keyed
dispatcher: outer-array of per-row maps, plain-column keys
("id", "naturalKey", "ts", "expiresAt"), tagged-column keys
("<section>.<column>"). Convert signature drops `[]PhysicalColumnDesc`
and `rowCount` — CBOR is self-delimiting. Decode reuses the
existing `factsschema/ra` typed readers via the same Arrow record
that rowbinaryarrow used to produce.

Two `arrowrowcbor` fixes the cutover surfaced:

- `RecordBuilder.SetActiveFields` mirrored from arrowrowbinary —
  per-kind active-fields hint gates the per-row emit walk; the
  prepass + emit loops match (otherwise the map header miscounts).
- `BinaryBuilder.emitValue` coerces nil → `[]byte{}` before
  `EncodeByteSlice`. Boxer's CBOR encoder rejects nil with an
  error and writes zero bytes; the codec's `SetId(id, nil)`
  naturalKey case had been leaving a phantom key without a value,
  breaking the map header count downstream.

Test-coverage migration: 5 chlocal-pipeline tests
(capabilitygrant×2, errkind, m1fixture×2 plus
`TestBatchMarshal_MultipleRows`) and 2 rowbinaryarrow codec-driven
smoke tests `t.Skip`'d — clickhouse-local doesn't understand
sparse-CBOR. Coverage moves to per-kind buscodec_test.go
Encode→Decode roundtrips; 4 `unmarshal_test.go` files
(capabilitygrant, errkind, m1fixture, inflightsnapshotreply) and
`codec/errkind/leewayrender/leewayrender.go` rewired through
`cborarrow.Convert`.

Commit `d24a4c5c`.

#### Phase C — delete RowBinary + sed-fork shims

Five package directories retired: `factsschema/{dml_rowbinary,
dml_sparserb, arrowrowbinary, arrowsparserb, rowbinaryarrow}`. Two
bench files retired alongside their backends:
`bench/{rowbinary_bench_test.go,sparserb_bench_test.go}`. Cross-
backend benchmark sweep (the 2026-05-22 entry above) preserved as
historical reference but no longer reproducible from the in-tree
sources.

`factsschema/codegen/codegen.go` lost the `GenerateDMLRowBinary` /
`GenerateDMLSparseRB` entry points plus their output-path
constants and shim-import-path constants;
`app/commands/runtimecodegen/runtimecodegen.go` lost the matching
`dml-rowbinary` / `dml-sparserb` urfave subcommands and `runAll`
arms. Survivors: `dml`, `dml-cbor`, `readaccess`, `ddl`.

ChickHouse ingest is unaffected — chstore continues to use
`dml.InEntityFacts` → `InsertArrow` (Arrow IPC), not RowBinary.

Per-row latency tax flagged in the 2026-05-19 driver-path pivot
entry stands; cross-backend numbers in the 2026-05-22 sweep entry
remain the canonical reference (re-running them would require
restoring the deleted backends).

- [`keelson/vdd/EXPLANATION.md`](../../public/keelson/vdd/EXPLANATION.md) — the schema model and codegen contract this ADR commits to generate against.
- [ADR-0026 — app runtime + capability subjects](0026-app-runtime-and-capability-subjects.md) — introduced `runtime.facts`.
- [ADR-0035 — keelson namespace introduction](0035-keelson-namespace-introduction.md) — situates this work under `public/keelson/`.
- [ADR-0036 — runtime/buscodec](0036-runtime-buscodec.md) — sibling codec ADR; same `CodecI`-swap design philosophy applied to bus payloads.
- [ADR-0041 — rowmarshall error shredding](0041-rowmarshall-error-shredding.md) — settled the no-reflection-on-hot-path rule and the parallel-array shape for tree-shaped fact kinds.
- [`public/keelson/vdd/`](../../public/keelson/vdd) — central membership registry consumed by the generator.
- [`public/keelson/runtime/codec/capabilitygrant/`](../../public/keelson/runtime/codec/capabilitygrant) and [`/errkind/`](../../public/keelson/runtime/codec/errkind) — the retrofits that supersede the (now-deleted) hand-coded `rowmarshall` package per the Updates entry below.
- [`public/keelson/runtime/factsschema/dml/`](../../public/keelson/runtime/factsschema/dml) — Arrow-builder codegen target (chstore-ingest path).
- [`public/keelson/runtime/factsschema/dml_cbor/`](../../public/keelson/runtime/factsschema/dml_cbor) — sparse-CBOR codegen target (codec wire).
- [`public/keelson/runtime/factsschema/cborarrow/`](../../public/keelson/runtime/factsschema/cborarrow) — sparse-CBOR → Arrow bridge used by codec Decode.
- [`public/keelson/runtime/factsschema/ra/`](../../public/keelson/runtime/factsschema/ra) — Arrow-read codegen target.
- `public/spinnaker/vdd/` — prior-art registry pattern that `keelson/vdd` mirrors.
