---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# marshallgen — Explanation

`marshallgen` is the code generator for the leeway DTO codec: it reads an
annotated Go struct (the DTO) via go/ast into a
[`mappingplan.Plan`](../mappingplan/) and emits a `.out.go` carrying typed
SoA columns plus generic `<Kind>BuildEntities` / `<Kind>FillFromArrow`
helpers that bind to any leeway DML / RA via Go's type inference at the
call site.

The DTO model itself is not defined here. The `Plan`, the `lw:` tag
grammar (`SplitLW`), per-field validation and assembly (`PlanBuilder`),
the `MembershipChannel` enum, section grouping (`ComputeGroups`), and
field-shape classification (`ClassifyBegin`) all live in the sibling
[`mappingplan`](../mappingplan/) package. `marshallgen` is the go/ast
front-end plus emitter over that model; the reflect-driven
[`marshallreflect`](../marshallreflect/) is the other front-end and a
runtime codec over the same model. The two front-ends share `mappingplan`
and do not depend on each other.

Schema-specific wiring (membership-id resolution, builder pools,
Marshal / Unmarshal / bus-codec wrappers) lives behind a
`WrapperEmitterI` hook the caller passes in. Pebble's `factswrapper`
implements the hook for `runtime.facts`; anchor uses the bundled
`NoOpWrapper`. The same generator drives both.

## Background

A leeway schema (`boxer/public/semistructured/leeway`) declares
**sections** that carry **attributes**; each attribute belongs to one
or more **memberships** (named keys identifying which axis the value
populates). The schema's DDL / DML / RA generators produce typed
builder + reader classes per schema. `marshallgen` produces the
codec on top — given a DTO whose fields name memberships, it emits
the per-row chain that drives the DML and the matching per-row read
from the RA.

The lift up to boxerstaging was motivated by (i) two consumer
schemas (`runtime.facts`, `anchor`) demonstrating the same emit
shape works generically, and (ii) the wrapper / core split letting
schema-coupled code live in the consumer instead of behind a
target flag in the generator.

## How it works

### Inputs

A DTO is a single struct in a single file. The grammar:

```go
type MyDTO struct {
    _ struct{} `kind:"<entity-kind>"`           // entity-level metadata
    _ struct{} `lw:"<m>,<s>,const=<value>"`     // optional: constant emits

    <PlainField> <T> `lw:",<col>"`              // plain row column
    <TaggedField> <T> `lw:"<m>,<s>[:<col>][,<flag>...]"`
}
```

Plain columns are `id` / `naturalKey` / `ts` / `expiresAt` — the
entity-header roles that drive `SetId` / `SetTimestamp` / `SetLifecycle`.
Their Go types map **1:1** onto the setter argument types (the codec
inserts no conversion); `goplan.PlainArrowArrayType` is the single
source of truth for the supported set. `naturalKey` is optional — its
presence selects `SetId`'s two-argument form — and plain fields are
mandatory (no `Option[T]` / slice / roaring). Tagged-value fields
bind to a leeway membership (`<m>`) routing into a section (`<s>`)
optionally targeting a sub-column (`:<col>`, e.g. `u32Range:beginIncl`).

### Go-shape × flag matrix

The DTO field's Go type plus the trailing lw: flags determine the
wire shape. There are five disjoint cases, classified by
`mappingplan.ClassifyBegin`:

| Go shape                    | Flags          | Wire shape                                | Per-attribute call            |
|-----------------------------|----------------|-------------------------------------------|-------------------------------|
| `T`                         | —              | 1 attr · 1 val                            | `BeginAttribute(v)`           |
| `T`                         | `,unit`        | 1 attr · 1 val (HA section, single-slot)  | `BeginAttributeSingle(v)`     |
| `Option[T]`                 | (Has guard)    | 0–1 attr · 1 val                          | per scalar above              |
| `[]T` / `*roaring.Bitmap`   | —              | 1 attr · N vals                           | `BeginAttribute()` + `AddToContainerP(v)*N` |
| `[]T` / `*roaring.Bitmap`   | `,explode`     | N attrs · 1 val                           | per-element `BeginAttribute(v)` |
| `[]T` / `*roaring.Bitmap`   | `,explode,unit`| N attrs · 1 val (HA, single-slot)         | per-element `BeginAttributeSingle(v)` |

The flag is the load-bearing signal — section names are not
inspected. `,unit` alone on a multi-element shape and `,explode`
alone on a scalar shape are rejected; everything else composes.

### Membership channel

Default `LowCardRef` emits a uint64 id via `AddMembershipLowCardRefP`;
the id is declared as a package-level `kindXxx` variable (FactsWrapper
resolves it from vdd in `init()`; NoOpWrapper assigns it as a const
in declaration order). The `,verbatim` flag switches to
`AddMembershipLowCardVerbatimP([]byte("<membership-name>"))` — the
literal name embeds directly on the wire and no kindXxx is declared.
A section's fields must agree on the channel; mixing is rejected
because the read-side dispatch iterator type differs (`iter.Seq[uint64]`
vs `iter.Seq[[]byte]`). The eight channels and their per-channel facts
(method suffix, carrier struct, read accessor, …) live in one table on
`mappingplan.MembershipChannel` — adding a channel is one row, not an
edit across the accessor methods.

### Carrier channels (mixed / parametrized)

The four carrier channels (`mixedLowCardRef`, `mixedLowCardVerbatim`,
`lowCardRefParametrized`, `highCardRefParametrized`) carry the membership
identity as **per-row data** rather than a `kindXxx` id or a literal name.
A carrier field pairs a value field with a `marshalltypes` carrier sibling
on one `(membership, section, channel)` triple; the carrier supplies the
id/name + params to the single `AddMembership…P` call of each attribute. A
carrier section is restricted to one such membership (a per-row identity
cannot be matched against a fixed id on read).

The value field takes the same Go-shape × flag matrix as any other field —
scalar `T`, `option.Option[T]`, a container `[]T`, or `[]T,explode`
(ADR-0008 OQ#4) — with one rule: **the carrier's multiplicity mirrors the
attribute count.** Every shape except `,explode` emits one carrier per
attribute and pairs with a scalar carrier (`marshalltypes.X`); `,explode`
emits one attribute per element and pairs with a slice carrier
(`[]marshalltypes.X`) zipped element-wise with the value slice. The
carrier's slice-ness is its Go type, not a flag — `,explode` stays on the
value field. `PlanBuilder.Finish` rejects a multiplicity mismatch, and the
per-row `len(value) == len(carrier)` agreement is a marshal-time check
(both are independent Go fields). `*roaring.Bitmap` is not accepted on a
carrier channel — a bitmap has no stable element index to pair with a
carrier slice. An empty container value emits no attribute, so its carrier
is not on the wire (splice semantics; SD8's carrier-presence signal is per
*emitted* attribute).

### Constants

A `_` blank-identifier field carrying `lw:"<m>,<s>,const=<value>"`
emits a fixed-string attribute on every row. No Go-side storage —
`Columns` / `Append` / `Row` skip const fields. Composes orthogonally
with `,unit` and `,verbatim`. Multiple `_` consts on the same
membership emit multiple attributes per row (cardinality is bounded
by the schema's membership-spec declaration).

Const fields still need a kindXxx symbol when the channel is ref:
the wrapper's init() resolves the membership name through whatever
registry it consults (pebble's FactsWrapper hits `vdd.Memb<Name>`),
so a const + ref pair requires the membership to be registered the
same way a regular ref field does. Const + `,verbatim` skips the
registry — the literal name embeds directly at the call site.

### Outputs

EmitPlan walks the plan and produces (in order):

```
writeHeader                         // pkg + DO NOT EDIT banner
writeImports(plan, wrapper)         // universal + wrapper-contributed
wrapper.KindVars(plan)              // kindXxx var/const declarations
wrapper.Init(plan)                  // package init() body
wrapper.BeforeCore(plan)            // pool, active-hints, ...
writeColumnsStruct + Len + Append + Row
writeBuildHelper                    // per-section AttrI + SecI + EntityI + BuildEntities
writeFillHelper                     // per-section AttrsReadI + MembsReadI + FillFromArrow
wrapper.AfterCore(plan)             // Marshal, Unmarshal, Codec
```

The schema-agnostic core is the middle four blocks. `BuildEntities`
and `FillFromArrow` are generic functions parameterised over the
derived per-section interfaces; Go's type inference at the call site
binds them against any concrete DML / RA whose method shapes satisfy
the interfaces.

## Invariants

- **Splice semantics.** Empty `[]T` / `Option[T].Has=false` /
  empty `*roaring.Bitmap` produce zero attributes on the wire.
  Leeway has no "present-but-empty" non-scalar representation.
- **No registry consulted during parsing.** Neither `mappingplan`'s
  grammar + validation (`SplitLW`, `PlanBuilder`) nor `marshallgen`'s
  go/ast front-end consult a membership registry. Membership-name typos,
  section / Go-type incompatibilities, and verbatim-vs-ref channel
  mismatches surface at `go build` time of the generated code, not at
  codegen time. The wrapper's `vdd.MembXxx` reference, the typed
  DML / RA at the `BuildEntities` / `FillFromArrow` call site, and
  the chosen `dmlruntime.InAttributeMembership…PI` interface are the
  three compile-time gates.
- **Section name → method PascalCase is convention-only.**
  `methodFor(section)` = `mappingplan.UpperFirst(section)`. The lw:
  section string is trusted verbatim; the Go compiler verifies the
  resulting `GetSection<X>()` call.
- **One channel per section.** All fields targeting a section must
  agree on `Verbatim`. The read-side decode iterates one channel and
  switches on one value type (uint64 or []byte).
- **AttrI carries no F-bounded `[Self]` parameter.** Per-attribute
  methods are P-variants (void); chain returns don't appear in the
  derived interfaces. EntityI's per-section constraint is
  `<Sec>Attr <Kind><Sec>AttrI` (non-recursive), not the F-bounded
  `<Sec>Attr <Kind><Sec>AttrI[<Sec>Attr, <Sec>Sec]` form.
- **One membership per attribute on write.** Both
  `marshallgen.<Kind>BuildEntities` and `marshallreflect.Marshal`
  call `AddMembership*P` exactly once per attribute. The leeway
  wire format permits more, but the codec writers never do.

## Read-side asymmetry between codegen and reflect

The codegen-emitted `<Kind>FillFromArrow` uses an inline switch
inside the membership loop, so if a single attribute happens to carry
memberships for both `Foo` and `Bar`, the value is consumed once per
matched DTO field — both `Foo`'s and `Bar`'s accumulators advance.
The reflect-driven `marshallreflect.Unmarshal` dispatches on the
first matching membership and stops. Both behaviours produce the
same result for codec-written wire (one membership per attribute, by
the invariant above) but diverge for third-party producers of leeway-
shaped data with multi-membership attributes. Codec-wire round-trip
parity is preserved; cross-producer compatibility on multi-membership
input is not. A fix path (split dispatch into "list all matched
fields", consume value per match) is straightforward when a real
consumer surfaces the need.

## Trade-offs

- **Codegen vs reflect.** marshallgen emits typed code at build time
  — zero reflection on the hot path, type errors surface at compile
  time. The sibling `marshallreflect` package uses the same
  `mappingplan.Plan` / `mappingplan.TaggedField` vocabulary at runtime
  via `reflect`, accepting the per-row cost and deferring "wrong type"
  errors to runtime. The marshallgen wire output and a marshallreflect
  wire output must round-trip through each other for the same DTO;
  verified by a shared round-trip test.
- **Verbosity at the source level for the cost of zero-overhead
  binding at runtime.** EntityI for an N-section DTO carries 2N+1
  type parameters; BuildEntities mirrors. Generated code is verbose
  but every type parameter is bound by one-argument inference at the
  call site, and there's no runtime dispatch cost.
- **Per-schema dispatch lives outside marshallgen.** The wrapper
  picks the membership-id source, the dml builder type, the active-
  hints computation, and the buscodec wiring. Adding a new target
  schema is a new wrapper implementation, not a marshallgen patch.
- **Plain columns are a closed set of roles, open in type.** The four
  plain column names (`id` / `naturalKey` / `ts` / `expiresAt`) are the
  fixed entity-header roles every leeway schema exposes via `SetId` /
  `SetTimestamp` / `SetLifecycle`. The *names* are not user-extensible,
  but the Go *types* are taken 1:1 from the DTO (constrained only to the
  `goplan.PlainArrowArrayType` set), so the plain wiring is no
  longer coupled to one schema's column types. Data outside these four
  roles is carried as per-field tagged values.

## Further reading

- Model: [`mappingplan/`](../mappingplan/) — the shared DTO model both front-ends build on: `Plan`, the `lw:` grammar (`SplitLW`), `PlanBuilder` validation, the membership channels, section grouping, and field-shape classification.
- Sibling: [`marshallreflect/`](../marshallreflect/) — runtime-reflection codec over the same `mappingplan.Plan` model.
- Wrapper consumer: `keelson/runtime/codec/factswrapper/` — facts target.
- Splice semantics: project-memory note `reference_leeway_splice_semantics.md` — empty non-scalars vanish on the wire (codec authors must emit 0 attributes for empty collections).
