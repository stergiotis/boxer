---
type: adr
status: accepted
date: 2026-06-07
reviewed-by: "p@stergiotis"
reviewed-date: 2026-06-07
---

# ADR-0071: Leeway value & emission

## Context

A section attribute carries a **value**, and that value is **emitted** onto the wire in
some layout. This ADR covers two independent axes of the basis (ADR-0070 §Concept basis):
plane **B** (what the value is) and plane **C** (how it is laid out). It supersedes
ADR-0008's D2 (attribute ordering) and its carrier-value-shape work, and the
canonical-native field-type decision recorded as a late update there.

## Decision

### B1 — The value type is a canonical type

A plan field's value type **is** a `canonicaltypes.PrimitiveAstNodeI`. The annotated Go
DTO is one front-end that produces it: the struct-tag and reflect classifiers map Go →
canonical (N:1), and `canonicaltypes/codegen.GenerateGoCode` is the canonical → Go (1:1)
inverse. No Go type string is the plan's source of truth — the codec emitter and the
playground's Go preview derive it from the canonical.

Multiplicity is part of the value type: a `[]T` element-array is the canonical
`ScalarModifier` `HomogenousArray`, and a set (`*roaring.Bitmap`) is `Set`. There is no
separate "is a slice" / "is roaring" flag in the model.

### B2 — Presence is orthogonal to the value type

Optionality is **not** a value type — there is no nullable scalar modifier. Presence is a
separate flag (`IsOption`). Splice semantics follow from presence and multiplicity, not
from the type: an empty container, an empty set, and `Option.Has = false` each emit
**zero** attributes.

### C1 — Scalars emit before containers, within a section

Within a section, fields are stable-sorted so single-attribute single-value fields
(scalars, consts) precede multi-attribute or multi-value fields (containers, explode).
Declaration order is preserved within each class. Section order inside the entity is
unchanged — it follows DTO declaration order. Readers get a stable layout: fixed-size
leaves land at known positions, container scans bunch together. No cross-section
reordering.

### C2 — Explosion is a layout choice, independent of the value

A multi-value field emits either as **one** attribute carrying N values, or — under
`,explode` — as **N** single-value attributes. This is an emission choice over the same
`[]T` value, not a different value type. Carrier value fields (the mixed / parametrized
channels of ADR-0072) may be scalar, `Option[T]`, `[]T`, or `[]T,explode`; the carrier's
slice-ness mirrors the value's, checked per row.

## Alternatives

- **Store the Go type in the plan as the source of truth.** Keep `GoType` / `IsSlice` /
  `IsRoaring` authoritative. Rejected: it duplicates what the canonical already encodes
  and defers the canonical meaning to a downstream target-schema resolution.
  Canonical-native puts the one authoritative type in the plan and derives the Go form
  1:1.
- **Cross-section reordering** (sort attributes globally by class). Rejected: locality
  matters per section, and it interacts badly with the per-row composer (ADR-0070), which
  would have to re-cluster sections into a global order.

## Consequences

- One authoritative type per field; the Go DTO is a front-end, not the definition. The
  `mappingplanview` editor can author the canonical type directly.
- Predictable attribute locality for readers.
- The C1 wire layout differs byte-for-byte from a declaration-order emit; in-memory
  round-trip parity is preserved, and no persisted wire exists to migrate.

## Status

Accepted on 2026-06-07. Re-cuts and supersedes parts of ADR-0008.

Implementation status (2026-06-07): scalar-first ordering and a canonical-authoritative
`FieldShape` are **implemented**; removing the *derived* Go-type/shape fields so the
canonical is the sole stored type is **pending** (refactor Phase 1).

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are
append-only; supersession is recorded, not deleted.

## References

- [ADR-0070 §Concept basis](0070-leeway-entity-assembly.md) — the shared axis model.
- [ADR-0008](0008-leeway-marshall-extensions.md) — superseded; D2 + value-shape work re-cut here.
- [`../../public/semistructured/leeway/mappingplan/`](../../public/semistructured/leeway/mappingplan/) — `FieldShape` (canonical), `ComputeGroups` (scalar-first).
- [`../../public/semistructured/leeway/canonicaltypes/codegen/`](../../public/semistructured/leeway/canonicaltypes/codegen/) — `GenerateGoCode` (canonical → Go).
