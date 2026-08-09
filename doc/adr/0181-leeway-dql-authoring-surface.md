---
type: adr
status: proposed
date: 2026-08-09
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

# ADR-0181: leeway DQL authoring surface — contracts, constructors, extraction sugar

## Context

Working with leeway data from SQL decomposes into three consumer intents:

- **(a) Filter** — `WHERE` predicates where a cheap superset answer is
  acceptable and index-prunable: false positives licensed, false negatives
  never.
- **(b) Extract** — attribute values pulled into ordinary, opaque columns a
  BI tool or data mart consumes with no leeway awareness.
- **(c) Transform** — a `SELECT` list that *produces* leeway shape (tagged
  sections or leeway-plain columns), for `INSERT … SELECT` /
  `CREATE TABLE … AS SELECT` between leeway tables.

(a) and (b) exist today across [ADR-0162](./0162-leeway-co-ragged-function-pack.md)'s
pack, [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md)'s
read-back family and generated artefacts, and
[ADR-0116](./0116-play-leeway-column-handle-resolution.md)'s handles —
[ADR-0171](./0171-leeway-sql-read-surface.md) names them as one read surface.
(c) is a missing *direction*, not a missing function: physical column names
are the schema, a human cannot compose one
(`tv:symbol:lr:lr:u64:2q:0:0:0::data`), and nothing exposes the naming seam
to SQL authoring. The driving acceptance criterion: a SQL practitioner must
do routine leeway work **without Go in the loop and without typing a
physical name** — in either direction.

Analysis, inventory, and the rejected options are recorded in
[the background document](../adr-background-work/leeway-dql-featureset.md).
Load-bearing facts from it:

- The exported naming seam needs no `TableDesc`/IR round-trip;
  `lwsql.NameConditions` ([ADR-0121](./0121-selection-condition-columns.md))
  already mints physical names for SQL-computed columns this way.
- A minted name must land in **identifier position** (alias), which no
  server-side UDF can reach — and a constructor's first argument is an
  expression, which the literal-only `MacroExpander` hard-fails by design
  (ADR-0162 §SD7). Both point at nanopass passes.
- grammar1 parses **SELECT only**; `INSERT`/`CREATE … AS SELECT` do not flow
  through the pipeline today.
- The generated Presence/Validator/Filter artefacts are built-ins-only —
  the `WHERE` story works against a server with nothing installed.
- Whether the bare-`indexOf` fast path is legal is structurally decidable
  from the names (no `<role>card` column ⇒ memberships cannot repeat).

## Design space (QOC)

**Question.** What carries the DQL featureset?

**Options.** O1 — document idioms only. O2 — server-UDF-maximal (extend the
installed families, name composition as string-returning functions). O3 —
authoring-time portfolio: nanopass passes for everything touching
identifiers or schemas; server UDFs stay expression vocabulary; generators
for durable artifacts. O4 — per-kind codegen only. O5 — a query language
(revive [ADR-0022](./0022-leeway-lwq-flwor-query-language.md)'s lwq).

**Criteria.** C1 SQL-practitioner acceptance (no Go, no physical names);
C2 explicit correctness contracts; C3 preserves index behaviour; C4 works
across endpoints incl. zero-install; C5 drift/versioning safety; C6 cost;
C7 portability beyond ClickHouse.

|      | O1 | O2 | O3 | O4 | O5 |
|------|----|----|----|----|----|
| C1   | −  | +  | ++ | −− | ++ |
| C2   | −  | +  | ++ | ++ | ++ |
| C3   | +  | +  | ++ | ++ | ?  |
| C4   | ++ | −  | ++ | +  | −  |
| C5   | ++ | −  | ++ | +  | −  |
| C6   | ++ | +  | +  | +  | −− |
| C7   | +  | −− | +  | +  | −  |

O2 fails on structure, not taste: identifier position is unreachable from
inside the engine, and every added server function widens the drift surface
ADR-0171 §SD2 exists to shrink. O4 fails C1 outright (the Go loop is the
complaint). O5 re-opens a parked decision and still needs everything O3
needs underneath. **O3, grounded on O1's documentation layer, is taken** —
with the explicit framing that authoring-time expansion produces portable
SQL text; the passes run where SQL is authored, not where it executes.

## Decision

Each sub-decision independently descope-able. Suggested order: SD1; SD2+SD6
together; SD5; SD3; SD4 anytime (independent).

### SD1 — The three contracts become normative documentation

The F/X/T contracts are written down once, as consumer-facing rules:

- **F (guard):** the necessary/(S,N) model — negation swaps the pair and
  stops pruning; false positives enter through grain mismatch (row-grain
  guard for an attribute-grain question) and erased cross-lane correlation;
  prunable shapes are an enumerated list (`has`-family with constant
  needles; `indexOf(a,x) > 0` but not `!= 0`); the guard-vs-exact slack is
  one `countIf` comparison away. **No guard-sugar function names ship in
  v0** — the documented idioms plus SD4's indexes are the guard surface;
  names follow demand.
- **X (extract):** the three-way absent semantics (type default / `NULL`
  variant / empty-array sentinel for non-scalars), the aliasing rule
  (`indexOf` returns the *first* match; the m2v form is the exact one), and
  the structural fast-path licence.
- **T (transform):** the closure rule — a `SELECT` whose output names parse
  under the naming convention and satisfy vertical subsetting (all plain
  columns, co-section groups whole) *is* a leeway table
  (`DiscoverTableFromColumnNames` is the witness). Expressions break
  closure; SD2's constructors re-admit computed columns.

The same documentation work records the canonical transform patterns
(re-tagging, annotation co-section overlays, the packed↔exploded pair of
ADR-0171 §SD5) and the lambda-free relational rendering the
second-substrate trial flagged as undocumented.

Home: the leeway explanation/howto docs; the three leeway skills gain
pointers (same move as ADR-0171 §SD1).

### SD2 — Constructor family: `LW_PLAIN`, `LW_TV`, `LW_TV_MEMB`, `LW_TV_SUPPORT`

Client-only authoring calls, expanded by a new standard-set pass
(`LwConstructExpand`, an ADR-0098 node-rule rewrite) into
`<expr> AS "<physical name>"`. Never installed server-side; a call reaching
a raw endpoint fails loudly as an unknown function.

- **Arguments** after the wrapped expression are string literals:
  logical name, canonical type (parsed with the `canonicaltypes` parser),
  then vocabulary-prefixed tokens — `item:` (**mandatory** on `LW_PLAIN`;
  prefixes carry semantics, calls read complete), `enc:`, `sem:`, `use:`
  (tagged only). Prefixes are required because aspect names collide across
  the three closed vocabularies (`json*`/`cbor*` exist twice).
- **Membership and support columns are constructed by channel**
  (`LW_TV_MEMB(expr, section, 'low-card-ref')`), never by properties —
  role, type and hints come from `ResolveMembership`, ClickHouse-filtered.
- **Position rule:** legal only as a whole projection item; anywhere else
  errors with the call's `SourceRange`.
- **Loud rejections:** non-literal spec arguments, unknown tokens,
  vocabulary misroutes (`use:` on `LW_PLAIN` — the error names the fix),
  unparseable canonical types, stylable-name violations.
- **Table-level segments** default (`tableRowConfig 0`, empty streaming and
  co-section groups); where a target table is known (play pin path) the
  pass adopts its separator and row config as `NameConditions` does.
- v0 is per-column; section-level one-call sugar is deferred (SD8).

### SD3 — Extraction sugar: `LW_GET`, `LW_GET_NULL`, `LW_GET_LIST`

A schema-bound factory pass (`LwExtractExpand`) expands
`LW_GET('<section>', '<tag>')` into the exact per-channel locate+extract.
Section→table binding via `BuildScopes`: one carrying table expands,
several demand qualification, none errors naming what was searched.

- **One expression builder, one renderer in v0.** The per-channel emission
  is refactored out of the read-back generator into a shared builder both
  consumers call; emitted generator SQL stays byte-identical
  (golden-pinned). v0 wires only the **pack-form** renderer (calls
  `LW_VALUE_BY_TAG_EQUAL` etc.). The inline builtins-only renderer is
  deliberately **not** shipped — it is ADR-0162 §SD8-1's deferred
  client-side expander, and its read-only-target trigger stands.
- **Fast path is structural:** absence of the membership's `<role>card`
  column proves MC ≡ 1 and licenses bare `indexOf` — closing ADR-0066's
  open fast-path-detection item.
- Mixed/parametrized channels stay out (ADR-0008 Cut 2 front-end, SD8).

### SD4 — Skip-index emission via `TableOptions`

`ComposeCreateTable` learns index emission options — `bloom_filter` on
membership lanes (and const-bearing scalar string value lanes), `set(N)`
where `countEqual`/`indexOf` shapes matter — per the shape matrix verified
in ADR-0066's 2026-06-09 Update, with documented defaults (fp rate,
GRANULARITY). This closes ADR-0066's skip-index gap and is what makes SD1's
guard story *prune* rather than merely be correct.

Deliberately **not** aspect-borne: encoding hints are part of column
identity, so aspect-borne intent would turn an index retrofit into a
rename. Index intent is deployment-scoped and accepted as not
name-recoverable.

### SD5 — Shape checking

Two halves of the transform contract's validation:

- **`LwShapeCheck`** — an opt-in analytical pass (discard-marker, hard-error
  mode): every output name parses; `DiscoverTableFromColumnNames` +
  `TableValidator` accept the implied table; section completeness holds per
  channel (a `val` without its membership lanes, a repeating channel
  without its `…card`, a dangling co-section-group half — all decidable
  from names).
- **A runtime audit-query generator** — from a schema, emit the invariant
  checks statics cannot see: co-length equality across a section's lanes,
  `card ≥ 1` positivity, membership-card sums consistent with membership
  lane lengths.

### SD6 — The Go micro-API lives in `lwsql`; the CLI gains `ddl compose`

`lwsql` (already owning handles, `NameConditions`, separator/row-config
adoption, and the `ddl` import) exports the spec→name seam: compose
plain/tagged names from hand-built `IntermediateColumnContext`/`Props`,
parse the vocabulary-prefixed tokens. The pass, tests, and a new
`leeway ddl compose` subcommand (durable `CREATE TABLE` with codecs through
the real generator; precedent `leeway id udf`) all consume it, so the spec
tokens mean the same thing everywhere.

### SD7 — Registration and bookkeeping

`LwConstructExpand` and `LwExtractExpand` join the standard pass set with a
cheap marker pre-scan before parsing (near-zero cost on queries without
`LW_` authoring calls); `LwShapeCheck` is opt-in. The new names register in
the vocabulary panel's **client** population
([ADR-0174](./0174-play-sql-vocabulary-panel.md)); nothing new is installed
server-side, so ADR-0171 §SD2's reconciler scope is untouched.

### SD8 — Deferrals, each with its trigger

- **Statement wrapping** (`INSERT`/`CREATE … AS SELECT` in-pipeline):
  deferred; direction fixed — port the productions from ClickHouse's
  upstream `utils/antlr` grammar (the lineage grammar0/1/2 derive from).
  Until then the wrapper is composed by hand around the expanded SELECT.
- **Section-level constructor sugar** — trigger: demonstrated authoring
  pain at per-column grain.
- **Guard-sugar names** (`LW_MAY_*` or other) — trigger: demand, under
  SD1's documented contract.
- **Inline extraction renderer** — trigger: a read-only ClickHouse target
  actually hosting leeway data (ADR-0162 §SD8-1).
- **Verbatim↔ref transforms** — blocked on ADR-0171 §SD4's name→id lookup.
- **Mixed/parametrized channel extraction** — ADR-0008 Cut 2 front-end.
- **Representation routing** (packed vs exploded) — stays ADR-0171 §SD5's
  open point, untouched here.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Client SQL function names (named registry, `LW_` + UPPER_SNAKE) | +`LW_PLAIN`, `LW_TV`, `LW_TV_MEMB`, `LW_TV_SUPPORT`, `LW_GET`, `LW_GET_NULL`, `LW_GET_LIST` — client-only | vocabulary panel client rosters; SD1 docs; skills pointers |
| `lwsql` (exported Go API under `public/`) | gains spec→name composition + token parsing | its importers; the two passes; the CLI |
| `passreg` standard set (named registry) | +`LwConstructExpand`, +`LwExtractExpand` | every pipeline host, incl. play |
| read-back generator emission (generated-code input) | refactored onto the shared expression builder; emitted SQL unchanged | recordstore `.out.go` goldens; round-trip tests |
| `ddl/clickhouse` `TableOptions` (exported Go API) | index emission options | `ComposeCreateTable` callers; DDL goldens for opted-in schemas |
| `leeway` CLI | +`ddl compose` | CLI help/docs |
| leeway docs + skills (documentation surface) | SD1 contracts section + pointers | none |

## Alternatives

- **Server-UDF constructors.** A minted name must land in identifier
  position; no engine-side macro reaches it. Killed by structure.
- **`MacroExpander` as the home.** Constructors carry an expression
  argument — exactly the shape the literal-only expander hard-fails by
  design (ADR-0162 §SD7).
- **Codegen-only.** Keeps Go in the loop for a one-column mart; fails the
  acceptance criterion this ADR exists for.
- **lwq revival (ADR-0022).** A language is heavier than a vocabulary and
  still needs this ADR's machinery underneath; stays parked.
- **Aspect-borne skip-index intent.** Name-recoverable and discoverable,
  but retrofit becomes a column rename; rejected in favour of
  `TableOptions`.
- **`oq` default item type.** Less typing for the common case; rejected for
  explicitness — constructor calls should read complete.
- **A `sql expand` wrapper CLI.** Set aside with the wrapping deferral in
  favour of the upstream-grammar port; authoring hosts are the pipeline
  hosts until then.

## Consequences

### Positive

- Aspect (c) exists: computed columns re-enter the closed set, so
  leeway→leeway mapping, datamart projection, and annotation overlays are
  writable in SQL with no Go and no hand-typed physical names.
- Extraction gains handle-grade ergonomics without a codegen round-trip,
  m2v-correct by default, with the fast path decided structurally.
- The guard story gains its missing enabler (indexes) with zero new server
  surface; the `WHERE` path remains zero-install.
- One source of truth for per-channel extraction emission (generator and
  sugar share the builder).

### Negative

- Two more standard-set passes; the pre-scan bounds the cost but does not
  zero it for queries that do carry markers.
- The authoring vocabulary is client-only: the same text fails on a raw
  endpoint. The failure is loud (unknown function), but the asymmetry is
  real and must be documented (it is the same asymmetry handles already
  have).
- `lwsql` widens from read-direction to both directions — more package
  scope, justified by the shared machinery.
- `TableOptions` index intent can drift from what a deployment actually
  created; an accepted trade-off of SD4.
- Wrapping stays manual until the upstream-grammar port lands.

### Neutral

- No leeway encoding, facts schema, or query-result change. The read-back
  refactor is behaviour-preserving and golden-pinned.
- No new server functions; ADR-0171 §SD2's reconcile scope is unchanged.

## Migration — Tier 1

- **Breaks.** Nothing at rest. No server-side changes. Queries not using
  the new names are untouched.
- **Path.** Additive: passes register, `lwsql` gains API, CLI gains a
  subcommand. Hosts pick up the standard set on rebuild.
- **Regeneration.** The read-back generator refactor must leave generated
  artefacts byte-identical (goldens enforce); `go generate ./...` output
  changes only for schemas opting into SD4 index options.
- **Old shape.** Hand-written physical names and raw idioms remain fully
  supported — the escape hatch stays cheap (ADR-0171 C4).

## Verification plan — Tier 1

- **Lane.** Unit (corpus + goldens + `clickhouse-local`) for SD2/SD3/SD5;
  the `//go:build integration` lane for SD4.
- **What would fail.**
  - `AssertProperties` over the shared corpus plus a new authoring corpus
    (constructor/extractor calls, nested and pathological) for both passes.
  - Golden expansions: constructor and `LW_GET` outputs — a naming-
    convention change goes red here.
  - `clickhouse-local` round-trip: `LW_GET` expansion equals the read-back
    oracle (`marshallreflect` round-trip data), scalar, array, and aliased
    (MC > 1) fixtures.
  - Read-back generator goldens unchanged across the shared-builder
    refactor.
  - An integration test that creates a table with SD4 options and asserts
    granule pruning on a `has` guard (skip stages chain — assert
    first-denominator vs last-numerator, per the ADR-0162 test's note).
  - `LwShapeCheck` negative fixtures: incomplete section, repeating channel
    without `…card`, dangling co-group half.
- **Gap.** SD1 is documentation and has no lane. The inline renderer is
  untested because it is unshipped (SD8). Statement wrapping is untestable
  until the grammar port.

## Status

Proposed — awaiting review by p@stergiotis.

The options considered for each naming and scoping choice, and their
kill-reasons, are recorded in
[the background document](../adr-background-work/leeway-dql-featureset.md)
and are not repeated here.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [Background analysis](../adr-background-work/leeway-dql-featureset.md) —
  taxonomy, inventory, name-grammar facts, QOC detail, kill-reasons.
- [ADR-0171](./0171-leeway-sql-read-surface.md) — the read surface this
  extends with a construct/transform direction.
- [ADR-0162](./0162-leeway-co-ragged-function-pack.md) — the pack; §SD3
  guard bundling, §SD7 expander hazard, §SD8 deferrals referenced here.
- [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md) — read-back
  artefacts; the skip-index gap SD4 closes; the fast-path item SD3 closes.
- [ADR-0116](./0116-play-leeway-column-handle-resolution.md) — handles, the
  read-direction dual of SD2.
- [ADR-0121](./0121-selection-condition-columns.md) — `NameConditions`, the
  minting precedent SD2/SD6 generalize.
- [ADR-0002](./0002-nanopass-discipline.md),
  [ADR-0006](./0006-nanopass-environment-and-first-class-pass.md),
  [ADR-0098](./0098-nanopass-local-rewrite-combinator-core.md) — the pass
  substrate.
- [ADR-0022](./0022-leeway-lwq-flwor-query-language.md) — the language road
  not taken.
- [ADR-0174](./0174-play-sql-vocabulary-panel.md) — where the client names
  register.
- [leeway-second-substrate trial](../trials/leeway-second-substrate/README.md)
  — the representation/expressibility evidence behind the acceptance
  criterion.
