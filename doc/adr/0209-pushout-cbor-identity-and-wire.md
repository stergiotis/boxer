---
type: adr
status: accepted
date: 2026-08-28
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-28
---

# ADR-0209: pushout identity and wire in deterministic CBOR

## Context

pushout used JSON in exactly two places, and they are unrelated to each
other:

- **The identity preimage.** `patch.Patch.ComputeHash` marshalled a Go
  struct with `encoding/json` and hashed the result with unkeyed BLAKE3.
  That preimage *was* the definition of `PatchHash`, and it was defined
  by whatever `encoding/json` happened to do with the `Change` struct.
- **The wire codec.** `envelope.CodecI`'s reference implementation
  (`json1`) marshalled `EnvelopeV1` with indented JSON inside a `PXE1`
  frame.

Neither is a good long-term commitment, and the identity one is actively
hazardous. Four properties of the JSON preimage were load-bearing without
having been chosen:

1. **The Go struct was the schema.** `Change` carries the union of every
   kind's fields; JSON emitted all of them, zero-filled, for every kind.
   Adding a field for one change kind moved *every* patch hash in the
   system, including patches that could not use it.
2. **nil and empty differed.** `null` vs `""` vs `[]`. Two patches equal
   in every semantic respect but nil-vs-empty content or context carried
   different identities.
3. **Bytes went through text.** Content base64, hashes hex — a 32-byte
   hash cost 64 bytes in the preimage and again in every envelope.
4. **The digest was unkeyed.** `blake3.Sum256` over the preimage, so
   there was nothing separating a patch identity from any other BLAKE3
   digest of the same bytes, and no way to version the form such that a
   new version provably cannot collide with the old.

A second consideration sits behind this. ADR-0201 defines the leeway
canonical record form: deterministic CBOR under RFC 8949 §4.2 plus dCBOR
§2.5 numeric reduction, digested with keyed BLAKE3. If pushout's identity
is ever to be computed from leeway rows rather than from Go structs — the
open question in the storage work — the cheap moment to make the two
rule-compatible is before any deployment holds history.

**Premise (code owner, 2026-08-28): no persisted pushout data needs
preservation.** Identity and envelope-format changes therefore cost
nothing today. That window closes at the first deployment that holds
history, which is what makes this a now-or-expensive decision rather than
a free-any-time one.

## Design space (QOC)

**Q1: What produces the identity item?**

- O1a: fxamacker over the domain structs.
- O1b: fxamacker over a frozen identity DTO that exists only to be hashed.
- O1c: an explicit encoder writing CBOR heads directly.

O1c chosen. O1a re-creates property 1 — the Go struct is again the
schema, now silently. O1b decouples the shape but pays for it with a
second set of types that must be kept in step with the first, and
reflection over tags is an indirect way to state a grammar that is
twenty lines when written out. O1c also makes the per-kind shape (SD2)
straightforward, which neither reflection-based option is: the item's
arity depends on the change kind.

`public/semistructured/cbor` already provides the encoder — hand-written,
shortest-argument heads, tag 258 defined, `WASMCompiles`, and every one
of its module dependencies is already in `patch`'s transitive set, so the
reuse is free at the dependency level.

**Q2: What relationship should the identity item have to canonform
(ADR-0201)?**

- O2a: canonform-*shaped* — the change item literally canonform's
  attribute item `[memberships, value]`, the patch in entity terms.
- O2b: canonform-*compatible* — the same encoding rules and digest
  pattern, pushout's own item shape.
- O2c: independent.

O2b chosen. O2a is the only option that would let a future leeway-row
representation of a patch become a *second producer of the same digest*
rather than a second hash break — but it requires fixing globally unique
membership ids now and defining a composite digest over the several
entities a patch is, neither of which exists, for a payoff that depends
on a variant nobody has decided to build. O2c throws away the alignment
for no saving, since the rules cost nothing to follow. O2b keeps the door
open at the price of one later hash break, which is free until persisted
history exists — the same premise this ADR rests on throughout.

**Q3: Keep `json1` alongside the CBOR codec, or replace it?**

- O3a: keep both, default to CBOR.
- O3b: replace.

O3b chosen. ADR-0079 named the readable `json1` payload as the mitigation
for framed envelopes no longer being raw JSON on disk. That mitigation is
now served by `boxer.sh cbor diagnostics`, which renders any CBOR payload
in RFC 8949 §8 diagnostic notation — so the debuggability argument no
longer distinguishes the options, and one wire codec is one surface to
maintain, test, and freeze. The mixed-fleet interop property that
motivated the self-describing frame is unaffected: it is a property of
the frame and the registry, and `exchange/mixed_codec_test` still
exercises it with two codecs.

**Q4: How is a stale-identity-scheme envelope detected?**

- O4a: an identity-version field on the envelope.
- O4b: infer from the frame's codec name.
- O4c: nothing — it fails validation.

O4c chosen. The keyed digest (SD5) already guarantees a v1 hash can never
be produced under a v2 form, and `envelope.Validate`'s recompute-compare
turns an old-scheme envelope into `ErrTampered`, whose documentation
already states "or the bytes were produced against a different identity
scheme". O4a costs an `EnvelopeV2`; O4b couples the identity form to the
wire codec, which the rest of this design keeps apart. Under the premise
there is nothing to detect. **This is a deferral, not a solved problem**
— see *Deferrals, recorded*.

## Decision

Replace both JSON uses with deterministic CBOR under RFC 8949 §4.2 core
deterministic encoding — the rule set ADR-0201 pins.

**The patch identity form v1** is a CBOR item, digested with keyed
BLAKE3-256. Its grammar (diagnostic notation; normative statement and
worked examples live with the code, in `pushoutgraph/patch/identityform.go`):

```text
patch        = [ dependencies, changes ]
dependencies = 258([ bytes .size 32, * ])   ; sorted, deduplicated
changes      = [ * change ]                 ; position order is identity

change       = [ 0, nodeid, bytes, context, context ]   ; NewNode: content, up, down
             / [ 1, nodeid ]                            ; DeleteNode
             / [ 2, nodeid, nodeid ]                    ; NewEdge: src, dest

context      = [ * nodeid ]
nodeid       = [ patchref, uint ]
patchref     = bytes .size 32 / null        ; null == "this patch"
```

**The wire codec** is `cbor1` (`envelope.CBORV1`), replacing `json1`,
which is removed. `EnvelopeV1` is encoded as-is under
`cbor.CoreDetEncOptions`.

### Subsidiary design decisions

- **SD1 — the two changes are independent.** The identity form is not the
  wire form, and re-encoding an envelope with a different codec preserves
  identity. ADR-0079 Q2's O2b ("ship as received") is untouched.
- **SD2 — per-kind change arrays.** A change carries only the fields its
  kind uses, tagged by the kind itself. Adding a field to one kind moves
  only that kind's hashes. An unknown kind is an error, not a fallthrough:
  a shape nobody defined must not silently collide with a defined one.
- **SD3 — dependencies as a tag-258 set.** `canonicalDeps` already sorts
  and deduplicates; tag 258 ("mathematical finite set") records that this
  is *intent* rather than an incidental ordering. canonform tags its sets
  the same way. Change order, by contrast, is identity-bearing — `Apply`
  and `validateAgainst` depend on it — and stays a plain array.
- **SD4 — nil and empty fold.** An absent context and an empty one both
  write as `80`; absent content writes as `40`. This is a deliberate
  narrowing of identity: two patches that differ only in nil-vs-empty
  were distinct before and are the same patch now. Nothing in the engine
  distinguished them semantically.
- **SD5 — self-reference is `null`.** A patch cannot contain its own
  hash, so pre-fixup NodeIDs mean "this patch". The item writes that as
  CBOR `null` — self-describing, one byte, and no real hash can collide
  with it. `PlaceholderHash` (`0xFF…`) remains an in-memory construction
  device but is no longer a reserved point in the hash space. The zero
  (root/genesis) hash stays a real 32-byte string, distinct from `null`.
- **SD6 — keyed digest with a versioned context.** BLAKE3 `derive_key`
  over a context string naming the form and its version, then BLAKE3-256
  in keyed mode — the ADR-0201 SD7 pattern. Keyed mode is BLAKE3's own
  domain separation: a v2 form can never collide with v1, and a patch
  hash is not reproducible by hashing the same bytes unkeyed. The context
  string is wire-frozen.
- **SD7 — the preimage is inspectable.** `Patch.IdentityPreimage` returns
  the exact bytes the digest is taken over. Production code never calls
  it; it exists so the form can be pinned by a test, printed with
  `boxer.sh cbor diagnostics`, and compared against another
  implementation. canonform's `NewRecordingDigester` serves the same need.
- **SD8 — `types.HashBytes` is removed.** It was a public helper that
  computed an unkeyed BLAKE3 digest of arbitrary bytes and had exactly
  one caller, the old `ComputeHash`. Leaving it would leave a plausible-
  looking way to compute something that is no longer a patch hash. The
  digest now lives with the form that defines it, as canonform's digester
  lives beside its encoder; `types` keeps only the value type.
- **SD9 — the codec normalises time to UTC.** RFC 3339 with nanosecond
  precision (matching buscodec, ADR-0036), because CBOR's default
  integer-seconds time truncates sub-second instants and the float
  variants cannot hold nanoseconds at current epochs. RFC 3339 renders
  the zone, so the same instant in two locations would otherwise encode
  to different bytes; determinism is a codec obligation, carrying the
  zone is not.
- **SD10 — the codec rejects duplicate map keys.** A payload binding a
  field twice has no single reading, and this is the untrusted-bytes
  path. Unknown fields stay ignored, so a peer on a newer build is still
  decodable by an older one.

### Deferrals, recorded

- **Stale-identity-scheme detection** (Q4). Today an envelope from an
  older identity form fails `Validate` as `ErrTampered`, which is
  accurate but says nothing useful. The trigger to revisit is the first
  deployment holding history: from that point a form change needs a
  version field on the envelope and a migration, not a flag day.
- **Canonform-shaped identity** (Q2). Deferred with a known cost: one
  further hash break, free until persisted history exists, at which point
  it becomes a fleet flag day. The prerequisites — globally unique
  membership ids, a composite multi-entity digest, and an Arrow-free
  canonform path that preserves `patch`'s WASM posture — are not this
  ADR's to build.

## Alternatives

Considered and rejected above per question. Additionally:

- **RFC 7049 §3.9 canonical CBOR** (length-first map-key sort), which
  `buscodec` uses, instead of RFC 8949 §4.2 core deterministic (bytewise
  sort). Both are deterministic and the choice is wire-frozen once made.
  RFC 8949 is the current standard and what ADR-0201 pins, so it is the
  one repo-wide rule worth converging on; `buscodec` predates ADR-0201
  and is not in scope here.
- **A compact wire DTO** (`cbor:",toarray"` / `keyasint`) instead of
  encoding `EnvelopeV1` as-is. Smaller on the wire, but a frozen
  positional layout is *less* tolerant of schema evolution than a
  field-keyed map, where an added field is ignored by old peers and
  zero-valued for new ones. The wire form is not identity and is
  versioned by its codec name, so shape-follows-Go is not the hazard here
  that it is in the identity item.

## Consequences

### Positive

- Adding a field to one change kind no longer moves every patch hash.
- Patch identity is a defined CBOR item with a written grammar, not a
  byproduct of `encoding/json`'s treatment of a Go struct.
- Keyed digest gives domain separation and a collision-free version bump.
- Envelopes shrink substantially — the conformance golden went from 3033
  to 1233 bytes, the same envelope with hashes and content as byte
  strings instead of hex and base64.
- Identity, digest and encoding rules are now the same family as ADR-0201,
  so a later leeway-row producer starts from alignment rather than from a
  translation.

### Negative

- Every `PatchHash` changes. Free under the premise; a flag day later.
- One hash break remains available and unclaimed (canonform-shaped, Q2).
- On-disk envelopes are no longer greppable text. `boxer.sh cbor
  diagnostics` renders them, but it is a step that `less` was not.
- Two CBOR encoders now live in the tree with different key-sort rules
  (`buscodec` on RFC 7049, this and canonform on RFC 8949). They do not
  interoperate and nothing forces them to; the divergence is now written
  down rather than latent.

### Neutral

- `repo.StorageI`, the recordstore adapter, the `GRG1` snapshot,
  `exchange`'s `PeerI`/`AcceptorI`, and the `PatchHash` type and size are
  untouched. A CBOR envelope is still a blob, so the blob-vs-rows
  question is exactly where it was.
- `PatchHash`'s hex `MarshalText`/`String` stay: they are display and
  JSON-side interop, not identity.
- `patch`, `envelope` and `repo` keep their `WASMCompiles` declarations.

## Verification

- `envelope/codectest` runs unchanged against `cbor1`, plus codec-local
  tests for the properties its fixture cannot reach: sub-second timestamp
  survival (the fixture's timestamp has zero nanoseconds, so the suite
  could not catch truncation), zone-independence of the bytes, and
  duplicate-key rejection.
- The identity form is pinned two ways: its diagnostic notation for a
  patch exercising every kind, and the digest, via the envelope golden's
  `goldenPatchHashHex`. Byte-level properties — stability across
  `NewPatch`'s fixup, the placeholder never appearing in the item,
  nil/empty folding, and the digest being keyed — are asserted on the
  preimage rather than inferred from digests.
- The `GRG1` snapshot golden must not move, and does not: it is built
  from synthetic hashes, not from `NewPatch`.

## Status

Accepted 2026-08-28. The implementation described here has landed:
the identity form and its digest, the `cbor1` codec, the removal of
`json1`, and the regenerated goldens.

The two deferrals above are the open items — neither blocks anything
today, and both have their trigger named.

## References

- ADR-0079 (pushout storage/codec/exchange seams) — Q2 distinguishes the
  identity codec from the wire codec; see its 2026-08-28 update.
- ADR-0201 (leeway canonical record form) — the deterministic-CBOR rule
  set and the keyed-digest pattern this form follows.
- ADR-0036 (`buscodec`) — the other in-tree CBOR codec, on RFC 7049.
- ADR-0025 (pushout erasure architecture) — its Context states what the
  patch hash covers.
- RFC 8949 §4.2 (core deterministic encoding), §8 (diagnostic notation);
  RFC 7049 §3.9 (canonical CBOR); IANA CBOR Tags registry, tag 258
  (mathematical finite set).
