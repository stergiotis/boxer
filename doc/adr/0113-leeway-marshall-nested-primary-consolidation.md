---
type: adr
status: proposed
date: 2026-07-10
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0113: leeway marshall — the nested authoring model becomes primary; the flat grammar freezes at its simple subset

## Context

The marshall stack has two authoring front-ends (codegen `marshallgen`, runtime
`marshallreflect`, ADR-0074) over one shared plan model. On top of that, the
flat `lw:` tag grammar accreted four *escalation* mechanisms, each added when a
section outgrew a one-line tag: multi-sub-column `:column` suffixes
([ADR-0101](0101-leeway-marshall-mixed-shape-sections.md)), dynamic-membership
tuples via the `@membership` sentinel
([ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md),
[ADR-0109](0109-leeway-marshall-multi-membership-ref-tuples.md)), membership
channel flags, and carrier siblings paired by identical tag.

A second spelling of the same map — the **nested attribute-struct model**, in
which every leeway concept is a typed Go construct (`lw.*` channel markers,
`lw.Single`, lane types, cardinality by field multiplicity) — shipped
2026-07-07..09, specified in the draft
[nested marshalling how-to](../howto/leeway-marshalling-nested.md) but without
an ADR. This ADR is also that front-end's retroactive design record.

A census (2026-07-10) framed the consolidation question:

- Every in-tree production DTO (the keelson bus codecs, the recordstore DTOs)
  uses only the **simple subset**: header roles plus one-line `m,section`
  fields, with occasional `,unit` / `const=` / `,verbatim` / `,ct=`.
- The escalation mechanisms have exactly one production-bound consumer — an
  external ontology adopter (downstream repository) whose hand-written generic
  strand uses ADR-0101 sections and ADR-0103/0109 tuples, and whose per-schema
  strand is *generated* and needs only the simple subset.
- `,explode`, `highCardVerbatim`, and the parametrized channel flags have no
  users anywhere, demos included.
- Maintenance cost is multiplicative: the byte-identity invariant must hold
  across gen × reflect × (flat | nested) × hand-written DML, so every feature
  lands several times and is gated by a cross-decode matrix. The interim
  nested capability matrix (reflect ahead of codegen, carriers unbuilt) is a
  symptom.

The nested model reaches every escalation shape with one mechanism — the
field's Go type — where the flat grammar needs four; it converts plan-time
string validation into compile-time errors; and it separates the two
multiplicity axes (attributes per row vs memberships per attribute) that the
flat tuple conflates. Carrying both complete paradigms indefinitely doubles the
documentation and the regression matrix without adding capability.

## Decision

**D1 — the nested model is the primary escalation surface.** The flat grammar
remains the authoring surface only for its simple subset (the degenerate case
of the nested model); everything past a one-line tag is authored nested.
Remaining parity work, in independently shippable slices:

- **P1** — the codegen value-marker bridge: entity-level `lw.Single` and the
  lane types across the SoA / `Append` / `Row` / decode paths (today
  reflect-only). Known-hard: a first attempt was reverted because the bridge
  touches every codegen value path (SoA and AoS drivers, both decode paths,
  `Append` / `Row`); the recorded design is SoA-stores-marker — the staging
  column keeps the marker type, `Append`/`Row` pass through, the scalar
  value expression unwraps, decode re-wraps.
- **P2** — `lw.Single` as a *nested* sub-column (`BeginAttributeSingle` inside
  the tuple path), both front-ends.
- **P3** — One / Optional dynamic-membership sections (lift the `[]Attr`-only
  restriction).

**Carriers are out of scope** for the parity program: their design is parked
in **D6** below (absorbing former ADR-0110) until the consumer's
statements/provenance slice commits; until then a carrier membership remains
flat-grammar-only, as today.

**D2 — no flat-grammar changes before the switchover.** The full flat grammar
(including the zero-consumer corners) stays intact while parity lands, so
consumers face exactly one migration moment. The **switchover gate**: P1–P3
green, and the escalation consumers migrated (the external generic-strand DTOs
and the in-tree demo DTOs — the production simple-subset DTOs are untouched by
construction). At switchover, one deprecation pass makes `goplan.PlanBuilder`
reject the flat escalation spellings — `:column` suffixes, `@membership`,
carrier siblings, `,explode`, `highCardVerbatim`, `lowCardRefParametrized`,
`highCardRefParametrized` — each rejection naming the nested (or DML-level)
replacement. The permanent simple subset is the closed list: the four header
roles; `m,section` one-liners (scalar, `Option`, bag) on the default,
`highCardRef`, or verbatim channels; `,unit`; `const=`; `,ct=`.

**D3 — ADR dispositions.** ADR-0103 and ADR-0109 flip to accepted (their
consumer exists and their gates are green). ADR-0110 is **folded into D6** and
its file deleted; the number stays retired (the ADR-0104 precedent), so the
marshalling arc carries no standing deferred ADR for an unbuilt slice. This ADR
supplies the previously missing design record for the nested front-end itself.

**D4 — documentation converges on one recipe at switchover.** The two how-tos
([flat](../howto/leeway-marshalling.md),
[nested](../howto/leeway-marshalling-nested.md)) merge into a single document:
the simple subset presented as the degenerate case, the nested model as the
escalation path, no per-front-end capability matrix (the gate guarantees
parity). Until then both stand as-is. The package EXPLANATIONs and the leeway
root EXPLANATION proceed through review on their own track.

**D5 — the authoring policy this encodes.** Humans hand-write simple DTOs;
rich models *generate* DTOs (a consumer-side generator targeting the simple
subset, as the external adopter's per-schema strand demonstrates); the nested
model is the one escalation mechanism; hand-written DML loops remain the
sanctioned path for shapes past the DTO model and stay a peer in the
byte-identity gate. Future expressiveness gaps are closed by generation or DML
first, by grammar only with a named consumer.

**D6 — the parked carrier design (absorbing former ADR-0110).** ADR-0110
proposed carrier-channel memberships in dynamic tuples; it is folded here
rather than kept as a standing deferred file. The design of record, preserved
for its unpark:

- **Shape.** Per-value provenance and edge qualifiers (source dataset,
  language, first-seen) are the membership **params** axis
  ([ADR-0072](0072-leeway-membership-carriage.md)), N per entity — a tuple
  whose `@membership` field (or nested marker field) is on a carrier channel.
- **Spelling.** The field *is* the carrier — flat: the matching
  `marshalltypes` struct (`MixedLowCardRef`, `MixedLowCardVerbatim`,
  `Parametrized`); nested: an `lw.MixedRef` / `lw.MixedVerbatim` /
  `lw.RefParams` marker. No value-sibling pairing (that stays rejected inside
  tuples) and no lookup: the codec emits one `AddMembership<Carrier>P`
  per attribute from the field's identity + params, preserving front-end
  byte-parity; params are wire-emitted even when empty (ADR-0008's SD8
  presence rule).
- **Read.** One element per attribute (ADR-0109 D3): identity from the
  channel's identity Seq, params from the params Seq, assembled into the
  carrier field; heterogeneous channels distribute independently.
- **Bounds.** Exactly one carrier membership per attribute — a repeated
  carrier list needs the same positional-split rule ADR-0109 D4 deferred; a
  params-only carrier's read assembly (its identity *is* the params) is
  confirmed at unpark; carriers stay barred as sub-columns (ADR-0101 D2
  untouched).
- **Why ADR-0103's O2 rejection does not bind.** It was placement-specific:
  byte-fidelity there was measured against a consumer schema committed to
  plain verbatim membership columns; a consumer that *wants* the params
  dimension declares carrier columns as its target.
- **Unpark condition.** The consumer's per-value-provenance (statements)
  slice commits — today its params ride a plain string sub-column, so nothing
  is blocked. Until then carriers remain flat-grammar-only and
  single-attribute (non-tuple), and the carrier slice of the parity program
  stays out of scope.

## Verification

- The existing byte-identity matrix stays load-bearing: `array.RecordEqual`,
  Arrow IPC byte equality, gen↔reflect cross-decode, and the nested-vs-flat
  equal-records gates, extended per parity slice.
- Every in-tree `.out.go` regenerates wire-stable at each slice.
- The switchover adds negative tests: each deprecated spelling fails at
  `PlanFor` / `Validate` with an error naming its replacement.

## Alternatives

- **Freeze the nested front-end as a parked experiment** (keep flat primary).
  Rejected: it leaves two half-paradigms and the deferral matrix in place
  permanently. The strongest argument for it — generated DTOs don't need
  authoring ergonomics, and generation is where rich models are headed — is
  real, but the compile-time safety and the single-mechanism end state were
  judged worth finishing.
- **Remove the nested front-end.** Rejected: it reverts verified work, loses
  the lane-type answer to where schema-fixed canonical metadata lives, and the
  revert is not clean (the lane commits interleave wire-level IPv4/CIDR
  representation changes that must stay).
- **Cull the zero-consumer corners before the switchover.** Rejected: two
  migration/deprecation moments instead of one; the corners cost little while
  parity lands and fall in the same switchover pass anyway.
- **Accept the carrier tuple slice (former ADR-0110) now.** Rejected: its
  consumer slice is not committed; parking keeps the design and kill-reasons
  without minting an accepted-but-unconsumed decision.
- **Keep ADR-0110 as a standing deferred ADR.** Rejected: its content
  compresses losslessly into D6 plus the nested how-to's carrier section, and
  the marshalling arc's document count is itself a complexity this
  consolidation exists to cap.

## Consequences

- Interim, the regression matrix is at its maximum (both paradigms live).
  Accepted temporarily and bounded by the switchover gate.
- The switchover migration is small and enumerable: the external
  generic-strand DTOs and the in-tree demos; production DTOs are in the
  permanent subset by construction.
- The nested struct gives store reads (`ReadRow`,
  [ADR-0100](0100-recordstore-generated-leeway-clickhouse-store.md) /
  [ADR-0105](0105-keelson-adopts-generated-record-stores.md)) a natural
  destination for tuple kinds — enabled by this program, not scheduled by it.
- Risk: parity stalls and the two-paradigm interim persists. Mitigation: P1–P3
  are independently shippable, carriers are already descoped, and the
  deprecation pass needs only P1–P3.

## Open questions

- Whether `,unit` and `,ct=` eventually join the deprecation set once
  `lw.Single` and the lanes are universal, or stay as permanent one-liner
  sugar (lean: stay).
- Marker coverage for channels deprecated from the flat grammar
  (`HighVerbatim`, the parametrized carriers as types): added only when a
  consumer arrives; until then those channels exist at the DML level only.
- Switchover sequencing relative to a ReadRow-over-tuples slice.

## Status

Proposed (2026-07-10). Outcome of a consolidation design dialogue held
2026-07-10 after a complexity review of the marshall stack; also records,
retroactively, the nested front-end shipped 2026-07-07..09 from the draft
how-to. No code changes accompany this ADR; implementation follows the parity
slices.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0042](0042-keelson-leeway-codec-soa-generator.md) — the original codegen paradigm
- [ADR-0072](0072-leeway-membership-carriage.md) — the membership channel grid (wire model; untouched here)
- [ADR-0074](0074-leeway-marshall-package-layout.md) — the front-end / plan-model split
- [ADR-0101](0101-leeway-marshall-mixed-shape-sections.md), [ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md), [ADR-0109](0109-leeway-marshall-multi-membership-ref-tuples.md) — the escalation arc (its carrier extension, formerly ADR-0110, is folded into D6)
- [doc/howto/leeway-marshalling.md](../howto/leeway-marshalling.md), [doc/howto/leeway-marshalling-nested.md](../howto/leeway-marshalling-nested.md) — the two recipes this converges
- The external ontology adopter's mapping design (downstream repository) — the escalation consumer
