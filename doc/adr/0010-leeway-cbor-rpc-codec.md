---
type: adr
status: deferred
date: 2026-05-15
---

# ADR-0010: Leeway CBOR codec for low-latency RPC

## Context

Leeway is optimised for high-throughput batched workloads: Go value → `dml/runtime` → Arrow `RecordBatch` → ClickHouse / Parquet / Arrow IPC, with per-batch overhead (section co-arrays, dictionaries, Arrow metadata) amortised across thousands of entities. **For one-entity-per-call RPC the amortisation never happens.** A one-row Arrow batch plus IPC framing costs far more than the payload warrants under a tens-of-microseconds round-trip budget.

Downstream consumers (the `runtime.facts` table, the spinnaker fact lineage) define **multi-shape** leeway tables: one fixed table accepts many domain payloads via per-canonical-type sections + memberships. The same Go struct can legitimately serialise into more than one target table, so the codec must keep the Go type target-agnostic.

The ergonomic target is `encoding/json`: `Marshal(v)` / `Unmarshal(b, v)` for nested structs, maps, and slices, without the caller constructing Arrow batches.

## Design space (QOC)

**Question.** How should leeway support sending a single entity over an RPC wire as CBOR, with `encoding/json`-grade ergonomics, while preserving leeway's shredding semantics and keeping the Go type target-agnostic?

**Options.**

- **O1** — Reuse Arrow IPC. `dml/runtime` builds a one-row batch; framing is Arrow Stream IPC. *(rejected — per-call overhead dominates the latency budget; not CBOR)*
- **O2** — Tree CBOR (`cbor.Marshal`-equivalent over a Go struct). Receiver re-shreds into the target table. *(rejected — defeats the point of leeway on the wire; receiver work doubles)*
- **O3** — Path-value triples in CBOR (`[[pathLowCard, params, value, sectionTag], ...]`). Row-oriented, flat. *(viable; rejected for O4 — loses section-level grouping; wire size comparable)*
- **O4** — Shredded sections, only non-empty sections appear on the wire, layered on `streamreadaccess.SinkI`. *(chosen)*
- **O5** — A custom binary protocol disjoint from `streamreadaccess`. *(rejected — fragments the protocol surface for no measured gain)*

**Criteria.**

- **C1 — Latency:** single-digit microsecond marshal + unmarshal for small entities.
- **C2 — Leeway fidelity:** does the wire carry section/membership/cardinality structure, or does it collapse them?
- **C3 — Target-agnostic Go types:** can a hand-written Go struct be encoded against more than one target table without modification?
- **C4 — Protocol reuse:** does it compose with the existing `streamreadaccess` ecosystem (Sinks, classifier, future emitters)?
- **C5 — Codegen alignment:** does it fit the existing `dml` / `readaccess` codegen pattern (TableDesc → Go API)?
- **C6 — Wire size:** bytes per call for representative entities.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | ++ | ++ | +  | +  |
| C2 | ++ | −− | +  | ++ | ++ |
| C3 | +  | +  | ++ | ++ | ++ |
| C4 | ++ | −  | +  | ++ | −− |
| C5 | +  | −− | +  | ++ | −  |
| C6 | −  | ++ | +  | +  | +  |

O4 dominates O3 on C2/C4 (section grouping preserved; SinkI reused). O4 trades a small amount of C1 versus O3 for protocol coherence, judged worth it because the SinkI calls map directly onto CBOR map-entry writes without intermediate allocation.

## Decision

We introduce **`public/semistructured/leeway/cbor`** — a CBOR codec for single-entity leeway payloads, reflection-based, scoped tight for v1. The decision has five parts.

### 1. Wire shape: shredded, non-empty sections only

A wire message is a CBOR map. Keys are section names (string); values are CBOR maps from column role to a CBOR array of values. Only sections that the entity actually touches appear in the wire map. Plain values appear under their own well-known map keys.

**Value-column key naming.** Sections with a single value column use the bare key `val`. Sections with multiple value columns (e.g. `u32Range`'s `beginIncl` + `endExcl`) qualify each per-column key as `val:<colname>`. This mirrors leeway's physical-column naming and keeps role visible on the wire.

**Membership wire keys.** Membership-related columns use the `common.ColumnRoleE` strings verbatim: `lv` / `lr` (low-card verbatim/ref), `hv` / `hr` (high-card verbatim/ref), `lmv` / `lmr` (mixed verbatim/ref), `mvhp` / `mrhp` (mixed high-card-parameter components). Cardinality-support columns append `card`: `lvcard`, `lrcard`, `lmvcard`, `valcard`, etc. The encoder picks which role columns to emit from the section's declared `MembershipSpecE`.

**Plain-value keying.** Each `PlainItemTypeE` that appears gets one top-level map entry (`_id`, `_ts`, `_lifecycle`, …) whose value is a CBOR map keyed by plain-value column name. The nesting is uniform — even single-column item types use a one-entry nested map, not a bare scalar. (`runtime.facts` has two `EntityId` columns, so `_id` is `{ "id": …, "naturalKey": … }`.)

Indicative shape, decoded for clarity:

```cbor
{
  "_v": 1,                                 ; codec version (small integer)
  "_id": { "blake3hash": h'…' },           ; plain values, always nested by column
  "_ts": { "ts": 1700000000000000000 },
  "string": {
    "lmv": ["/hostname"],
    "val": ["server-a"],
    "lmvcard": [1],
    "valcard": [1]
  },
  "symbol": {
    "lmv":  ["/tags/_", "/tags/_"],
    "mvhp": ["0", "1"],
    "val":  ["prod", "eu-west"],
    "lmvcard": [1, 1],
    "valcard":  [1, 1]
  },
  "u32Range": {
    "lmv":           ["/validity"],
    "val:beginIncl": [100],
    "val:endExcl":   [200],
    "lmvcard":       [1],
    "valcard":       [1]
  }
}
```

All co-array columns of a non-empty section are emitted unconditionally. v2 may add default-value omission rules if wire size becomes load-bearing.

### 2. Layering: SinkI internally, no new exported interface

Encode walks `*T` and emits calls into an internal `CborSink` that implements `streamreadaccess.SinkI`. Decode parses CBOR bytes and drives an internal `SinkI` implementation that populates `*T`. `SinkI` reuse is a private implementation choice; promotion of the CBOR writer/reader to public API is deferred (see §4).

### 3. Mapping: handle-on-tag + runtime binding

Go field tags carry an **abstract handle**, not a target-specific coordinate:

```go
type FactMsg struct {
    Host    string             `lw:"host"`
    CPU     float64            `lw:"cpu"`
    Active  bool               `lw:"active"`
    Tags    []string           `lw:"tags"`
    Labels  map[string]string  `lw:"labels"`
}
```

At codec construction time, the caller supplies a **binding** that maps handles to concrete leeway coordinates against a specific target `TableDesc`:

```go
codec, err := lwcbor.NewCodec[FactMsg](targetTable, lwcbor.Bindings{
    "host":   {Section: "string",  Path: "/hostname"},
    "cpu":    {Section: "float64", Path: "/metrics/cpu"},
    "active": {Section: "bool",    Path: "/active"},
    "tags":   {Section: "symbol",  Path: "/tags/_",   HighCardParam: lwcbor.ParamArrayIndex},
    "labels": {Section: "string",  Path: "/labels/_", HighCardParam: lwcbor.ParamMapKey},
})
```

The binding entry has a small surface:

- **Slot:** `Section` (string) for tagged values, or `Plain` (`common.PlainItemTypeE`) for plain values — mutually exclusive.
- **Column:** required for plain values (`Column: "naturalKey"`); optional override for multi-value-column tagged sections without struct recursion.
- **Membership:** one of `Path` (verbatim membership, e.g. `"/hostname"`), `MembershipRef` (uint64 ref ID for ref-based sections like every data section in `runtime.facts`), or `Memberships []MembershipBinding` (multi-membership aliasing). The codec picks the column role from the section's declared `MembershipSpecE` and the binding's chosen field — e.g. `Section: "string"` + `MembershipRef: 101` against a `LowCardRef` section emits the `lr` role on the wire.
- **High-card-param strategy:** `ParamArrayIndex` for `[]T`, `ParamMapKey` for `map[K]V`.

`NewCodec[T]` reflects once at construction, resolves bindings against the target `TableDesc`, and caches a section→accessor walker; the hot path never re-reflects.

#### 3.1 Co-arrays via type-driven recursion

Leeway sections can have multiple value columns that move together as co-arrays — `u32Range` (`beginIncl`, `endExcl`) is the canonical example. Since structs in projects using leeway are co-designed with the target table, the codec exploits the Go type system: when a bound field's type is a struct, the codec recurses; the inner field types determine the attribute shape.

**Recursion rule.** For each binding entry, the codec inspects the bound field's Go type:

| Field type | Shape | Wire effect |
|---|---|---|
| Scalar (`string`, `uint32`, `float64`, …) | Single attribute, single value | One value cell |
| `[]Scalar` | Many attributes via param-by-index, single value column | N value cells |
| `map[K]V` (V scalar) | Many attributes via parametrized membership | N value cells |
| `struct { …slices… }` | **SoA**: one attribute group, columns = inner slice fields | Per-column co-arrays |
| `[]struct { …scalars… }` | **AoS**: many attributes, columns = inner scalar fields | Per-column co-arrays |
| `*T` | Optional `T` (nil → absent) | Wraps any of the above |

```go
// SoA: struct of parallel slices
type Validity struct {
    BeginIncl []uint32  // matches column "beginIncl" by name
    EndExcl   []uint32
}

// AoS: slice of per-attribute struct
type Range struct {
    BeginIncl uint32
    EndExcl   uint32
}

type FactMsg struct {
    Validity  Validity `lw:"validity"`
    Histories []Range  `lw:"histories"`
}

codec, _ := lwcbor.NewCodec[FactMsg](target, lwcbor.Bindings{
    "validity":  {Section: "u32Range", Path: "/validity"},
    "histories": {Section: "u32Range", Path: "/history/_", HighCardParam: lwcbor.ParamArrayIndex},
})
```

**Column matching.** Inner field names match section value-column names case-folded. Override per field with `lw:",col=beginIncl"`. Renamed columns across target tables require explicit tag overrides; this is the only frictional residue of "same struct → multiple targets."

**Construction errors.**

- **Mixed inner kinds** — a struct has some scalar fields and some slice fields. Pick AoS (all scalars, wrap in `[]struct`) or SoA (all slices). Error names the offending field.
- **Coverage gap** — recursed struct's fields do not cover the section's value-column set, or vice versa. Error lists the diff.
- **Unknown column** — `lw:",col=..."` names a column the section does not declare.

**Marshal-time error.**

- **Length divergence in SoA** — parallel slices in a co-array struct have unequal length. No wire bytes emitted. (Users can wrap their co-array struct with a typed constructor / `Append` method that maintains the invariant — none of that is the codec's responsibility.)

**Single-value-column sections** (the common case — `bool/value`, `string/value`, `symbol/value`, `float64/value`, …) are reached with a plain scalar or slice-of-scalar field; no struct recursion, no `Column` override.

### 4. v1 scope and deferrals

**In v1:**

- Plain values (all `PlainItemTypeE` variants).
- Tagged-value sections, scalar and homogenous-array values.
- All five membership shapes (low/high card × verbatim/ref × parametrized; plus the two mixed forms).
- Multi-membership aliasing (`membership-card > 1`).
- Maps via high-card parametrized memberships (`map[K]V` → param `K`, value `V`).
- Slices via high-card-by-index (`[]T` → param = array index).
- Multi-value-column sections (`u32Range` etc.) via type-driven recursion into struct types (§3.1) — both SoA (struct of slices) and AoS (slice of struct).
- Generics-typed `Codec[T]`.
- **Optional = Go pointer types.** Nil `*T` absent on the wire; non-nil `*T` and non-pointer `T` always emitted. No `omitempty` — present-zero ≠ absent under leeway's membership semantics.
- **Not goroutine-safe.** Callers wrap with their own mutex or use a per-goroutine codec.

**Deferred to v2:**

- **Codegen tool** (`cmd/lwcborcodegen`) — type-specialised marshal/unmarshal methods, parallel to `dml.GoClassBuilder` / `readaccess.GoClassBuilder`. v1 measures whether the reflection-vs-codegen gap matters before committing.
- **`BindingsFromTableDesc(target, &T{})` auto-binder** — convenience for the case where field canonical type uniquely identifies a target section.
- **Classifier integration** (ADR-0007) — secondary memberships flowing to a catch-all labels field. v1 bindings are authoritative; unknown memberships error.
- **Exported `CborSink` and CBOR-to-`SinkI` parser** — promoted when a concrete consumer asks (Arrow→CBOR, CBOR→Unicode card, etc.).
- **Internal `sync.Pool` concurrency** — goroutine-safe `Codec[T]`; v1 leaves concurrency to the caller.
- **Bench-in-CI gate** for the §5 latency budget.
- **Default-value omission rules** for `lmvcard`/`valcard` co-arrays (§1).
- **Co-sections** — v1 rejects bindings touching co-grouped sections; the non-empty-only wire rule conflicts with aligned attribute counts.
- **Set values** (canonical type with `m` marker) — unused by current targets.
- **Streaming groups** — v1 treats the whole non-empty set as one message.
- **Wire-level encoding aspects** — leeway encoding hints are advisory for storage; the v1 wire ignores them.
- **Schema fingerprint** — peers coordinate schema versions out-of-band.

### 5. Performance intent

**Target:** ≤10µs each (marshal, unmarshal) for ≤32-attribute / ≤4-section entities on commodity x86-64; per-call allocation bounded by section count, not field count.

Achieved by: bindings resolved once at construction; per-codec scratch buffer and `fxamacker/cbor/v2` encoder reused across calls; decode into reusable pre-allocated co-array slices; no `interface{}` on the hot loop.

Benchmarks live in `cbor/bench_test.go` for developer use; no CI gate in v1.

## Alternatives

The full design space is laid out under [§ Design space (QOC)](#design-space-qoc); options and rejection rationale in brief:

- **O1 — Reuse Arrow IPC.** Rejected on C1: a one-row Arrow batch plus IPC framing exceeds the tens-of-microseconds round-trip budget; also not CBOR.
- **O2 — Tree CBOR over a Go struct.** Rejected on C2/C4/C5: the receiver re-shreds into the target table, defeating leeway's shredding on the wire and doubling receiver work.
- **O3 — Path-value triples in CBOR.** Viable but rejected for O4: row-oriented framing loses section-level grouping with no compensating wire-size win.
- **O4 — Shredded sections, only non-empty sections on the wire, layered on `streamreadaccess.SinkI`.** Chosen. Dominates O3 on C2/C4 (section grouping preserved, SinkI reused) and pays only a small C1 cost relative to O3 because SinkI calls map directly to CBOR map-entry writes.
- **O5 — Custom binary protocol disjoint from `streamreadaccess`.** Rejected on C4: fragments the protocol surface with no measured gain.

## Consequences

**Positive.**

- Same Go type encodes against multiple target tables by swapping the binding.
- Co-array handling is structural, not policy: the Go type carries the SoA/AoS shape, the codec recurses, and length invariants live on user-defined wrapper types where they belong. Coverage gaps and mixed-kind errors surface at codec construction.
- v1 surface fits in one sitting: wire format, binding indirection, type-driven recursion — three decisions, not a dozen.
- AoS and SoA fall out of one recursion rule; co-designed Go types make table structure visible at the type level.
- Internal `streamreadaccess.SinkI` reuse keeps the door open for v2 promotion to public emitters without redesign.

**Negative.**

- A third codec surface joins `dml` and `readaccess`; the three must stay coherent as leeway evolves (new aspects, new column roles).
- Handle+binding indirection is one more concept; mitigated by short tags and the v2 auto-binder.
- Reflection on the hot path leaves perf on the table vs. codegen; v1 measures before committing to a codegen tool.
- "Same struct → different targets" with renamed columns requires per-target `lw:",col=..."` tag overrides on inner fields. Friction is real but bounded to the renamed sections.
- Co-section bindings rejected; the spinnaker / runtime.facts annotation overlay pattern (ADR-0007) waits for v2.
- One `Codec[T]` per goroutine; RPC handlers need a pool or per-handler instance until v2.

**Neutral.**

- Wire is not an Arrow IPC subset. Landing CBOR-received entities into an Arrow batch means driving an Arrow-building sink with the CBOR parser (deferred to v2 promotion).

## Open questions

None blocking v1. Items revisited in v2 are listed under §4 *Deferred to v2*.

## Status

Deferred 2026-05-15. The design captured under §1–§5 is the target shape if and when the abstraction is built; until the trigger below is met, the hand-coded `runtime.facts` encoder is the interim path.

**Trigger to un-defer:** a second leeway target table (or a third) where hand-coding a second encoder would visibly duplicate the first. At that point the generic codec is justified by concrete duplication rather than anticipation, and this ADR moves to `proposed`/`accepted`.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`, with `Deferred` as an off-ramp from `Proposed` when implementation is intentionally postponed pending a future trigger. ADRs are append-only; supersession is recorded, not deleted.

## Updates

### 2026-05-15 — Deferred pending a second leeway target table

Status moved from `proposed` to `deferred`. A cost-benefit comparison against plain CBOR and a hand-coded `runtime.facts`-specific encoder/decoder showed the generic codec is over-engineered for a one-table scenario:

- For `runtime.facts` with ~5-20 fact kinds, a hand-coded encoder + shared `FactsBuilder` helper totals ~150-350 LOC, runs sub-microsecond per call with no reflection, and introduces no new abstractions. The generic codec is ~500-1000 LOC of machinery plus per-fact binding boilerplate, with reflection on the hot path costing 5-10x more per call.
- Wire format and storage alignment are identical between the two — both produce the shredded-non-empty CBOR layout that ingests directly into `runtime.facts`.
- Schema-drift risk for the hand-coded path is mitigated by a test that diff-checks `factsschema.GetSchemaInManipulator()` against `FactsBuilder`'s expected wire shape.

The design captured in §1–§5 above remains accurate as the target shape for a generic codec — co-array recursion, handle+binding indirection, SinkI layering, all of it. Trigger to un-defer: a second leeway target table (or a third) where hand-coding a second encoder would visibly duplicate the first. At that point the abstraction is justified by concrete duplication rather than anticipation.

The hand-coded interim path is not yet covered by its own ADR; on the order of ~200 LOC plus tests, it may not need one.

