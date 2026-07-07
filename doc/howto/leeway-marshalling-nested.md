---
type: how-to
audience: engineer with a specific task
status: draft
# reviewed-by: "@<handle>"   # fill in when flipping to stable
# reviewed-date: YYYY-MM-DD
---

> **Status: draft — pre-human-review.** DESIGN TARGET, NOT IMPLEMENTED — this
> how-to is written *as if* the nested-struct front-end and the named DML
> already existed end-to-end. Nothing here ships yet. The shipped, authoritative
> recipe is the flat `lw:` tag grammar in [leeway-marshalling.md](./leeway-marshalling.md);
> this document is the target that a forthcoming ADR (generalising ADR-0103)
> would build toward. Do not cite it as behaviour in force.

# How to marshal a Go struct to and from a leeway table — nested model

This recipe maps a typed Go struct onto a leeway columnar table and reads it
back, using a **nested attribute model** in which every leeway concept has a
direct, typed Go representation. It is a second front-end onto the same
marshall stack: it parses into the same [`mappingplan.Plan`](../../public/semistructured/leeway/mappingplan/),
drives the same DML/RA classes, and its wire output is **byte-identical** to the
flat grammar's for the same data (see [How it lowers](#how-it-lowers)). It does
not change storage, the wire, or the schema — only how you *author* the mapping.

The flat one-liners still work unchanged; the nested form is what you **escalate
to** when a section outgrows a single tag: mixed scalar/container shapes,
per-row (dynamic) memberships, more than one membership per attribute, or carrier
channels. Where the flat grammar reaches those with a `:column` suffix, a
`@membership` sentinel, a channel flag, and an implicitly-paired carrier sibling
— four different mechanisms — the nested model reaches all of them with **one**:
the field's Go type.

## The model in one paragraph

**A section is a Go struct — an *attribute struct*.** Its fields play exactly
three roles, discriminated by their type; two multiplicities close the model.

- **membership fields** — typed `lw.Ref` / `lw.Verbatim` / `lw.MixedRef[P]` … .
  The *type* is the channel; the *value* is the per-row membership identity
  (plus params for carriers). One field = one membership; a slice field
  (`[]lw.Ref`) = a repeated membership; several fields = several memberships,
  possibly on different channels.
- **scalar sub-column fields** — `T` or `option.Option[T]`. The attribute-level
  scalars.
- **container sub-column fields** — `[]T` (one per co-container). A section's
  containers zip in lockstep (a shared per-attribute length); each co-container
  is its own `[]T` field, as in the flat tuple element. A single container is
  just one `[]T`.

And:

- **attributes per row = the *section field's* multiplicity** in the entity:
  `T` (exactly one) · `Option[T]` / `*T` (zero or one) · `[]T` (N, in order).
- **memberships per attribute = the *membership field's* multiplicity** inside
  the attribute struct.

Two different `[]` for two different axes — the thing the flat grammar's tuple
conflates into one construct. Finally: **a static (compile-time constant)
membership lives in the tag; a dynamic (per-row) membership is a field.** You add
a struct only when an axis stops being degenerate; until then the tag one-liner
is the whole story.

## When to reach for it

| Situation | Author as |
|---|---|
| one static-membership scalar / bag / optional | flat one-liner (unchanged) — `lw:"name,symbol"` |
| a constant attribute | flat `_` one-liner (unchanged) — `lw:"src,symbol,const=v2"` |
| scalar **and** container(s) in one section | an attribute **struct** (a scalar field + parallel `[]T` container fields) |
| the membership varies per row | an attribute struct with a **membership field** |
| more than one membership per attribute | an attribute struct with **several** membership fields |
| a carrier channel (identity carries params) | an attribute struct with a **carrier membership field** |
| an array/set column filled with one value per attribute | an `lw.Single[T]` field (a single-element container) |
| an IP / CIDR / u8-lane value (a canonical relabel) | a **lane** type — `lw.IPv4`, `lw.IPv6`, `lw.U8Array`, … |
| N attributes of any of the above | make the section field a **`[]Attr`** |

If none of those apply, stay on the flat grammar — it *is* the nested model's
degenerate case, and rewriting a working scalar DTO buys nothing.

## Prerequisites

- **Build tags on every `go` command:** `-tags="$(cat ./tags)"`
  ([AGENTS.md §Build & test](../../AGENTS.md)).
- **A leeway schema with generated DML + RA classes** — e.g. `anchor`. The nested
  codec *drives* those classes; it does not define the schema.
- **The marker package** (`lw`, name provisional) carries three kinds of marker,
  each replacing a flat-grammar flag with a **type**: *channel* markers (one per
  row of the `channelTable` in [`mappingplan`](../../public/semistructured/leeway/mappingplan/plan.go)
  — no new registry); the *value-shape* marker `Single` (the `,unit` shape); and
  the *lane* types (the `,ct=` canonical relabels — a small, bounded registry).

```go
// package lw (provisional).

// channel markers — one per channel-table row (a membership's channel IS its type):
type Ref          uint64            // low-card ref  — per-row id, carried directly
type HighRef      uint64            // high-card ref
type Verbatim     string            // low-card verbatim — literal name on the wire
type HighVerbatim string            // high-card verbatim
type MixedRef[P any]      struct{ Id   uint64; Params P }  // carrier: id  + params
type MixedVerbatim[P any] struct{ Name string; Params P }  // carrier: name + params
type RefParams[P any]     struct{ Params P }               // carrier: params only
type HighRefParams[P any] struct{ Params P }

// value-shape marker — a container (array/set) sub-column carrying exactly ONE
// element per attribute, supplied inline as T (the ,unit / BeginAttributeSingle
// shape). lw.One(v) is the terse constructor.
type Single[T any] struct{ Val T }

// lane types — named Go types the classifier maps to a specific canonical (the
// ,ct= relabels), so a struct field stays tag-free. Same bytes, relabelled:
type IPv4    [4]byte    // canonical "v"     (vs a plain [4]byte)
type IPv6    [16]byte   // canonical "w"
type U8Array []byte     // u8 container lane (vs a plain []byte = scalar byte-string)
// Adding a lane = one registry entry; the set is bounded by the canonicals ,ct= reaches.
```

`Ref` and friends are newtypes, so literals stay terse (`a.Kind = 0x1f`,
`a.Name = "author"`); values need the usual conversion (`lw.Ref(id)`). A
single-element container value is `lw.One(v)` (or `lw.Single[T]{Val: v}`).

## The entity

The entity level is unchanged from the flat grammar. A `_` kind field, plain
header columns, and — for simple sections — the same one-liners:

```go
type Person struct {
    _        struct{}  `kind:"person"`   // entity kind — required, once
    ID       uint64    `lw:",id"`        // plain header column — required
    Tracking []byte    `lw:",naturalKey"`
    Ts       time.Time `lw:",ts"`

    Name string   `lw:"name,symbol"`     // scalar sugar — one static membership, one scalar
    Tags []string `lw:"tag,symbol"`      // bag sugar — one attribute, N container values
}
```

`Name`'s one-liner is exactly the degenerate attribute struct
`struct{ _ lw.Ref /* = "name" */; Val string }` in section `symbol`, cardinality
one — spelled inline. You never write that struct out for the simple case.

## A section is an attribute struct

Escalate when a section carries a scalar *and* containers, or needs a membership
the tag can't state. The struct's field types carry every role:

```go
// section `text`: one scalar sub-column + two co-container sub-columns.
type Prose struct {
    Text       string   `lw:"text"`       // scalar sub-column
    WordLength []uint32 `lw:"wordLength"`  // co-container sub-column
    WordBag    []string `lw:"wordBag"`     // co-container sub-column (zips with WordLength)
}

type Doc struct {
    _  struct{} `kind:"doc"`
    ID uint64   `lw:",id"`

    Body Prose `lw:"body,text"`   // static membership "body", section "text", ONE attribute
}
```

Typing rules the front-end applies to an attribute struct's fields, **in this
order** (first match wins):

1. an **`lw.*` channel marker** (`lw.Ref`, `lw.Verbatim`, a carrier, …) is a
   **membership** — one field is one membership, a `[]lw.Ref` slice a *repeated*
   membership;
2. an **`lw.Single[T]`** field is a **single-element container** sub-column;
3. a **`[]T` (`T ≠ byte`) or `*roaring.Bitmap`** field is a **container
   sub-column** — a section may declare several; they zip in lockstep (a shared
   per-attribute length, checked at marshal time), like the flat tuple element;
4. everything else — `T`, `option.Option[T]`, a **lane** type, and crucially
   `[]byte` / `[N]byte` (leeway byte-strings are **scalars**, never a container)
   — is a **scalar sub-column**.

A section's containers are all `lw.Single` (the unit shape) or all plain `[]T`,
never mixed. Each sub-column's leeway **column name** is its `lw:"<column>"` tag,
or — when untagged — `"value"` (the flat single-sub-column default); a section
with two or more sub-columns therefore gives each field an explicit column tag,
while a single-sub-column section can stay tag-free.

The tag on a *struct-typed* section field is read by **segment count**:

- **one segment** — `lw:"text"` — the segment is the **section**; the memberships
  come from the struct's `lw.*` fields (dynamic), which must number **≥1**;
- **two or more** — `lw:"body,text[,channel]"` — `membership,section[,channelFlag]`
  (static); the struct declares **no** `lw.*` field, and a static membership
  carries its channel as a flag, exactly as the flat grammar does.

The two signals are redundant by design — the tag states *intent*, the struct
states *shape* — so a disagreement (a static tag over a struct that has
membership fields, or a bare-section tag over one that has none) is a plan-time
error, never silent. A *scalar*-typed field is untouched: the sugar one-liner is
always `lw:"membership,section"`.

## Attribute cardinality

Make the section field a slice for N attributes; an `Option`/pointer for 0-or-1.
This **subsumes the flat grammar's `,explode`** — "N attributes" is now a
property of the section field, not a flag on a sub-column.

```go
Body  Prose          `lw:"body,text"`   // exactly one attribute
Note  option.Option[Prose] `lw:"note,text"` // zero or one
Paras []Prose        `lw:"para,text"`   // N attributes, in slice order
```

The splice rule is unchanged and now structural: an empty `[]Attr`, an absent
`Option`, or a nil pointer emit **zero attributes**.

## Memberships

A membership is a field. Its multiplicity and its siblings give you every shape
the flat tuple grammar reached — without a second grammar.

```go
// N ref memberships on ONE attribute (a repeated membership):
type LineageTag struct {
    Ancestors []lw.Ref   // many memberships, carried directly (no lookup)
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
    Name lw.Verbatim                        // membership #1 (verbatim)
    Kind lw.Ref                             // membership #2 (ref)
    Text       string   `lw:"text"`         // scalar sub-column
    WordLength []uint32 `lw:"wordLength"`    // co-container
    WordBag    []string `lw:"wordBag"`       // co-container
}
```

`[]lw.Ref` (many memberships, one attribute) and `[]NamedText` (many attributes)
are visibly different — the two axes the flat tuple collapsed. Heterogeneous
channels per attribute are just several `lw.*` fields; this generalises what
ADR-0109 proved for tuples, at the same read-side cost (one iterator per
channel).

## Channels

A **dynamic** membership's channel is its field's **type** — no flag. The default
is `lw.Ref` (low-card ref). Ref memberships as *fields* carry the id **directly**
per row (no registry lookup — ADR-0109). Verbatim markers embed the literal name;
carrier markers ride identity **and** params in the one field. A **static**
membership (named in the tag) keeps the flat grammar's channel **flag** and, on a
ref channel, still resolves via the wrapper's `kind…` symbol, exactly as today —
so only dynamic memberships get channel-by-type.

## Carriers

A carrier channel's identity is per-row data. In the flat grammar that means a
value field plus a `marshalltypes` sibling sharing an identical tag, paired by
the builder. Here it collapses to a **membership field that is the carrier** —
the sibling is gone:

```go
// FLAT (shipped): two fields, identical tag, paired by goplan.PlanBuilder
//   Reading  string                        `lw:"sensor,symbol,mixedLowCardRef"`
//   ReadingC marshalltypes.MixedLowCardRef `lw:"sensor,symbol,mixedLowCardRef"`

// NESTED: one struct; the membership IS the carrier
type Reading struct {
    M     lw.MixedRef[[]byte]   // was ReadingC — id + params, one field
    Value string                // was Reading  — the scalar sub-column
}

type Sensor struct {
    _  struct{} `kind:"sensor"`
    ID uint64   `lw:",id"`
    R  Reading  `lw:"symbol"`    // dynamic membership (carrier) → section only
}
```

Carriers only appear in single-attribute sections; they remain rejected inside a
`[]Attr` tuple (their identity is per-row carrier data, not a plain field).

## Mixed scalar + co-containers

A section's co-containers are **parallel `[]T` fields** — the same shape the flat
tuple element uses. They zip in lockstep: every container field must have the
same length per attribute, checked at marshal time (a mismatch is an error, not
a panic).

```go
type Prose struct {
    Text       string   `lw:"text"`       // scalar sub-column
    WordLength []uint32 `lw:"wordLength"`  // co-container
    WordBag    []string `lw:"wordBag"`     // co-container (len == len(WordLength))
}
```

A section with only containers (no scalar) is a struct of `[]T` fields; a single
container is one `[]T` (`T ≠ byte`; a `[]byte` field is a scalar byte-string, not
a container), with its column defaulting to `"value"`. One element per attribute
is `lw.Single[T]`, next.

(A *bundled* form — co-containers gathered into one `[]ElemStruct` so equal-length
is structural — was considered and dropped: parallel `[]T` fields reuse the flat
tuple machinery unchanged, at the cost of a runtime length check.)

## Single-element array columns

Some sections are physically arrays or sets, but each attribute carries exactly
one element — drone telemetry stores one battery reading per dispatch in a
`u64Array` column. `lw.Single[T]` is that shape: one element, supplied inline as
`T`, emitted via `BeginAttributeSingle` (whose sole purpose is a length-1
`AddToContainer`) rather than opening a list:

```go
type Telemetry struct {
    Battery lw.Single[uint64]   // u64Array column — one reading per attribute
    Fix     lw.Single[[]byte]   // blob column — one signature per attribute
}
```

`lw.Single` co-exists with scalars and memberships in one struct, and a `[]Attr`
of such structs is the flat grammar's `,explode,unit` shape (N attributes, each a
single-element container). The one constraint: a section's containers are all
`lw.Single` (unit) or all plain `[]T`, never mixed.

It also stands alone at the **entity** level as sugar — a known `lw.*` type, so
it reads like a scalar one-liner, not a user attribute struct:

```go
Battery lw.Single[uint64] `lw:"battery,u64Array"`   // degenerate one-attribute, static-membership section
```

(the `dronemission` example below).

## Canonical lanes

A field's canonical type is derived from its Go type. When you need a *different*
canonical over the **same bytes** — a `[4]byte` read as IPv4, a `[]byte` read as
the u8 array lane rather than a scalar byte-string — reach for a **lane type**
instead of the flat grammar's `,ct=` flag:

```go
type Endpoint struct {
    Addr lw.IPv4     // [4]byte bytes, canonical "v"
    Mask lw.U8Array  // []byte bytes, u8 container lane (not a scalar blob)
}
```

Lanes keep the attribute struct tag-free (the relabel is the type, not a tag)
and answer *where schema-fixed canonical metadata lives*: a small,
registry-backed set of named types. The registry's full contents are reserved —
this cut ships the lanes the shipped `,ct=` already reaches (`v` / `w` / CIDR /
u8).

## Constants

Unchanged — a constant is a static-membership section with a fixed value and no
Go storage, so it stays the flat `_` one-liner:

```go
_ struct{} `lw:"source,symbol,const=ingest-v2"`
```

## The named DML

The DML generator emits, per section, a value-struct `Add` beside the existing
positional primitives. Supplying each sub-column **by name** removes the
positional contract (DTO field order == schema column order == arg order) that
the flat path documented but the compiler only caught when types differed:

```go
sec := dml.GetSectionText()
sec.Add(anchor.TextAttr{           // schema-generated attr struct — named, order-free
    Membership: myKindID,          // present only for dynamic-membership sections
    Text:       "hello world",
    WordLength: []uint32{5, 5},    // parallel co-containers, zipped
    WordBag:    []string{"hello", "world"},
})
```

`Add` is a **compile-time skin**: it destructures the value struct into today's
lean, allocation-free positional calls — `BeginAttribute(Text)`,
`AddMembership…P(Membership)`, one `AddToCoContainersP(WordLength[k], WordBag[k])`
per element — so the hot path is byte-for-byte what it is now. (The
schema-generated `TextAttr` is distinct from your DTO's `Prose`; the codec
bridges DTO→DML by field role, keeping the DTO independent of the schema
package. A hand-written producer uses the generated struct directly.)

## Drive it: codegen and reflect

Both front-ends accept the nested DTO and produce the same `Plan`.

```sh
# codegen — emits doc.out.go next to the source, as today
./boxer.sh keelsoncodec --target=anchor path/to/doc.go
```

```go
// reflect — same call surface as the flat grammar
if err := marshallreflect.Validate[Doc](dml); err != nil { /* mis-wired DML */ }
if err := marshallreflect.Marshal(dml, rows, lookup); err != nil { … }
recs, err := dml.TransferRecords(nil)
```

The codegen SoA batch for a nested DTO stores a section's attributes AoS at
attribute grain (`[]Prose` / `[][]NamedText`) — the same representation
ADR-0103 already uses for tuples — and `BuildEntities` re-columnarises to one
Arrow array per physical sub-column at write time. Columnar scans belong on the
Arrow record, not on the staging batch.

## Read it back

Read is symmetric: you unmarshal into **the same struct you wrote from**. A
tuple section reads its N attributes into the `[]Attr` field — the destination
the flat DTO never had:

```go
readers := marshallreflect.NewSectionReaders(idR.Len()).
    PlainColumn("id", idR.ValueId).
    Section("text", txR.GetAttributes(), txR.GetMemberships())

var out []Doc
err := marshallreflect.Unmarshal(readers, &out, lookup)
// out[i].Paras is []Prose — round-trips to the value you appended.
```

Because one value type threads write → codec → DML → read, you can diff
what-you-wrote against what-you-read at the struct level (closed-loop
verification, structurally).

## How it lowers

Nothing below the front-end moves. The nested DTO parses to the **same
`mappingplan.Plan`** the flat grammar produces for equivalent data
(`goplan.ComputeGroups` still groups sections → sub-columns → memberships), so:

- the DML calls, the Arrow layout, and the wire bytes are **byte-identical** to
  the flat path and to a hand-written DML loop;
- the ClickHouse read-back generator (ADR-0066) and the DDL are untouched;
- the regression gate is the existing byte-identity matrix (`array.RecordEqual`
  + Arrow IPC equality + gen↔reflect cross-decode), extended so a nested DTO and
  the flat DTO for the same data produce equal records.

The nested model is a *spelling* of the same map. If it and a flat DTO ever
disagree on the wire for the same data, that is the bug.

## What fails at build time

The nested front-end rejects an unrepresentable DTO at `PlanFor` / `Validate`,
never as a `reflect` panic:

- an attribute struct that mixes `lw.Single` (unit) containers with plain `[]T`
  containers in one section, or gives two sub-columns the same column name (e.g.
  two untagged fields, both defaulting to `"value"`);
- a membership named **both** in the tag and by an `lw.*` field, or a bare-section
  tag over a struct that declares no `lw.*` field (the disambiguation cross-check);
- a **carrier** membership field inside a `[]Attr` tuple;
- a section targeted by **two** entity fields, or by a static field and a tuple
  (a section is static-mode or dynamic-mode, not both);
- an attribute-struct field that is none of the roles (marker / `lw.Single` /
  container / scalar); a foreign-package element struct;
- a plain/header field carrying a section, membership, channel, or shape type.

Because column identity is now a **field name checked against the
schema-generated `Add`**, a mistyped or reordered sub-column is a compile error
(`unknown field Wrd…`), not silent garbage caught later by the byte tests.

## Boundaries — reserved, not in this cut

Designed *not to foreclose*, but explicitly **not built** here:

- **Store `ReadRow` over tuple/carrier kinds (ADR-0100).** The nested struct
  gives `ReadRow` a destination for N attributes, but generalising the store
  read path is a separate slice.
- **Schema-from-DTO.** A fully-typed attribute struct is structurally sufficient
  to *emit or verify* a `TableDesc`; this cut only *reads* an existing schema.
  `TableDesc` remains the single source of truth (Go type → canonical is not
  total — hence lane types).
- **Recursive nesting.** A container element field is one level of nested
  record; an element field that is itself `[]SubElem` is left expressible but
  unimplemented, pending a check that the physical columnar model carries nested
  repetition.

## Cheat-sheet

| I want to map… | Author as |
|---|---|
| id / naturalKey / ts / expiresAt | plain field — `lw:",id"` … (unchanged) |
| one static-membership value / bag / optional | flat one-liner — `lw:"m,section"` (unchanged) |
| a constant on every row | `_ struct{} lw:"m,section,const=…"` (unchanged) |
| scalar + co-containers, one attribute | `struct{ S T; C1 []T; C2 []T }` (parallel), `lw:"m,section"` |
| the same, N attributes | make the field `[]Attr` |
| membership that varies per row | an `lw.Ref` / `lw.Verbatim` field; `lw:"section"` |
| many memberships, one attribute | `[]lw.Ref` (repeated) or several `lw.*` fields |
| heterogeneous channels per attribute | several `lw.*` fields of different marker types |
| a carrier (identity + params) | one `lw.MixedRef[P]` field — no sibling |
| an array/set column, one element per attribute | `lw.Single[T]` (the `,unit` shape) |
| a canonical relabel (IPv4 / u8 lane) | a lane type — `lw.IPv4`, `lw.U8Array`, … |

## Further reading

- [leeway-marshalling.md](./leeway-marshalling.md) — the **shipped** flat `lw:`
  grammar. Authoritative for behaviour in force; this document is its
  design-stage sibling.
- [the `mappingplan` model](../../public/semistructured/leeway/mappingplan/) — the
  shared IR both front-ends target; the `channelTable` the markers mirror.
- [the `goplan` toolkit](../../public/semistructured/leeway/marshall/go/goplan/) —
  section grouping (`ComputeGroups`) and shape classification the front-end reuses.
- Worked DTOs (flat form): [`anchor/codecdemo/`](../../public/semistructured/leeway/anchor/codecdemo/)
  — `textdoc`, `labeledtextdoc`, `lineagedoc`, `sensorreading`; the
  [Worked examples](#worked-examples) below re-express each in nested form.
- Decisions: [ADR-0101](../adr/0101-leeway-marshall-mixed-shape-sections.md),
  [ADR-0103](../adr/0103-leeway-marshall-dynamic-membership-tuples.md),
  [ADR-0109](../adr/0109-leeway-marshall-multi-membership-ref-tuples.md),
  [ADR-0100](../adr/0100-recordstore-generated-leeway-clickhouse-store.md).

## Worked examples

The four `codecdemo` DTOs, re-expressed. Diff against the flat originals to see
each ad-hoc mechanism dissolve into a field type.

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
    Label      lw.Verbatim                       // was: `lw:"@membership,verbatim"`
    Text       string   `lw:"text"`
    WordLength []uint32 `lw:"wordLength"`
    WordBag    []string `lw:"wordBag"`
}

// lineagedoc — three shapes: repeated ref memberships, two fixed ref, heterogeneous pair.
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
type NamedText struct {
    Name lw.Verbatim; Kind lw.Ref                // two memberships (verbatim + ref)
    Text       string   `lw:"text"`              // scalar + parallel co-containers
    WordLength []uint32 `lw:"wordLength"`
    WordBag    []string `lw:"wordBag"`
}

// sensorreading — carrier channel, one attribute, no sibling.
type SensorReading struct {
    _        struct{} `kind:"sensorReading"`
    ID       uint64   `lw:",id"`
    Tracking []byte   `lw:",naturalKey"`

    Reading ReadingAttr `lw:"symbol"`
}
type ReadingAttr struct {
    M     lw.MixedRef[[]byte]                  // was: ReadingC sibling on an identical tag
    Value string
}

// dronemission — a scalar section plus a single-element array column (,unit).
type DroneMission struct {
    _        struct{} `kind:"droneMission"`
    ID       uint64   `lw:",id"`
    Tracking []byte   `lw:",naturalKey"`

    Status  string            `lw:"droneStatus,symbol"`  // scalar section — unchanged sugar
    Battery lw.Single[uint64] `lw:"battery,u64Array"`    // was: uint64 `lw:"battery,u64Array,unit"`
}
```
