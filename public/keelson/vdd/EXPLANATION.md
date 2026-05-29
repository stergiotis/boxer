---
type: explanation
audience: keelson runtime contributor authoring DTOs or implementing the codec generator
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# `keelson/vdd` — Schema model for Go ↔ leeway codecs

`vdd` is the **schema source of truth** for keelson-side fact kinds.
Every leeway membership a keelson DTO references must be registered
here as a `Memb*` constant with a stable `TaggedId`. This document
explains the **interpretation contract** that consumers — DTO authors,
the codec generator, ClickHouse query writers — agree on when reading a
vdd declaration: how cardinality and sub-type combine to dictate both
the wire encoding and the Go-side type the DTO uses.

The decision to generate codecs against this model lives in
[ADR-0042](../../../../../doc/adr/0042-keelson-leeway-codec-soa-generator.md);
the parallel-array shredding the wire format inherits from sits in
[ADR-0041](../../../../../doc/adr/0041-rowmarshall-error-shredding.md).

## Background

A leeway "membership" is a typed-value tag attached to an entity. The
`runtime.facts` table — keelson's primitive observability and audit
table — stores one row per entity, with columns shredded by leeway's
canonical type vocabulary (`string`, `symbol`, `text`, `blob`,
`u8`…`u64`, `i8`…`i64`, `f32`, `f64`, `bool`, `z64` for DateTime64(9) nanos).

vdd registers each membership with:

- a **natural-key name** (`MembGitHash`, `MembPeerIPv6`, …) that is
  unique within the registry's tag-value namespace,
- a **parent hierarchy** (virtual umbrellas + concrete leaves, so
  `MembGitHash` inherits from `MembHashSha1` from `MembHash`),
- a **restriction** that pins three things at registration time:
  - the **section name** the value lives in (matches a leeway canonical
    type, e.g. `"symbol"` or `"u32"`),
  - the **membership spec** that says how the membership *identity* is
    encoded on the wire (`LowCardRef` / `HighCardRef` / parametrized /
    mixed — see `boxer/public/semistructured/leeway/common`),
  - the **cardinality** — how many times the membership-value-cell
    appears for a single entity.

There is a fourth axis the registry tracks implicitly via the
`MembColumnSubType*` umbrella in `keelson_dimdata_lw.go` (HomogenousArray
/ Set / Membership): whether the value-cell itself carries one item or
a non-scalar container. **Cardinality and sub-type are orthogonal**,
and the combination determines both the wire encoding and the Go-side
type a DTO must use to bind to that membership.

## The two-axis schema model

### Cardinality — how many value-cells per entity

| Value | Meaning | ClickHouse `indexOf` acceleration |
|---|---|---|
| `ExactlyOne` | Always one occurrence | Yes — single position, always present |
| `ZeroOrOne`  | Zero or one occurrences | Yes — `indexOf(lr, kind) > 0` distinguishes presence; position is fixed |
| `OneOrMore`  | At least one, possibly many | No — must walk `val` window via `lrcard` prefix-sum |
| `Arbitrary`  | Zero or more, no upper bound | No — same |

Cardinality drives the **`lr` / `lrcard` shape** on the wire (see ADR-0041):
`lr` lists distinct kinds present for the row; `lrcard` records each
kind's count. For `ExactlyOne` and `ZeroOrOne`, the count is implicitly
0 or 1 — the kind's presence in `lr` is what matters, and `indexOf` is
index-accelerated. For `OneOrMore` and `Arbitrary`, retrieval requires
slicing `val` by `lrcard` prefix-sums, which ClickHouse can do but
cannot index-accelerate.

### Sub-type — what a single value-cell carries

| Value | Meaning | Wire support column |
|---|---|---|
| `Membership` (scalar)        | The value-cell is one atomic value | — (only `lrcard` for count) |
| `HomogenousArray`            | The value-cell is an ordered, non-empty array of one type | `card` (the array's length) |
| `Set`                        | The value-cell is an unordered, deduplicated, non-empty set | `card` (the set's size) |

The key invariant for non-scalar sub-types: **empty arrays and sets are
not representable on the wire**. A present `HomogenousArray` membership
always carries `card ≥ 1`; absence means the membership does not appear
for this row at all. This pairs naturally with Go: `len(nil) == 0` for
slices, and a nil/empty container at the DTO surface emits "no
membership for this row" — symmetric on both sides.

## DTO grammar

A keelson DTO is a flat Go struct whose fields fall into exactly these
four shapes:

| DTO shape | What it binds to |
|---|---|
| `T`              | A scalar value, always present (`Membership` + `ExactlyOne`) |
| `Option[T]`      | A scalar value, present or absent (`Membership` + `ZeroOrOne`) |
| `[]T`            | Either a scalar membership repeated N times *or* a single non-scalar value with N items — **vdd disambiguates**, see the mapping table |
| `*roaring.Bitmap` / `*roaring.Bitmap64` | A `HomogenousArray` / `Set` value over `u32` / `u64`, wire-equivalent to `[]uint32` / `[]uint64` |

`T` ranges over `string`, `uint{8,16,32,64}`, `float{32,64}`, `bool`,
`time.Time`, `[4]byte` (IPv4), `[16]byte` (IPv6). No pointers, no
nested structs, no anonymous structs, no slices of structs, no
`[]Option[T]`, no `Option[[]T]`. `Option[T]` is the only allowed
generic-typed wrapper — it is a typed total carrier (the type system
forces `Has` checking before reading `Val`), not pointer-style
nullability.

The DTO carries no metadata of its own; entity-level wiring (fact-kind
name, plain-column mapping, table override) goes on a `_ struct{}`
field with struct tags. See "The `_ struct{}` entity-level idiom"
below.

## Mapping vdd declaration ↔ DTO field shape

| vdd sub-type | vdd cardinality | DTO field shape | "Absent" spelling | Wire encoding |
|---|---|---|---|---|
| `Membership` (scalar) | `ExactlyOne` | `T` | — | scalar slot, index-accelerated |
| `Membership` (scalar) | `ZeroOrOne`  | `Option[T]` | `Has = false` | scalar slot under `lr`-presence, index-accelerated |
| `Membership` (scalar) | `OneOrMore`  | `[]T` + codegen `len ≥ 1` | — | `val ‖ lr ‖ lrcard` shredded, no `indexOf` acceleration |
| `Membership` (scalar) | `Arbitrary`  | `[]T` | `len == 0` | same as above |
| `HomogenousArray` / `Set` | `ExactlyOne` | `[]T` *or* `*roaring.Bitmap{,64}` + codegen `len ≥ 1` | — | `val ‖ lr ‖ card`, value is the array payload |
| `HomogenousArray` / `Set` | `ZeroOrOne` | `[]T` *or* `*roaring.Bitmap{,64}` | `len == 0` (idiomatic Go nil-slice; `bitmap.IsEmpty()`) | same as above; absent ⇒ kind omitted from `lr` |

Two consequences of this table the codegen relies on:

- **`[]T` is intentionally polysemic.** The same DTO syntax binds to two
  different wire encodings — multi-membership shredding versus a single
  non-scalar value cell — and vdd is what picks. Codegen reads the
  membership's sub-type at generation time and emits the right
  emission/parse code for the field. The DTO author writes `[]T`
  either way.
- **`Option[T]` is reserved for `Membership` + `ZeroOrOne`.** A nil/empty
  slice carries presence implicitly for non-scalar sub-types; only
  scalar `ZeroOrOne` needs an external presence marker because a
  scalar `T` has no "length" to overload.

## Wire encodings (recap)

Three shapes appear on the wire, all driven by vdd:

- **Scalar slot.** A single physical column entry per row. Used by
  `Membership` + `ExactlyOne` / `ZeroOrOne` (the `lr` array is the
  presence signal for `ZeroOrOne`).
- **Multi-membership `val ‖ lr ‖ lrcard`.** Ragged: `val` carries all
  values across all kinds in this section for this row, `lr` lists the
  distinct kinds present, `lrcard` counts each kind's occurrences.
  Used by `Membership` + `OneOrMore` / `Arbitrary`. Reconstructed via
  `lrcard` prefix-sums; no `indexOf` acceleration.
- **Non-scalar `val ‖ lr ‖ card`.** The value-cell is a homogeneous
  array or set; `val` carries the cell's items, `card` carries the
  cell's length. Used by `HomogenousArray` / `Set`. Each row has at
  most one cell per membership (cardinality is ≤1 *of* the cell, not
  of items within it).

ADR-0041 records the per-section grouping rule (`val` is grouped by
kind in emission order, `lr` is the distinct-kinds list in that same
order).

## The empty-as-absent idiom

Leeway forbids empty arrays/sets. Go expresses "nothing here" as
`nil` (or a zero-length slice) for slices, and as `bitmap.IsEmpty()`
for roaring. The codegen wires these up symmetrically:

- A `[]T` field at marshal time with `len == 0` emits "no membership"
  for that row — the kind is omitted from `lr`, the value-cell is not
  written. At unmarshal time, the absence of the kind in `lr`
  populates the DTO field with `nil`.
- A `*roaring.Bitmap` field at marshal time with either a `nil`
  pointer *or* a non-nil bitmap whose `IsEmpty()` returns true is
  treated as absent. At unmarshal time, an unread/absent kind
  populates the field with `nil`.

This means **the DTO author never has to write `if value == nil`
checks** — building up a column by appending values is the same code
path whether the membership ends up present or absent. The result
disappears into the wire if the column ends empty.

The only place this asymmetry leaks is `Option[T]`: a scalar has no
"length" to overload, so `ZeroOrOne` scalar fields *must* use the
explicit `Option[T]` wrapper. The codegen decomposes `Option[T]` in
the SoA storage form as `Values []T` paired with a
`Validity *roaring.Bitmap` (bit *i* set ⇔ row *i*'s `Option` is
`Some`) — same wire codec as the homogeneous-array roaring path, but
encoding a typed presence concept.

## Roaring bitmaps as homogeneous-array values

`*roaring.Bitmap` (the 32-bit variant) and `*roaring.Bitmap64` are
first-class DTO field types for `HomogenousArray` / `Set`
memberships whose inner type is `u32` or `u64`. The contract:

- **Wire codec is identical to `[]uint32` / `[]uint64`.** The codegen
  emits the bitmap's payload via roaring's portable serialization
  format (or `ToArray()` followed by the standard array codec — the
  RowBinary length-prefixed u32/u64 sequence — whichever is cheaper at
  the call site).
- **Empty ⇒ absent.** `nil` and `IsEmpty()` both map to "no
  membership"; a non-empty bitmap maps to a present `HomogenousArray` /
  `Set` membership with `card = bitmap.GetCardinality()`.
- **ClickHouse query interop.** CH's `AggregateFunction(groupBitmap,
  UInt{32,64})` is roaring-compatible on storage, so SQL queries
  against these fields can use the native bitmap functions without
  UDF gymnastics.

The choice between `[]uint32` and `*roaring.Bitmap` for a u32-section
membership is **the DTO author's** — there is no vdd-side distinction.
Pick `*roaring.Bitmap` when the application logic already works in
bitmap operations (intersection, union, cardinality estimation); pick
`[]uint32` when the values are produced/consumed as ordinary slices.
Both round-trip identically through the wire.

## The `_ struct{}` entity-level idiom

Fact-kind-level metadata (the kind name, which fields fill the plain
columns, table-name overrides, future schema-version markers) lives on
a Go blank field at the head of the struct:

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

Why a blank field instead of a comment marker or sidecar file:

- **Go-idiomatic.** `database/sql` and other established packages
  attach metadata to `_` fields via struct tags; Go's reflection
  surface walks them like any other.
- **Co-located with the struct.** No separate config file to keep in
  sync; renaming the struct moves its metadata with it.
- **Extensible without grammar churn.** A future plain-column wiring
  tag, a schema-version constant, or a vdd-registry override slots in
  as another struct tag without touching the codegen entry point.

The reserved tags on the `_` field (v1):

- `kind:"<name>"` — the fact-kind identifier; becomes the `<Kind>Columns`
  type name and a constant in the generated package.
- `plain:"<col>=<Field>,<col>=<Field>,…"` — wires plain identity columns
  (`id`, `ts`, `naturalKey`, `expiresAt`) to specific DTO fields.

Per-field tags stay on regular fields: `lw:"<membership>,<section>"`
where `<membership>` is a vdd-registered natural-key name. Section
overrides like `"symbol"` are optional when the vdd declaration
unambiguously chooses one.

## Codegen-time consistency checks

The generator validates four invariants before emitting any `.out.go`:

1. **Membership exists in vdd.** Every `lw:"<membership>,…"` tag's
   first component resolves in `vdd.KeelsonHrNkRegistry`. Unknown
   memberships abort with a "register `Memb<Name>` in `keelson/vdd`
   before referencing it" error.
2. **One membership per field within a DTO.** No two fields in a
   single fact-kind reference the same membership. This is the
   "single membership identifies the field uniquely" rule that makes
   the inverse (membership → DTO field) deterministic at unmarshal.
3. **Field shape matches vdd's cardinality × sub-type.** The DTO
   field's Go type lies in the cell of the mapping table matching the
   membership's declared (sub-type, cardinality). Mismatch (e.g.
   declaring `T` in the DTO when vdd says `OneOrMore`) aborts codegen.
4. **No forbidden shapes.** Nested structs, anonymous structs,
   `[]Option[T]`, `Option[[]T]`, `*T`, and any non-listed primitive
   abort codegen with a pointer to this document's "DTO grammar"
   section.

All four checks happen *before* code emission, so a broken DTO never
produces a broken generated file.

## Invariants

- **vdd is the unique schema authority.** A membership name appearing
  in any DTO's `lw:` tag must resolve in `vdd.KeelsonHrNkRegistry`;
  there is no fallback registry, no "untyped" membership, no implicit
  registration from a DTO declaration.
- **`TaggedId`s are stable.** Once a `Memb*` is registered with a
  given hierarchy position, its `TaggedId` does not change between
  rebuilds; persisted rows referencing it stay valid.
- **Empty containers are never on the wire.** `card ≥ 1` for every
  present `HomogenousArray` / `Set` cell; `lrcard[i] ≥ 1` for every
  kind listed in `lr`. The codegen enforces this on emit and treats
  any decoded `card == 0` as a protocol violation.
- **The DTO type, the vdd declaration, and the wire encoding agree.**
  If they disagree, codegen fails — production code never sees a
  mismatch.

## Trade-offs

- **`[]T` is intentionally ambiguous in Go syntax.** Reading a DTO file
  alone does not tell you whether `[]T` is multi-membership or a
  homogeneous-array value cell. The trade-off is real but principled:
  Go has one slice type, leeway has two semantic shapes, and vdd is
  the disambiguator. The alternative — introducing wrapper types like
  `ArrayCell[T]` versus `MultiMembership[T]` — would put schema
  information in two places and risk drift.
- **No row-level optionality for non-scalar values.** A `Set + ZeroOrOne`
  membership represents "this entity may have no preferences" by
  emitting nothing; it cannot represent "this entity has an explicit
  empty preference set". If that distinction matters, the schema
  needs to lift it to a separate scalar boolean membership ("has
  configured preferences") alongside the set.
- **`Option[T]` and roaring decompose differently in SoA storage.**
  `Option[T]` lives as `Values []T` + `Validity *roaring.Bitmap` in
  the codegen-emitted `Columns` struct; a roaring field lives as a
  single `*roaring.Bitmap` per column slot. The asymmetry follows from
  the cardinality difference (per-row presence vs per-row payload)
  but is a thing future maintainers must keep straight.

## Caller contracts

The codec is correct on the wire when callers respect three contracts
the generator does not (and cannot cheaply) enforce on the hot path.
Each one is documented inside the generated `.out.go` at the relevant
emission site so the contract is visible at the point of use.

- **`time.Time` → `uint32` epoch range.** Plain `ts`/`expiresAt`
  columns and `time` section values are encoded as `UInt32` seconds-
  since-epoch. Valid range is `[1970-01-01, 2106-02-07]`; values
  outside that range silently wrap. Callers are responsible for
  rejecting / clamping out-of-range timestamps upstream. A future
  milestone may add a debug-build runtime guard, but the production
  path stays unguarded so the cost is paid only by callers who need
  it.
- **`Append` aliasing.** The generated `Append(row T)` stores slice
  and pointer fields (`[]T`, `*roaring.Bitmap`) **by reference**, not
  by copy. Callers must not mutate `row.<F>` after `Append` unless
  they intend `Marshal` to observe the mutation. Scalar fields (`T`,
  `Option[T]`) are copied by value and unaffected. The trade-off:
  copying every slice/bitmap on Append would defeat the SoA fast
  path; aliasing is the deliberate default.
- **Section emission order = DTO declaration order.** The generated
  `appendRow` walks sections in the order their first member appears
  in the DTO struct. `lr`'s "distinct kinds in emission order"
  contract (ADR-0041) is what readers depend on; reordering DTO
  fields changes the wire layout. Within a section, members
  contribute to `val`/`lr`/`lrcard` in DTO declaration order as well.
  Changing this would be a wire-format break — handle as an ADR Tier-
  3 supersession if ever needed.

## Open questions

The model above settles the M1+M2+M3 cases. Items still genuinely
unresolved:

- **`Set` with non-`u32`/`u64` inner types.** Roaring covers the
  numeric sets; a `Set + string` membership emitted as `[]string`
  needs a contract on whether the codegen sorts + dedupes the input at
  marshal time, or whether the DTO author is responsible for handing
  in a deduped slice. Deferred to the milestone that first introduces
  a non-numeric Set membership.
- **`Unmarshal(arrow.Record)`.** Promised in the ADR-0042 Decision
  surface but not delivered by M3. The chlocal external round-trip
  proves the wire shape today; an in-process Arrow reader is its own
  design pass (column dispatch, Option/slice/roaring reconstruction,
  schema-version handling). Tracked as a follow-up milestone; see
  ADR-0042 §Updates "Unmarshal deferred".

Resolved during M2/M3 (recorded here for documentary continuity):

- **Wire form of `Membership` + `OneOrMore`.** Settled as the
  multi-membership `val ‖ lr ‖ lrcard` form. Codegen for `OneOrMore`
  itself is still deferred (Arbitrary covers the common case); when
  it lands it will add a runtime `len ≥ 1` assert on the slice.
- **`HomogenousArray` sub-type vs Arbitrary multi-membership on the
  wire.** Empirically wire-equivalent on the current `runtime.facts`
  schema — both produce `val ‖ lr ‖ lrcard` where `lrcard[i]` is the
  number of values contributed by the kind. The vdd sub-type
  distinction is preserved in the schema for query-time semantics but
  does not change the wire shape; codegen treats them identically
  today and the parser does not yet require the sub-type virtual
  parent.

## Further reading

- [ADR-0042 — Generated SoA codec for keelson runtime.facts rows](../../../../../doc/adr/0042-keelson-leeway-codec-soa-generator.md)
- [ADR-0041 — rowmarshall: boxer error fully-shredded row layout](../../../../../doc/adr/0041-rowmarshall-error-shredding.md)
- [ADR-0026 — app runtime + capability subjects](../../../../../doc/adr/0026-app-runtime-and-capability-subjects.md)
- [ADR-0035 — keelson namespace introduction](../../../../../doc/adr/0035-keelson-namespace-introduction.md)
- [`keelson_dimdata.go`](keelson_dimdata.go) — bootstrap memberships and the registry instances.
- [`keelson_dimdata_lw.go`](keelson_dimdata_lw.go) — leeway-meta memberships and the `Resolve*` bridges.
- [`src/go/public/boxerstaging/spinnaker/vdd/`](../../boxerstaging/spinnaker/vdd/) — prior-art registry the keelson layout mirrors.
- [`src/go/public/keelson/runtime/codec/`](../runtime/codec/) — the ADR-0042 codec generator output (one subdirectory per fact-kind / broker DTO). Subsumed the hand-coded `rowmarshall/` reference codec in M11; the no-reflection-on-hot-path rule this vdd model inherits is now realized through the generator emit rather than hand-written.
- [`src/go/public/keelson/runtime/factsschema/`](../runtime/factsschema/) — the DML/RA/RowBinaryArrow building blocks the codec generator targets.
