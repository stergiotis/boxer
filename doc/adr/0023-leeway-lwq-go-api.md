---
type: adr
status: proposed
date: 2026-05-08
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0023: leeway lwq — Go API as the v1 implementation surface

## Context

[ADR-0022](0022-leeway-lwq-flwor-query-language.md) proposes `lwq` as a FLWOR-style text language for querying Leeway-stored data, with a parser, AST, and lowerer to ClickHouse SQL. Subsequent design review examined the existing `public/semistructured/leeway/streamreadaccess/` package and concluded that the `SinkI` / `Driver` substrate already exposes the full Leeway protocol structure as a streaming visitor pattern — entity boundaries, sections, co-section grouping, use-aspects, value-aspects, membership types, cardinality, all available via SAX-style events.

This changes the implementation calculus:

- The structural capabilities ADR-0022 sets out to surface — multi-membership predicates, primary/secondary role distinction, co-value joins, aspect-driven section selection, tree-shaped construction — can be delivered through a typed Go API over `SinkI` without a grammar, parser, or separate AST.
- Go's type system, IDE support (`gopls`), and integration with the existing Leeway codegen give the Go API benefits the text frontend cannot match: compile-time schema/path/type validation, refactoring safety, and native composition with arbitrary Go code.
- The Go API admits two execution targets cleanly: in-process (a `SinkI` implementation interpreting the query graph against an Arrow `RecordBatch`) and ClickHouse (the same query graph lowered to CH SQL via the existing `public/db/clickhouse/dsl/ast` and `ToSQL` infrastructure).

The text-form syntax is still required: a downstream consumer needs `lwq` integrated into SQL as a "little language" served via a SQL proxy, and that requires a textual surface. This ADR therefore does not supersede ADR-0022; it sequences it. The Go API ships first; the FLWOR text frontend, when built, parses to the *same operator graph* the Go API constructs and reuses the same backends.

## Decision

We will implement `lwq` v1 as a typed Go API at `public/db/leeway/lwq/`, with two execution targets: in-process via `SinkI` against Arrow data, and ClickHouse via the existing CH AST and `ToSQL` emitter. The FLWOR text frontend (per ADR-0022) is deferred until the SQL-proxy integration becomes a concrete deliverable; when built, it parses to the same operator graph and reuses both backends.

## API specification

### Package layout

```
public/db/leeway/lwq/
├── lwq.go              # entry: New(env) → *Plan; Run/Collect/Reduce
├── plan.go             # *Plan, operators, internal operator graph
├── binder.go           # *Binder — typed accessor inside a binding scope
├── path.go             # path parsing + resolution against schema env
├── role.go             # multi-membership and role helpers
├── aspect.go           # value-aspect / use-aspect helpers
├── target.go           # Target interface
├── target_inproc.go    # in-process executor (SinkI consumer of Driver events)
├── target_ch.go        # ClickHouse executor (lowers to CH AST → ToSQL)
└── EXPLANATION.md
```

### Core types

```go
// LeewayEnv provides schema metadata: section descriptors, path → section
// resolution, use-aspects, value-aspects, membership specs. Sourced from
// the Leeway SDK's TableDesc / IntermediateTableRepresentation.
type LeewayEnv interface {
    ResolvePath(p Path) (PathResolution, error)
    Sections() []SectionDesc
    SectionByName(name string) (SectionDesc, bool)
    SectionsWithAspect(useaspects.AspectE) []SectionDesc
}

// Path is a Leeway path with `_` placeholders for high-cardinality positions.
//   Path("/items/_/qty") → resolves to (int64 section, param at index 1).
type Path string

type PathResolution struct {
    Section            string
    LowCardPrefix      []string
    ParameterPositions []int
    CanonicalType      canonicaltypes.PrimitiveAstNodeI
    UseAspects         useaspects.AspectSet
    ValueAspects       valueaspects.AspectSet
}

// Plan is the type-erased operator graph. Operators on *Plan compose
// fluently; type-introducing terminals (Collect, Reduce, Sum) are free
// functions because Go does not allow generic methods.
type Plan struct { /* internal */ }

// Binder is the per-binding accessor passed to operator closures.
// It carries the parameter scope of the current binding so path accesses
// are scoped automatically.
type Binder struct { /* internal */ }

// Target abstracts execution.
type Target interface { /* internal */ }

// RoleKindE distinguishes the membership-role classifier output
// (per ADR-0007). This is orthogonal to
// MembershipSpec — every membership has both a kind and a spec.
type RoleKindE uint8

const (
    RoleAny RoleKindE = iota
    RolePrimary
    RoleSecondary
)
```

### Membership model

Memberships are the protocol-level mechanism by which values are tagged in Leeway sections. Every section declares a `MembershipSpec` that determines the shape of memberships its values carry, and the API must address all eight specs — not only the path-with-slots case favoured by JSON-over-Leeway tutorials. The taxonomy is the cross-product of three dimensions:

- **Cardinality** — high-card (many distinct values, dictionary-compressed in storage) vs low-card (few distinct values).
- **Value form** — `Ref` (numeric reference into a dictionary), `Verbatim` (literal bytes/string), or parametrized variants of either.
- **Mixed specs** — sections may combine a low-card prefix with high-card params (the path-with-slots case).

The eight `MembershipSpec` variants from `public/semistructured/leeway/common/`:

| Spec | Meaning | Typical use |
|---|---|---|
| `HighCardRef` | high-card ref into a dictionary | foreign-key-like references |
| `HighCardVerbatim` | high-card literal bytes | unique byte-string keys |
| `HighCardRefParametrized` | high-card ref + params | parametrized references |
| `LowCardRef` | low-card ref (small enum) | role enums, status codes |
| `LowCardVerbatim` | low-card literal string | named tags |
| `LowCardRefParametrized` | low-card ref + params | parametrized enum |
| `MixedLowCardRefHighCardParameters` | low-card ref + high-card params | low-card grouping with per-instance params |
| `MixedLowCardVerbatimHighCardParameters` | low-card verbatim + high-card params | path-with-slots (JSON-over-Leeway) |

The `lwq` API exposes memberships via two surfaces: a `Membership` interface (one struct per spec, for inspection inside closures) and a `MembershipMatcher` interface with cardinality- and spec-aware constructors (for declarative selection in operators).

```go
// Membership is the shape of an individual tag attached to a value.
// Use a Go type-switch in closures to dispatch by spec.
type Membership interface { isMembership() }

type HighCardRef                            struct { Ref uint64 }
type HighCardVerbatim                       struct { Bytes []byte }
type HighCardRefParametrized                struct { Ref uint64; Params []byte }
type LowCardRef                             struct { Ref uint64 }
type LowCardVerbatim                        struct { Verbatim string }
type LowCardRefParametrized                 struct { Ref uint64; Params []byte }
type MixedLowCardRefHighCardParameters      struct { Ref uint64; Params []byte }
type MixedLowCardVerbatimHighCardParameters struct { Verbatim string; Params []byte }

// MembershipMatcher selects bindings by membership criteria. Constructors
// are spec-specific so the matcher carries enough information to drive
// cardinality-aware predicate pushdown into the right section column(s).
type MembershipMatcher interface { matchMembership() }

// Single-spec matchers.
func MatchHighCardRef(ref uint64) MembershipMatcher
func MatchHighCardRefIn(refs ...uint64) MembershipMatcher
func MatchHighCardVerbatim(bytes []byte) MembershipMatcher
func MatchHighCardVerbatimPattern(pattern string) MembershipMatcher
func MatchHighCardRefParametrized(ref uint64, p ParamMatcher) MembershipMatcher

func MatchLowCardRef(ref uint64) MembershipMatcher
func MatchLowCardRefIn(refs ...uint64) MembershipMatcher
func MatchLowCardVerbatim(verbatim string) MembershipMatcher
func MatchLowCardVerbatimIn(verbatims ...string) MembershipMatcher
func MatchLowCardRefParametrized(ref uint64, p ParamMatcher) MembershipMatcher

func MatchMixedLowCardRef(ref uint64, p ParamMatcher) MembershipMatcher
func MatchMixedLowCardVerbatim(verbatim string, p ParamMatcher) MembershipMatcher

// Boolean combinators.
func MatchAnyOf(matchers ...MembershipMatcher) MembershipMatcher
func MatchAllOf(matchers ...MembershipMatcher) MembershipMatcher
func MatchNot(matcher MembershipMatcher) MembershipMatcher

// ParamMatcher is the constraint on the params component of a parametrized
// membership. Distinguishes "I want any params, just bind them" (Project)
// from "I want params equal to X" (Equal) from "I don't constrain params"
// (Wildcard).
type ParamMatcher interface { matchParam() }

func ParamWildcard() ParamMatcher                  // ignore params
func ParamEqual(bytes []byte) ParamMatcher         // exact bytes match
func ParamPattern(pattern string) ParamMatcher     // pattern over params bytes
func ParamCBORShape(shape CBORShape) ParamMatcher  // structural CBOR match
func ParamProject() ParamMatcher                   // bind for closure access via b.Params*()
```

The matchers compile to **cardinality- and spec-aware predicate pushdown**: `MatchLowCardRef(42)` pushes into the section's low-card-ref column (per the `lr` column-role naming convention); `MatchHighCardVerbatim(...)` pushes into the high-card-verbatim column (`hv`); mixed matchers push into both `low_card_*` and `high_card_*` simultaneously. The lowerer reads the section's `MembershipSpec` to determine which physical columns each matcher addresses.

The path-with-slots `Path` form remains valid as a convenience for one specific case (`MixedLowCardVerbatimHighCardParameters` only) — see `ForEachAt` in the operator surface below.

### Constructor and entry points

```go
// New starts a new query plan against a Leeway schema env.
func New(env LeewayEnv) *Plan

// Collect runs the plan and projects each binding through fn into a slice of T.
func Collect[T any](
    ctx context.Context,
    p *Plan,
    t Target,
    project func(*Binder) T,
) ([]T, error)

// Reduce folds bindings into a single accumulated value.
func Reduce[T any](
    ctx context.Context,
    p *Plan,
    t Target,
    init T,
    fold func(T, *Binder) T,
) (T, error)

// Run executes the plan with a side-effecting visitor (no projection);
// useful for streaming consumers that emit elsewhere.
func Run(
    ctx context.Context,
    p *Plan,
    t Target,
    visit func(*Binder),
) error
```

### Operator surface

Type-preserving operators on `*Plan`:

```go
// --- iteration ---

// ForEachInSection iterates one canonical-type section directly. All
// memberships of bound values are accessible inside the closure via
// b.CurrentMembership() and b.AllMemberships().
func (p *Plan) ForEachInSection(name string) *Plan

// ForEachValueWithMembership iterates values whose memberships satisfy
// the matcher. The role kind further constrains by primary/secondary
// classification (per ADR-0007). The matcher carries the
// MembershipSpec it targets, so the lowerer pushes the correct
// cardinality- and spec-aware predicate into the section's columns.
func (p *Plan) ForEachValueWithMembership(kind RoleKindE, m MembershipMatcher) *Plan

// ForEachAt is a convenience wrapper for the JSON-over-Leeway path-with-
// slots case (sections with MixedLowCardVerbatimHighCardParameters spec).
// Equivalent to:
//   ForEachValueWithMembership(RoleAny,
//     MatchMixedLowCardVerbatim(string(path), ParamProject()))
//
// For sections with other MembershipSpecs (HighCardRef, LowCardRef,
// HighCardVerbatim, parametrized variants), use ForEachValueWithMembership
// directly with the appropriate matcher.
func (p *Plan) ForEachAt(path Path) *Plan

// ForEachValueWithAspect iterates values across all sections whose value-
// aspects include the given aspect. Compiles to UNION ALL across qualifying
// sections (CH target) or multi-section walk (in-process target).
func (p *Plan) ForEachValueWithAspect(a valueaspects.AspectE) *Plan

// --- filtering / ordering / limiting ---

func (p *Plan) Where(pred func(*Binder) bool) *Plan
func (p *Plan) OrderByAsc(key func(*Binder) any) *Plan
func (p *Plan) OrderByDesc(key func(*Binder) any) *Plan
func (p *Plan) Limit(n int) *Plan
func (p *Plan) Offset(n int) *Plan

// --- co-section composition ---

// JoinCoSection joins the named co-section under the current scope.
// Inside subsequent operator closures, the binder exposes both the
// primary value and the co-section attributes.
func (p *Plan) JoinCoSection(name string) *Plan
```

Aggregation terminals (free functions because they introduce or fix types):

```go
func Sum[T constraints.Ordered](
    ctx context.Context, p *Plan, t Target, fn func(*Binder) T,
) (T, error)
func Min[T constraints.Ordered](
    ctx context.Context, p *Plan, t Target, fn func(*Binder) T,
) (T, error)
func Max[T constraints.Ordered](
    ctx context.Context, p *Plan, t Target, fn func(*Binder) T,
) (T, error)
func Count(ctx context.Context, p *Plan, t Target) (int, error)
```

### Binder methods

```go
// Typed value access. Path is resolved against the schema env at query
// build time; runtime ensures the binder's parameter scope matches the
// path's parameter scope. An empty path means "the current binding's
// own value" — used after ForEachInSection / ForEachValueWith*.
func (b *Binder) String(path Path) string
func (b *Binder) Int64(path Path) int64
func (b *Binder) Float64(path Path) float64
func (b *Binder) Bool(path Path) bool
func (b *Binder) Float32Array(path Path) []float32   // embeddings, ragged tensors
func (b *Binder) Int64Array(path Path) []int64

// EntityID returns the current binding's entity identifier.
func (b *Binder) EntityID() string

// --- membership inspection ---
//
// The binder treats all eight MembershipSpec variants uniformly. Use a Go
// type-switch on Membership to dispatch by spec.

// CurrentMembership returns the membership instance that triggered the
// current binding. For ForEachValueWithMembership iteration this is the
// matched membership; for ForEachInSection / ForEachAt it is one of the
// memberships bearing the bound value.
//
//   switch m := b.CurrentMembership().(type) {
//   case lwq.LowCardRef:                                useRef(m.Ref)
//   case lwq.HighCardRef:                               lookupHighCard(m.Ref)
//   case lwq.MixedLowCardVerbatimHighCardParameters:    usePathParams(m.Verbatim, m.Params)
//   case lwq.LowCardRefParametrized:                    useRefParams(m.Ref, m.Params)
//   // ... eight cases total
//   }
func (b *Binder) CurrentMembership() Membership

// AllMemberships returns every membership of the current value.
// Multi-membership values yield multiple entries.
func (b *Binder) AllMemberships() []Membership

// MembershipsByKind filters AllMemberships by primary/secondary classification.
func (b *Binder) MembershipsByKind(kind RoleKindE) []Membership

// HasMembership tests whether the current value bears any membership
// matching the matcher (additional to the one that triggered the binding).
// Useful for "value plays role X AND also role Y" queries.
func (b *Binder) HasMembership(m MembershipMatcher) bool

// MembershipCardinality is the count of memberships on the current value.
func (b *Binder) MembershipCardinality() int

// --- params access (for parametrized membership specs) ---
//
// When the current binding's membership is one of the parametrized specs
// (HighCardRefParametrized, LowCardRefParametrized,
//  MixedLowCardRefHighCardParameters, MixedLowCardVerbatimHighCardParameters)
// the params component is accessible via these methods. Returns nil/zero
// for non-parametrized specs.

func (b *Binder) Params() []byte

// Typed param decoders for common shapes. ParamsAsInt64Array is the typical
// path-with-slots case (params encode array indices [item_idx, sub_idx, …]).
func (b *Binder) ParamsAsInt64Array() []int64
func (b *Binder) ParamsAsString() string
func (b *Binder) ParamsAsCBOR(out any) error

// --- aspect inspection ---
func (b *Binder) HasUseAspect(a useaspects.AspectE) bool
func (b *Binder) HasValueAspect(a valueaspects.AspectE) bool

// --- nested-scope iteration (for tree-shaped construction) ---
//
// SubCollect iterates rows in a sub-scope rooted at the current binding,
// projecting each through fn. Used inside Collect closures to produce
// nested arrays in tree-shaped output. Constraint: sub must extend the
// current binding's path with one or more additional parameter positions.
func SubCollect[T any](b *Binder, sub Path, project func(*Binder) T) []T
func SubReduce[T any](b *Binder, sub Path, init T, fold func(T, *Binder) T) T
```

### Target abstraction

```go
type Target interface {
    // executePlan runs the type-erased plan; the result is consumed by
    // the Collect / Reduce / Run wrapper that called the Target.
    executePlan(ctx context.Context, p *Plan) (resultStream, error)
}

// InProcess drives a SinkI implementation against an Arrow batch.
// Useful for Parquet/Arrow snapshot queries and streaming consumers.
func InProcess(driver *streamreadaccess.Driver) Target

// ClickHouse lowers the plan to a CH SQL AST via the existing
// public/db/clickhouse/dsl/ast package, runs ToSQL, executes
// against the connection, and streams results back into Go values.
func ClickHouse(conn ClickHouseConn) Target
```

The split lets the same plan run either in-process (over Arrow data) or pushed to CH for execution at the storage layer. The branch is per-call: `Collect(ctx, plan, lwq.InProcess(driver), ...)` vs `Collect(ctx, plan, lwq.ClickHouse(conn), ...)`.

### Worked examples

**Framing.** The examples below mix membership specs and access patterns to illustrate the API surface, but they should not be read as a representative distribution of production Leeway usage. Examples that use `ForEachAt` and `SubCollect` (the first two below, and the last) operate over the `MixedLowCardVerbatimHighCardParameters` spec — the JSON-over-Leeway path-with-slots case from the SKILLS.md tutorial — kept here because the path syntax illustrates compositional construction concisely and connects to the most familiar Leeway shape. **In production Leeway tables, individual attributes more commonly carry non-parametrized memberships** (`LowCardRef` for small enums, `HighCardRef` for foreign-key-style references into entity dictionaries, `LowCardVerbatim` for named tags), with parametrized variants reserved for indexed dimensions such as time buckets, array elements, or sequence positions. The membership-spec series further down — `LowCardRef` enum filter, `HighCardRef` foreign-key lookup, parametrized binding, combinators, alias queries — is more representative of typical production patterns. A typical production query starts with `ForEachInSection` or `ForEachValueWithMembership(matcher)`, not with `ForEachAt(path)`; reach for the path form only when the section's `MembershipSpec` is `MixedLowCardVerbatimHighCardParameters` and the JSON-over-Leeway shape is what you actually have.

**Flatten + project (relational output, JSON-over-Leeway):**

```go
type ItemRow struct {
    Product string
    Qty     int64
}

rows, err := lwq.Collect(ctx,
    lwq.New(env).
        ForEachAt("/items/_").
        Where(func(b *lwq.Binder) bool { return b.Int64("/items/_/qty") > 0 }),
    target,
    func(b *lwq.Binder) ItemRow {
        return ItemRow{
            Product: b.String("/items/_/product"),
            Qty:     b.Int64("/items/_/qty"),
        }
    },
)
```

**Aggregation with inner scope (tree-shaped output via Go structs, JSON-over-Leeway):**

```go
type Order struct {
    ID    string  `json:"id"`
    Total float64 `json:"total"`
    Items []Item  `json:"items"`
}
type Item struct {
    Product string `json:"product"`
    Qty     int64  `json:"qty"`
}

orders, err := lwq.Collect(ctx,
    lwq.New(env).ForEachAt("/orders/_"),
    target,
    func(o *lwq.Binder) Order {
        items := lwq.SubCollect(o, "/orders/_/items/_", func(i *lwq.Binder) Item {
            return Item{
                Product: i.String("/items/_/product"),
                Qty:     i.Int64("/items/_/qty"),
            }
        })
        total := lwq.SubReduce(o, "/orders/_/items/_", 0.0,
            func(acc float64, i *lwq.Binder) float64 {
                return acc + float64(i.Int64("/items/_/qty"))*i.Float64("/items/_/price")
            })
        return Order{ID: o.String("/orders/_/id"), Total: total, Items: items}
    },
)
// JSON output: json.Marshal(orders); CBOR: cbor.Marshal(orders).
```

JSON / CBOR / Protobuf marshalling of the result is the concern of the standard encoding packages. The Go API does not need a "construction emitter" — Go's type system *is* the construction emitter.

**Membership: low-card-ref enum filter (LowCardRef spec):**

```go
const (
    refRoleAdmin  uint64 = 1
    refRoleEditor uint64 = 2
    refRoleViewer uint64 = 3
)

type UserRow struct {
    Entity  string
    RoleRef uint64
}

rows, err := lwq.Collect(ctx,
    lwq.New(env).
        ForEachValueWithMembership(lwq.RolePrimary,
            lwq.MatchLowCardRefIn(refRoleAdmin, refRoleEditor)),
    target,
    func(b *lwq.Binder) UserRow {
        m := b.CurrentMembership().(lwq.LowCardRef)
        return UserRow{Entity: b.EntityID(), RoleRef: m.Ref}
    },
)
```

**Membership: high-card-ref foreign-key-style lookup (HighCardRef spec):**

```go
// Find every value tagged with a high-card-ref membership pointing to
// dictionary entry 42 (e.g. "all values referenced by user #42").
type RefHit struct {
    Entity string
    Value  string
    Ref    uint64
}

hits, err := lwq.Collect(ctx,
    lwq.New(env).
        ForEachValueWithMembership(lwq.RoleAny, lwq.MatchHighCardRef(42)),
    target,
    func(b *lwq.Binder) RefHit {
        m := b.CurrentMembership().(lwq.HighCardRef)
        return RefHit{Entity: b.EntityID(), Value: b.String(""), Ref: m.Ref}
    },
)
```

**Membership: parametrized — bind params for projection (LowCardRefParametrized spec):**

```go
// Find values tagged "windowedAvg" with parametrized time windows;
// bind the params and decode them in the closure.
type WindowRow struct {
    Entity string
    Start  int64
    End    int64
    Value  float64
}

rows, err := lwq.Collect(ctx,
    lwq.New(env).
        ForEachValueWithMembership(lwq.RoleAny,
            lwq.MatchLowCardRefParametrized(refWindowedAvg, lwq.ParamProject())),
    target,
    func(b *lwq.Binder) WindowRow {
        var w struct{ Start, End int64 }
        _ = b.ParamsAsCBOR(&w)
        return WindowRow{
            Entity: b.EntityID(),
            Start:  w.Start,
            End:    w.End,
            Value:  b.Float64(""),
        }
    },
)
```

**Membership: combinator — union across spec classes:**

```go
// Find values whose primary membership is EITHER one of the small enum
// (low-card-ref) OR a high-card-verbatim matching a regex.
hits, err := lwq.Collect(ctx,
    lwq.New(env).
        ForEachValueWithMembership(lwq.RolePrimary,
            lwq.MatchAnyOf(
                lwq.MatchLowCardRefIn(refRoleAdmin, refRoleEditor),
                lwq.MatchHighCardVerbatimPattern(`^special-key-.*$`),
            )),
    target,
    func(b *lwq.Binder) Hit {
        return Hit{Entity: b.EntityID(), Value: b.String("")}
    },
)
```

**Membership: alias query via multi-membership (any spec, cardinality-2+):**

```go
// Find values bearing two specific primary memberships simultaneously
// (i.e. the value plays both roles — the multi-membership "alias" pattern).
type Alias struct {
    Value  float64
    Roles  []lwq.Membership
    Entity string
}

aliases, err := lwq.Collect(ctx,
    lwq.New(env).
        ForEachValueWithMembership(lwq.RolePrimary,
            lwq.MatchAllOf(
                lwq.MatchLowCardVerbatim("/price/current", lwq.ParamWildcard()),
                lwq.MatchLowCardVerbatim("/stats/min",     lwq.ParamWildcard()),
            )),
    target,
    func(b *lwq.Binder) Alias {
        return Alias{
            Value:  b.Float64(""),
            Roles:  b.MembershipsByKind(lwq.RolePrimary),
            Entity: b.EntityID(),
        }
    },
)
```

**Path-with-slots (the JSON-over-Leeway sugar, one specific spec):**

```go
// Convenience for the MixedLowCardVerbatimHighCardParameters case only.
// Equivalent to:
//   ForEachValueWithMembership(RoleAny,
//     MatchMixedLowCardVerbatim("/items/_/qty", ParamProject()))
type ItemRow struct {
    Entity  string
    ItemIdx int64
    Qty     int64
}

rows, err := lwq.Collect(ctx,
    lwq.New(env).ForEachAt("/items/_/qty"),
    target,
    func(b *lwq.Binder) ItemRow {
        idx := b.ParamsAsInt64Array()[0]
        return ItemRow{Entity: b.EntityID(), ItemIdx: idx, Qty: b.Int64("")}
    },
)
```

**Aspect-driven vector search:**

```go
type DistResult struct {
    Entity string
    Dist   float64
}

queryVec := []float32{ /* ... */ }

results, err := lwq.Collect(ctx,
    lwq.New(env).
        ForEachValueWithAspect(valueaspects.AspectMachineLearningEmbedding).
        OrderByAsc(func(b *lwq.Binder) any {
            return cosineDistance(b.Float32Array(""), queryVec)
        }).
        Limit(10),
    target,
    func(b *lwq.Binder) DistResult {
        return DistResult{
            Entity: b.EntityID(),
            Dist:   cosineDistance(b.Float32Array(""), queryVec),
        }
    },
)
```

**Co-section overlay (PII annotation join):**

```go
type PIIRow struct {
    Value          string
    Classification string
}

rows, err := lwq.Collect(ctx,
    lwq.New(env).
        ForEachInSection("string").
        JoinCoSection("string__pii_labels"),
    target,
    func(b *lwq.Binder) PIIRow {
        secondary := b.SecondaryRoles()
        cls := ""
        if len(secondary) > 0 {
            cls = secondary[0]
        }
        return PIIRow{Value: b.String(""), Classification: cls}
    },
)
```

## Phased delivery

The decision recorded by this ADR is to start through v1.

- **v0** — core operators (`ForEachAt`, `ForEachInSection`, `Where`, `OrderBy`, `Limit`, `Collect`, `Reduce`), `*Binder` typed accessors, in-process target via `SinkI`. Path-string runtime validation against the schema env. Target audience: in-process pipelines over Arrow batches.
- **v1** — ClickHouse target (CH SQL emit via existing AST and `ToSQL`), full membership matcher surface covering all eight `MembershipSpec` variants (single-spec matchers, parametrized variants, `ParamMatcher` family, boolean combinators), `ForEachValueWithMembership` operator with cardinality- and spec-aware predicate pushdown, aspect operators (`ForEachValueWithAspect`, `HasValueAspect`), co-section join (`JoinCoSection`), nested-scope helpers (`SubCollect`, `SubReduce`). Both execution targets fully usable.
- **v2** — Leeway codegen extension: emit typed `*Binder` structs per section so paths and types are checked by `go build` (e.g. `b.Items.Product()` instead of `b.String("/items/_/product")`). Major ergonomic win for codegen-heavy callers.
- **v3** — FLWOR text frontend per [ADR-0022](0022-leeway-lwq-flwor-query-language.md): the parser produces the same `*Plan` as the Go API and reuses both backends. Enables the SQL-proxy use case (text-form `lwq` embedded in SQL, parsed by the proxy, executed via the CH target).

Indicative scope: v0 + v1 together is on the order of a few thousand lines of net-new code, smaller than ADR-0022's full v0/v1 because no grammar, parser, or separate AST is involved at this stage. Wall time is deferred to the implementation plan.

## Alternatives

- **Implement ADR-0022 verbatim (FLWOR text frontend first).** Rejected for v1: the Go API delivers the same structural capabilities with strictly less new infrastructure (no grammar, no parser, no AST). The text frontend remains valuable for the SQL-proxy use case and is sequenced as v3.
- **Skip the Go API entirely and ship only the text frontend.** Rejected: Go consumers (the dominant downstream audience) lose compile-time schema validation, IDE autocompletion, refactoring safety, and native composition. The text frontend serves only the proxy and external-consumer audiences.
- **Build both frontends simultaneously.** Rejected: doubles the v1 cost without doubling the value while one of the two has no concrete consumer yet. Sequencing is cheaper and lets the Go API's adoption signal shape the text frontend's design.
- **Single execution target (CH only) for the Go API.** Rejected: the in-process target via `SinkI` is essentially free given the existing `streamreadaccess` package, and it unlocks Parquet / Arrow snapshot queries the CH target cannot serve.
- **Build the Go API on top of `clickhouse/dsl/astbuilder` directly, skipping the `*Plan` abstraction.** Rejected: the `*Plan` abstraction is what lets the same query target both backends. Without it, the Go API is locked to CH, the in-process target requires a separate API, and the future text frontend has no shared substrate to lower onto.

## Consequences

### Positive

- Compile-time schema validation via Go's type system; `gopls` provides autocompletion and refactoring without LSP investment.
- Two execution targets share one frontend; the in-process target enables Parquet / Arrow snapshot queries that the text-plus-CH path could not serve.
- No grammar, parser, or AST in v1 — the parser-shaped infrastructure is deferred to v3 with the text frontend, where it is justified by the SQL-proxy use case.
- Reuses the existing `SinkI` / `Driver` pattern — the natural data-flow shape for Leeway-aware operations, already used by the schema-doc and debug emitters.
- Reuses the existing CH AST and `ToSQL` emitter for the CH target.
- Tree-shaped output is Go's native struct-plus-marshalling story; no construction-emitter abstraction is needed.
- Nested-scope iteration (`SubCollect`, `SubReduce`) gives the same compositional power as FLWOR's nested `for` / `return`, in Go syntax.
- The text frontend (v3) parses to the same `*Plan`, so the Go API's design is forward-compatible with ADR-0022's trajectory.

### Negative

- Verbosity vs the FLWOR text form. Go's lambda-and-builder syntax is heavier than nested `for` / `return` constructors.
- Go-only authoring at v1; non-Go consumers must wait for v3 (text frontend).
- The `*Plan` abstraction is type-erased at the Go API level; type-introducing terminals (`Collect`, `Reduce`, `Sum`) are free functions because Go does not allow generic methods. This is idiomatic Go but less fluent than fully-chainable APIs in other languages.
- Codegen-typed binders (v2) require an extension to the Leeway SDK; until v2 lands, paths are strings checked at runtime.
- Two execution targets in v0 and v1 expand the surface vs a CH-only path. The split pays off when in-process Parquet queries become a real consumer; before then it is design overhead.
- The full membership-model surface (eight `MembershipSpec` variants, parametrized matchers, `ParamMatcher` family, boolean combinators) is wider than the path-with-slots case used in tutorials. Users coming from JSON-over-Leeway will find `ForEachAt` familiar, but real-world Leeway sections often use other specs (`HighCardRef`, `LowCardRef`, parametrized variants) and require fluency with the broader matcher surface to query at full power.

### Neutral

- ADR-0022 stands. This ADR sequences its phasing rather than superseding it. The text frontend remains the long-term direction; only the order of implementation changes.
- The Go API is a peer of `public/db/clickhouse/dsl/astbuilder` (CH SQL fluent builder) and the Leeway SDK's read-access codegen. The pattern of "fluent typed Go API" is established in the boxer ecosystem.
- The decision to start with `*Plan` as a type-erased core (rather than `*Plan[T]` generic) is a Go-language pragmatic choice. Switching to generic later would require breaking-change migration; deciding now keeps options open.

## Status

Proposed — awaiting review by Leeway and CH DSL maintainers and a downstream architecture review.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0022: leeway lwq — FLWOR-style query language for Leeway-stored data](0022-leeway-lwq-flwor-query-language.md) — the trajectory this ADR sequences.
- Leeway `SinkI` / `Driver` substrate — [`../../public/semistructured/leeway/streamreadaccess/leeway_onlineapi_types.go`](../../public/semistructured/leeway/streamreadaccess/leeway_onlineapi_types.go).
- [Leeway protocol skill](../skills/leeway-advanced/SKILLS.md) — sections, memberships, aspects, co-sections, canonical types.
- [ADR-0007](0007-leeway-membership-role-classifier.md) — primary/secondary role classifier consumed by `ForEachValueWithRoles`.
- [ADR-0018](0018-leeway-card-json-canonical-format.md) — a representative tree-shaped construction target.
- Boxer CH DSL infrastructure — [`../../public/db/clickhouse/dsl/`](../../public/db/clickhouse/dsl/) (AST and `ToSQL` reused by the ClickHouse target; `astbuilder` is a precedent for fluent Go API at the lower CH-SQL level).
