---
type: adr
status: proposed
date: 2026-08-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0201: leeway canonical record form — deterministic CBOR for hashing, invariant under aspects and numeric width

## Context

Nothing in the repository today can say whether two leeway records carry the
same content. The representations that exist are all *physical*: the Arrow
batch and the ClickHouse row differ when a column is widened (`i32` → `i64`,
`f32` → `f64`, `z32` → `z64`), when an encoding hint, value aspect or section
use-aspect is added, when an attribute moves to the section its new canonical
type lives in (the `boxer.facts` convention names sections after their type, so
a width change *is* a section change), or when a producer emits the same
attributes in another order. Card-JSON
([ADR-0018](./0018-leeway-card-json-canonical-format.md)) is byte-deterministic
but deliberately lossless and isomorphic: it keeps section names, column names,
value shapes, and it reads values back off the driver's *text* lane — so it is
the wrong preimage for a content hash. Its `fingerprint` is a hash of the
*schema*, not of records.

We want one function, `hash(record)`, whose value survives every change that
does not change content:

- **use-aspect, value-aspect and encoding-aspect changes** — on sections,
  columns and membership columns;
- **numeric precision changes on every canonical type** — integer width and
  signedness where the value fits, float width, temporal width, fixed vs.
  variable string width, byte order, and the section move that follows such
  changes;
- **representation order** — attribute order within an entity, membership
  order within an attribute, element order within a set.

The output is never stored. Hashes derived from it may be (as identities, as
change detectors, as dedup keys), which makes the form a durable contract even
though the bytes are transient.

Two recorded deferrals touch this space and are *not* what is proposed here.
[ADR-0010](./0010-leeway-cbor-rpc-codec.md) (deferred) is a lossless shredded
CBOR *wire* for single entities; ADR-0018 open question 5 asks whether a
card-CBOR mirror of card-JSON is worth building. Both are serializations. This
ADR defines a **quotient**: one representative per equivalence class under the
changes above, obtained by mapping a record into the CBOR data model and
letting RFC 8949 §4.2 deterministic encoding pin the bytes. It cannot round-trip
a record and does not try to.

Three facts from the survey shaped the design:

- **CBOR preferred serialization already erases float width.** With the
  fxamacker encoder in `go.mod` (v2.9.2) in `CoreDetEncOptions` mode,
  `float32(0.1)` and `float64(float32(0.1))` both encode as `fa3dcccccd`,
  `1.5` of either width as `f93e00`, and NaN of either width as `f97e00`;
  `float64(0.1)` stays `fb3fb999999999999a` because it is a different number.
  Integers of every Go width encode to the same shortest argument. Measured in
  a scratch spike against the pinned version, not assumed.
- **What it does not erase must be decided here.** The same spike shows `3.0`
  encodes as `f94200`, not `03`; `-0.0` as `f98000`, not `00`; and a nil slice
  as `f6` (null), not `80`. dCBOR (draft-mcnally-deterministic-cbor-18 §2.5,
  an individual submission) specifies the numeric reduction; the container
  rule is ours.
- **The existing read driver's value lane is text.** `streamreadaccess.Driver`
  emits every value as `arrow.Array.ValueStr` — for `Float32` that is
  `strconv.FormatFloat(float64(v), 'g', -1, 32)` — and card-JSON re-parses it
  with `ParseFloat(s, 64)`, which turns the exact `f32` value `0.1f` into the
  *different* `f64` value `0.1`. A form that claims width invariance cannot be
  built on that lane; it needs the typed Arrow value. Membership identities
  are already typed, except that mixed channels arrive as two half-populated
  calls per membership (a known driver issue the `EXPLANATION.md` of
  `streamreadaccess` records).

## Decision

We define **the leeway canonical record form** — a mapping from one leeway
entity into the CBOR data model, encoded under RFC 8949 §4.2 core deterministic
rules plus dCBOR §2.5 numeric reduction — and implement it as a
`streamreadaccess` sink (`canonform.Encoder`) fed by a new *typed* value
capability of the existing driver. The digest is BLAKE3 over those bytes.

### SD1 — The unit is one entity; plains are opt-in by item type

A record is one entity as the driver delivers it (one Arrow row; the only
`TableRowConfig` in existence is multi-attributes-per-row). Its form is a CBOR
map with two integer keys, both always present:

- `0` → plain values: a map `name → value` over the plain item types the
  caller selected (`PlainItemType*` mask; **default: none**). A content hash
  normally wants neither the entity id (circular when the hash *becomes* the
  id), nor timestamps, lifecycle or transaction plains. Item type and name
  style are erased; the nominal name is the key.
- `1` → tagged attributes: an array of attribute items, **sorted bytewise by
  their encoding**, duplicates kept (a multiset).

### SD2 — What the form erases

Section names (two exceptions in SD6), section use-aspects, column value
aspects and encoding hints, membership *spec* and cardinality channel
(low-card vs. high-card is carriage), co-section and streaming groups,
physical column names and roles, `TableRowConfig`, attribute order, membership
order, set element order, integer width and signedness, float width, temporal
width and Arrow unit, byte-order modifier, fixed-width modifier. None of these
is read.

Invariance is under the *declaration* of an aspect, not under a transformation
the aspect implies: a column re-declared NFC-normalized whose stored bytes were
rewritten hashes differently, as it should.

### SD3 — Values: the number, not the width

Every machine-numeric value encodes as its mathematical value, following
RFC 8949 preferred serialization plus dCBOR §2.5:

- integers (`u8`…`u64`, `i8`…`i64`): CBOR major type 0/1, shortest argument;
- floats (`f32`, `f64`, and `f16` should it appear on an Arrow lane): **numeric
  reduction** — a value numerically equal to an integer in `[-2^63, 2^64-1]`
  encodes as that integer (`3.0` ≡ `3`, `-0.0` ≡ `0`); all NaNs encode as
  `f97e00`; ±Inf as float16; everything else as the shortest float that
  preserves the value (float16/32/64). `f32 x` and `f64(x)` are therefore
  byte-identical, and `i32 3` ≡ `u8 3` ≡ `f64 3.0`.
- 128/256-bit integers: **refused** with an error at M0 (no Go lane produces
  them — the Go and Arrow code generators fail on those widths). If they
  arrive later they become bignums (tags 2/3) with the RFC 8949 rule that
  values fitting 64 bits use major type 0/1 — which leaves every existing
  encoding unchanged. Recorded here as the consuming ADR.

Strings: `s` → CBOR text string, bytes verbatim, **no Unicode normalization**
(this is where the form deliberately departs from full dCBOR, whose §2.7
mandates NFC — normalization is a value aspect, not a representation);
`y` → byte string verbatim. The fixed-width modifier (`yx32`, `sx16`) is
erased; bytes are taken as stored, **padding is not stripped** (a
`FixedString` holding a 32-byte hash must equal the same 32 bytes in a `y`
column; text in fixed width is rare here). `b` → `true`/`false`. Bit strings
(`b` with a width) are unimplemented on every lane and refused.

Temporal `z32`/`z64`: RFC 9581 tag 1001 with key `1` = integer Unix seconds
(floor), and key `-9` = integer nanoseconds present only when non-zero. The
Arrow unit (the `z32` lane is milliseconds in Arrow, seconds in ClickHouse)
and the width are erased by integer arithmetic; a whole-second instant encodes
identically from either width. `d` (zoned datetime) and `t` (zoned time) are
unimplemented on every lane and refused.

Network `v`/`w`/`vc`/`wc`: RFC 9164 tags 52 (IPv4) and 54 (IPv6); an address
is the 4/16-byte string, a prefix is `[prefix-length, truncated address bytes]`
per that RFC's encoder rules (unused bits zero, trailing zero bytes omitted).
An IPv6 value in `::ffff:0:0/96` (the IPv4-mapped range, which is what a
`v` → `w` widening produces on ClickHouse) is **reduced to its IPv4 form**, and
a prefix of length ≥ 96 in that range reduces by 96.

### SD4 — Containers: arrays keep order, sets are normalized

`h` (homogenous array) → CBOR array of element forms, order preserved, always
definite-length, **empty array is `80`, never `null`** (the encoder must not
take the library's nil-slice default). `m` (set) → tag 258 ("mathematical
finite set") wrapping an array of the element forms **sorted bytewise and
deduplicated on canonical bytes**. `h` and `m` of equal elements differ; the
modifier is structure, not precision.

### SD5 — Memberships: the identity minus its cardinality

An attribute's memberships form a CBOR array sorted bytewise by element
encoding, duplicates kept. The element is the read-side identity shape
(`membership.IdentityEncoding`, "the channel minus cardinality"):

| Identity | Form |
| --- | --- |
| ref | CBOR unsigned integer |
| verbatim | byte string |
| per-row id + params (mixed ref) | `[uint, bytes]` |
| per-row name + params (mixed verbatim) | `[bytes, bytes]` |
| per-row blob (parametrized) | `[bytes]` |

Major types keep the five shapes distinct without tags. A ref is **not**
resolved to its name: registry ids are timeless by
[ADR-0183](./0183-leeway-component-consumer-simplification.md) D0, no
registry is needed to hash, and a ref → verbatim migration is not a supported
no-op anywhere else either.

### SD6 — Attributes: memberships plus a value, section erased

An attribute item is the 2-element array `[memberships, value]`. The value
shape is the smallest that disambiguates:

- one value column → the bare value form (the column name carries no
  information and is erased);
- several value columns → a map `column name → value form`;
- a co-section group (the driver already merges the group's sections into one
  tagged value per attribute index) → a map keyed by `section:column` handles
  — the [ADR-0116](./0116-play-leeway-column-handle-resolution.md) spelling — because inside a
  group the section is the disambiguator; the section name therefore survives
  erasure only here;
- a value-less section (`null`, `emptyObject`, `emptyArray`, a membership-only
  overlay) → the **section name as a text string**: it is the only content
  such an attribute has, and without it JSON `null` and an empty object would
  collide.

Multi-membership aliasing is **content**, not representation: one value tagged
`[/price/current, /stats/min]` and two attributes each tagged once hash
differently. The alternative is recorded under Alternatives.

### SD7 — Digest

`Digest = BLAKE3-256, keyed` over the canonical bytes, the key derived once
(`blake3.DeriveKey`) from a pinned context string that names the form and its
version; key bytes are declared in source and pinned by a golden. Keyed mode is
BLAKE3's own domain-separation mechanism; it lets a later form version change
every digest without colliding with this one, while the canonical bytes stay a
pure RFC 8949 item a second implementation can reproduce. The bytes are
exposed for tests and debugging; consumers store digests, not bytes.

### SD8 — Implementation seam: a typed capability on the existing driver

The form is implemented once, at the protocol level, as a sink:

- `streamreadaccess` gains an **optional capability interface**
  `TypedValueSinkI` (the `MembershipSinkI` pattern from
  [ADR-0072](./0072-leeway-membership-carriage.md)): typed writes for the
  Arrow scalar families (`uint8`…`uint64`, `int8`…`int64`, `float16/32/64`,
  bool, utf8, binary, fixed-size binary, timestamp with unit). The driver
  type-asserts once; sinks that implement it receive typed values in place of
  the `WriteString` lane, everything else is unchanged. The four existing
  sinks keep the text lane.
- The driver's **mixed-channel membership emission is fixed** to one paired
  call per membership (`(ref, params)` / `(name, params)`), closing the
  recorded known issue. This is a behavioural change for the four
  `MembershipSinkI` implementers, which already declare the paired signature;
  no committed golden contains a mixed membership.
- `public/semistructured/leeway/canonform` — `Encoder` implements `SinkI`,
  `MembershipSinkI`, `TypedValueSinkI`; buffers one entity, encodes each
  attribute into a scratch buffer, sorts, concatenates, digests. CBOR heads are
  written directly (integers, strings, arrays, maps, tags); floats go through
  the fxamacker encoder in `CoreDetEncOptions` mode after numeric reduction,
  so the shortest-float logic stays in a library the repo already pins.
- Input is whatever the driver reads: any Arrow batch carrying leeway's
  self-describing column names — a generated DML's output, a ClickHouse
  Arrow result, a `marshallreflect` one-row batch.

### Milestones

- **M0 — form and encoder.** `TypedValueSinkI` + driver typed emission; mixed
  pairing fix; `canonform.Encoder`; goldens (hex bytes and digests) over the
  `anchor` fixtures and a `boxer.facts` sample; SD3's refusals.
- **M1 — the invariance suite.** `pgregory.net/rapid` properties: random
  entities, then width widening (`u8`→`u64`, `i32`→`i64`, `f32`→`f64`,
  `z32`→`z64`, `y`→`yx`), aspect toggles, section renames and moves, attribute
  / membership / set permutations, channel cardinality flips — equal bytes;
  value edits, aliasing vs. separate attributes, `h` vs. `m` — distinct bytes.
  Plus the self-check: decode every emitted item with fxamacker in strict mode
  and re-encode under `CoreDetEncOptions`; bytes must match.
- **M2 — first consumer.** Not chosen here (see Status). The candidates seen in
  the survey are content-addressed ids and dedup on a facts-shaped table; the
  plains mask (SD1) exists so that either can be expressed without a second
  form.
- **M3 — deferred.** A DTO-direct producer path (no Arrow in the loop) with a
  byte-parity test against the driver path; a SQL-side reimplementation as a
  ClickHouse UDF (no native CBOR there). Both only when a consumer pays for
  them.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `streamreadaccess.SinkI` family (exported Go API under `public/`) | added: optional `TypedValueSinkI` capability; driver emits typed values to sinks that implement it | none — additive; text lane unchanged for existing sinks |
| `streamreadaccess.Driver` mixed-channel membership emission | reshaped: two half-populated calls → one paired call per membership | the four `MembershipSinkI` implementers (card-JSON, Unicode card, `DebugSink`, `Table2CardEmitter`) receive pairs they already declare; their tests |
| `public/semistructured/leeway/canonform` (new exported Go API) | added: `Encoder`, `Options` (plain-item mask), `Digest`, context string and derived key | goldens under `testdata/`; the M1 property suite |
| The canonical record form itself (a hash preimage contract) | added: SD1–SD7 are the contract; any byte change is a new version under SD7 | whatever stores digests — none at M0 |

## Alternatives

- **Hash card-JSON bytes (ADR-0018).** Deterministic, but lossless by design:
  section names, column names, value shapes and the text lane's float
  rendering are all in the bytes, so none of the required invariances hold.
- **Hash the ADR-0010 wire.** Same objection, and that codec is deferred.
- **JCS canonical JSON (RFC 8785).** JSON cannot carry bytes, sets, 64-bit
  integers, or non-finite floats without application conventions; the
  conventions would *be* the form, with JSON's parsing cost on top. CBOR has
  the types natively and a standard deterministic encoding.
- **Hash Arrow IPC bytes.** Physical layout is the thing to erase, not hash.
- **A hand-rolled binary layout.** Every decision in SD3–SD6 would still have
  to be made, without a standard a second implementation can check against.
- **Full dCBOR conformance.** Its NFC mandate (§2.7) is a value transformation;
  applying it would make NFC-equivalent but byte-different strings collide and
  cost a normalization pass per string. The numeric model is taken; the string
  rule is not.
- **Flatten aliasing** (one value tagged twice ≡ two attributes). Insensitive
  to how a producer groups tags, but it erases a graph edge the protocol
  states explicitly; under the "content, not representation" test aliasing is
  content.
- **RFC 8949 core deterministic without numeric reduction** (`3` ≠ `3.0`).
  Simpler, but an `int` → `float` column change is a precision widening in
  every practical sense, and JCS makes the same call for JSON.

## Consequences

### Positive

- One bytes-level contract for "same content", specified against public
  standards, so a second implementation can be checked byte-for-byte.
- Width, aspect and section-placement changes stop being identity changes.
- The driver gains a typed lane that any future sink can use; the mixed
  membership known issue closes.

### Negative

- The form is lossy on purpose: it cannot be decoded back into a record, and
  it is one more leeway concept to keep consistent with the protocol.
- Co-section groups keep section names (SD6) and the driver emits only the
  first section's memberships for a group — a pre-existing driver limitation
  the form inherits; no committed schema declares a membership-bearing
  secondary co-section today. Recorded, not fixed here.
- Per-entity buffering and sorting cost memory proportional to entity size
  (the ADR-0018 SD10 trade-off).
- `u128`/`u256`, `f16` on non-Arrow lanes, `d`/`t`, bit strings: refused at
  M0.

### Neutral

- The plains mask is a domain selection, not a form knob; the form of selected
  content is unique.
- Nothing is stored; the blast radius of a version bump is whatever has
  stored digests, which at M0 is nothing.

## Migration — Tier 1

- **Breaks.** Nothing compiles, parses or round-trips differently. The only
  behavioural change is the paired mixed-membership call (SD8); sinks that
  ignored the empty half of the split calls behave the same.
- **Path.** None for existing code. A consumer that later stores digests pins
  the form version it stored under.
- **Regeneration.** None; no generated code changes.
- **Old shape.** The text value lane stays indefinitely; it is what card and
  widget sinks render.

## Verification plan — Tier 1

- **Lane.** Default `go test`: goldens of canonical bytes and digests for the
  `anchor` fixtures and a facts sample (M0); the `rapid` invariance /
  distinctness properties and the strict-decode → `CoreDetEncOptions`
  re-encode pin (M1).
- **What would fail.** A byte change in any golden (the form drifted), an
  invariance property (a representation leaked into the bytes), a
  distinctness property (content collapsed), the re-encode pin (the output
  stopped being RFC 8949 §4.2 deterministic).
- **Gap.** No SQL-side implementation exists to cross-check against, and the
  co-group membership gap is documented rather than tested.

## Status

Proposed — awaiting review by the leeway code owner. Decisions that want a
human call before M0 starts, each with the default the text above takes:

1. **Numeric reduction** (SD3): `3.0` ≡ `3` and `-0.0` ≡ `0` — default yes.
2. **Aliasing is content** (SD6): one value with two tags ≠ two attributes —
   default yes.
3. **IPv4-mapped IPv6 reduces to IPv4** (SD3) — default yes.
4. **Fixed-width padding is kept** (SD3) — default yes (keep).
5. **Refs are not resolved to names** (SD5) — default yes (not resolved).
6. **Plains excluded by default; keyed BLAKE3 for domain separation** (SD1,
   SD7) — default yes on both.
7. **Value-less attributes carry the section name** (SD6) — default yes.
8. **First consumer** (M2): not decided here.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0018](./0018-leeway-card-json-canonical-format.md) — card-JSON: the
  lossless canonical serialization; its schema fingerprint; open question 5.
- [ADR-0010](./0010-leeway-cbor-rpc-codec.md) (deferred) — the shredded CBOR
  wire this is not.
- [ADR-0060](./0060-leeway-data-contracts-odcs.md) SD4 — card-JSON remains the
  lossless JSON form; this ADR does not change that.
- [ADR-0072](./0072-leeway-membership-carriage.md) — the optional sink
  capability pattern and the identity vocabulary SD5 reuses.
- [ADR-0116](./0116-play-leeway-column-handle-resolution.md) — the `section:column` handle
  spelling SD6 borrows for co-section groups.
- [ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md) — the three
  aspect vocabularies the form ignores.
- [ADR-0183](./0183-leeway-component-consumer-simplification.md) D0 — timeless
  membership ids.
- RFC 8949 §4.2 (core deterministic encoding), RFC 9164 (tags 52/54),
  RFC 9581 (tag 1001), IANA CBOR tag 258 (mathematical finite set),
  draft-mcnally-deterministic-cbor-18 §2.5 (numeric reduction; individual
  submission, not a working-group document).
- `public/semistructured/leeway/streamreadaccess/EXPLANATION.md` — the
  driver's known issues, including the mixed-channel emission SD8 fixes.
