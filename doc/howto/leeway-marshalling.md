---
type: how-to
audience: engineer with a specific task
status: stable
reviewed-by: "@stergiotis"
reviewed-date: 2026-07-19
---

# How to marshal a Go struct to and from a leeway table

This recipe maps a typed Go struct — a *DTO* — onto a leeway columnar table and
reads it back, using the marshall stack. It documents the behaviour in force as
of 2026-07-19 and does not argue the design; the ADRs and package docs in
[Further reading](#further-reading) carry the *why* and the authoritative method
contracts.

One authoring model, two levels
([ADR-0113](../adr/0113-leeway-marshall-nested-primary-consolidation.md)):

- the **simple subset** — flat `lw:` one-liners for header roles, scalar /
  optional / bag values, constants, and channel flags. Most DTOs never leave it.
- the **nested attribute model** — what you escalate to when a section outgrows
  a one-line tag: mixed scalar/container shapes, per-row (dynamic) memberships,
  several memberships per attribute, N attributes per row. A section becomes a
  Go struct; the struct's field *types* carry every role.

The simple subset is the nested model's degenerate case — one spelling, not
two systems. The older **flat escalation spellings** (`:column` sub-columns and
`@membership` tuples) remain supported but **frozen** — no new features. They
are also the **generation IR**: DTO generators emit them (plain tags, no marker
imports — ADR-0113's settled generated-over-hand-authored answer), so they are
documented [near the end](#the-frozen-flat-escalation-spellings) for existing
DTOs and generator authors.

Two front-ends share the model. [The `marshallgen` generator](../../public/semistructured/leeway/marshall/go/marshallgen/)
emits a codec at build time; [the `marshallreflect` codec](../../public/semistructured/leeway/marshall/go/marshallreflect/)
drives the same mapping at runtime through `reflect`. Both parse the same
grammar into a [`mappingplan.Plan`](../../public/semistructured/leeway/mappingplan/)
via the shared [`goplan` plan builder](../../public/semistructured/leeway/marshall/go/goplan/),
and their wire output must be **byte-identical** (the load-bearing invariant,
see [below](#the-invariant-you-preserve)). The few deliberate accept-set
differences are listed under [Deferred surfaces](#deferred-surfaces) and gated
mechanically by the front-end parity corpus
(`marshallreflect_test/parity_corpus_test.go`).

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
  keelson's `boxer.facts`. The codec *drives* those builder/reader classes; it
  does not define the schema. A DTO field names a membership and a section that
  the schema already declares; the Go compiler (codegen) or `Validate` (reflect)
  checks the binding.
- **A membership resolver for ref channels** (the default channel). codegen
  declares a package-level `kindXxx` symbol per ref membership and lets a
  `WrapperEmitterI` fill it (keelson's `factswrapper` resolves it from `vdd`);
  reflect takes a `LookupI` at call time — or `NoLookup{}` when every membership
  in the DTO carries `,verbatim`.
- **The `lw` marker package** (nested model only) — channel markers (`lw.Ref`,
  `lw.Verbatim`, …), the value-shape marker `lw.Single`, and the lane types
  (`lw.IPv4`, `lw.IPv6`, the CIDR prefixes). Each replaces a flat-grammar flag
  with a *type*.

## The DTO in one page

```go
type MyDTO struct {
    _ struct{} `kind:"myEntity"`                 // entity kind — required, exactly once
    _ struct{} `lw:"<m>,<s>,const=<value>"`      // optional: a constant attribute

    Id       uint64 `lw:",id"`                   // plain row column (entity header)
    Name     string `lw:"name,symbol"`           // tagged value → membership `name`, section `symbol`
    Tags     []string `lw:"tag,symbol"`          // one container attribute carrying N values

    Body     Prose   `lw:"body,text"`            // nested: section as an attribute struct (below)
}
```

- The **`_` kind field** carries `kind:"<entityKind>"`. Exactly one is required.
- A **plain column** has an *empty* membership: `lw:",<role>"`. The role is one of
  `id` / `naturalKey` / `ts` / `expiresAt` — the entity-header slots that drive
  `SetId` / `SetTimestamp` / `SetLifecycle`. `id` is mandatory.
- A **tagged field** binds `lw:"<membership>,<section>[,<flag>…]"`.
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

## Map a value: the simple subset

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
were removed by ADR-0113 D1: author a nested
[`[]Attr` section](#attribute-cardinality) instead. `,ct=<canonical>` may
**relabel** a field's canonical type (e.g. a `uint32` as IPv4 via `,ct=v`, or a
`[]byte` blob as the u8 array lane via `,ct=u8h`) without reshaping its Go
type.

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
identity comes entirely from the carrier. Carriers are **flat-grammar-only and
single-attribute** for now (ADR-0113 D5 parks the nested spelling):

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

## Emit a constant

A `_` field emits a fixed-string attribute on every row, with no Go-side storage:

```go
_ struct{} `lw:"source,symbol,const=ingest-v2"`
```

Composes with `,unit` and `,verbatim`; several consts on one membership emit
several attributes. A const on a ref channel still needs the membership resolved
(the wrapper's `kindXxx`); const + `,verbatim` embeds the name directly.

## Escalate: the nested attribute model

Escalate when a section outgrows a one-line tag. **A section is a Go struct —
an *attribute struct*.** Its fields play exactly three roles, discriminated by
their type; two multiplicities close the model.

- **membership fields** — typed `lw.Ref` / `lw.HighRef` / `lw.Verbatim` /
  `lw.HighVerbatim`. The *type* is the channel; the *value* is the per-row
  membership identity. One field = one membership; a slice field (`[]lw.Ref`) =
  a repeated membership; several fields = several memberships, possibly on
  different channels.
- **scalar sub-column fields** — `T` or `option.Option[T]`. The attribute-level
  scalars.
- **container sub-column fields** — `[]T` (one per co-container). A section's
  containers zip in lockstep (a shared per-attribute length); each co-container
  is its own `[]T` field. A single container is just one `[]T`.

And:

- **attributes per row = the *section field's* multiplicity** in the entity:
  `S` (exactly one) · `option.Option[S]` (zero or one) · `[]S` (N, in order);
- **memberships per attribute = the *membership field's* multiplicity** inside
  the attribute struct.

Two different `[]` for two different axes — the thing the flat tuple grammar
conflates into one construct. A static (compile-time constant) membership lives
in the tag; a dynamic (per-row) membership is a field. You add a struct only
when an axis stops being degenerate; until then the tag one-liner is the whole
story:

| Situation | Author as |
|---|---|
| one static-membership scalar / bag / optional | flat one-liner — `lw:"name,symbol"` |
| a constant attribute | flat `_` one-liner — `lw:"src,symbol,const=v2"` |
| scalar **and** container(s) in one section | an attribute **struct** (a scalar field + parallel `[]T` container fields) |
| the membership varies per row | an attribute struct with a **membership field** |
| more than one membership per attribute | an attribute struct with **several** membership fields |
| an array/set column filled with one value per attribute | an `lw.Single[T]` field (see [Deferred surfaces](#deferred-surfaces)) |
| an IP / CIDR value (a canonical relabel) | a **lane** type — `lw.IPv4`, `lw.IPv6`, `lw.IPv4Prefix`, `lw.IPv6Prefix` |
| N attributes of any of the above | make the section field a **`[]Attr`** |

If none of those apply, stay on the one-liners — they *are* the nested model's
degenerate case, and rewriting a working scalar DTO buys nothing.

### A section is an attribute struct

```go
// section `text`: one scalar sub-column + two co-container sub-columns.
type Prose struct {
    Text       string   `lw:"text"`        // scalar sub-column
    WordLength []uint32 `lw:"wordLength"`  // co-container sub-column
    WordBag    []string `lw:"wordBag"`     // co-container sub-column (zips with WordLength)
}

type Doc struct {
    _  struct{} `kind:"doc"`
    ID uint64   `lw:",id"`

    Body Prose `lw:"body,text"`   // static membership "body", section "text", ONE attribute
}
```

Typing rules the front-ends apply to an attribute struct's fields, **in this
order** (first match wins):

1. an **`lw.*` channel marker** (`lw.Ref`, `lw.Verbatim`, …) is a
   **membership** — one field is one membership, a `[]lw.Ref` slice a *repeated*
   membership;
2. a **`[]T` (`T ≠ byte`) or `*roaring.Bitmap`** field is a **container
   sub-column** — a section may declare several; they zip in lockstep (a shared
   per-attribute length, checked at marshal time — a mismatch is an error,
   never a panic);
3. everything else — `T`, `option.Option[T]`, a **lane** type, and crucially
   `[]byte` / `[N]byte` (leeway byte-strings are **scalars**, never a container)
   — is a **scalar sub-column**.

Each sub-column's leeway **column name** is its `lw:"<column>"` tag, or — when
untagged — `"value"` (the flat single-sub-column default); a section with two
or more sub-columns therefore gives each field an explicit column tag, while a
single-sub-column section can stay tag-free. Only `,ct=` is meaningful on a
sub-column tag.

The tag on a *struct-typed* section field is read by **segment count**:

- **one segment** — `lw:"text"` — the segment is the **section**; the memberships
  come from the struct's `lw.*` fields (dynamic), which must number **≥1**;
- **two or more** — `lw:"body,text[,channel]"` — `membership,section[,channelFlag]`
  (static); the struct declares **no** `lw.*` field, and a static membership
  carries its channel as a flag, exactly as a one-liner does.

The two signals are redundant by design — the tag states *intent*, the struct
states *shape* — so a disagreement (a static tag over a struct that has
membership fields, or a bare-section tag over one that has none) is a plan-time
error, never silent.

### Attribute cardinality

Make the section field a slice for N attributes; an `option.Option[S]` for
0-or-1. This replaced the flat grammar's `,explode` (removed by ADR-0113 D1) —
"N attributes" is a property of the section field, not a flag on a sub-column:

```go
Body  Prose                `lw:"body,text"` // exactly one attribute
Note  option.Option[Prose] `lw:"note,text"` // zero or one
Paras []Prose              `lw:"para,text"` // N attributes, in slice order
```

The splice rule is unchanged and now structural: an empty `[]Attr` or an absent
`Option` emits **zero attributes**. (Reflect also accepts `*S` as a second
Optional spelling; codegen rejects the pointer under its scalar-pointer policy
— see [Deferred surfaces](#deferred-surfaces).)

### Memberships

A membership is a field. Its multiplicity and its siblings give you every shape
the flat tuple grammar reached — without a second grammar:

```go
// N ref memberships on ONE attribute (a repeated membership):
type LineageTag struct {
    Ancestors []lw.Ref   // many memberships, ids carried directly (no lookup)
    Kind      string     // the attribute value
}

// TWO fixed memberships on one attribute, same channel:
type EdgeTag struct {
    Predicate lw.Ref     // membership #1
    Generic   lw.Ref     // membership #2
    Target    uint64     // the value
}

// heterogeneous channels on one attribute — verbatim + ref together:
type NamedText struct {
    Name lw.Verbatim                         // membership #1 (verbatim)
    Kind lw.Ref                              // membership #2 (ref)
    Text       string   `lw:"text"`          // scalar sub-column
    WordLength []uint32 `lw:"wordLength"`    // co-container
    WordBag    []string `lw:"wordBag"`       // co-container
}
```

`[]lw.Ref` (many memberships, one attribute) and `[]NamedText` (many attributes)
are visibly different — the two axes the flat tuple collapsed. Heterogeneous
channels per attribute are just several `lw.*` fields; this generalises what
ADR-0109 proved for tuples, at the same read-side cost (one iterator per
channel).

A **dynamic** membership's channel is its field's **type** — no flag. The
default is `lw.Ref` (low-card ref). Ref memberships as *fields* carry the id
**directly** per row (no registry lookup — ADR-0109); verbatim markers embed
the literal name. A **static** membership (named in the tag) keeps the channel
**flag** and, on a ref channel, still resolves via the wrapper's `kindXxx`
symbol — so only dynamic memberships get channel-by-type. Carrier memberships
have **no nested spelling** — parked by ADR-0113 D5; use the
[flat carrier grammar](#choose-a-membership-channel).

### Canonical lanes

A field's canonical type is derived from its Go type. When you need a
*different* canonical over the **same Go shape**, a **lane** type is the
tag-free spelling of the `,ct=` relabel:

```go
type Endpoint struct {
    Addr lw.IPv4               // uint32 (big-endian IPv4), canonical "v"
    Via  lw.IPv6               // [16]byte, canonical "w"
    Net  lw.IPv4Prefix         // [5]byte — 4 address bytes + 1 prefix-length byte, "vc"
}
```

The lane set is deliberately small — `lw.IPv4` / `lw.IPv6` / `lw.IPv4Prefix` /
`lw.IPv6Prefix` — and grows only with a consumer (ADR-0113 D5's authoring
policy). The u8 array lane has no marker type: spell it `,ct=u8h` on the tag
(flat field or nested sub-column).

### The named DML

The DML generator emits, per section, a value-struct `Add` beside the existing
positional primitives. Supplying each sub-column **by name** removes the
positional contract (DTO field order == schema column order == arg order) that
the flat path documented but the compiler only caught when types differed:

```go
sec := dml.GetSectionText()
a := sec.Add(InEntityTestTableSectionTextAttr{ // schema-generated attr — named value sub-columns
    Text:       "hello world",
    WordLength: []uint32{5, 5},                // parallel co-containers, zipped
    WordBag:    []string{"hello", "world"},
})
a.AddMembershipLowCardVerbatimP([]byte("title")) // membership stays chained on the cursor
a.EndAttributeP()
```

`Add` binds the **value sub-columns** by name and lowers to the lean,
allocation-free positional calls, then **returns the attribute cursor**, so
membership stays a chained `AddMembership…P` call and the close a chained
`EndAttributeP` (the `…P` methods are void, so hold the cursor rather than
chaining off them). Membership deliberately did **not** move into the value
struct: one field can't express a repeated or heterogeneous membership, and the
positional hazard was the *sub-columns*, which `Add` fixes. Unequal
co-container lengths are recorded via `CheckErrors`, not a panic. The generated
type is `<Section>Attr`, distinct from your DTO's attribute struct; the codecs
drive the positional primitives directly, so `Add` is the surface for
**hand-written** producers — a first-class write peer to the DTO model
(ADR-0113 D5).

### How the nested form lowers

Nothing below the front-end moves. A nested DTO parses to the **same
`mappingplan.Plan`** the flat grammar produces for equivalent data
(`goplan.ComputeGroups` still groups sections → sub-columns → memberships), so
the DML calls, the Arrow layout, and the wire bytes are byte-identical to the
flat path and to a hand-written DML loop; the ClickHouse read-back generator
(ADR-0066) and the DDL are untouched. The codegen SoA batch stores a section's
attributes AoS at attribute grain (`[]Prose` / `[][]NamedText`) and
`BuildEntities` re-columnarises at write time. The nested model is a *spelling*
of the same map — if it and a flat DTO ever disagree on the wire for the same
data, that is the bug.

## The frozen flat escalation spellings

Frozen by ADR-0113 D2: supported, no new features. New **hand-written**
escalation is authored nested (above); **DTO generators** target these
spellings as their permanent IR (ADR-0113's D3 resolution — plain tags, no
marker imports, compile-time safety moot for generated code). Both spellings
stay wire-identical and regenerate byte-stable.

### Multi-sub-column (`:column`)

A section that declares two or more physical sub-columns
([ADR-0101](../adr/0101-leeway-marshall-mixed-shape-sections.md)): **one Go
field per sub-column** with a `:<column>` suffix; all share one membership and
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
differ; the nested model's schema-generated `Add` removes the hazard). All
container fields must have **equal length** per row (checked at marshal time).
With ≥1 scalar sub-column the attribute always emits (empty containers are
legal, N = 0); an all-container section with every container empty is spliced.

### Dynamic-membership tuple (`@membership`)

The flat spelling for **many attributes, each with its own membership**
([ADR-0103](../adr/0103-leeway-marshall-dynamic-membership-tuples.md),
[ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md)): a
**slice-of-struct** field tagged with the bare section name:

```go
type LabeledText struct {
    Label      string   `lw:"@membership,verbatim"` // per-attribute membership
    Text       string   `lw:"text:text"`
    WordLength []uint32 `lw:"text:wordLength"`
    WordBag    []string `lw:"text:wordBag"`
}
Texts []LabeledText `lw:"text"` // N elements → N attributes, in slice order
```

Each element emits one attribute, the membership taken from the element's
`@membership` field — `string` / `[]byte` on a verbatim channel (the literal
name; `highCardVerbatim` is selected here, by flag), or `uint64` on a ref
channel (the id carried directly, no lookup). A repeated `[]T` `@membership` is
the sole membership on its channel. The tuple **owns its section exclusively**:
no static field, const, or second tuple may target it. An element always emits
(its slice presence is the signal); zero elements emit zero attributes. On
read, each attribute decodes to one element in wire order. The nested
equivalent is a `[]Attr` whose struct carries `lw.Verbatim` / `lw.Ref`
membership fields.

## Generate the codec (codegen path)

Run the generator over the DTO source file:

```sh
./boxer.sh keelsoncodec --target=anchor  path/to/mydto.go
# or, explicitly:  go run -tags "$(cat ./tags)" ./public/app keelsoncodec --target=facts path/to/mydto.go
```

`--target` picks the `WrapperEmitterI` (`anchor` = `NoOpWrapper`, schema-agnostic
surface only; `facts` = keelson's `factswrapper`, adds `kindXxx` resolution +
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
    Section("symbol", symR.GetAttributes(), symR.GetMemberships()).
    Section("text", txR.GetAttributes(), txR.GetMemberships())
var out []MyDTO
err = marshallreflect.Unmarshal(readers, &out, lookup)
// A nested []Attr field round-trips to the value you appended — you unmarshal
// into the same struct you wrote from, so what-you-wrote diffs against
// what-you-read at the struct level.
```

`Validate[T]` reports every missing / wrong-arity DML method in one error
before the first row (otherwise a mismatch panics mid-marshal via `mustCall`).
`SectionReaders` runs an up-front coverage check so a forgotten reader is one
clear error, not a nil dereference at row *i*. Pass `NoLookup{}` when the DTO is
all-verbatim. Use `PlanFor[MyDTO]()` to inspect the plan without marshalling.

## The invariant you preserve

For one DTO, the bytes from **codegen** `<Kind>BuildEntities`, from **reflect**
`Marshal`, and from a **hand-written DML loop** must be equal, and must round-trip
back to equal DTOs. Changes are checked by `array.RecordEqual` + Arrow IPC byte
equality + cross-decode (gen-write → reflect-read and vice versa) +
nested-vs-flat equal records, and every in-tree `.out.go` regenerates
byte-stable. The front-end **parity corpus**
(`marshallreflect_test/parity_corpus_test.go`) additionally gates the two
accept sets against each other: an undocumented accept/reject divergence fails
the gate. If you touch the shared plan/grouping layer, that whole matrix is
your regression gate.

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
  carrier channel. (This is the rule the tuple / nested `[]Attr` exists to
  work around.)
- **Tuple**: a second field/const/tuple on a tuple-owned section; a missing or
  wrongly-typed `@membership`; a ref-typed field on a verbatim channel (or the
  reverse); a repeated `@membership` mixed with another field on one channel; a
  foreign-package or stray element struct.
- **Nested**: a membership named both in the tag and by an `lw.*` field, or a
  bare-section tag over a struct with no `lw.*` field (the disambiguation
  cross-check); two sub-columns claiming one column name (e.g. two untagged
  fields both defaulting to `"value"`); a carrier or channel flag on a nested
  sub-column tag; a One / Optional section carrying a dynamic `lw.*`
  membership field.
- **Channel mix** within one section; a **ref membership** that is not a Go
  identifier; a top-level membership beginning with `@` (reserved for tuples).
- **Carrier** value without its sibling (or a channel mismatch).

`<Kind>ReadRow` (ADR-0100 store reads) additionally excludes tuple, carrier,
and const-only kinds — `ReadRowSupported` reports the reason.

## Deferred surfaces

Deferred surfaces are **rejected at plan/validate time with a clear error**,
never silently wrong; the parity corpus records each accept-set asymmetry with
its documentation reference. Not built:

- **Value markers in codegen** — entity-level `lw.Single` and lane fields ship
  in reflect only; codegen rejects them (they need the top-level marker→plain
  bridge across the SoA / `Append` / decode paths — a first attempt was
  reverted, ADR-0113 D3). A codegen'd DTO uses the flat `,unit` / `,ct=`
  spelling.
- **`*S` nested-Optional in codegen** — reflect accepts the pointer as a second
  Optional spelling; codegen rejects it under its scalar-pointer policy (its
  Optional emit arms assume `option.Option[S]`). Spell it `option.Option[S]`.
- **`lw.Single` as a nested sub-column** — deferred in both front-ends (it
  needs `BeginAttributeSingle` inside the tuple path). Use a plain `[]T` or the
  flat `,unit` field.
- **One / Optional dynamic-membership sections** — a per-attribute `lw.*`
  membership requires the section be `[]Attr` (Many).
- **Carrier memberships in a nested section** — parked with a trigger
  (ADR-0113 D5); use the flat carrier grammar.
- **Store `ReadRow` over tuple/carrier kinds** (ADR-0100) — generalising the
  store read path is a separate slice.
- **Recursive nesting** — an element field that is itself `[]SubElem` is
  expressible but unimplemented.

These build **on consumer demand** (ADR-0113 D3), not to retire the frozen flat
spellings.

## Shape cheat-sheet

| I want to map… | Go shape | Spelling |
|----------------|----------|----------|
| the entity id / natural key / timestamp | `uint64` / `[]byte` / `time.Time` | `lw:",id"` / `lw:",naturalKey"` / `lw:",ts"` |
| one value | `T` | `lw:"m,section"` |
| an optional value | `option.Option[T]` | `lw:"m,section"` |
| a bag of values (one attribute) | `[]T` | `lw:"m,section"` |
| a constant on every row | `_ struct{}` | `lw:"m,section,const=…"` |
| a literal-named membership | `T` / `[]T` | `lw:"m,section,verbatim"` |
| scalar + co-containers, one attribute | `struct{ S T; C1 []T; C2 []T }` | nested — `lw:"m,section"` |
| the same, N attributes | `[]Attr` | nested — `lw:"m,section"` |
| membership that varies per row | an `lw.Ref` / `lw.Verbatim` field | nested — `lw:"section"` |
| many memberships, one attribute | `[]lw.Ref` or several `lw.*` fields | nested |
| heterogeneous channels per attribute | several `lw.*` marker types | nested |
| a canonical relabel (IPv4 / IPv6 / CIDR) | a lane type — `lw.IPv4`, … | nested (reflect); `,ct=` (codegen) |
| a carrier (identity + params per row) | value + `marshalltypes.X` sibling | flat — `lw:"m,sec,mixed…"` |
| frozen: sub-columns by tag | one field per `:<col>` | flat — `lw:"m,sec:col"` |
| frozen: tuple with `@membership` | `[]ElemStruct` | flat — `lw:"section"` |

## Worked examples

The `codecdemo` DTOs in nested form. Diff against the flat originals to see
each ad-hoc mechanism dissolve into a field type:

```go
// textdoc — mixed scalar + co-containers, one static membership, one attribute.
type TextDoc struct {
    _        struct{} `kind:"textDoc"`
    ID       uint64   `lw:",id"`
    Tracking []byte   `lw:",naturalKey"`

    Prose ProseAttr `lw:"prose,text"`         // was: Text/WordLength/WordBag + :col suffixes
}
type ProseAttr struct {
    Text       string   `lw:"text"`
    WordLength []uint32 `lw:"wordLength"`
    WordBag    []string `lw:"wordBag"`
}

// labeledtextdoc — N attributes, dynamic verbatim membership, mixed section.
type LabeledTextDoc struct {
    _        struct{} `kind:"labeledTextDoc"`
    ID       uint64   `lw:",id"`
    Tracking []byte   `lw:",naturalKey"`

    Texts []LabeledText `lw:"text"`           // []Attr = N attributes
}
type LabeledText struct {
    Label      lw.Verbatim                    // was: `lw:"@membership,verbatim"`
    Text       string   `lw:"text"`
    WordLength []uint32 `lw:"wordLength"`
    WordBag    []string `lw:"wordBag"`
}

// lineagedoc — repeated ref memberships, two fixed refs, heterogeneous pair.
type LineageDoc struct {
    _        struct{} `kind:"lineageDoc"`
    ID       uint64   `lw:",id"`
    Tracking []byte   `lw:",naturalKey"`

    Types []LineageTag `lw:"symbol"`
    Edges []EdgeTag    `lw:"foreignKey"`
    Notes []NamedText  `lw:"text"`
}
type LineageTag struct { Ancestors []lw.Ref; Kind string }       // []lw.Ref = N memberships
type EdgeTag    struct { Predicate lw.Ref; Generic lw.Ref; Target uint64 }

// sensorreading — carrier channel: stays on the FLAT grammar (nested parked, ADR-0113 D5).
//   Reading  string                        `lw:"sensor,symbol,mixedLowCardRef"`
//   ReadingC marshalltypes.MixedLowCardRef `lw:"sensor,symbol,mixedLowCardRef"`

// dronemission — a scalar section plus a single-element array column.
type DroneMission struct {
    _        struct{} `kind:"droneMission"`
    ID       uint64   `lw:",id"`
    Tracking []byte   `lw:",naturalKey"`

    Status  string            `lw:"droneStatus,symbol"`  // scalar sugar — unchanged
    Battery lw.Single[uint64] `lw:"battery,u64Array"`    // reflect-only; codegen: flat `,unit`
}
```

## Further reading

- [`marshallreflect` package doc](../../public/semistructured/leeway/marshall/go/marshallreflect/) — the runtime codec, the full DML write contract, and the RA read contract (`pkgsite` is canonical).
- [`marshallgen` EXPLANATION](../../public/semistructured/leeway/marshall/go/marshallgen/EXPLANATION.md) — how the generator works, the channel table, the read-side asymmetry, and the emit trade-offs.
- [The `goplan` toolkit](../../public/semistructured/leeway/marshall/go/goplan/) and [the `mappingplan` model](../../public/semistructured/leeway/mappingplan/) — the shared tag grammar, `PlanBuilder` validation, section grouping, and the membership channels.
- Worked DTOs: [`anchor/codecdemo/`](../../public/semistructured/leeway/anchor/codecdemo/) — `textdoc` (multi-sub-column), `labeledtextdoc` (tuple), `lineagedoc` (multi-membership + ref tuple), `sensorreading` (carriers), and [`codecdemo/nested/`](../../public/semistructured/leeway/anchor/codecdemo/nested/) for the nested forms.
- Decisions: [ADR-0113](../adr/0113-leeway-marshall-nested-primary-consolidation.md) (nested primary, the frozen flat escalation, the D1 cull), [ADR-0074](../adr/0074-leeway-marshall-package-layout.md) (package layout), [ADR-0101](../adr/0101-leeway-marshall-mixed-shape-sections.md) (mixed shapes), [ADR-0103](../adr/0103-leeway-marshall-dynamic-membership-tuples.md) / [ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md) (tuples), [ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md) (`ReadRow` / store).
