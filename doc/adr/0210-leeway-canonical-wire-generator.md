---
type: adr
status: accepted
date: 2026-08-28
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-28
---

# ADR-0210: leeway canonical wire — a lossless, type-signature-keyed serialization with generated per-table codecs

## Context

Leeway data lives in three places today: the generated DML builders (`dml`),
the Arrow batch they produce, and the generated read accessors (`readaccess`)
over that batch. Moving one entity between two processes, or persisting it
outside Arrow, has no lossless form:

- [ADR-0201](./0201-leeway-canonical-record-form.md) (`canonform`) is a
  **quotient**, built for hashing. It erases numeric width, attribute and
  membership order, secondary memberships and aliasing by design, and states
  that it "cannot round-trip a record and does not try to".
- [ADR-0018](./0018-leeway-card-json-canonical-format.md) (card-JSON) is
  lossless but section-centric: section and column names are in every record,
  and values travel on the driver's text lane.
- [ADR-0010](./0010-leeway-cbor-rpc-codec.md) (deferred) is a lossless
  shredded CBOR wire, reflection-bound, whose map keys are **section names**.

The requirement that none of these meet: the serialization must be keyed by
**canonical type signatures only**. A record written from one table
description must decode into another that declares the same *types* under
different section names, column names, aspects or hints — the table
description is a property of the endpoint, not of the data. Two consequences
follow and are the substance of this ADR:

- A type signature does not always identify a section. The JSON mapping table
  in the `leeway-advanced` skill has `string` and `symbol` (both `s`) and
  `null` / `undefined` / `emptyObject` / `emptyArray` (all value-less). Which
  section an incoming attribute belongs to must then be decided by a
  **pluggable dispatch**, invoked only when the signature is ambiguous in the
  *target* table.
- The codec must be lossless in every dimension `canonform` erases: widths,
  order, secondary memberships, aliasing, `h` vs `m`, NaN payloads, `-0.0`.

Two further forces:

- The `dml` and `readaccess` generators already emit typed, reflection-free
  per-table Go code (the ADR-0041 posture: no reflection on the hot path).
  The codec is the third such generator, not a reflective runtime.
- The `canonicaltypes` grammar already has the unit this needs: a *group*
  (`f32-f32-u64-u64`, columns joined by `-`) is exactly a section's value
  columns, and a *signature* (`f32-f32_u64`, groups joined by `_`) is exactly a
  co-section group. The slot key can be the signature string itself, produced
  and parsed by code the repository already has.

## Design space (QOC)

**Question.** What identifies an attribute's slot on the wire, given that the
table description must not?

**Options.**

- **O1** — section name (the ADR-0010 shape). *Rejected by the requirement.*
- **O2** — the section's value-column CT group, columns in declaration order;
  a co-section group is the CT signature of its sections in declaration order.
- **O3** — as O2, but canonicalised: columns sorted by CT string (stable) within
  a group, groups sorted bytewise within a signature.
- **O4** — O3 plus the section's accepted membership spec (`MembershipSpecE`)
  folded into the key.

**Criteria.**

- **C1 — portability.** Does a record decode into a target table whose
  declaration order differs from the source's?
- **C2 — ambiguity rate.** How often does the key fail to identify a single
  target section, forcing dispatch?
- **C3 — losslessness.** Is anything about the *data* erased by the key?
- **C4 — self-description.** Can tooling read the key without the table?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | −  | ++ | +  |
| C2 | ++ | +  | +  | ++ |
| C3 | ++ | ++ | +  | ++ |
| C4 | −  | ++ | ++ | +  |

O3 is chosen. It gives up one thing against O2 (C3): two columns of equal CT
inside a group — `lat`/`lng` — are told apart only by declaration order, which
stable sorting preserves; two columns of *different* CT lose their declared
order, which carries no information the types do not. O4's lower ambiguity
comes from putting carriage (how a membership is stored) into a key meant to
carry content; the same narrowing is obtained at decode time without
polluting the key (SD5).

## Decision

We define **the leeway canonical wire** — a lossless, deterministic CBOR
encoding of one entity whose tagged-attribute slots are keyed by canonical
type signature — and add a third leeway code generator, `canonwire`, that
emits per-table encoder and decoder classes bridging the existing `readaccess`
and `dml` generated APIs, with a pluggable dispatch pair for the slots the
signature does not resolve.

### SD1 — The unit is one entity; a stream is a CBOR sequence

One entity encodes as one CBOR item under RFC 8949 §4.2 core deterministic
encoding: shortest integer arguments, shortest *value-preserving* float,
definite lengths, map keys sorted bytewise. No dCBOR numeric reduction —
`3.0` stays a float, `-0.0` stays `-0.0` — because the slot type fixes how a
value is read back and the reduction would be lossy for exactly those values.
**All NaNs are one NaN** (`f97e00`); the payload is the one float bit pattern
the form does not keep (decided 2026-08-28: the shared writer's
core-deterministic mode folds it, and no lane in this repository produces a
NaN payload on purpose). A stream of entities is a CBOR sequence (RFC 8742); no
batch framing, no header. The entity item is a 3-element array:

```
[ version:uint, plains:map, tagged:map ]
```

The version is a single small integer declared in source; any change to
SD2–SD6 bumps it. A decoder refuses a version it does not implement.

### SD2 — Slot keys are CT signatures, computed from the table

For every tagged section the generator computes its **group**: the value
columns' canonical types, stable-sorted by CT string, joined by `-`; a
value-less section has the empty group. For every co-section group the
generator computes its **signature**: the member sections' groups sorted
bytewise, joined by `_`; a standalone section's signature is its group. The
`tagged` map is keyed by these signature strings (CBOR text); a key is present
only when the entity has at least one attribute in that slot. The empty
signature `""` is a valid key (value-less sections); a co-section group of two
value-less sections is `"_"` — the separator says "two groups" and is not
the same slot as one value-less section.

The slot table — signatures, the sections behind each, the ambiguity sets and
the per-section accepted-channel masks — is computed **at generation time**
from the `TableDesc`, in the generator package; the wire runtime carries only
the string helpers, and generated code bakes what it needs as constants. The
runtime therefore never sees a table description. The wire does not carry a
plain group; a decoder checks the column count per item type and the typed
reads catch a width mismatch value by value — there is no construction-time
comparison, because construction has no wire to compare against.

Column names, section names, co-group and streaming-group names, use-aspects,
value aspects and encoding hints are neither on the wire nor in the key.

Plain values ride under `plains`, a map from **plain item type** (its
`PlainItemTypeE` ordinal, CBOR uint) to the array of that section's column
values in the same stable-sorted order. The item type is fixed leeway
vocabulary, not a table-authored name, so it is allowed in the key (see
*Forks*); the plain group's signature is not on the wire.

### SD3 — Attribute and value forms

A slot's value is an array of attributes in **canonical order** (decided
2026-08-28, fork 3): attributes are sorted by their cardinalities first —
membership count, then each column's cardinality in key order — then by the
memberships' encoded bytes, then by the values' encoded bytes (the
discriminator, when present, is the last of those); ties are duplicates and
stay adjacent. A column's **cardinality is a function of its value form
alone**: the element count of an array or set, `0` for `null`, `1` for any
other item — so a table-free checker can verify the order from the bytes.
Within one slot every attribute has the same number of columns and either
every attribute carries a discriminator or none does (SD5), so the keys
compared are always the same length. Attribute order is
representation, not content (the ADR-0201 view), so erasing it costs nothing
lossless and makes two producers that emit the same attributes in different
orders byte-identical. Membership order *within* an attribute is
canonicalised the same way (decided 2026-08-28): sorted bytewise on the
encoded membership item, duplicates kept. An attribute is
`[memberships, v_1, …, v_n]` with one `v_i` per column of the signature, in
key order; a value-less slot's attribute is `[memberships]`. In a co-section
group each member section carries its own memberships, so for a slot of
`k > 1` sections the `memberships` element is an array of `k` membership
arrays in signature order (decided 2026-08-28); for a standalone section it
is the membership array itself. Container columns of one section are
co-containers in the DML (one element appended to all of them at once), so a
decoder rejects an attribute whose `h`/`m` values differ in length within
one section.

Scalar and container forms follow ADR-0201 SD3/SD4 **except where that ADR
erases**: integers as major type 0/1; floats as the shortest float that
preserves the value bit-for-bit (a `f32` may travel as float16, never as an
integer); `s` text, `y` bytes with fixed-width padding kept, `b` bool;
temporal as RFC 9581 tag 1001 seconds + nanoseconds; network as RFC 9164 tags
52/54 with **no** IPv4-mapped reduction. A prefix (`vc`/`wc`) travels
**masked**, as that RFC requires: host bits beyond the prefix length are not
content of a network and do not round-trip (decided 2026-08-28). `h` is a
definite array in stored order; `m` is tag 258 over elements sorted bytewise,
**duplicates kept** — set order is not content and sorting is what makes the
bytes canonical, but a duplicate is what the producer wrote: `m` columns are
co-containers with `h` columns in the DML, so dropping one would change the
attribute's length and the decoder could not rebuild it (decided
2026-08-28; deduplication is the ADR-0201 quotient's rule, not this form's).
128-bit integers, bit strings, `d` and `t` are refused, as in ADR-0201.

### SD4 — Memberships carry their channel

A membership is `[channel:uint, identity…]` where `channel` is the
`mappingplan.MembershipChannel` ordinal (all eight cells — cardinality is
carriage, but a lossless form must restore it) and the identity payload is
the ADR-0201 SD5 shape for the channel's `IdentityEncoding`: `uint` for ref,
`bytes` for verbatim, `[uint, bytes]` / `[bytes, bytes]` for the mixed
carriers, `[bytes]` for the parametrized ones. The membership array is sorted
bytewise on the encoded items (SD3), duplicates kept; **all** memberships travel — role
classification is a reader's concern and is not consulted. Aliasing is
preserved as one attribute with several memberships.

### SD5 — Dispatch: narrowing, then a plugin, only where the key is ambiguous

At generation time the generator groups the target table's slots by signature.
A signature with one slot is **unambiguous** and decodes without any hook. For
a signature with several slots the decoder, per attribute:

1. **narrows** the candidates to those whose declared `MembershipSpecE`
   accepts every channel the attribute's memberships carry (a section that
   cannot store the memberships cannot be the target);
2. if exactly one remains, takes it; otherwise calls the table's generated
   **`DispatcherI`** with the attribute's decoded view — the signature, the
   memberships, the values — and the remaining candidate slot ordinals, and
   takes what it returns, or fails the entity if it returns none.

The generated decoder's constructor takes a `DispatcherI`; `nil` is accepted
iff the table has no ambiguous signature, checked at construction, so an
ambiguous table without a dispatcher fails once and early.

Some ambiguities are unresolvable from content alone — `null` vs `undefined`
above have identical signatures *and* identical memberships. For these the
encoder side gets the mirror hook, **`TaggerI`**: for an attribute in a slot
whose signature is ambiguous *in the source table*, it may return a small
opaque discriminator (`uint`) that rides as an optional trailing element of
the attribute item and is handed to the `DispatcherI` on the other side. The
pair is the plug-in's contract; the wire carries only an integer. The
discriminator is uniform per slot: in a source slot whose signature is
ambiguous the tagger is consulted for every attribute and every item carries
one; elsewhere none does. A built-in
`OrdinalTagger` / `OrdinalDispatcher` pair uses the slot's ordinal within its
ambiguity set — it works between identical tables and is *explicitly* a
declaration-order coupling; a consumer that wants section-name independence
supplies its own pair. Without a tagger, the discriminator is absent and the
dispatcher decides from content.

### SD6 — Generated surface: encoder over `readaccess`, decoder into `dml`

The generator is a peer of `dml` and `readaccess`: same `GeneratorDriver` /
`GoClassBuilder` shape, the same `gocodegen.GoClassNamerI` (extended with a
`GoClassNamerCanonWireI`), a `leeway canonwire table generate go` CLI
subcommand, goldens under an `example/` package. For a table it emits:

- **`Encoder`** — takes the table's generated `readaccess` classes (already
  loaded from a batch) and an entity index, writes the entity item into an
  `io.Writer`. Typed accessors are called directly; no `arrow.Array` type
  switch at runtime.
- **`Decoder`** — reads one entity item and drives the table's generated
  `dml` entity: `BeginEntity`, the plain setters, per attribute the slot's
  `BeginAttribute(v_1, …)` / `AddToCoContainers`, the channel-keyed
  `AddMembership<Channel>P` calls, `EndAttributeP`, `CommitEntity`.
- **`DispatcherI`**, **`TaggerI`** and the **signature constants**, with the
  ambiguity sets as generated tables.

The CBOR reader/writer, the value forms (SD3), the membership, attribute and
entity forms (SD1, SD4) and a table-free canonical-order checker are runtime,
not generated: `canonwire/runtime`, which takes over `canonform`'s private
CBOR writer as a shared package (`canonform` keeps its reduction and set
rules on top of it; its bytes do not move). The slot table (SD2) lives in the
generator package.

Encoding straight from a DML entity under construction, and decoding into a
`readaccess`-shaped in-memory view without an Arrow batch, are deferred to a
consumer that needs them (M3).

### Milestones

- **M0 — form and runtime.** ✓ (2026-08-28) `canonwire/runtime`: the
  shared CBOR writer extracted from `canonform` with its goldens unchanged, a
  strict reader for the deterministic subset, the SD3 value forms over Go
  lane types (raw network lanes included), the SD4 membership form, the SD3
  attribute and SD1 entity writers and reader cursors, and a table-free
  `VerifyCanonical`; the SD2 slot table in the generator package.
- **M1 — generator.** ✓ (2026-08-28) Encoder over the generated
  `readaccess` classes, decoder into the generated `dml` classes with
  channel narrowing and the dispatcher/tagger pair (built-in ordinal pair
  included), slot enum and signature constants, CLI; goldens in
  `canonwire/example` over the `readaccess/example` tables, the JSON mapping
  table (`""` ×4 and `s` ×2) and a co-group table; a random-table fuzz over
  `common.PopulateManipulator` for well-formedness of the generated source;
  the four-table round trip (`dml` → batch → encode → decode → batch →
  encode, bytes equal on the second pass, batches equal under the
  `readaccess` accessors) and the refusal cases (nil dispatcher on an
  ambiguous table, unknown slot or plain, rejected channel, co-container
  length, version).
- **M2 — the invariance suite.** ✓ (2026-08-28) Cross-table decode into a renamed,
  re-aspected, column- and section-permuted copy of the source table (same
  types, same specs) and back — the test of the requirement — plus the
  refusal when the target narrows a section's membership spec;
  `pgregory.net/rapid` properties over random *entities* of the golden
  tables: round trip, permutation invariance (attributes, memberships, set
  elements shuffled → identical bytes), content sensitivity; a set column
  beside an array column with a duplicate. Random *tables* stay with the M1
  fuzz: a compiled round trip per random table needs a per-table build and
  is not worth an integration-lane harness until a consumer asks.
- **M3 — deferred.** DML-direct encoding; batch-level framing with a hoisted
  signature table if per-entity key bytes are measured to matter.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| The canonical wire form (a serialization contract, SD1–SD5) | added; versioned by a source-declared integer | any stored bytes — none at M0 |
| `public/semistructured/leeway/canonwire` (new exported Go API) | added: `GeneratorDriver`, `GoClassBuilder`, `runtime` | goldens under `example/` |
| `gocodegen.GoClassNamerI` | reshaped: gains `GoClassNamerCanonWireI` | `DefaultGoClassNamer`, `MultiTablePerPackageClassNamer` |
| `leeway` CLI | added: `canonwire table generate go`, `canonwire table slots` (the SD2/SD5 view of a table), `canonwire verify` (the table-free checker over an item or a sequence) | none |
| `canonform` private CBOR writer | moved to a shared runtime package | `canonform` goldens must not move |

## Alternatives

- **Section-name keys (ADR-0010).** Rejected by the requirement; the QOC
  records the rest.
- **Reflection-bound codec (ADR-0010 §3 bindings).** Would need no generator,
  but the repository's posture since ADR-0041 is generated, typed hot paths,
  and the dispatch tables are exactly what is cheap to compute at generation
  time and expensive to rediscover per record.
- **Make `canonform` lossless.** Its whole value is what it erases; a
  lossless variant would be a different form sharing a writer, which is what
  this is.
- **Fold the membership spec into the key (O4).** Narrowing (SD5 step 1)
  obtains the same discrimination at decode time without putting carriage
  into a content key.
- **Attribute order as stored.** Cheaper — no per-entity sort — but two
  producers emitting the same entity in different attribute orders would then
  differ in bytes, which defeats "canonical". The sort's hold-back is bounded
  by one entity and the encoder's source is an in-memory batch, so the
  streaming objection ADR-0201 had does not apply here.
- **A typed binary lane (RowBinary-like) instead of CBOR.** Smaller, but not
  self-describing, and every value rule in SD3 would still have to be written
  without a standard to check against. CBOR is already the repository's
  choice for `TableDesc`, `canonform` and ADR-0010.
- **Positional column order in the key (O2).** Loses cross-table portability
  for no information gain; see QOC.

## Consequences

### Positive

- One lossless, deterministic byte form for a leeway entity, decodable into
  any table that declares the same types — renames, re-aspecting and
  re-hinting of the endpoint tables do not touch the data.
- Ambiguity is a generation-time fact: the generator knows which signatures
  need dispatch and the decoder refuses to be built without one.
- The encoder and decoder are typed and reflection-free, and reuse the two
  generated APIs instead of adding a third value model.

### Negative

- Tables whose sections differ **only** by name (`null` vs `undefined`) are
  not losslessly round-trippable from content; they need a tagger/dispatcher
  pair, and the built-in ordinal pair reintroduces a declaration-order
  coupling the form otherwise avoids. This is inherent in the requirement,
  not in the design, and is stated rather than hidden.
- Co-section topology is part of the key: a record from a co-grouped source
  does not decode into a target that declares the same sections standalone.
  Subsetting at the co-section boundary is leeway's own atomic unit, so this
  is consistent, but it is a limit.
- A third generator to keep in step with `dml` and `readaccess` whenever
  their generated APIs move.

### Neutral

- Set element order, membership order, attribute order and the order of
  sections are all erased (by sorting; sections by sorted map keys). Stated so a consumer knows which equalities the bytes
  witness — and that a decoded entity's attributes come back in canonical,
  not written, order.
- The form is not a hash preimage; `canonform` remains the identity.
- Two normalisations sit at the edge of "lossless" and are stated as such:
  NaN payloads (SD1) and host bits of a prefix (SD3). Both are values no
  producer in this repository writes deliberately.
- An `m` column's element order is erased, so after a round trip the
  position-wise pairing between a section's `h` co-container and its `m`
  co-container is not the one that was written: the multiset and the length
  survive, the correspondence does not. A producer that relies on that
  pairing has an array, not a set.
- The `null` value form (cardinality 0) is defined for the wire but not
  produced by the generated encoder: the `readaccess` getters read a value
  unconditionally and expose no validity bit, so an Arrow null does not
  round-trip through that read path today. A producer that needs it goes
  through the runtime directly.

## Migration — Tier 1

- **Breaks.** Nothing; the form and generator are additive. `GoClassNamerI`
  gains methods, which breaks external implementers of that interface (none
  are known in this repository beyond the two default namers).
- **Path.** Implementers of `GoClassNamerI` add the `GoClassNamerCanonWireI`
  methods.
- **Regeneration.** None for existing `dml` / `readaccess` outputs; new
  `canonwire` outputs are generated fresh.
- **Old shape.** n/a.

## Verification plan — Tier 1

- **Lane.** Default `go test`: generator goldens (`example/`), the M2
  round-trip and cross-table suites, the `rapid` properties; a
  strict-decode → `CoreDetEncOptions` re-encode self-check on every emitted
  item (the ADR-0201 M1 pattern).
- **What would fail.** A byte change in the form moves the goldens without a
  version bump; a lossy value rule fails the second-pass byte equality; an
  attribute-permutation property (same entity, shuffled attribute order,
  equal bytes) fails if the sort regresses; an
  ambiguous table constructing a decoder with `nil` dispatch fails the
  construction test; a `canonform` golden moving fails the writer extraction.
- **Gap.** No benchmark at M1; the encoder's cost against card-JSON and Arrow
  IPC is measured only when a consumer's latency budget asks for it.

## Forks — decided at acceptance

The six points below were the open choices while the ADR was proposed;
acceptance on 2026-08-28 confirms each as written.

1. **Plain item type in the key (SD2).** Treated as fixed vocabulary, not
   table description. The alternative is to key plains by signature too and
   dispatch them like tagged slots.
2. **Column order inside a group is canonicalised (O3).** The alternative,
   O2, keeps declaration order and forfeits cross-table portability.
3. **Attribute and membership order are canonicalised (SD3)** — attributes
   by cardinalities, then memberships, then values; memberships bytewise.
   ✓ Decided 2026-08-28.
4. **Encoder reads `readaccess`, decoder writes `dml` (SD6).** The prompt's
   wording could also be read as encoding straight from a DML entity under
   construction; that is M3 unless it is the primary need.
5. **The tagger/dispatcher discriminator (SD5).** It keeps section names off
   the wire while making name-only-distinguished sections round-trippable;
   the alternative is to declare such tables out of scope.
6. **NaN payloads and prefix host bits are not content** (SD1, SD3). ✓
   Decided 2026-08-28 while building M0; the alternative for NaN is a
   different float mode in the shared writer plus a `canonform`
   pre-normalisation.

## Status

Accepted 2026-08-28.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0201](./0201-leeway-canonical-record-form.md) — the quotient form this
  form shares a writer and value rules with, and deliberately differs from.
- [ADR-0010](./0010-leeway-cbor-rpc-codec.md) (deferred) — the section-keyed
  lossless wire.
- [ADR-0018](./0018-leeway-card-json-canonical-format.md) — card-JSON.
- [ADR-0072](./0072-leeway-membership-carriage.md),
  [ADR-0073](./0073-leeway-membership-role.md) — channel and role planes.
- [ADR-0041](./0041-rowmarshall-error-shredding.md) — no reflection on the hot path.
- RFC 8949 §4.2, RFC 8742, RFC 9164, RFC 9581.
