---
type: adr
status: accepted
date: 2026-08-09
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-13
---

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
(`tv:symbol:lr:lr:u64:1247:::0::data`), and nothing exposes the naming seam
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

Each sub-decision independently descope-able (SD2 alone presupposes SD6 —
the pass consumes the seam it exports; SD7's bookkeeping lands with
whichever of SD2/SD3 ships first). Suggested order: SD1; SD2+SD6 together;
SD5; SD3; SD4 anytime (independent). One dependency carried from the
background analysis: ADR-0171 §SD2's version handshake should land before
SD3 makes the authoring surface a second caller of the installed read-back
family.

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
  (`indexOf` returns the *first* match; the m2v position→attribute form is
  the exact one), and
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

Home: the leeway explanation/howto docs; the leeway skills gain
pointers (same move as ADR-0171 §SD1).

### SD2 — Constructor family: `LW_PLAIN`, `LW_TV`, `LW_TV_MEMB`, `LW_TV_SUPPORT`

Client-only authoring calls, expanded by a new standard-set pass
(`LwConstructExpand`, an ADR-0098 node-rule rewrite) into
`<expr> AS "<physical name>"`. Never installed server-side; a call reaching
a raw endpoint fails loudly as an unknown function.

- **Call shape.** Every argument after the wrapped expression is a string
  literal. The tagged constructors name their **section explicitly, in
  second position** — `LW_TV(expr, 'section', 'name', 'type', tokens…)`,
  `LW_TV_MEMB(expr, 'section', 'channel')` — while `LW_PLAIN` takes no
  section at all, because a plain column has none; its mandatory `item:`
  token is what files it instead. Inferring the section from the
  statement's target would make the same text mean different things in
  different hosts, and folding it into the name as `'section:name'` would
  spell a mint exactly like ADR-0116's handle for an existing column,
  collapsing the duality this family exists to keep legible.
- **Aspect tokens** carry a vocabulary prefix — `item:` (**mandatory** on
  `LW_PLAIN`; prefixes carry semantics, calls read complete), `enc:`,
  `sem:`, `use:` (tagged only) — after the logical name and the canonical
  type (parsed with the `canonicaltypes` parser). Prefixes are required
  because aspect names collide across the three closed vocabularies
  (`json*`/`cbor*` exist twice).
- **Membership and support columns are constructed by channel**
  (`LW_TV_MEMB(expr, 'section', 'low-card-ref')`), never by properties —
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

The two halves register differently. SD2's constructors expand into an
expression and an alias, so they run wherever the statement does; the
`LW_GET` family expands into pack-form calls (SD3), so it registers with the
declared expansion dependencies of ADR-0174 §SD6 and is marked against that
panel's existing probe. Client-side expansion makes a name portable only when
what it expands *into* is.

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
| `LwShapeCheck` + SD5 audit-query generator (opt-in pass; generator home decided at implementation) | new, outside the standard set | hosts and schemas that opt in |
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
- `LW_GET` carries a second, different asymmetry: expansion succeeds and the
  *expanded* statement fails, because SD3's v0 renderer emits pack-form
  calls. Still loud, but one step further from the text that was typed, and
  it makes an authoring name endpoint-dependent where the constructors are
  not. It ends when the inline renderer ships (SD8).
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
  - SD5 audit queries: zero violations on the round-trip fixtures; a
    deliberately corrupted co-length fixture goes red (`clickhouse-local`).
- **Gap.** SD1 is documentation and has no lane. The inline renderer is
  untested because it is unshipped (SD8). Statement wrapping is untestable
  until the grammar port.

## Status

Accepted 2026-08-13.

- **M0 — SD1: the three contracts and transform patterns as normative docs; skills pointers.** ✓
- **M1 — SD6: `lwsql` spec→name seam and token parsing; `leeway ddl compose`.** ✓
- **M2 — SD2+SD7: `LwConstructExpand`; standard-set registration; client vocabulary entries.** ✓
- **M3 — SD5: `LwShapeCheck` and the audit-query generator.** ✓
- **M4 — SD4: skip-index emission policy over `TableOptions`.** ✓
- **M5 — SD3: shared extraction builder and `LwExtractExpand`.** ✓

The options considered for each naming and scoping choice, and their
kill-reasons, are recorded in
[the background document](../adr-background-work/leeway-dql-featureset.md)
and are not repeated here.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Update 2026-08-13 — M0–M4 implemented; homes and two verification deviations

The choices this ADR left to implementation, as landed:

- **Homes.** The spec→name seam is `lwsql.Composer` plus the token parsers
  (`ParsePlainSpecTokens` / `ParseTaggedSpecTokens` /
  `ParseMembershipSpec`); `LwConstructExpand` and `LwShapeCheck` live in the
  new `constructsql` package beside `lwsql`; the SD5 audit-query generator is
  `lwsql.AuditQueries`. Machine-chosen membership/support lane properties are
  derived by running the production section-loading path
  (`ResolveMembership` included) over a synthetic section — never restated —
  and a generator-equality test pins every minted name against the DDL
  generator's output.
- **SD4 scope.** The raw `INDEX` plumbing (`TableOptions.Indexes`) predated
  this milestone; what landed is the policy layer — `SkipIndexPolicy`,
  `DeriveSkipIndexes`, `TableOptions.SkipIndexes`, and
  `leeway ddl compose --skip-indexes` — with defaults documented as starting
  points (bloom_filter(0.01), GRANULARITY 4).
- **Verification deviation, SD4.** Granule pruning is asserted via
  `clickhouse-local` `EXPLAIN indexes = 1` in the unit lane (skip-if-absent)
  — the same method ADR-0066's matrix was verified with — rather than the
  integration lane; hermetic, and the skip-stage chain is asserted
  first-denominator vs last-numerator as planned.
- **Verification deviation, SD5.** The audit queries' clickhouse-local
  fixtures are composer-minted synthetic tables (clean fixture green, one
  corruption per invariant class red); the marshallreflect round-trip
  fixtures stay in the read-back package and are not re-wired here.
- **Analytical contract.** `LwShapeCheck` returns its input unchanged on
  success instead of splicing the discard marker: the marker mechanism
  serves handler flows, and for a plain pass the observable contract —
  body rides through, violations are hard errors — is identical.
- **SD7 split, applied.** The four constructor names are registered in the
  vocabulary panel's client population; the `LW_GET` family registers when
  SD3 lands, together with ADR-0174 §SD6's dependency marking (that panel's
  M4, sequenced behind SD3). SD3 remains blocked: the read-back family
  carries no version marker today (ADR-0171 §SD2 open).

## References

- [Background analysis](../adr-background-work/leeway-dql-featureset.md) —
  taxonomy, inventory, name-grammar facts, QOC detail, kill-reasons.
- [leeway-query-algebra](../explanation/leeway-query-algebra.md) — the
  (S,N) guard model SD1 makes normative.
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

## Update 2026-08-14 — M5 implemented; two surface additions and one dormant path

ADR-0171 §SD2's handshake landed, unblocking §SD3. What that turned into:

- **Homes.** The shared expression builder is the new package `lwextract`
  (`Value`, `Present`, `CountEqual`, `NullWhenAbsent`) — a leaf that resolves
  nothing, taking already-escaped lane names and an already-rendered
  membership literal, which is what lets both consumers import it. The
  read-back generator now calls it and its goldens pin the output
  byte-identical. The schema side is `lwsql.ExtractLanesFor`, deciding
  value/identity/cardinality/length lanes from the physical column NAMES
  alone. The pass is `constructsql.ExtractExpandPass`.
- **Registration is a Factory, not an Entry.** §SD7 anticipated a
  standard-set entry with a marker pre-scan; the pass is schema-bound, so it
  is late-bound at Order 120 exactly as `ResolveColumnNames` is. A consumer
  with no schema binding — the unbound `/query` path — simply does not get
  the sugar, rather than getting a pass that cannot answer.

**Two additions to the family's signature**, both forced by real schemas:

- `'col:<name>'` — a section with several value columns (`geoPoint` has
  `lat` and `lon`) has no obvious "the" value. Rather than guess, the pass
  asks and lists the candidates.
- `'chan:<channel>'` — a section carrying more than one membership channel
  is likewise ambiguous.

Both follow §SD2's vocabulary-prefixed token convention, and both are
optional: the single-value-column, single-channel section — the common case
— needs neither.

**A limitation worth stating plainly.** A *ref* channel identifies
memberships by registry id, and a client-side pass holds no registry. So
`LW_GET('metric', 'cpuLoad')` on a ref channel is an error naming the gap;
`LW_GET('metric', '6917529027641081861')` works. Verbatim channels take the
name and need nothing. This is ADR-0171 §SD4 seen from the authoring side,
and it is the one place where "no physical names, no Go" is not yet true.

*Closed 2026-08-14 by ADR-0171 §SD4's Update: the pass takes an optional
membership registry off the same binding the schema comes from, so a ref
channel names its membership where a host carries one. The id form still
works and needs no binding, so this paragraph describes what an unbound host
still sees.*

**The fast path is implemented, proven, and currently unreachable.** The
absence of a `<role>card` column licenses the bare-`indexOf` form, and a
`clickhouse-local` test proves the fast and general forms agree on the same
fixture (a deliberately broken fast form makes it fail, which was checked).
But `TableRowConfigMultiAttributesPerRow` is the only row config the
generator emits today, and it always emits the cardinality column — so no
schema this repository produces takes the path. ADR-0066's fast-path item is
closed in the builder, dormant in practice, and will light up when a
single-attribute-per-row config exists.

**Verification deviation.** The planned check was a `clickhouse-local`
round-trip asserting the `LW_GET` expansion equals the read-back artefact.
What landed instead pins the two halves separately, which is stricter about
the part that can actually drift: the *expression* is one function in one
package, pinned by its own tests and by the generator's goldens, so it
cannot differ; the *lanes* are resolved by two independent roads — an IR
loaded off a `TableDesc` versus parsed physical names — and a test compares
them per section and sub-column. A round-trip would have exercised both at
once and told us less about which one was wrong.

## Update 2026-08-15 — §SD8 statement wrapping: decided; INSERT-only, target-bound

The statement-wrapping deferral is now a decided design (four-question
dialogue). It stays in this ADR as this Update rather than becoming its own
ADR — reviewer's choice, resolving the background analysis's "growing an
insertStmt is a separate Tier-1 decision" note: the decision is this Update,
and its Tier-1 surface is listed below.

**Scope: `INSERT INTO <table> [(columns)] SELECT …` only.** CTAS stays
deferred, and the kill-reason is structural rather than effort: a table
minted by `CREATE … AS SELECT` cannot carry codecs, and §SD4 deliberately
made skip indexes `TableOptions`-borne — an in-pipeline CTAS would make the
codec-less, index-less table the path of least resistance, working against
`leeway ddl compose`, which exists to create tables right. The sanctioned
flow is create-with-compose, fill-with-INSERT-SELECT. `VALUES` sources add
nothing to authoring (writes at rest ride the DML generators), and
materialized views are ADR-0171 §SD3's separate deferral; both stay out.
CTAS's trigger, should it return: demonstrated need for scratch tables from
inside the pipeline, documented as codec-less.

**Grammar mechanics.** grammar1 and grammar2 grow in lockstep — grammar2
because `ValidateGrammar2` is every chain's terminal proof, so a wrapper the
canonical grammar cannot parse would fail exactly the statements the port
exists to admit. Productions come from the upstream `utils/antlr` lineage
per the §SD8 direction; grammar0 is the untouched provenance baseline (it
has no importer outside its own package). `BuildScopes` learns that an
INSERT target is a **sink** — never a readable scope table, so no handle or
extraction binds against it.

**Target adoption is in scope, and it includes spelling.** The parsed
target is what finally feeds `ExpandPassWithSegments` — the target-adoption
variant that has waited for a host that knows the destination: constructors
mint in the target's segment convention, and a shape check verifies the
SELECT's output columns against the target's physical names (the
vertical-subset rule applied to a concrete table). Adoption must reconcile
**spelling**, not only segments: constructors mint folded names
(`geoPoint` → `geo-point`; the 2026-08-15 anchor query 4/5 modernization
shows it live) while an existing target may spell camelCase — the check
resolves fold-equivalent names to the target's spelling and errors only on
true misses.

**Pass contract under a wrapper — a refusal matrix, not best effort.**
Passes whose output changes the result schema refuse loudly under an INSERT
wrapper, because the target's column match *is* the schema:
`ExposeSelectionConditions` (adds condition columns) and the `ts*`
client-CTE lane (its result never exists as SQL) are the known two; the
port's pass audit enumerates the rest. Macro expanders and the
resolve/extract/construct chain operate on the inner SELECT unchanged.

**Host policy: expand everywhere, execute gated.** Expansion, diagnostics
and preview work on a wrapped statement in every host. Executing one from
play requires an explicit write opt-in, and FORMAT appending becomes
statement-kind-aware (an INSERT takes none — the appended FORMAT is exactly
why DDL from play fails today). Without the opt-in, Run refuses with a
copy-out hint. sqlapplet inherits the same rule.

Milestones:

- **M0 — grammar port.** ✓ grammar1 + grammar2 productions and
  regeneration; a parse-corpus pin against the upstream lineage forms.
  Shipped 2026-08-15 (`8e09ac79`, `9050c762`): the wrapper is an unlabeled
  `queryStmt` alternative so `QueryStmtContext` keeps its type, and
  `nanopass.Parse` refuses a parsed wrapper explicitly until M1 — the
  grammar admitting what the passes cannot yet carry must fail with its
  real reason, not a nil-scope panic mid-chain.
- **M1 — pipeline semantics.** ✓ Scope sink, canonicalize node rules for
  the wrapper clause, the pass refusal matrix with tests. Shipped
  2026-08-15 (`0a574507`): the M0 entry guard is out;
  `ParseResult.InsertStmt` is the accessor the matrix keys on; the sink is
  structural (the target sits outside every `tableExpr`); canonical form
  needs only the TABLE-word drop, since quoting and keyword case walk the
  whole CST already. The audit's one find beyond the named two:
  `ClassifyStatementKind`'s "a successful parse is the read proof"
  predated the port — it now checks the tree for the wrapper before
  answering read-only, keeping the classifier default-deny for the write
  gate M3 builds on.
- **M2 — target adoption.** Segment + spelling adoption via
  `ExpandPassWithSegments`; the target shape check; anchor gains `query8`
  (INSERT of constructor-minted columns into a compose-created scratch
  table, executed in the integration lane; the snippets sweep extends).
- **M3 — host policy.** play's write opt-in (ADR-0009 registry entry) and
  statement-aware FORMAT; docs — the reading-and-authoring how-to's
  wrapper paragraph and the read-surface page's known-gaps line.

Tier-1 surface added by this Update: the grammar1/grammar2 statement
productions, the pass refusal matrix as a pipeline contract, and play's
write opt-in.

## Update 2026-08-15 — §SD2's call shape: the membership slot takes an unquoted id

§SD2 states the call shape as "every argument after the wrapped expression is
a string literal", and §SD3 inherited it for the extraction family. That rule
is now relaxed for exactly one slot: the **membership** of `LW_GET`,
`LW_GET_NULL` and `LW_GET_LIST` also accepts an unsigned decimal literal.
`LW_GET('symbol', 22, 'chan:low-card-ref')` is the same call as
`LW_GET('symbol', '22', …)` and expands to the same SQL.

**Why the original rule reads as it does, and why it does not reach here.**
The string form must exist and must stay the general spelling: on a ref
channel the slot holds a name *or* an id (ADR-0171 §SD4), and which of those
a call means is not known until the section resolves against the schema —
well after parsing. That argument establishes the string carrier; it does not
forbid a second, narrower spelling. A bare number can only ever be the id, so
it adds no ambiguity to resolve later.

**Scope, and the kill-reason for anything wider.** Only the membership slot.
The section and the `col:`/`chan:` tokens are names and vocabulary, where a
number is never meaningful, and the constructor family has no numeric slot at
all — so `specString` keeps its rule and both families keep one decoder for
everything else. Widening would buy a second spelling for values that have
only one.

**The one combination the unquoted form adds** is a bare number against a
*verbatim* channel, which carries names. It is refused, naming the fix, and
it is decidable: the channel is resolved before the membership is rendered.
`'22'` on a verbatim channel still means the name `22`, which is what keeps
the two spellings from colliding. Numeric literals that are not unsigned
decimals — signed, floating, hex, octal, `INF`, `NAN` — are refused by shape,
so the diagnostic is about the id rather than about a failed conversion.

**Declined, and worth recording.** Once a bare number unambiguously means the
id, the quoted decimal could be redefined to mean a *name* spelled in digits,
closing §SD4's one wart (a ref membership named in digits is unreachable by
name). Rejected: §SD4's decimal-first rule is what lets every query written
before it keep working, and every query written since relies on it. The
unquoted spelling is additive; the wart stands.

No Tier-1 surface moves. The emitted SQL is unchanged for both spellings, so
the read-back goldens and the pass's idempotence are untouched; what changes
is the authoring surface, the declared parameter list the vocabulary panel
prints, and the snippets that carry ids.

## Update 2026-08-15 — mixed channels are readable, and the plural question gets its own call

§SD3 excluded the mixed and parametrized channels, and §SD8 deferred them on
"ADR-0008 Cut 2 front-end". Both statements are revised here.

**The recorded trigger had already fired, and it was never the binding one.**
ADR-0008's 2026-06-04 Update records all four Cut-2 channels implemented
across `mappingplan`, `marshallgen` and `marshallreflect`. More to the point,
`LwExtractExpand` never consumed that front-end: it resolves lanes from a
table's physical column NAMES through `lwsql.Resolver`, not from a
`mappingplan.Plan`. The mappingplan gap is real, but it belongs to the
read-back *generator*, whose `channelSpec` still maps only the four simple
channels — so the two consumers of the shared builder were blocked on
different things, and only one of them was blocked at all.

**The two mixed channels join the extraction vocabulary**, spelled as
`common.MembershipSpecE.String()` spells them —
`low-card-ref-high-card-params` and `low-card-verbatim-high-card-params` — so
`chan:` keeps one vocabulary with the constructor family's
`ParseMembershipSpec`. A new `param:` token names the high-cardinality half.
Nothing about the parameter lane needed discovering: `lwsql`'s section index
already keyed it by role, `MembershipParamPartner` already mapped it back to
the identity lane whose `…card` covers both, and §SD5's audit generator
already emits `arraySum(card) = length(param)`.

**Parametrized channels stay out, for a different reason than the one
recorded.** A parametrized membership is one opaque blob carrying identity and
parameters together — no separate identity lane to match, and no shared codec
saying how the blob is laid out (`membership/params.go` covers the *mixed*
parameter channel; the one in-tree parametrized writer hand-rolls
`[]byte("k=10")`). There is no literal a caller could pass, so it needs a
serialization contract first, not a lane lookup. The two deferrals should not
have been one bullet.

### The plural question, and why it is not a plural getter

A mixed channel's membership is shared **by design** — the parameter lane
exists because several attributes carry the same membership — so `LW_GET`'s
contract (*locate THE attribute*) is structurally false there. `LW_GET` on a
mixed channel therefore requires `param:`; without it the pass refuses.

A refusal with nowhere to go would just push the caller into the hand-written
lane arithmetic [ADR-0171](./0171-leeway-sql-read-surface.md) exists to
prevent, so the plural read gets a call of its own: **`LW_SEL` and
`LW_SEL_ATTRS`**, returning the membership-lane positions and the attribute
indices a membership occupies, co-indexed with each other so both pass to one
lambda. `param:` is **optional** on these. The two rules are one rule: *a
parameter is required exactly when the answer must be unique.*

Three choices inside that, each with a live alternative:

- **A selector, not a plural getter.** The alternative — `LW_GET_ALL`,
  `LW_GET_ALL_PARAMS`, … — needs one function per lane, multiplies with every
  lane a section carries, and cannot reach a co-section's lane at all. One
  selector reaches all of them. This is the "argwhere + gather" plan the
  array-idioms how-to already prescribes for the hand-written form; the pack
  shipped the gather half (`LW_CO_GATHER`) and no argwhere half, and that
  asymmetry is what this closes.
- **Two co-indexed selectors, not one tuple-returning iterator.** A
  `(attrIdx, membPos)` iterator reads worse (`t.1`/`t.2`), cannot be handed to
  `LW_CO_GATHER`, and invites recomputing `LW_RAGGED_PARENT_IDS` *inside* the
  lambda — the one real performance trap in this shape. Two selectors evaluate
  it once.
- **No new server-side function.** Every new form expands to built-ins plus
  `LW_RAGGED_PARENT_IDS` and `LW_CO_GATHER`, both already in the declared set,
  so `LW_SURFACE_VERSION` does not move and a server provisioned before this
  lands runs the output unchanged. The mixed arm of `LW_GET` is likewise
  built-ins — a partial exception to §SD3's pack-form-only rule, taken because
  the read-back family's signatures predate the second lane and adding three
  functions to carry it would have cost a surface revision for expressions
  ClickHouse renders natively.

`LW_SEL` is the one member exempt from the cardinality-lane requirement: it
selects positions in the identity lane and never crosses to the attribute
axis, so it has nothing to map. `col:` is refused on both selectors rather
than ignored — they return indices, so naming a value column is a request
they cannot honour.

**Verified** against `clickhouse-local` on a ragged fixture where one
attribute owns two membership positions, so a position is not an attribute
index for anything after it: the mixed value forms, the pair-scoped presence
guard, both selectors, their co-indexing, and the empty-selector case. The
oracle is hand-decoded from the SoA layout rather than computed by a second
expression from the same package. The play snippet corpus additions execute
under `TestSnippetsAgainstFixture`, confirmed by mutation.

**Surfaces.** `LW_SEL` / `LW_SEL_ATTRS` join the client-side names of the
Tier-1 table above and the vocabulary panel's client population;
`lwextract.Lanes` gains `Param`, `Request` gains `Params`/`ParamsGiven`, and
`PresentFor` / `NullWhenAbsentFor` join `lwextract` as the parameter-aware
forms — the single-lane spellings keep their signatures and their meaning.
`ExtractExpansionDependencies` gains `LW_CO_GATHER`. No server-side name
changes, so ADR-0171 §SD2's reconciler scope is untouched.

**Not done here.** The read-back generator still refuses mixed channels; a
`LW_GET_NULL` guard on a mixed channel matches the pair (which is what makes
it meaningful) but is only as prunable as its two `has()` conjuncts; and the
selectors do not prune at all, which the docs say rather than the pass fixing.
