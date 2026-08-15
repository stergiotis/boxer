---
type: adr
status: proposed
date: 2026-08-15
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0189: component SQL as an authoring surface — export the artefacts, register them, expand them

## Context

[ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md) generates four
ClickHouse artefacts from one component definition: **Presence** (a cheap,
index-eligible prefilter), **Validator** (the exact conformance check),
**Filter** (Presence AND Validator, the form for `WHERE`) and **Projection**
(a `CAST(tuple(…), 'Tuple(slot T, …)')` extracting every field).

`recordstore/gen` computes all four and keeps one. In
[`store_emit.go`](../../public/storage/recordstore/gen/store_emit.go) the
generator runs `readback.Generate` and assigns only `artefacts.Filter`; the
Projection is produced and discarded. What it bakes is a per-kind string
constant named `<table>Scan<Kind>Filter` — unexported, and unexported only
because the table name it is built from happens to be lower-case.

So a component definition already produces the predicate a SQL author wants,
and the author cannot reach it. Today the routes are:

- **Copy it out of generated code.** `factsScanSysMemFilter` is ~4.5 KB of
  `hasAll` / `countEqual` terms over physical column names. It is correct, and
  it goes stale silently the moment a section is re-aspected.
- **Hand-write the read.** [ADR-0184](./0184-sysmetrics-persistence-tee.md)
  §SD6 records how that goes: the `chan:` token is mandatory on a
  multi-channel table, the verb follows the section's arity rather than the Go
  field's type, and a ref membership takes a registry id rather than a name.
  All three were written down wrong in that ADR before an expansion golden was
  built for it.

The [jsonbench-on-facts trial](../trials/jsonbench-on-facts/README.md) is the
standing evidence for what this costs: four separate findings, each one the
trial hand-writing something the repository already provided, one of them
silently truncating.

**One property makes this more than an ergonomics gap.** ADR-0066's 2026-07-27
Update states it plainly: `Projection` alone is **not** the exact read. It
locates an attribute with `indexOf`, so under a membership carried by more
than one attribute it silently returns the first. `Validator` rejects that
row; `Filter` is the form carrying both. *A caller embedding Projection
without Filter gets first-match semantics, not conformance.* Any surface that
hands authors the Projection has to make the Filter travel with it, or it
ships a footgun whose failure mode is a plausible wrong answer.

## Design space (QOC)

**Question.** How does a component definition's generated SQL reach a SQL author?

**Options.**

- **O1** — export the Filter constant; nothing else changes.
- **O2** — publish all four artefacts per kind, register them, and expand them
  through a client-side `LW_` pass.
- **O3** — install per-kind UDFs on the server (`CREATE FUNCTION` per kind).
- **O4** — status quo: copy out of generated code.

**Criteria.** C1 authoring ergonomics; C2 conformance by construction (the
Projection/Filter trap); C3 index pruning preserved; C4 server provisioning
burden; C5 works against an endpoint carrying nothing; C6 generated-output
size.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | ++ | ++ | −− |
| C2 | −  | ++ | +  | −− |
| C3 | ++ | ++ | −  | ++ |
| C4 | ++ | ++ | −− | ++ |
| C5 | ++ | +  | −− | ++ |
| C6 | ++ | −  | ++ | ++ |

O4 is what exists and what the trial measured the cost of. O1 is a one-line
change that solves the predicate and leaves the extraction — the half the
author actually writes queries with — unreachable, and it hands over the
Projection trap unguarded the day someone exports that too. O3 buys the
nicest call site and loses the most: per-kind UDFs are a server-side install
re-run on every vocabulary or schema change, thirteen functions for
sysmetrics alone, and they break the client/server split the
[read-surface page](../explanation/leeway-sql-read-surface.md) draws — a
client-expanded name works against any endpoint, including one carrying
nothing. It also discards ADR-0100 S2's property that the Filter needs no
install at all. **O2 is taken.**

## Decision

### SD1 — `recordstore/gen` publishes all four artefacts per kind

The generator emits, beside the existing per-kind constants, one exported
package-level value per store:

```go
var SysmetricsComponentSQL = componentsql.Set{
    Table: "boxer.facts",
    Kinds: map[string]componentsql.Artefacts{
        "SysMem": {Presence: …, Validator: …, Filter: factsScanSysMemFilter, Projection: …},
        …
    },
}
```

The Filter field references the existing constant rather than repeating it, so
the string has one definition and the `Scan` verb and the authoring surface
cannot disagree. Presence and Validator are published because they are already
computed and because the trichotomy is what makes the Filter's index story
legible; only Filter and Projection get SQL-visible names in v1 (SD3).

`Projection` stops being discarded. This is the whole cost of SD1 on the
generator side: it is one more field kept from a value already built.

### SD2 — the registry is a leeway leaf, not a storage one

`componentsql` lives beside
[`leeway/marshall/clickhouse/readback`](../../public/semistructured/leeway/marshall/clickhouse/readback),
which already owns the `Artefacts` type this publishes. It holds the types
(`Artefacts`, `Set`), a `Registry` with `Register` / `Lookup`, and a `Default`.

The alternative — placing it under `storage/recordstore` — reads naturally
from "the record store generates it" and inverts the layering: the nanopass
pass that consumes it sits in the SQL layer, and would import a storage
package to do so. Artefacts are a leeway concept that `recordstore/gen`
happens to bake; the registry follows the concept.

The package imports nothing from `recordstore`, so generated stores and the
pass both import it without a cycle.

### SD3 — the pass: `LW_COMPONENT` and `LW_COMPONENT_FILTER`

A new client-side pass, `LwComponentExpand`, registered through
[`keelson/data/passreg`](../../public/keelson/data/passreg) like the extraction family, and
ordered before it so one statement may mix both.

- **`LW_COMPONENT('<Kind>')`** expands to that kind's Projection — a named
  tuple, so `SELECT LW_COMPONENT('SysMem') AS m … m.TotalBytes` addresses
  slots by their Go field names.
- **`LW_COMPONENT_FILTER('<Kind>')`** expands to the Filter predicate alone,
  for an author who wants the row set without the tuple.

Names follow the one settled `LW_` convention: single prefix, `UPPER_SNAKE`.

### SD4 — the Filter is injected into `WHERE`, once per kind per scope

Seeing a `LW_COMPONENT` call in a scope, the pass ANDs that kind's Filter into
the scope's `WHERE`, deduplicated per kind. A scope with no `WHERE` gains one;
an existing predicate is parenthesised, so `a OR b` becomes
`(a OR b) AND <filter>`.

This is the answer to the ADR-0066 trap, and it is chosen over the two
alternatives on one property: **the Presence terms only prune granules from
`WHERE`.** ADR-0066 records that ClickHouse prunes for `has`/`hasAll` through
a bloom-filter skip index and never for the validator's `countEqual`, so a
guarded `if(<Filter>, <Projection>, NULL)` expression — self-contained, and a
much smaller pass — would be correct and would force a full scan on every
component read. Injection keeps the artefact where its index story works.

Two kinds referenced in one scope inject both filters, AND-ed. That is the
right reading rather than a conflict: a row may carry several components, and
the conjunction is exactly "the row conforms to both".

The precedent for a pass restructuring `WHERE` is
[ADR-0121](./0121-selection-condition-columns.md)'s
`ExposeSelectionConditions`, which rebuilds the clause from named conditions —
and which also sets the discipline SD5 follows.

### SD5 — the pass declines rather than guessing

`ExposeSelectionConditions` refuses on a name collision because rewriting
anyway would silently shadow a stored column. The same posture here. The pass
returns an error, naming the call, when:

- the kind is not in the registry — a typo must not expand to nothing;
- no table in the scope's `FROM` is the kind's bound table;
- the bound table appears more than once, or under an alias (SD6).

An unresolvable call is an error, never a partial rewrite — the property
`ExtractExpandPass` already states about itself.

### SD6 — v1 binds a single unaliased table; qualification is deferred

The baked artefacts carry **bare, unqualified** physical column names. That
was free for the `Scan` verb, which only ever reads one table, and it is a
constraint the moment the strings are embedded in arbitrary SQL: in a join,
those names are ambiguous.

v1 therefore supports the scope whose `FROM` names exactly one table, and that
table the bound one; everything else is refused with a message that says so.
Two refinements landed while building it: the check is on table *count*, not
on the alias (a single aliased source resolves unqualified names
unambiguously — it is the second source that creates the problem, M2); and
the database half is compared only when one is known, since every pass in the
standard registry is wired with an empty default and a strict comparison
would refuse `FROM facts` everywhere (M3). Qualifying the
columns means the artefacts can no longer be opaque strings — the generator
would emit them structured, or emit a qualifier hole — which is a change to
ADR-0066's output shape and is **deferred with the trigger: the first query
that needs a component read joined to another table.**

Descoping this rather than gating on it is deliberate: the single-table read
is what every consumer named so far wants, including `loadstudy`.

### SD7 — hosts wire the registry explicitly

Generated stores publish; nothing self-registers. A host calls
`componentsql.Default.Register(sysmfacts.SysmetricsComponentSQL)` at its
wiring site, the way `passreg.RegisterStandard` and `play.RegisterPasses`
already work, and `play` is the first such host.

Import side-effect registration was the alternative — the leeway vocabularies
populate that way. It loses here because a registry populated by package init
has a link-set-dependent extent, which is the caveat `storegen.MembershipIds`
already documents about exactly this pattern; an authoring surface that
silently knows about fewer kinds in one binary than another is worse than one
line of wiring.

### SD8 — the two names have different server dependencies, and the panel says so

`LW_COMPONENT_FILTER` expands to ClickHouse built-ins only (`has`, `hasAll`,
`countEqual`) — the ADR-0100 S2 property — so it runs against any endpoint.
`LW_COMPONENT` expands to the Projection, which ADR-0066 records as
referencing the leeway DQL helper UDFs. play's Vocabulary tab marks the family
against `ExpansionDependencies` the way the extraction family already is, so
"client-expanded" does not read as "works everywhere".

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `recordstore/gen` emitted output | new — one exported `componentsql.Set` per store; Projection no longer discarded | all nine generated stores regenerate |
| `leeway/marshall/clickhouse/componentsql` (exported Go API) | new — `Artefacts`, `Set`, `Registry`, `Default` (SD2) | the pass; every generated store |
| `LwComponentExpand` (named pass, `passreg`) | new — the pass and its two `LW_` functions (SD3) | play's pass set; the Vocabulary tab |
| `LW_COMPONENT` / `LW_COMPONENT_FILTER` (SQL-visible names) | new — the `LW_` namespace gains two entries | the vocabulary panel; user-facing docs |
| `play` wiring | +registry registration for the stores it reads (SD7) | a second host when one appears |
| `<table>Scan<Kind>Filter` constants | **unchanged** — still unexported, still what `Scan` uses | nothing |
| `boxer.facts` schema, DDL, rows | **unchanged** — this is a read-authoring change only | nothing |

## Alternatives

- **Export the Filter constant only (O1).** One line, and it leaves the
  extraction half — the half queries are written with — unreachable. It also
  postpones the Projection/Filter trap rather than answering it.
- **Per-kind server UDFs (O3).** The nicest call site, bought with a
  provisioning step re-run on every vocabulary change and a break in the
  client/server split. Rejected in the QOC above.
- **Guarded `if(<Filter>, <Projection>, NULL)` (SD4).** Correct, self-contained
  and a much smaller pass; loses skip-index pruning outright, because Presence
  prunes only from `WHERE`. The reason SD4 exists as written.
- **Refuse unless the author also wrote the filter (SD4).** Fully explicit and
  index-preserving, and it makes the common read two calls that must agree.
  Injection with a named refusal (SD5) gets the same safety without the
  duplication.
- **Import side-effect registration (SD7).** Rejected on link-set-dependent
  extent, a caveat this repository has already written down once.
- **Registry under `storage/recordstore` (SD2).** Rejected on layering: it
  would put a storage import in the SQL pass.

## Consequences

### Positive

- The predicate and the extraction a component definition already produces
  become reachable, from SQL, without copy-paste and without hand-written
  array arithmetic.
- The ADR-0066 first-match trap stops being a caveat a caller must remember:
  the surface that offers the Projection is the one that injects the Filter.
- A component read gains an independent oracle — `LW_COMPONENT('SysMem')` and
  the store's own `ScanSysMem` are two read paths over one definition, and can
  be asserted equal.
- ADR-0184 §SD6's ergonomic cost shrinks for whole-component reads: no
  registry ids, no `chan:` token, no section-arity rule to remember.

### Negative

- Generated stores grow by the Projection strings, which are per-field rather
  than per-membership and so larger than the Filters already baked. Measured
  at M1: 5–10% on seven stores, **+55% on the facts-bound one**, of which
  about a third is Presence and Validator text the Filter constant already
  carries. If that becomes objectionable the two unnamed artefacts are the
  first thing to drop; the `SharedRA` precedent (ADR-0184) is the shape to
  reach for after that.
- A pass that rewrites `WHERE` is a heavier thing to own than one that rewrites
  an expression in place, and it interacts with ADR-0121's condition pass —
  ordering between them is a thing that must now be got right and tested.
- The `LW_` namespace gains two names whose expansion is *table-bound*, unlike
  every other member of the family, which is section-bound. SD6's single-table
  restriction is the visible edge of that.
- One more registry a host must wire, and one more thing that is empty if
  nobody wires it.

### Neutral

- No change to the leeway encoding, the facts schema, any DDL, any wire
  format, or the `Scan` verb's behaviour. Additive at every seam.
- ADR-0100 SD5's point-lookup path stays as it is: it deliberately does not
  use the SQL artefacts, and this ADR does not change that.

## Migration — Tier 1

- **Breaks.** Nothing. The existing per-kind constants keep their names and
  their unexported status; nothing outside the generated file referenced them.
- **Path.** Additive. The new package is new; the pass is opt-in per host;
  an unregistered kind is an error only for a query that names it.
- **Regeneration.** All nine generated stores regenerate; the eight carrying components gain their `Set` (M1).
  Output is otherwise byte-identical, so the diff is reviewable as "one new
  value per store". No FFI boundary is involved.
- **Data.** Nothing at rest changes. This ADR adds no writer and no column.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the emitted-output golden, the pass's
  expansion goldens and its refusal cases; the `//go:build integration` lane
  for the cross-oracle equality against a live server.
- **What would fail.**
  - **The trap, made mechanical.** A test asserting that no expansion path
    yields a Projection without that kind's Filter in the same scope's
    `WHERE` — the ADR-0066 property, which is otherwise a caveat nothing
    checks.
  - **The trap, demonstrated.** A row carrying one membership twice: the
    component read must reject it rather than returning the first attribute.
    This is the failure the Validator exists for, and it is the one a
    Projection-only surface would return a plausible wrong answer for.
  - Filter injection: exactly once per kind per scope; two kinds AND together;
    an existing `OR` predicate is parenthesised before the conjunction.
  - Each SD5 refusal, by its own message: unknown kind, no bound table in
    `FROM`, aliased or repeated bound table (SD6).
  - An expansion golden per kind for sysmetrics, pinned like ADR-0184's
    read-surface golden, so a re-aspected section or a re-keyed vocabulary
    goes red at the authoring surface too.
  - **Cross-oracle.** `LW_COMPONENT('<Kind>')` and the generated
    `Scan<Kind>` return the same values for the same rows, against a live
    server. Two read paths from one definition disagreeing is the thing most
    worth catching, and neither path can be its own oracle.
- **Gap.** SD6's join/alias case is refused rather than supported, so nothing
  verifies qualified emission — that arrives with the trigger. Presence and
  Validator are published but have no SQL-visible name in v1, so only their
  Go-level shape is pinned.

## Status

Proposed — awaiting review by the code owner.

- **M0 — `componentsql`: the types, the registry, `Default` (SD2).** ✓
  Landed as
  [`leeway/marshall/clickhouse/componentsql`](../../public/semistructured/leeway/marshall/clickhouse/componentsql).
  `Register` is all-or-nothing and refuses a duplicate kind naming both
  publishers, rather than resolving by wiring order; a kind with no Filter or
  no Projection is refused at registration rather than at query time.
- **M1 — `recordstore/gen` publishes the `Set` (SD1).** ✓
  Nine stores regenerate; **eight gain a Set**. `example/widget` carries no
  components at all, so the `len(comps) == 0` guard emits neither the Set nor
  its import — a store with nothing to publish publishes nothing.

  **Growth, measured.** Seven stores grew 5–10% (1.8–5.6 KB). The
  facts-bound one is the outlier at **+70.6 KB, +55%** (128.8 → 199.4 KB),
  which is the same thing ADR-0184 recorded about binding *this* table: the
  generic schema's 21 sections across 13 kinds are what make it large. By
  artefact, for that store: Projection 40.0 KB, Validator 24.0 KB, Presence
  2.4 KB.

  **A duplication this exposes, and why it is not fixed here.** `readback`
  builds `Filter` as `joinAnd(presence ++ validator)`, so publishing Presence
  and Validator beside Filter emits ~26 KB of text the file already carries
  inside the Filter constant. The obvious fix — emit the two halves as
  constants and define `Filter` as their concatenation — is **not safe by
  construction**: `joinAnd` returns `"1"` for an empty term list, so a kind
  with no presence terms has `Filter == Validator` rather than
  `"1 AND " + Validator`, and the composition would silently diverge. No
  kind is degenerate today, which is exactly what would make the breakage
  land later and quietly. Deferred with the trigger: **the growth becoming
  objectionable, or a kind emitting a degenerate artefact.** Dropping
  Presence and Validator from the Set instead would save the same 26 KB and
  is the cheaper answer if only the two named artefacts are ever wanted.
- **M2 — `LwComponentExpand`: the functions, injection, refusals (SD3–SD5).** ✓
  Landed in
  [`leeway/constructsql`](../../public/semistructured/leeway/constructsql)
  rather than a package of its own: that is where the rest of the `LW_`
  authoring family and the `Function` type the vocabulary panel reads already
  live, and one prefix reading from one package is the point.

  Two refinements to §SD5/§SD6 as written, both from building it:

  - **An explicit `LW_COMPONENT_FILTER` discharges the injection, but only
    from `WHERE`.** The same call in a projection list computes a boolean per
    row and restricts nothing, so counting it as satisfying the kind would
    hand back precisely the first-match read SD4 exists to prevent. The pass
    checks which clause the call sits in.
  - **The join refusal is stated as "exactly one table in scope"** rather than
    §SD6's "once and unaliased". An alias on a single source is harmless —
    unqualified names still resolve — while a second source is the ambiguity,
    so the check is on table count. A CTE, subquery or table-function source
    is refused separately and by name.

  The wiring — `passreg` registration and play's registry — is M3's, so the
  pass exists and is tested but nothing runs it yet.
- **M3 — play wires the registry and the pass (SD7, SD8).** ✓
  `LwComponentExpand` is registered in the standard set as an **Entry, not a
  Factory**, at order 110 — after the identity macros (100) and before the
  extraction family (120). The passes around it are factories because each
  needs a per-consumer schema binding; this one needs a registry a host
  populates once, which is a different thing and does not belong on the
  binding. `play.RegisterComponents` publishes `sysmfacts`, and both hosts
  that wire play — the standalone binary and the carousel — call it beside
  their pass registration. The Vocabulary tab lists the family, with the
  read-back dependencies on `LW_COMPONENT` only.

  **One correction to §SD6, forced by the wiring.** Every pass in the standard
  registry is constructed with an empty default database, so a strict
  database comparison would refuse `FROM facts` everywhere and make the
  family unusable through `passreg`. The check now requires the table name to
  match, and compares the database only when one is known — an explicit
  qualifier that disagrees is still refused, and an unqualified reference is
  left to the server's session database.

  Verified end to end against the live server, which is what makes the three
  packages' agreement more than an assertion:
  `SELECT LW_COMPONENT('SysMem') AS m FROM boxer.facts` returns a named tuple
  whose slots are the DTO's Go field names, with its conformance filter in the
  WHERE the statement did not have.
- **M4 — the cross-oracle test against `Scan<Kind>`,** and the sysmetrics
  expansion goldens.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md) — the artefact
  generator; the 2026-07-27 Update is the Projection/Filter property SD4
  answers.
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) — the
  record store that bakes the Filter; S2 is why it needs no UDF, SD5 why point
  lookups bypass the artefacts.
- [ADR-0146](./0146-leeway-marshall-component-read-contract.md) — the
  back-end-neutral `ReadContract` the artefacts take their arity from.
- [ADR-0121](./0121-selection-condition-columns.md) — the precedent for a
  pass that rebuilds `WHERE`, and for declining rather than rewriting.
- [ADR-0181](./0181-leeway-dql-authoring-surface.md) — the `LW_` authoring
  family this extends, and its client-side expansion contract.
- [ADR-0184](./0184-sysmetrics-persistence-tee.md) — §SD6, the hand-authoring
  cost this reduces, and the first corpus of components to expand.
- [leeway SQL read surface](../explanation/leeway-sql-read-surface.md) — the
  client/server split SD8 marks the new names against.
- [jsonbench-on-facts trial](../trials/jsonbench-on-facts/README.md) — the
  measured cost of hand-writing what the repository already provides.
