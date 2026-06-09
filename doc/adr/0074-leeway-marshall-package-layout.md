---
type: adr
status: proposed
date: 2026-06-09
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0074: Leeway marshall package layout

## Context

ADR-0070…0073 re-cut the leeway DTO-codec onto an orthogonal conceptual basis (entity
assembly, value, emission, membership carriage, membership meaning). The **package
layout** never caught up: the three packages that realise that model sit flat and
unrelated in `leeway/`.

- `mappingplan` — the schema-agnostic **Plan IR**: `Plan`, `PlainCol`, `TaggedField`,
  `FieldFlags`, the `MembershipChannel` taxonomy, *and* the Go-DTO construction machinery
  (`PlanBuilder`, `FieldShape`, `ScalarCanonicalForGoType`, section grouping, `lw:` tag
  parsing).
- `marshallgen` — go/ast front-end + code emitter; builds a `Plan`, emits a `.out.go` codec.
- `marshallreflect` — reflect front-end + runtime interpreter; builds the *same* `Plan`,
  drives a DTO↔DML chain by reflection.

What the dependency graph actually shows (non-test imports):

- `mappingplan` ← `marshallgen`, `marshallreflect`, **and three non-marshall consumers**:
  `dql` (the query/read-back generator, ADR-0066), `keelson/runtime/codec/factswrapper`,
  and the imzero2 `mappingplanview` widget (the "plan playground").
- The two marshallers do **not** import each other; every `marshallgen`↔`marshallreflect`
  reference is comment-only (doc prose, generated-file header).
- `marshalltypes` (the Cut-2 carrier structs) ← both `marshallgen` and `marshallreflect`.

Those three non-marshall consumers split cleanly along the IR/machinery seam this ADR
draws: `dql` and `factswrapper` touch only the Plan IR (`Plan` / `TaggedField` /
`PlainCol` / `MembershipChannel`), while `mappingplanview` *also* drives the construction
machinery (`PlanBuilder`, `FieldShape`, `ScalarCanonicalForGoType`, `SplitLW`) — it
straddles the seam exactly as the two marshallers do.

Two facts shape the decision:

1. **The Plan IR is a neutral foundation, not a marshall target.** `dql` is *itself* a
   marshall target — the ClickHouse-SQL implementation — consuming `Plan` / `TaggedField` /
   `MembershipChannel` / `FieldFlags` / `PlainCol` to generate read-back SQL. The IR is
   consumed by *every* marshall target (the Go codec front-ends and `dql` alike) plus
   non-marshall IR readers (`factswrapper`), so it must stay out of any one target's
   namespace. A second neutral foundation — `ddl`, the multi-technology type generators
   (`ddl/arrow`, `ddl/clickhouse`, `ddl/golang`), shared by ~10 callers — plays the same
   role; it likewise stays put.
2. **The two Go front-ends share a contract, not code.** `marshallgen.classifyType` (over
   `go/ast`) and `marshallreflect.classifyReflectType` (over `reflect.Type`) are parallel
   implementations of one classification contract, both funnelling through
   `PlanBuilder` / `FieldShape` / `ScalarCanonicalForGoType`. The genuine common
   substance is that **construction machinery**, not the front-ends themselves.

A literal `leeway/marshall/go` package is also blocked by the language: `go` is a keyword,
so no package may be named `go`. `leeway/marshall/go` can only be a *namespace directory*
whose child packages carry legal names.

## Design space (QOC)

**Question.** How should `mappingplan`, `marshallgen`, and `marshallreflect` be laid out
to make their kinship explicit without inverting the pipeline's layering?

**Options.**

- **O1 — Reparent only.** Leave `mappingplan` and `marshalltypes` in place; nest the two
  marshallers under a `marshall/go/` namespace dir.
- **O2 — Whole move.** Move all of `mappingplan` to `leeway/marshall/go` (package renamed,
  since `go` is illegal); nest the marshallers under it. `dql` then imports `marshall/go`.
- **O3 — Split by tier.** Keep the Plan IR neutral in `mappingplan`; extract the Go-DTO
  construction machinery to a shared `leeway/marshall/go/goplan` package; nest the two
  marshallers as siblings of it.

**Criteria.**

- **C1 — Layering fidelity.** Does the Plan IR stay a neutral foundation that every target
  consumes, rather than being owned by one target's namespace?
- **C2 — gen∩reflect cohesion.** Does the new shared home hold *exactly* what the two
  front-ends share, and nothing else?
- **C3 — Blast radius.** Import sites touched, goldens, generated-header churn.
- **C4 — Naming legality / convention.** Avoids `package go`; avoids dir≠package.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | −− | ++ |
| C2 | −  | +  | ++ |
| C3 | ++ | −  | −  |
| C4 | +  | −  | +  |

O3 wins on the two criteria that motivated the request (layering and cohesion) at the cost
of the largest blast radius.

## Decision

We will adopt **O3 — split by tier**. Target layout:

```
leeway/
  mappingplan/                   the Plan IR — UNCHANGED import path (foundation)
  ddl/                           technology-specific type generators — UNCHANGED (foundation)
  marshall/
    marshalltypes/               carrier structs (moved from leeway/marshalltypes)
    go/                          namespace directory (no package — `go` is a keyword)
      goplan/                    package goplan: Go-DTO → Plan construction machinery
      marshallgen/               go/ast front-end + emitter
      marshallreflect/           reflect front-end + interpreter
    clickhouse/                  namespace directory
      readback/                  package readback: ClickHouse read-back SQL generator
                                 (moved + renamed from leeway/dql)
```

The two **foundations** — `mappingplan` (the logical Plan IR) and `ddl` (the
`arrow` / `clickhouse` / `golang` type generators) — stay where they are. Every marshall
*target* under `marshall/` consumes them: the Go target builds and drives a `Plan`; the
ClickHouse target (`readback`) joins a `Plan` against a physical ClickHouse schema and
emits read-back SQL, importing `ddl/clickhouse` for type rendering. `ddl` is not
ClickHouse-specific and has ~10 callers, so it is not pulled under `marshall/clickhouse`.

**Symbol partition.**

- *Stays in `mappingplan` (the IR, consumed by `dql` + both marshallers):* `Plan`,
  `PlainCol`, `TaggedField`, `FieldFlags`; `MembershipChannel` with `ChannelCardinalityE` /
  `ChannelIdentityE`, the channel constants, the private `channelTable` / `channelDescriptor`
  / `desc` / `validateChannelTable`, and all `MembershipChannel` methods; the model methods
  `PlanCol.GoType`, `TaggedField.{GoType,IsSlice,IsRoaring,IsMulti,KindVar,Section}`; and the
  two helpers those methods depend on — `UpperFirst` and `DeriveGoShape` (today's unexported
  `deriveGoShape`, **exported** so `goplan` can reuse the single canonical→Go rule).
- *Moves to `leeway/marshall/go/goplan`:* `PlanBuilder` (`NewPlanBuilder`, `AddField`,
  `AddUnderscoreField`, `Finish`), `FieldShape`, `ScalarCanonicalForGoType`,
  `RoaringElemCanonical`, `FixedByteArrayLen`, `IsFixedByteArray`, `CopyStratE` /
  `CopyStrategy`, `ValidatePlainColumnShape`, `ComputeGroups`, `SectionGroup`, `SubColumn`,
  `FieldBeginShape`, `ClassifyBegin`, `SingleValueReadAccessor`, `FindPlainCol`, `SplitLW`,
  `ParsedLWTag`, `PlainArrowArrayType`, `IsSupportedPlainType`.

The resulting dependency edges are acyclic: `goplan → mappingplan`; `marshallgen` /
`marshallreflect → {goplan, mappingplan, marshalltypes}`; `dql → mappingplan` (unchanged).
`mappingplan` imports neither marshaller nor `goplan`, and pulls in no `go/ast` or
`reflect`.

`go` is a directory, not a package. The shared package is `goplan` — a legal identifier
naming "the Go-DTO plan builder."

## Alternatives

- **O1 — Reparent only.** Rejected: cheapest, but `marshall/go` would hold *nothing*
  shared — the common substance stays at `leeway/mappingplan`, so the move buys directory
  cosmetics without expressing the gen∩reflect commonality the request is about.
- **O2 — Whole move.** Rejected: buries the shared Plan IR under the *Go* target, so the
  ClickHouse target (`readback`) and non-marshall IR readers (`factswrapper`) would import
  `marshall/go` for an IR that is not Go-specific — and it pairs a directory named `go`
  with a differently-named package (a known footgun).

## Consequences

### Positive

- The package tree mirrors the ADR-0070…0073 conceptual basis: the IR is one neutral
  package; `marshall/go/goplan` is *exactly* the gen∩reflect construction machinery; the two
  back-ends are visibly siblings under `marshall/go`.
- `factswrapper` (a non-marshall IR reader in keelson runtime) is untouched — its entire
  `mappingplan` surface stays put — the test that the IR/machinery seam sits in the right
  place. `dql` *moves* to `marshall/clickhouse/readback` but needs no internal surgery: it
  already consumes only the IR, joins it against the physical schema, and emits SQL.
- Marshall targets sit side by side under `marshall/` (`go/`, `clickhouse/`); a future
  target slots in as `leeway/marshall/<target>/…`, and a ClickHouse write side would sit
  beside `readback/` under `clickhouse/`.

### Negative

- Largest blast radius of the three: `mappingplan` is split across two packages; the two
  marshallers and the imzero2 `mappingplanview` widget re-point their machinery imports to
  `goplan`, and `marshalltypes`' importers re-point to `marshall/marshalltypes`. The move
  ripples beyond leeway into imzero2 (the playground widget + its demo/tests).
- Exporting `DeriveGoShape` widens `mappingplan`'s API by one function (the canonical→Go
  rule, previously private).

### Neutral

- Generated `.out.go` files carry a "Code generated by …/marshallgen" header naming the new
  path; the `dronemission` golden is refreshed by hand (it is already out of `generate.sh`).
- `goplan` gains `mappingplan` as an import where the code previously lived in the same
  package; same-package private references (`deriveGoShape`) become the exported call.
- The `dql` move is low-risk in isolation — it has zero importers — but renames its package
  (`dql` → `readback`) and files (`lw_dql_*` → `lw_readback_*`). ADR-0066 (the read-back
  generator design, accepted) stands; it gains a dated `## Updates` entry recording the
  relocation, per the Tier-2 in-place policy — no new superseding ADR.

## Status

Proposed — awaiting review by the leeway code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0066](0066-leeway-dql-clickhouse-readback-generator.md) — the read-back generator (today's `dql`) reframed here as the ClickHouse marshall target; gains a dated `## Updates` entry for the relocation to `marshall/clickhouse/readback`.
- [ADR-0070](0070-leeway-entity-assembly.md) · [ADR-0071](0071-leeway-value-and-emission.md) · [ADR-0072](0072-leeway-membership-carriage.md) · [ADR-0073](0073-leeway-membership-role.md) — the conceptual orthogonalisation this layout mirrors.
- [`../../public/semistructured/leeway/mappingplan/`](../../public/semistructured/leeway/mappingplan/) · [`marshallgen/`](../../public/semistructured/leeway/marshall/go/marshallgen/) · [`marshallreflect/`](../../public/semistructured/leeway/marshall/go/marshallreflect/) — the packages reshaped here.
