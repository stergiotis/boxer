---
type: adr
status: accepted
date: 2026-06-07
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-07
---

# ADR-0070: Leeway entity assembly

## Context

A leeway entity (one row) is assembled from a DTO's **plain columns** (the entity
header — id, timestamps, lifecycle) and its **sections** (the tagged values). This ADR
covers one axis: how DTOs map onto an entity's `BeginEntity`…`CommitEntity` frame.

The original codec bound one DTO to one entity. Callers wanted to compose an entity from
several small, independent DTOs — one carrying audit fields, one the spatial payload, one
the facts — without unioning them into a god-DTO that couples otherwise-independent code
paths.

This ADR is part of a four-ADR re-cut of the leeway DTO-codec onto an orthogonal basis.
It supersedes ADR-0008's D1 (stacked entities) and SD3 (plain ownership), and hosts the
shared **Concept basis** below.

## Concept basis (shared by ADR-0070…0073)

The leeway DTO-codec and its membership model decompose into independent axes. Moving
along one does not constrain the others; they compose as a product.

| Plane | Axes |
|---|---|
| **A · Entity assembly** | composition (1 DTO↔entity • N↔entity); plain-column ownership |
| **B · Value** | canonical value type (incl. array/set modifier); presence (optional → splice) |
| **C · Emission** | attribute order (scalar-first); explosion (one multi-value attribute • N single-value attributes) |
| **D · Membership carriage** | cardinality {low, high} × identity {ref, verbatim, per-row} × params {absent, present} — jointly, the "channel" |
| **E · Membership meaning** | role {primary, secondary}; param-treatment {identity, index, none}; representation (value ↔ string) |
| **F · Mechanism** | pluggable role classifier + default policy |

A membership has a **carriage** (plane D — how the tag rides the wire) and a **meaning**
(plane E — what it does to the attribute model), and the two are independent: any channel
can carry a primary or a secondary tag.

**The one coupling.** Carriage (plane D) is held *constant per section* — every field
targeting one section agrees on a single channel. This is a deliberate lever for
read-side dispatch cost, not a law of the model; relaxing it is possible at a known price
(ADR-0072). Every other axis composes freely.

Naming the axes dissolves two apparent complexities: the "eight membership channels" are
the sparse product of plane D's three sub-axes, and the "four-quadrant role model" is
plane E's role × param-treatment product. Neither is atomic.

Plane → ADR: **A → ADR-0070**, **B/C → ADR-0071**, **D + value identity + representation
→ ADR-0072**, **E meaning + F → ADR-0073**.

## Decision

1. **Two composition modes.** The codec supports both one-DTO-per-entity and
   many-DTOs-per-entity. Multi-DTO composition is a runtime concern, served by a
   reflect-side `RowComposer` that drives one `BeginEntity`…`CommitEntity` frame with
   sections from several DTOs interleaved. Codegen stays one-DTO-per-entity; static
   composition is unusual, and a hot stacked path can be added later — the Plan
   vocabulary already supports it.

2. **Plain-column ownership is explicit per row.** `BeginRow(plainOwner)` names the one
   DTO that drives the entity header for that row; DTOs added afterward contribute only
   their sections. A DTO may be the plain owner in one row and sections-only in another;
   different rows may pick different owners.

3. **Sections may repeat across DTOs in one entity.** The DML state machine permits
   `InEntity → InSection → InEntity → InSection` re-entry, so two DTOs both declaring
   section `Foo` cleanly produce two `BeginSectionFoo`…`EndSection` cycles. The codec
   does not merge them; each DTO's run is emitted as that DTO declares it, in
   plain-owner-then-`AddSections`-invocation order.

## Alternatives

- **God-DTO union.** Define one DTO with every contributing field. Rejected: couples
  independent code paths and defeats the composability that motivates this plane.
- **Batch-shaped `MarshalStack([]rowsA, []rowsB)`.** Emit one entity per row index across
  matched-length batches. Rejected: forces callers to materialise every DTO's rows up
  front in equal-length slices; `RowComposer` lets the caller drive the row loop and
  compose each entity independently (streaming, conditional, per-row-varying mixes).

## Consequences

- Independent fragments (audit, spatial, facts) live in their own DTOs and stack at
  marshal time.
- State-machine misuse (`AddSections` before `BeginRow`) is a runtime error, not a
  compile-time one; messages name the missing transition.
- Codegen stacking is unaddressed; hot stacked writes pay the reflect path's per-row
  cost. Accepted: stacking is a wiring concern, not a hot loop.

## Status

Accepted on 2026-06-07. Re-cuts and supersedes parts of ADR-0008.

Implementation status (2026-06-07): **implemented** — `RowComposer` and per-row plain
ownership exist (formerly ADR-0008 D1/SD3); this ADR only re-homes the decision.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are
append-only; supersession is recorded, not deleted.

## References

- [ADR-0008](0008-leeway-marshall-extensions.md) — superseded; D1/SD3 re-cut here.
- [ADR-0071](0071-leeway-value-and-emission.md) · [ADR-0072](0072-leeway-membership-carriage.md) · [ADR-0073](0073-leeway-membership-role.md) — the other three planes.
- [`../../public/semistructured/leeway/marshallreflect/stack.go`](../../public/semistructured/leeway/marshallreflect/stack.go) — `RowComposer`.
- [`../../public/semistructured/leeway/dml/statemachine.dot`](../../public/semistructured/leeway/dml/statemachine.dot) — the `InEntity → InSection → InEntity` cycle.
