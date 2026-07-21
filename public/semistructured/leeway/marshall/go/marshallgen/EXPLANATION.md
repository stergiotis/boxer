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
[`mappingplan.Plan`](../../../mappingplan/) and emits a `.out.go` carrying typed
SoA columns plus generic `<Kind>BuildEntities` / `<Kind>FillFromArrow`
helpers that bind to any leeway DML / RA via Go's type inference at the
call site.

The DTO model itself is not defined here. The `Plan` and the
`MembershipChannel` table live in
[the `mappingplan` model](../../../mappingplan/); the `lw:` tag grammar
(`SplitLW`), per-field validation and assembly (`PlanBuilder`), section
grouping (`ComputeGroups`), and field-shape classification
(`ClassifyBegin`) live in [the sibling `goplan` toolkit](../goplan/),
shared with the reflect front-end. `marshallgen` is the go/ast front-end
plus emitter over that stack;
[the reflect-driven `marshallreflect` codec](../marshallreflect/) is the
other front-end and a runtime codec over the same stack. The two front-ends share
`mappingplan` + `goplan` and do not depend on each other; the front-end
**parity corpus** (`marshallreflect_test/parity_corpus_test.go`) gates their
accept sets against each other mechanically.

Schema-specific wiring (membership-id resolution, builder pools,
Marshal / Unmarshal / bus-codec wrappers) lives behind a
`WrapperEmitterI` hook the caller passes in. The keelson facts target
(`keelson/runtime/codec/factswrapper`) implements the hook for
`boxer.facts`; anchor uses the bundled `NoOpWrapper`. The same generator
drives both.

## Background

A leeway schema (`boxer/public/semistructured/leeway`) declares
**sections** that carry **attributes**; each attribute belongs to one
or more **memberships** (named keys identifying which axis the value
populates). The schema's DDL / DML / RA generators produce typed
builder + reader classes per schema. `marshallgen` produces the
codec on top — given a DTO whose fields name memberships, it emits
the per-row chain that drives the DML and the matching per-row read
from the RA.

The lift into this repository was motivated by (i) two consumer
schemas (`boxer.facts`, `anchor`) demonstrating the same emit
shape works generically, and (ii) the wrapper / core split letting
schema-coupled code live in the consumer instead of behind a
target flag in the generator.

**Who authors DTOs** is settled by
[ADR-0113](../../../../../../doc/adr/0113-leeway-marshall-nested-primary-consolidation.md):
humans hand-write simple DTOs and escalate via the nested attribute model;
rich models are **generated**, and generators target the flat spellings —
including the frozen escalation spellings (`:column`, `@membership`) — as
their permanent IR (plain tags, no marker imports, compile-time safety moot
for machine output). The full authoring reference is the
[marshalling how-to](../../../../../../doc/howto/leeway-marshalling.md);
this document explains how the generator works underneath it.

## How it works

### Inputs

A DTO is a single struct in a single file (plus any tuple / nested element
structs it references, in the same file). The flat grammar:

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
wire shape. There are four disjoint cases, classified by
`goplan.ClassifyBegin`:

| Go shape                    | Flags          | Wire shape                                | Per-attribute call            |
|-----------------------------|----------------|-------------------------------------------|-------------------------------|
| `T`                         | —              | 1 attr · 1 val                            | `BeginAttribute(v)`           |
| `T`                         | `,unit`        | 1 attr · 1 val (HA section, single-slot)  | `BeginAttributeSingle(v)`     |
| `Option[T]`                 | (Has guard)    | 0–1 attr · 1 val                          | per scalar above              |
| `[]T` / `*roaring.Bitmap`   | —              | 1 attr · N vals                           | `BeginAttribute()` + `AddToContainerP(v)*N` |

The flag is the load-bearing signal — section names are not
inspected. `,unit` requires a scalar shape. The former N×1 shapes
(`,explode` / `,explode,unit` — one attribute per element) were removed by
ADR-0113 D1; the per-element spelling is a nested `[]Attr` section, whose
emit reuses the tuple machinery below.

### Multi-sub-column sections (incl. mixed shapes)

Fields sharing one section under distinct `:<column>` suffixes form a
**multi-sub-column section**: one tuple attribute per row, one Go field
per sub-column, a single membership. The sub-columns partition into two
classes ([ADR-0101](../../../../../../doc/adr/0101-leeway-marshall-mixed-shape-sections.md)):

- **scalars** (`T` fields) become the `BeginAttribute(<scalars…>)`
  arguments (an all-container section opens with `BeginAttribute()`);
- **containers** (`[]T` fields) zip through
  `AddToContainerP(v)` (exactly one container) or
  `AddToCoContainersP(c1, …, cK)` (two or more), one call per element —
  so every container of the section carries the same per-attribute
  length, checked at marshal time.

Within each class the DTO declaration order must match the schema's
column order — the same positional contract the all-scalar tuple
already carried (the schema-generated named `Add` removes the hazard for
hand-written DML). With S ≥ 1 scalars the attribute always emits (empty
containers are legal, N = 0); an all-container tuple with every
container empty is spliced, and its row decodes to nil slices. The
read side pairs a direct accessor per scalar sub-column with an
`iter.Seq` accessor per container sub-column. `Option[T]`,
`*roaring.Bitmap`, `,unit`, consts and carrier channels
are rejected in such sections at plan time (`goplan.PlanBuilder.Finish`),
so both front-ends and `marshallreflect.Validate` refuse the same DTOs.

### Dynamic-membership tuple sections

A static multi-sub-column section carries a **single** membership; a
DTO needing MANY attributes in one section — each with its own
membership(s) — declares a **dynamic-membership tuple**
([ADR-0103](../../../../../../doc/adr/0103-leeway-marshall-dynamic-membership-tuples.md),
extended by [ADR-0109](../../../../../../doc/adr/0109-leeway-marshall-multi-membership-ref-tuples.md)):
a slice of a named element struct, tagged with the bare section name.
The element struct spells **one or more** `@membership` fields plus one
value field per sub-column as `<section>:<column>`:

```go
type LabeledText struct {
    Label      string   `lw:"@membership,verbatim"`
    Text       string   `lw:"text:text"`
    WordLength []uint32 `lw:"text:wordLength"`
    WordBag    []string `lw:"text:wordBag"`
}
Texts []LabeledText `lw:"text"` // N elements → N attributes
```

Each element emits one attribute — the multi-sub-column call sequence
above with one `AddMembership…P` call per membership field — in slice
order; the zip-length guard applies per element; an element always emits
(no per-element splice) and zero elements emit zero attributes.

A membership field is `string` / `[]byte` on a **verbatim** channel (the
literal name embeds on the wire) or `uint64` on a **ref** channel — the id
is carried **directly** per element, no `kindXxx` symbol and no lookup
(ADR-0109). An element may declare several membership fields, possibly on
heterogeneous channels, and a repeated `[]T` membership field carries N
memberships on one attribute (the sole membership of its channel). Carrier
channels are rejected inside a tuple — their identity is per-row carrier
data, not an element field.

The element struct must live in the DTO's file (the go/ast
front-end resolves it there; a file may declare the DTO plus its
element structs — the DTO is the one carrying the `_` kind field). The
tuple owns its section exclusively and works at any sub-column count
(S + C ≥ 1). The SoA column is the outer `[][]Elem` slice — jagged like
every `[][]T` container column, with a struct leaf, so within one row's
attribute list the sub-column values are interleaved per element (AoS
at attribute grain; the wire stays one Arrow array per physical
sub-column, and columnar scans belong on the Arrow record, not on the
staging `Columns`). `FillFromArrow` appends one element per attribute in
wire order; `ReadRow` does not cover tuple kinds (like carriers and
const-only kinds — `ReadRowSupported` names the reason).

### Nested attribute sections

The nested model
([ADR-0113](../../../../../../doc/adr/0113-leeway-marshall-nested-primary-consolidation.md),
the primary hand-authoring escalation surface) reaches the same machinery
through types instead of tag spellings: an attribute struct's fields are
classified into membership markers (`lw.Ref` / `lw.Verbatim` / …), scalar
sub-columns, and container sub-columns; the *section field's* multiplicity
(`S` / `option.Option[S]` / `[]S`) is the attributes-per-row cardinality.
Static-membership nested sections and dynamic-membership `[]Attr` tuples
both lower onto the tuple emit path above (`goplan.AddNestedSliceField` /
`AddTupleSliceField` feed the same builder); an Optional section's SoA
column decomposes into `<F>Val []S` + `<F>Has []bool`, mirroring the
scalar-Option split.

One deliberate front-end asymmetry: **entity-level value markers**
(`lw.Single`, the lane types) ship in reflect only — this generator
rejects them with a clear plan-time error (the top-level marker→plain
bridge across SoA / `Append` / decode was attempted once and reverted, and
with generation settled on the flat IR it is not planned). A codegen'd DTO
spells those `,unit` / `,ct=`. The parity corpus records each asymmetry
with its documentation reference.

### Membership channel

Default `LowCardRef` emits a uint64 id via `AddMembershipLowCardRefP`;
the id is declared as a package-level `kindXxx` variable (the facts
wrapper resolves it from `vdd` in `init()`; NoOpWrapper assigns it as a
const in declaration order). The `,verbatim` flag switches to
`AddMembershipLowCardVerbatimP([]byte("<membership-name>"))` — the
literal name embeds directly on the wire and no kindXxx is declared.
(`highCardVerbatim` survives only as a tuple `@membership` / nested /
DML channel — its value-field spelling was removed by ADR-0113 D1.)
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
scalar `T`, `option.Option[T]`, or a container `[]T` (ADR-0008 OQ#4) — and
the carrier is always **scalar** (`marshalltypes.X`, one per attribute; the
element-wise `[]marshalltypes.X` slice pairing was removed with `,explode`,
ADR-0113 D1). `*roaring.Bitmap` is not accepted on a carrier channel. An
empty container value emits no attribute, so its carrier is not on the wire
(splice semantics; SD8's carrier-presence signal is per *emitted*
attribute). The parametrized pair currently has no consumer and is parked
under a re-arm trigger (ADR-0113 D5).

### Constants

A `_` blank-identifier field carrying `lw:"<m>,<s>,const=<value>"`
emits a fixed-string attribute on every row. No Go-side storage —
`Columns` / `Append` / `Row` skip const fields. Composes orthogonally
with `,unit` and `,verbatim`. Multiple `_` consts on the same
membership emit multiple attributes per row (cardinality is bounded
by the schema's membership-spec declaration).

Const fields still need a kindXxx symbol when the channel is ref:
the wrapper's init() resolves the membership name through whatever
registry it consults (the facts wrapper hits `vdd.Memb<Name>`),
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

- **Byte-identity across producers.** For one DTO, `<Kind>BuildEntities`,
  `marshallreflect.Marshal`, and a hand-written DML loop must emit equal
  bytes (`array.RecordEqual` + Arrow IPC equality + cross-decode +
  nested-vs-flat equal records), and every in-tree `.out.go` regenerates
  byte-stable.
- **Front-end parity is gated mechanically.** The parity corpus runs one
  DTO set through `ParsePlan` and `PlanFor`, asserting identical
  accept / reject and, where both accept, equal plans; a divergence
  without a documentation citation fails.
- **Splice semantics.** Empty `[]T` / `Option[T].Has=false` /
  empty `*roaring.Bitmap` produce zero attributes on the wire.
  Leeway has no "present-but-empty" non-scalar representation.
- **No registry consulted during parsing.** Neither `goplan`'s
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
  agree on the channel. The read-side decode iterates one channel and
  switches on one value type (uint64 or []byte).
- **AttrI carries no F-bounded `[Self]` parameter.** Per-attribute
  methods are P-variants (void); chain returns don't appear in the
  derived interfaces. EntityI's per-section constraint is
  `<Sec>Attr <Kind><Sec>AttrI` (non-recursive), not the F-bounded
  `<Sec>Attr <Kind><Sec>AttrI[<Sec>Attr, <Sec>Sec]` form.
- **One membership per attribute on static-section write.** For static
  sections both codecs call `AddMembership*P` exactly once per attribute.
  Dynamic tuples legitimately carry several memberships per attribute —
  one call per element membership field / repeated-membership element
  (ADR-0109) — decoded by the dedicated tuple path.

## Read-side asymmetry between codegen and reflect

For **static** sections, the codegen-emitted `<Kind>FillFromArrow` uses an
inline switch inside the membership loop, so if a single attribute happens
to carry memberships for both `Foo` and `Bar`, the value is consumed once
per matched DTO field — both `Foo`'s and `Bar`'s accumulators advance.
The reflect-driven `marshallreflect.Unmarshal` dispatches on the
first matching membership and stops. Both behaviours produce the
same result for codec-written wire (one membership per static-section
attribute, by the invariant above) but diverge for third-party producers
of leeway-shaped data with multi-membership attributes on static
sections. Codec-wire round-trip parity is preserved; cross-producer
compatibility on such input is not. A fix path (split dispatch into
"list all matched fields", consume value per match) is straightforward
when a real consumer surfaces the need. (Tuple sections are unaffected —
their decode path handles multi-membership attributes by design.)

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

- Authoring: [doc/howto/leeway-marshalling.md](../../../../../../doc/howto/leeway-marshalling.md) — the single recipe (simple subset, nested escalation, frozen flat spellings).
- Decision record: [ADR-0113](../../../../../../doc/adr/0113-leeway-marshall-nested-primary-consolidation.md) — nested primary, the D1 cull, the generation-IR resolution.
- Model: [`mappingplan/`](../../../mappingplan/) — the shared Plan IR + the membership channel table; [`goplan/`](../goplan/) — the shared grammar, `PlanBuilder` validation, grouping, and shape classification.
- Sibling: [`marshallreflect/`](../marshallreflect/) — runtime-reflection codec over the same stack.
- Wrapper consumer: `keelson/runtime/codec/factswrapper/` — the facts target.
