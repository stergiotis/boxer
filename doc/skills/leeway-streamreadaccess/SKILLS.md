---
type: reference
audience: agent reading this skill
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.
# SKILL: Implementing `streamreadaccess.SinkI`

## Purpose

`SinkI` is a SAX-like push protocol for rendering Leeway table data.
A `Driver` walks Arrow records and calls `SinkI` interface methods in strict nesting order.
Implementors produce output (text, JSON, HTML, data frames, etc.) by responding to these calls.

## Call Protocol

```
BeginBatch()
  [BeginEntity()
    [BeginPlainSection(itemType, names, types, nAttrs)
      [BeginPlainValue()
        [BeginColumn(addr, name, type)
          BeginScalarValue() | BeginHomogenousArrayValue(card) | BeginSetValue(card)
            [WriteString(s) | Write(p)]          // value content
            [BeginValueItem(idx) ... EndValueItem()]*  // only for array/set
          EndScalarValue() | EndHomogenousArrayValue() | EndSetValue()
        EndColumn()]*
      EndPlainValue()]*
    EndPlainSection()]*

    BeginTaggedSections()
      [BeginCoSectionGroup(key)]*  // optional wrapper
      [BeginSection(name, names, types, nAttrs)
        [BeginTaggedValue()
          [BeginColumn(addr, name, type)
            // same scalar/array/set pattern as above
          EndColumn()]*
          BeginTags(nTags)
            [AddMembership*(...)]*
          EndTags()
        EndTaggedValue()]*
      EndSection()]*
      [EndCoSectionGroup()]*
    EndTaggedSections()
  EndEntity()]*
EndBatch()
```

## Key Types

| Type | Package | Role |
|---|---|---|
| `naming.StylableName` | `leeway/naming` | Column/section names. Use `.String()` for display; `.Equal()` for comparison. IR carries the desired naming style — never call `.Convert()` in emitters. |
| `canonicaltypes.PrimitiveAstNodeI` | `leeway/canonicaltypes` | Canonical type (e.g. `f32`, `u64`, `sh`, `u64m`). May be `nil`. Use `.String()` for display. |
| `naming.Key` | `leeway/naming` | Co-section group key. Cast to `string` for display. |
| `common.PlainItemTypeE` | `leeway/common` | Plain section item type (e.g. entity ID, natural key). Use `.String()` for display. |
| `PhysicalColumnAddr` | this package | `{Index int, FullColumnName string}` — Arrow column identity. |

## Invariants

1. Every `Begin*` has exactly one matching `End*`.
2. `BeginPlainValue` never appears inside tagged sections. `BeginTaggedValue` never appears inside plain sections.
3. `BeginTags`/`EndTags` only appears inside `BeginTaggedValue`...`EndTaggedValue`, never inside plain values.
4. Column names within a section are unique.
5. `nAttrs == 0` → no `BeginTaggedValue`/`BeginPlainValue` calls follow.
6. `WriteString`/`Write` calls only occur between `BeginScalarValue`...`EndScalarValue` or `BeginValueItem`...`EndValueItem`.
7. `BeginTaggedSections()` is called exactly once per entity, after all plain sections.

## Error Handling

- Return errors from `End*` methods that return `(err error)`.
- Use error-as-state: store the first error, short-circuit subsequent writes, surface at the next `End*` call.
- The driver stops processing entities on first error from `EndTaggedValue` but always calls `EndBatch`.

## Implementation Checklist

### Data Conversion Emitters (JSON, Protobuf, Arrow, data frames)

1. **`BeginBatch`/`EndBatch`**: Open/close the top-level container (array, stream header/footer).
2. **`BeginEntity`/`EndEntity`**: Open/close per-entity container.
3. **`BeginPlainSection`**: Store `itemType` + column schema. Plain sections have exactly 1 value row per entity.
4. **`BeginPlainValue`/`EndPlainValue`**: Open/close the value record.
5. **`BeginTaggedSections`/`EndTaggedSections`**: Transition from plain to tagged data. May open a new container or be a no-op.
6. **`BeginSection`**: Store section `name` + column schema + `nAttrs`. Skip rendering if `nAttrs == 0`.
7. **`BeginTaggedValue`/`EndTaggedValue`**: Open/close an attribute record. Tags follow columns.
8. **`BeginColumn`/`EndColumn`**: Write a named field. Use `name.String()` as the key.
9. **Scalar/Array/Set dispatch**: Check which `Begin*Value` was called. Arrays and sets may need distinct representation (e.g. JSON arrays vs `{"set": [...]}`).
10. **`BeginTags`...`EndTags` + `AddMembership*`**: Serialize membership tags with their type discriminator and display values.

### Human-Readable Emitters (text, HTML, TUI)

1. **Buffer per section or per row.** Columnar alignment requires knowing all values before rendering. Buffer rows during `BeginTaggedValue`...`EndTaggedValue`, flush at `EndSection`.
2. **Column lookup.** `BeginColumn` provides a `name`. Match it to the column index from `BeginSection`'s `valueNames` using `.Equal()`.
3. **Collection rendering.** `BeginHomogenousArrayValue(card)` means `card` items follow. Render with index prefixes (`[0]`, `[1]`...) for arrays, bullets (`•`) for sets.
4. **Tag rendering.** Tags arrive via `AddMembership*` calls. Five membership types exist:
    - `Ref` (low/high card reference)
    - `Verbatim` (low/high card verbatim string)
    - `RefParametrized` (ref + params)
    - `MixedLowCardRefHighCardParam` (mixed ref)
    - `MixedLowCardVerbatimHighCardParam` (mixed verbatim)
5. **Empty sections.** `nAttrs == 0` → collapse or hide.
6. **Plain vs tagged visual distinction.** Plain values have no tags — render without a tag footer/area.
7. **Co-section groups.** `BeginCoSectionGroup(key)` wraps multiple sections that share topology. Render as a visual group.

## Minimal Skeleton

```go
type MySink struct { err error }

func (inst *MySink) BeginBatch()              {}
func (inst *MySink) EndBatch() error           { return inst.err }
func (inst *MySink) BeginEntity()              {}
func (inst *MySink) EndEntity() error          { return inst.err }
func (inst *MySink) BeginPlainSection(...)     {}
func (inst *MySink) EndPlainSection() error    { return inst.err }
func (inst *MySink) BeginPlainValue()          {}
func (inst *MySink) EndPlainValue() error      { return inst.err }
func (inst *MySink) BeginTaggedSections()      {}
func (inst *MySink) EndTaggedSections() error  { return inst.err }
func (inst *MySink) BeginCoSectionGroup(...)   {}
func (inst *MySink) EndCoSectionGroup() error  { return inst.err }
func (inst *MySink) BeginSection(...)          {}
func (inst *MySink) EndSection() error         { return inst.err }
func (inst *MySink) BeginTaggedValue()         {}
func (inst *MySink) EndTaggedValue() error     { return inst.err }
func (inst *MySink) BeginColumn(...)           {}
func (inst *MySink) EndColumn()                {}
func (inst *MySink) BeginScalarValue()         {}
func (inst *MySink) EndScalarValue() error     { return inst.err }
func (inst *MySink) BeginHomogenousArrayValue(int) {}
func (inst *MySink) EndHomogenousArrayValue()  {}
func (inst *MySink) BeginSetValue(int)         {}
func (inst *MySink) EndSetValue()              {}
func (inst *MySink) BeginValueItem(int)        {}
func (inst *MySink) EndValueItem()             {}
func (inst *MySink) Write(p []byte) (int, error)       { return len(p), nil }
func (inst *MySink) WriteString(s string) (int, error)  { return len(s), nil }
func (inst *MySink) BeginTags(int)             {}
func (inst *MySink) EndTags()                  {}
func (inst *MySink) AddMembershipRef(...)                          {}
func (inst *MySink) AddMembershipVerbatim(...)                     {}
func (inst *MySink) AddMembershipRefParametrized(...)              {}
func (inst *MySink) AddMembershipMixedLowCardRefHighCardParam(...) {}
func (inst *MySink) AddMembershipMixedLowCardVerbatimHighCardParam(...) {}

var _ SinkI = (*MySink)(nil)
```

## Membership Role Classification

Per boxer ADR-0007 (and pebble2impl [ADR-0017](../../adr/0007-leeway-membership-role-classifier.md)), sinks that produce attribute-centric output (JSON, ODCS, anything with a "primary key + annotations" shape) consume a `membershiprole.ClassifierI` to decide whether each `AddMembership*` call delivers a primary or secondary tag.

The classifier interface lives in boxer at `github.com/stergiotis/boxer/public/semistructured/leeway/membershiprole`. It takes a `MembershipValue` (which mirrors the `AddMembership*` payload shapes) plus a `SectionContext` (section name + `useaspects.AspectSet`) and returns `(MembershipRoleE, ParamTreatmentE)`.

### Sink-side adapter pattern

A classifier-aware sink wraps each `AddMembership*` call:

```go
func (inst *MySink) AddMembershipVerbatim(lowCard bool, verbatim, humanReadable string) {
    mv := membershiprole.MembershipValue{
        Kind:               membershiprole.MembershipKindVerbatim,
        LowCard:            lowCard,
        Verbatim:           verbatim,
        HumanReadableValue: humanReadable,
    }
    role, _ := inst.classifier.Classify(inst.currentSectionCtx, mv)
    if role == membershiprole.MembershipRolePrimary {
        inst.routeToAttributeKey(verbatim)
    } else {
        inst.routeToLabelSlot(verbatim)
    }
}
```

The five `AddMembership*` shapes map onto the five `MembershipKindE` values one-to-one. `SectionContext` is populated at `BeginSection` and `BeginPlainSection` from the section's `UseAspects` field, which carries `useaspects.AspectSectionMembershipsAllPrimary` / `useaspects.AspectSectionMembershipsAllSecondary` when the application sets them.

`DefaultClassifier` covers the common case (path-prefix verbatim → primary, plain identifier → secondary, ref-shaped → primary). Applications with different conventions implement `ClassifierI` directly.

### When to use the classifier

- **Attribute-centric data conversion emitters** (JSON, NDJSON, ODCS field lists, JSON-Schema generation): always — the role decides keying versus annotation.
- **Schema document writers**: yes — section uniformity and per-section role inventory are emitted from the classifier output.
- **Section-centric or row-oriented emitters** (Unicode tables, HTML cards, SVG): optional — the role information is useful for visual distinction (different cell style for labels) but not load-bearing.
- **Wire-protocol emitters that mirror the IR** (raw Arrow round-trip, debug dumps): no — the protocol carries memberships uniformly; role classification is consumer policy.

## Reference Implementations

| Emitter              | File                     | Strategy | Key Technique |
|----------------------|--------------------------|---|---|
| `UnicodeCardEmitter` | `leeway_card_unicode.go` | Buffered per section | Accumulates `textRow` cells, flushes box-drawn table at `EndSection` |
| `JsonCardEmitter`    | `leeway_card_json.go`    | Streaming | `jsontext.Encoder.WriteToken()` — zero buffering |
| `HtmlCardEmitter`    | `leeway_card_html.go`    | Streaming + cell buffer | Streams HTML tags directly; buffers one `cellBuf` per column, flushes at `EndColumn` |