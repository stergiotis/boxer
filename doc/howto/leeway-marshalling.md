---
type: how-to
audience: engineer with a specific task
status: stable
reviewed-by: "@stergiotis"
reviewed-date: 2026-07-10
---

# How to marshal a Go struct to and from a leeway table

This recipe maps a typed Go struct — a *DTO* — onto a leeway columnar table and
reads it back, using the marshall stack. It documents the behaviour in force as
of 2026-07-05 and does not argue the design; the ADRs and package docs in
[Further reading](#further-reading) carry the *why* and the authoritative method
contracts.

Two front-ends share one model. [The `marshallgen` generator](../../public/semistructured/leeway/marshall/go/marshallgen/)
emits a codec at build time; [the `marshallreflect` codec](../../public/semistructured/leeway/marshall/go/marshallreflect/)
drives the same mapping at runtime through `reflect`. Both parse the same `lw:`
struct-tag grammar into a [`mappingplan.Plan`](../../public/semistructured/leeway/mappingplan/)
via the shared [`goplan` plan builder](../../public/semistructured/leeway/marshall/go/goplan/),
so they accept exactly the same DTOs — and their wire output must be
**byte-identical** (the load-bearing invariant, see [below](#the-invariant-you-preserve)).

## When to use which front-end

- **codegen (`marshallgen`)** — the hot path. Typed code, no reflection per row,
  type errors at compile time. Production ingestion uses this.
- **reflect (`marshallreflect`)** — DTOs not known to a generator pass (config
  files, ad-hoc tooling, tests that drive several DMLs from one DTO). Pays
  per-row reflection cost; defers "wrong type" errors to runtime.

They round-trip through each other. Author the DTO once; pick the driver per use.

## Prerequisites

- **Build tags on every `go` command:** `-tags="$(cat ./tags)"`. Without them the
  leeway packages fail to compile with misleading "undefined" errors
  ([AGENTS.md §Build & test](../../AGENTS.md)).
- **A leeway schema with generated DML + RA classes** — e.g. `anchor`, or
  pebble's `runtime.facts`. The codec *drives* those builder/reader classes; it
  does not define the schema. A DTO field names a membership and a section that
  the schema already declares; the Go compiler (codegen) or `Validate` (reflect)
  checks the binding.
- **A membership resolver for ref channels** (the default channel). codegen
  declares a package-level `kindXxx` symbol per ref membership and lets a
  `WrapperEmitterI` fill it (pebble's `factswrapper` resolves it from `vdd`);
  reflect takes a `LookupI` at call time — or `NoLookup{}` when every membership
  in the DTO carries `,verbatim`.

## The DTO in one page

```go
type MyDTO struct {
    _ struct{} `kind:"myEntity"`                 // entity kind — required, exactly once
    _ struct{} `lw:"<m>,<s>,const=<value>"`      // optional: a constant attribute

    Id       uint64 `lw:",id"`                   // plain row column (entity header)
    Name     string `lw:"name,symbol"`           // tagged value → membership `name`, section `symbol`
    Tags     []string `lw:"tag,symbol"`          // one container attribute carrying N values
}
```

- The **`_` kind field** carries `kind:"<entityKind>"`. Exactly one is required.
- A **plain column** has an *empty* membership: `lw:",<role>"`. The role is one of
  `id` / `naturalKey` / `ts` / `expiresAt` — the entity-header slots that drive
  `SetId` / `SetTimestamp` / `SetLifecycle`. `id` is mandatory.
- A **tagged field** binds `lw:"<membership>,<section>[:<sub-column>][,<flag>…]"`.
- The tag string is trusted verbatim; nothing here consults a registry. A typo in
  a membership or section name surfaces at `go build` of the generated code
  (codegen) or as a `mustCall` panic / `Validate` error (reflect), not at
  plan-build time.

## Map the entity header

```go
_        struct{}  `kind:"person"`
Id       uint64    `lw:",id"`         // required
NaturalK []byte    `lw:",naturalKey"` // optional — its presence selects SetId(id, naturalKey)
Ts       time.Time `lw:",ts"`         // optional
ExpiresAt time.Time `lw:",expiresAt"` // optional
```

Plain fields are scalar `T` only — no `Option`, slice, or roaring — and their Go
type is passed to the setter **1:1** with no conversion (`goplan.PlainArrowArrayType`
is the supported set). Top-level `[]byte` is a scalar byte-string (fine for
`naturalKey`), *not* a slice.

## Map a value: the Go-shape × flag matrix

For a single-sub-column section, the field's Go type plus the trailing flags fix
the wire shape (classified by `mappingplan.ClassifyBegin`):

| Go type                   | Flags            | Wire shape           | Per-attribute call |
|---------------------------|------------------|----------------------|--------------------|
| `T`                       | —                | 1 attr · 1 val       | `BeginAttribute(v)` |
| `T`                       | `,unit`          | 1 attr · 1 val (HA)  | `BeginAttributeSingle(v)` |
| `option.Option[T]`        | (Has guard)      | 0–1 attr · 1 val     | as scalar above |
| `[]T` / `*roaring.Bitmap` | —                | 1 attr · N vals      | `BeginAttribute()` + `AddToContainerP(v)`×N |

The flag is the signal; section names are not inspected. `,unit` requires a
scalar shape. The former N×1 shapes (`,explode` — one attribute per element)
were removed by [ADR-0113](../adr/0113-leeway-marshall-nested-primary-consolidation.md)
D1: author a nested `[]Attr` section instead (see the
[nested how-to](leeway-marshalling-nested.md)). `,ct=<canonical>` may
**relabel** a field's canonical type (e.g. a `uint32` as IPv4, or a `[]byte`
blob as the `u8` array lane) without reshaping its Go type.

**Splice semantics.** An empty `[]T`, a `*roaring.Bitmap` that is nil/empty, and
`Option[T]{Has:false}` all emit **zero attributes**. Leeway has no
"present-but-empty" non-scalar; a codec author never writes one.

## Choose a membership channel

A **channel** fixes how a field's *membership identity* rides the wire — the
field's *value* still lands in the section named by the tag. Append the channel
flag; the default (no flag) is `lowCardRef`. The eight channels form a
cardinality × identity grid ([ADR-0072](../adr/0072-leeway-membership-carriage.md),
[ADR-0008](../adr/0008-leeway-marshall-extensions.md)): a **low-** or
**high-cardinality** dictionary crossed with a **ref** id, a **verbatim** name,
or **per-row** carrier data. The realized eight are a sparse subset of the grid —
there is deliberately no mixed-high-card channel.

| Flag | Identity on the wire | Resolver |
|------|----------------------|----------|
| *(none)* / `lowCardRef` | `uint64` id | `kindXxx` (codegen) / `LookupI` (reflect) |
| `highCardRef` | `uint64` id | same |
| `verbatim` / `lowCardVerbatim` | the literal name as `[]byte` | none — embeds directly |
| `highCardVerbatim` — tuple `@membership` / nested memberships / DML only (the value-field spelling was removed, ADR-0113 D1) | literal name `[]byte` | none |
| `mixedLowCardRef` | per-row `uint64` id **+ params** | a `marshalltypes.MixedLowCardRef` sibling |
| `mixedLowCardVerbatim` | per-row `[]byte` name **+ params** | a `marshalltypes.MixedLowCardVerbatim` sibling |
| `lowCardRefParametrized` / `highCardRefParametrized` | per-row opaque params blob | a `marshalltypes.Parametrized` sibling |

The snippets below are the tag *shapes* you write; the schema's section must
declare the channel's `AddMembership…P` method — the codec drives it, it does not
define it, and an absent method fails at `go build` / `Validate` (no in-tree
schema declares all eight on one section).

**The four simple channels** name the membership once, in the tag, and every row
on the section shares it. `ref` resolves that name to a `uint64` id (via
`kindXxx` / `LookupI`); `verbatim` embeds the name literally as `[]byte`. Low vs
high card only picks the dictionary the section builds — the Go shape is
identical:

```go
Author  string `lw:"author,symbol"`              // lowCardRef (default): membership `author` → kindAuthor uint64
Session string `lw:"session,symbol,highCardRef"` // highCardRef: same identity, high-card dictionary
Locale  string `lw:"locale,symbol,verbatim"`     // lowCardVerbatim: the bytes "locale" embed on the wire
```

(`highCardVerbatim` has no value-field spelling — reach it from a tuple
element's `@membership` field, a nested membership marker, or hand-written
DML; ADR-0113 D1.)

**The four carrier channels** let the membership identity vary **row by row**, so
it can't be a fixed name in the tag: it rides in a `marshalltypes` sibling field
sharing the value's full `(membership, section, channel)` triple. The tag's
membership slot is then only the design-time pairing/grouping key — the wire
identity comes entirely from the carrier:

```go
// mixedLowCardRef — per-row (uint64 Id + []byte Params); the in-tree example is sensorreading.go
Reading  string                        `lw:"sensor,symbol,mixedLowCardRef"`
ReadingC marshalltypes.MixedLowCardRef `lw:"sensor,symbol,mixedLowCardRef"`

// mixedLowCardVerbatim — per-row (literal []byte Name + []byte Params)
Metric  string                             `lw:"gauge,symbol,mixedLowCardVerbatim"`
MetricC marshalltypes.MixedLowCardVerbatim `lw:"gauge,symbol,mixedLowCardVerbatim"`

// lowCardRefParametrized — the whole identity is one opaque []byte Params blob
Signal  string                     `lw:"probe,symbol,lowCardRefParametrized"`
SignalC marshalltypes.Parametrized `lw:"probe,symbol,lowCardRefParametrized"`

// highCardRefParametrized — same Parametrized carrier, high-card dictionary
Trace  string                     `lw:"span,symbol,highCardRefParametrized"`
TraceC marshalltypes.Parametrized `lw:"span,symbol,highCardRefParametrized"`
```

Carriers are **scalar-only** — one `marshalltypes.X` per attribute, whatever
the value shape (scalar / `Option` / container; the element-wise
`[]marshalltypes.X` slice pairing went with `,explode`, ADR-0113 D1).
`Parametrized` serves both parametrized channels — the flag, not the carrier
type, picks low- vs high-card. `Params` is wire-emitted even when empty:
carrier *presence*, not params content, is the "attribute is here" signal.

**One channel per section.** All fields targeting a section must agree on the
channel — the read-side decode iterates a single channel whose iterator element
type (`uint64` vs `[]byte`) differs per channel. Ref-channel membership names
must be Go identifiers (they become the `kindXxx` symbol); verbatim names are
arbitrary; and a carrier section holds one membership only.

## Map several sub-columns as one attribute (multi-sub-column)

A section that declares two or more physical sub-columns — a scalar plus
containers, or several containers — is a **multi-sub-column section**
([ADR-0101](../adr/0101-leeway-marshall-mixed-shape-sections.md)). Declare **one
Go field per sub-column** with a `:<column>` suffix; all share one membership and
form **one attribute per row**:

```go
// anchor `text` section: text (scalar) + wordLength + wordBag (co-containers).
Text       string   `lw:"prose,text:text"`
WordLength []uint32 `lw:"prose,text:wordLength"`
WordBag    []string `lw:"prose,text:wordBag"`
```

- **scalar** sub-columns (`T`) become the `BeginAttribute(<scalars…>)` arguments;
- **container** sub-columns (`[]T`) zip through `AddToContainerP` (exactly one
  container) or `AddToCoContainersP(c1,…,cK)` (two or more), one call per element.

Within each class the DTO field order must match the schema's column order (a
positional contract — the Go compiler catches it only when the sub-column types
differ). All container fields must have **equal length** per row (checked at
marshal time; a mismatch is an error, never a panic). With ≥1 scalar sub-column
the attribute always emits (empty containers are legal, N = 0); an all-container
section with every container empty is spliced.

## Map many attributes into one section (dynamic-membership tuple)

When one section needs **many attributes, each with its own membership** — the
shape the static grammar rejects — declare a **slice-of-struct** field tagged
with the bare section name
([ADR-0103](../adr/0103-leeway-marshall-dynamic-membership-tuples.md),
[ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md)):

```go
type LabeledText struct {
    Label      string   `lw:"@membership,verbatim"` // per-attribute membership
    Text       string   `lw:"text:text"`
    WordLength []uint32 `lw:"text:wordLength"`
    WordBag    []string `lw:"text:wordBag"`
}
Texts []LabeledText `lw:"text"` // N elements → N attributes, in slice order
```

Each element emits one attribute (the multi-sub-column call sequence above, with
the membership taken from the element's `@membership` field). An element declares
**one or more** `@membership` fields — `string` / `[]byte` on a verbatim channel
(the literal name), or `uint64` on a ref channel (the id carried **directly**, no
lookup — ADR-0109). A repeated `[]T` `@membership` is the sole membership on its
channel. The tuple **owns its section exclusively**: no static field, const, or
second tuple may target it. An element always emits (its slice presence is the
signal); zero elements emit zero attributes. On read, each attribute decodes to
one element in wire order.

The tuple's SoA column is `[][]Elem` — a struct leaf, AoS at attribute grain (the
wire still re-columnarises to one Arrow array per sub-column). Carrier channels
are rejected inside a tuple; `<Kind>ReadRow` does not cover tuple kinds.

## Emit a constant

A `_` field emits a fixed-string attribute on every row, with no Go-side storage:

```go
_ struct{} `lw:"source,symbol,const=ingest-v2"`
```

Composes with `,unit` and `,verbatim`; several consts on one membership emit
several attributes. A const on a ref channel still needs the membership resolved
(the wrapper's `kindXxx`); const + `,verbatim` embeds the name directly.

## Generate the codec (codegen path)

Run the generator over the DTO source file:

```sh
./boxer.sh keelsoncodec --target=anchor  path/to/mydto.go
# or, explicitly:  go run -tags "$(cat ./tags)" ./public/app keelsoncodec --target=facts path/to/mydto.go
```

`--target` picks the `WrapperEmitterI` (`anchor` = `NoOpWrapper`, schema-agnostic
surface only; `facts` = pebble's `factswrapper`, adds `kindXxx` resolution +
`Marshal` / `Unmarshal` / bus codec). It writes `mydto.out.go` next to the source
carrying:

- `<Kind>Columns` — the SoA batch (one slice per DTO field), plus `Len`,
  `Append`, and a row-extract adapter;
- `<Kind>BuildEntities(dml, cols)` — the generic write helper;
- `<Kind>FillFromArrow(...)` — the generic read helper;
- the derived per-section interfaces they bind against by Go type inference.

The `.out.go` is checked in. After editing the DTO, regenerate and confirm it is
**byte-stable** except for your intended change (`go build`/`test` with the tags).

## Marshal & read back (reflect path)

```go
// Write: preflight the DML, marshal rows, drain to Arrow records.
if err := marshallreflect.Validate[MyDTO](dml); err != nil { /* mis-wired DML */ }
if err := marshallreflect.Marshal(dml, rows, lookup); err != nil { … }
recs, err := dml.TransferRecords(nil) // wire bytes live in the records

// Read: bind each section's RA readers, register, unmarshal.
readers := marshallreflect.NewSectionReaders(idR.Len()).
    PlainColumn("id", idR.ValueId).
    Section("symbol", symR.GetAttributes(), symR.GetMemberships())
var out []MyDTO
err = marshallreflect.Unmarshal(readers, &out, lookup)
```

`Validate[T]` reports every missing / wrong-arity DML method in one error
before the first row (otherwise a mismatch panics mid-marshal via `mustCall`). `SectionReaders`
runs an up-front coverage check so a forgotten reader is one clear error, not a
nil dereference at row *i*. Pass `NoLookup{}` when the DTO is all-verbatim. Use
`PlanFor[MyDTO]()` to inspect the plan without marshalling.

## The invariant you preserve

For one DTO, the bytes from **codegen** `<Kind>BuildEntities`, from **reflect**
`Marshal`, and from a **hand-written DML loop** must be equal, and must round-trip
back to equal DTOs. Changes are checked by `array.RecordEqual` + Arrow IPC byte
equality + cross-decode (gen-write → reflect-read and vice versa), and every
in-tree `.out.go` regenerates byte-stable. If you touch the shared plan/grouping
layer, that whole matrix is your regression gate.

## What fails at plan time (the boundaries)

Both front-ends reject these before any wire is written, so an unrepresentable
DTO fails at `PlanFor` / `Validate`, never as a `reflect` panic:

- **Removed grammar** (ADR-0113 D1): `,explode` anywhere, and
  `,highCardVerbatim` on a value field — each names its replacement in the
  error.
- **Plain field** carrying a channel / `unit` / `const` / `ct=` flag,
  or an `Option` / slice / roaring shape; a missing `id`; an unknown role.
- **Multi-sub-column section** with: more than one field on a sub-column; more
  than one membership; a const, `Option[T]`, `*roaring.Bitmap`, `,unit`, or
  carrier channel. (This is the rule the tuple exists to work around.)
- **Tuple**: a second field/const/tuple on a tuple-owned section; a missing or
  wrongly-typed `@membership`; a ref-typed field on a verbatim channel (or the
  reverse); a repeated `@membership` mixed with another field on one channel; a
  foreign-package or stray element struct.
- **Channel mix** within one section; a **ref membership** that is not a Go
  identifier; a top-level membership beginning with `@` (reserved for tuples).
- **Carrier** value without its sibling (or a channel/multiplicity mismatch).

`<Kind>ReadRow` (ADR-0100 store reads) additionally excludes tuple and carrier
kinds — `ReadRowSupported` reports the reason.

## Shape cheat-sheet

| I want to map… | Go type | tag |
|----------------|---------|-----|
| the entity id / natural key / timestamp | `uint64` / `[]byte` / `time.Time` | `lw:",id"` / `lw:",naturalKey"` / `lw:",ts"` |
| one value | `T` | `lw:"m,section"` |
| an optional value | `option.Option[T]` | `lw:"m,section"` |
| a bag of values (one attribute) | `[]T` | `lw:"m,section"` |
| a value per element (N attributes) | `[]Attr` nested section | see the [nested how-to](leeway-marshalling-nested.md) |
| a literal-named membership | `T` / `[]T` | `lw:"m,section,verbatim"` |
| a scalar + co-containers as one attribute | one field per sub-column | `lw:"m,section:col"` |
| many attributes, membership per attribute | `[]ElemStruct` | `lw:"section"` (element uses `@membership`) |
| a constant on every row | `_ struct{}` | `lw:"m,section,const=…"` |

## Further reading

- [`marshallreflect` package doc](../../public/semistructured/leeway/marshall/go/marshallreflect/) — the runtime codec, the full DML write contract, and the RA read contract (`pkgsite` is canonical).
- [`marshallgen` EXPLANATION](../../public/semistructured/leeway/marshall/go/marshallgen/EXPLANATION.md) — how the generator works, the channel table, the read-side asymmetry, and the emit trade-offs.
- [The `goplan` toolkit](../../public/semistructured/leeway/marshall/go/goplan/) and [the `mappingplan` model](../../public/semistructured/leeway/mappingplan/) — the shared tag grammar, `PlanBuilder` validation, section grouping, and the membership channels.
- Worked DTOs: [`anchor/codecdemo/`](../../public/semistructured/leeway/anchor/codecdemo/) — `textdoc` (multi-sub-column), `labeledtextdoc` (tuple), `lineagedoc` (multi-membership + ref tuple), `sensorreading` (carriers).
- Decisions: [ADR-0074](../adr/0074-leeway-marshall-package-layout.md) (package layout), [ADR-0101](../adr/0101-leeway-marshall-mixed-shape-sections.md) (mixed shapes), [ADR-0103](../adr/0103-leeway-marshall-dynamic-membership-tuples.md) / [ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md) (tuples), [ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md) (`ReadRow` / store).
