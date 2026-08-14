---
type: adr
status: accepted
date: 2026-08-02
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-02
---

# ADR-0162: co/ragged query primitives as a ClickHouse SQL-UDF pack

## Context

Leeway data lands in ClickHouse as **co** lanes (positionally aligned arrays)
and **ragged** streams (flat values plus a lengths lane). Queries against that
layout keep re-deriving the same compositions: keyed lookups across sibling
lanes, index-select-then-gather, per-run reductions, cross-plane predicates
with sargable guards. The
[array-idioms how-to](../howto/leeway-clickhouse-array-idioms.md) catalogues
the working idioms; the
[query-algebra explanation](../explanation/leeway-query-algebra.md) states the
underlying model — axes, planes, ten primitives, and the exact/guard/cost
interpretations — and was deliberately kept decision-free. This ADR makes the
decisions: through which substrate reusable primitives reach queries, under
which conventions, and what ships first.

The load-bearing facts were measured against a live ClickHouse 26.7 and are
recorded in those two documents:

- SQL `CREATE FUNCTION` definitions are pure macros: inlined at analysis
  time, byte-identical in `EXPLAIN actions = 1` to handwritten expansions,
  transparent to skip-index pruning and PREWHERE, and they accept lambda
  parameters, nest, and fold constructed constants.
- Index analysis recognizes an enumerated list of syntactic shapes, even for
  builtins (`indexOf(a, x) > 0` prunes; the equivalent `!= 0` scans), so
  explicit `has(...)`-family guards are more robust than relying on
  recognition.
- Fused bodies beat materializing per-instance lists by ~1.5–2.4× on a
  5.6M-element shape; a wide-lane broadcast measured slower than the nesting
  it avoided.
- An out-of-tree Grafana proxy prototype composes the same
  [pass-registry](./0108-keelson-sql-pass-registry.md) pre-execute stage that
  `apps/play` uses, demonstrating wire-level preprocessing for HTTP clients;
  native-protocol clients bypass any proxy.

## Design space (QOC)

**Question.** Where do leeway-informed query and value-transformation
primitives live, so that every ClickHouse client of a boxer-provisioned
server can use them without re-deriving the layout algebra?

**Options.**

- **(a) Client/proxy preprocessing** — nanopass passes expand a vocabulary
  before the SQL reaches the server (in-process for play; at the wire for
  HTTP tools via a proxy).
- **(b) SQL UDFs** — the vocabulary is installed server-side as
  `CREATE FUNCTION` macros.
- **(c) Executable / WASM UDFs** — an external process (potentially a wazero
  host running leeway-emitted WASM) computes values per block.
- **(d) Custom ClickHouse build, upstream-first** — native functions,
  submitted upstream.

**Criteria.** Reach across both protocol populations (native TCP and HTTP);
intent preservation in stored queries — queries are data in this system;
index/PREWHERE transparency; runtime overhead; server-state footprint and
reconcile cost; sovereignty and supply-chain posture; implementation cost;
evolvability.

## Decision

### SD1 — A portfolio split by primitive class; the expression vocabulary is a SQL-UDF pack

No single substrate wins every class, so the split is by what is being
added:

- **Expression-level vocabulary** (the `LW_CO_*`/`LW_RAGGED_*` functions of SD6) ships
  as a **SQL-UDF pack** — option (b). It reaches both protocol populations,
  costs nothing at runtime (macro inlining), stays transparent to index
  analysis, and keeps the authored form intact in stored queries and
  `system.query_log`.
- **Statement- and table-level constructs** (`keelson('…')`-style macros,
  format policy, column dynamization) remain nanopass passes — option (a),
  as already practiced per [ADR-0108](./0108-keelson-sql-pass-registry.md).
  SQL UDFs cannot occupy table position or inject clauses.
- **Schema-aware intelligence** — validating co-lane and descriptor
  pairings, plane discipline, guard presence and usefulness — is future
  nanopass *analysis*, not expansion, at the seam sketched by
  [ADR-0139](./0139-semantic-layer-text2dsl.md).
- A proxy stays **additive** (canonicalization, auth, statement-level
  macros for HTTP tools) and never becomes load-bearing for the expression
  vocabulary: with the pack server-side, a proxy may fail open.
- Options (c) and (d) are deferred behind explicit triggers (SD8).

### SD2 — Pack conventions follow ClickHouse, under one owned namespace

Semantics lean on the built-in function family, which is modern and
consistent enough to extend rather than replace: lambda-first argument order
(`LW_RAGGED_EXISTS(f, vals, card)` mirrors `arrayExists(f, arr)`), 1-based
indexing, and ClickHouse's own suffix patterns as the template for variants
(`arrayReduceInRanges` is the precedent).

Names are UPPER_SNAKE under the single owned namespace `LW_`, with a family
segment naming the axis — `LW_CO_*` for co-lanes, `LW_RAGGED_*` for ragged
streams. The namespace groups the whole leeway vocabulary in
`system.functions` regardless of which package declares it, signals leeway
semantics (positivity, alignment), and keeps clear of present and future
builtin names. Exact-case spelling is mandatory everywhere (ClickHouse UDF
names are case-sensitive; some builtin lookups are not). The pack wraps
**compositions only, never renames**: a builtin that already is the operation
is used directly.

*The case and namespace rules above are as amended by the Updates of
2026-08-06 and 2026-08-07; the original decision was camelCase under bare
`co`/`ragged` prefixes.*

### SD3 — Predicate functions bundle their sargable guards

Every pack predicate that implies a constant-membership condition embeds
the corresponding `has`/`hasAny` conjunct in its own body (e.g.
`LW_CO_EXISTS_EQ2` carries `has(a, x) AND has(b, y)` beside its lambda). Measured
basis: multi-lane lambdas are opaque to index analysis (245/245 granules
without the guard, 4/245 with it), only single-lane pure equality is
rewritten by the server itself, and even builtin index support is
shape-enumerated. Stream-level `has(vals, x)` remains a valid guard under
raggedness (a necessary condition for any run to contain `x`). Call sites
therefore cannot forget the pruner.

### SD4 — Bodies are fused; nesting is a boundary operation

No pack body materializes `Array(Array)` unless the function's codomain is
genuinely nested. Per-run reductions and predicates go through
`arrayReduceInRanges` over ranges derived from the lengths lane
(`LW_RAGGED_EXISTS` is range-max over a lifted boolean stream, not
exists-over-nest). `LW_RAGGED_NEST` stays in the pack as the explicit
presentation-boundary operation, documented as such. Positivity (both
descriptors are at least 1 on leeway data) means empty-run aggregate
defaults are dead cases; the bodies remain total for foreign data that does
carry zeros.

### SD5 — One Go spec; idempotent install; additive-only evolution

The pack is defined once in Go — name, parameters, body — and emitted as
`CREATE OR REPLACE FUNCTION` statements. Installation is an idempotent
reconcile at connect time (play startup, and appliance provisioning), the
same reconcile-at-startup pattern other ClickHouse-adjacent state uses. A
zero-argument marker function, `LW_PACK_VERSION()`, returns the pack
revision so client/server skew is a query. Shipped names are **append-only
in semantics**: a published function's meaning never changes; changed
behavior gets a new name. Install performs a collision check — if a name
resolves to a builtin (e.g. after a server upgrade), install fails loudly
for that name rather than shadowing or silently skipping.

Install also **drops the names this repository has withdrawn**, from a list
that is itself append-only (`chpack.RetiredNames`). `CREATE OR REPLACE`
cannot remove a renamed function, so without this step every rename leaves
its old roster installed and callable — see the 2026-08-07 Update, which is
where the step came from.

### SD6 — The roster

Definitions below are denotational; the Go spec is normative, and the
how-to carries executable forms. Everything not listed is deliberately not
in v1.

| function | definition |
|---|---|
| `LW_CO_LOOKUP(keys, lane, k)` | `lane[indexOf(keys, k)]` |
| `LW_CO_LOOKUP_NULL(keys, lane, k)` | `NULL` when `indexOf(keys, k) = 0`, else the lookup |
| `LW_CO_GATHER(lane, sel)` | `arrayMap(i -> lane[i], sel)` |
| `LW_CO_ARG_SORT(keys)` | permutation that sorts `keys` (argsort) |
| `LW_CO_ARG_MAX(lane, keys)` | `arrayReduce('argMax', lane, keys)` |
| `LW_CO_EXISTS_EQ2(a, x, b, y)` | guarded two-lane equality existence (SD3) |
| `LW_RAGGED_STARTS(card)` | run start offsets, 1-based |
| `LW_RAGGED_RANGES(card)` | `(start, len)` tuples for `arrayReduceInRanges` |
| `LW_RAGGED_PARENT_IDS(card)` | per-element instance index (broadcast carrier) |
| `LW_RAGGED_IOTA(card)` | per-element position within its run |
| `LW_RAGGED_NEST(vals, card)` | per-instance lists; boundary op (SD4) |
| `LW_RAGGED_REDUCE(agg, vals, card)` | `arrayReduceInRanges(agg, LW_RAGGED_RANGES(card), vals)` |
| `LW_RAGGED_EXISTS(f, vals, card)` | range-max over `arrayMap(f, vals)` |
| `LW_RAGGED_COUNT(f, vals, card)` | range-sum over `arrayMap(f, vals)` |
| `LW_RAGGED_ELEM(vals, card, i, k)` | k-th value of instance i |

### SD7 — The expression vocabulary never enters the literal-only MacroExpander

The existing `MacroExpander`
(`public/db/clickhouse/dsl/nanopass/nanopass_macro.go`) accepts literal
arguments only and — by design — hard-fails a pipeline when a registered
macro keeps non-literal arguments. Registering co*/ragged* names there
would break every query that passes lanes or lambdas. Statement-level,
literal-argument macros remain its territory; the expression vocabulary is
server-side only until a dedicated expression expander exists (SD8).

### SD8 — Deferred, deliberately

- **Client-side expression expander** (expression/lambda-capable nanopass
  substitution). Trigger: a read-only ClickHouse target that actually hosts
  leeway data. Until then, leeway data lives on boxer-provisioned servers
  where the pack installs.
- **Executable / WASM UDF host** (option c). Trigger: a per-value transform
  SQL cannot reasonably express (heavy decoding, inference). Shape when it
  comes: an executable UDF host embedding wazero running leeway-emitted
  WASM. Constraint to record now: optimizer-opaque, so never load-bearing
  in prunable predicates. `allow_experimental_executable_udf_drivers`
  exists server-side and deserves a survey then.
- **Upstream contributions** (option d). Trigger: a measured hot path the
  macro form cannot reach, or descriptor-only reshapes where native
  implementations are asymptotically better (offset-sharing re-segmentation
  is O(instances) natively versus O(elements) as a macro). First
  candidates: `indicesOf` (also the ragged-join kernel) with its
  index-analysis mapping, and re-segmentation. The macro stays as interim
  implementation and as the differential oracle; no standing fork.
- **Ends-taking twins** (`raggedReduceAt(agg, vals, card, ends)` style,
  consuming a materialized `cusumcard`). Trigger: measured cumulative-sum
  cost on real shapes.
- **Guard-audit and axis-checking passes**, and a home for measured
  guard-slack calibration. Lands with the ADR-0139-adjacent checking work.
- **General per-run `scan`** (prefix aggregation beyond `arrayCumSum`).
  Realization gap noted in the algebra document.

## Surfaces — Tier 1

- New global SQL function names on every boxer-provisioned ClickHouse: the
  SD6 roster plus `LW_PACK_VERSION()`. Visible to every client via
  `system.functions` (`origin = 'SQLUserDefined'`).
- A Go spec-and-emitter package under `public/semistructured/leeway/`
  (exact home decided at implementation, adjacent to the ClickHouse DDL
  mapping), and an install-reconcile call at the play connect seam and in
  appliance provisioning.
- No new environment variables; no wire-protocol changes; the
  [read-back generator](./0066-leeway-dql-clickhouse-readback-generator.md)
  is unaffected (it emits its own SQL).

## Alternatives

- **Pass-primary (a)** — expand the expression vocabulary client-side.
  Killed: native-protocol clients never see it; the existing expander is
  literal-only so new substitution machinery is required first; and
  expansion-before-storage destroys authored intent in a system that
  persists queries as data. Retained for statement-level constructs, where
  it is the only option.
- **Proxy-primary** — expand at the wire for HTTP tools. Killed as the
  load-bearing path: it makes a proxy a semantic single point of failure
  with a single-statement SELECT-only grammar in front of foreign SQL, and
  native-protocol clients bypass it. Retained as an additive layer.
- **Executable/WASM-primary (c)** — killed for array plumbing: block-IPC
  overhead orders above inlined macros, and permanently opaque to index
  analysis. Deferred to its earn-condition (SD8).
- **Native-primary (d)** — killed as the default path: a standing fork
  fights the supply-chain posture (pinned official binaries, monthly
  upstream releases), and the measured shape-enumeration of index support
  shows "native" does not imply "accelerated" without separate analyzer
  work. Deferred upstream-first (SD8).
- **Adopting an external naming system** (a Wolfram-terms formulation was
  explored). Rejected: the executable-oracle benefit gated on a proprietary
  engine, and ClickHouse's own function vocabulary is consistent enough to
  extend natively (SD2). The algebra document keeps a groundings section as
  a reader aid only.

## Consequences

### Positive

- One vocabulary for every client — `clickhouse-client`, play, HTTP tools —
  with zero runtime overhead and uniform pruning discipline (guards cannot
  be forgotten at call sites).
- Stored queries and `system.query_log` keep the authored, intention-bearing
  form; replay needs only a server with the pack installed.
- The pack is simultaneously the specification and the differential oracle
  for any future native successor of a function (same-equations criterion,
  per the algebra document's laws).

### Negative

- Server state that must be reconciled: a window exists between a spec
  change and the next connect-time reconcile on servers reached by other
  clients. `LW_PACK_VERSION()` makes the skew observable; provisioning
  closes it.
- A global, flat namespace claims names forever; the additive-only policy
  (SD5) is a real constraint on renaming mistakes.
- Guard bodies restate predicate semantics inside one function (lambda plus
  `has` conjuncts); divergence is prevented only by single-spec emission
  today, an audit pass later (SD8).
- Verified on ClickHouse 26.7 only. The minimum server version for
  lambda-parameter UDFs is untested; install must feature-probe and fail
  loudly rather than assume.

### Neutral

- The vocabulary is ClickHouse-specific by construction; DuckDB or Arrow
  equivalents would be separate packs over the same algebra.
- The how-to and the algebra document already use the SD6 names
  (`LW_RAGGED_NEST`, `LW_RAGGED_EXISTS`), so no committed prose changes on
  acceptance.

## Migration — Tier 1

Nothing exists to migrate; installation is purely additive. The install
path's collision check (SD5) covers the one future migration hazard: a
server upgrade introducing a builtin with a pack name. Pre-acceptance name
changes are free; post-ship changes follow additive-only (SD5).

## Verification plan — Tier 1

Already verified against a live 26.7 (recorded in the how-to and the
algebra document's evidence section): macro inlining and plan identity,
index/PREWHERE transparency through wrappers, lambda parameters and UDF
nesting, guard-vs-lambda granule counts, fused-versus-nested timings, and
the correctness of every SD6 body shape including empty-run boundary
behavior on foreign data.

For acceptance, the implementation must add:

- golden tests of the emitted `CREATE OR REPLACE FUNCTION` statements from
  the Go spec;
- an integration-lane test (`//go:build integration`) that installs the
  pack on a live server, asserts `LW_PACK_VERSION()`, and re-runs the
  plan-identity and guard-pruning probes as regressions;
- differential tests of pack calls against their handwritten expansions on
  randomized co/ragged data (positivity respected);
- the install-time feature probe and collision check, each failing loudly.

## Status

Accepted 2026-08-02. Changes now arrive as dated `## Update` sections.

## References

- [Array-idioms how-to](../howto/leeway-clickhouse-array-idioms.md) —
  executable forms and measurements.
- [The leeway query algebra](../explanation/leeway-query-algebra.md) — the
  model this ADR realizes; deliberately decision-free.

### Related ADRs

- [ADR-0108](./0108-keelson-sql-pass-registry.md) — pass registry; home of
  statement-level rewrites and the future checking passes.
- [ADR-0139](./0139-semantic-layer-text2dsl.md) — the semantic-layer seam
  the schema-aware analysis belongs to.
- [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md) — generated
  read-back SQL; unaffected by this ADR.
- [ADR-0094](./0094-keelson-introspection-tables.md) — the `url()`
  introspection path, the standing example of a read-only target (SD8
  trigger).

## Update 2026-08-02 — the read-back helper family layers on the pack

The reference note that the
[read-back generator](./0066-leeway-dql-clickhouse-readback-generator.md)
is unaffected is superseded in one particular: its hand-use helper
`LEEWAY_UNFLATTEN` is retired in favor of `RAGGED_NEST`, and
`readback.HelperUDFsSQL()` now emits this pack's statements ahead of its
own `LEEWAY_LU_*` family. The generator's *emitted* SQL remains pack-free.

Later the same day the consolidation reached the generator's emissions:
`LEEWAY_LU_MEMB_IDX_TO_VAL_IDX` retired onto `RAGGED_PARENT_IDS`, so generated
read-back SQL now names pack functions and the sentence above about emitted
SQL staying pack-free no longer holds. Provisioning is unchanged —
`HelperUDFsSQL()` emits the pack first.

## Update 2026-08-06 — one naming style: UPPER_SNAKE

The roster shipped in camelCase (`coLookup`, `raggedStarts`) beside the
read-back family's UPPER_SNAKE (`LEEWAY_VALUE_BY_TAG_EQUAL`,
`LEEWAY_LU_ATTR_BY_TAG`). Since the previous Update the two are one stack —
`HelperUDFsSQL()` provisions both, generated read-back SQL names pack
functions, and a query written against leeway lanes routinely calls both in
one expression. Two styles in one expression is a reader's problem with no
compensating benefit, and the case boundary carried no information: nothing
distinguishes a pack function from a read-back helper except which document
you happen to have read.

**Every pack function is renamed to UPPER_SNAKE** — `coLookup` →
`CO_LOOKUP`, `raggedParentIds` → `RAGGED_PARENT_IDS`, `leewayPackVersion` →
`LEEWAY_PACK_VERSION`, and so on for the whole roster. The `co`/`ragged`
prefixes survive as `CO_`/`RAGGED_`, so the two axes of the algebra remain
readable at a glance. The §SD6 table above is rewritten to the new names
rather than left stale, since it is the roster's specification and a reader
following it would otherwise write SQL that does not resolve.

Nothing else changes: same bodies, same semantics, same dependency order,
same macro-inlining properties. **`Version` is bumped to 2** — every name in
the pack changed, which is exactly the skew `LEEWAY_PACK_VERSION()` exists to
detect, and a server provisioned before this Update answers 1 while carrying
none of the new names.

The rename is not backward compatible and no aliases are installed. Because
every statement is `CREATE OR REPLACE`, provisioning the new pack **leaves the
old camelCase functions in place** on a server that had the previous one;
they must be dropped explicitly. That is a property of SQL-UDF provisioning
this ADR did not previously call out, and it applies to any future rename.

## Update 2026-08-07 — one namespace: `LW_`

The previous Update unified the pack's *case* with the read-back family's.
It left the *prefixes* alone, and that is where the remaining inconsistency
was: four of them — `CO_`, `RAGGED_`, `LEEWAY_` and identsql's `LW_` — for
one product, so nothing in a name said "this is boxer's" and no single
question asked a server for the vocabulary it carries.

**Every function moves under `LW_`**, keeping the family segment that names
the axis: `CO_LOOKUP` → `LW_CO_LOOKUP`, `RAGGED_PARENT_IDS` →
`LW_RAGGED_PARENT_IDS`, `LEEWAY_PACK_VERSION` → `LW_PACK_VERSION`. The
read-back family (ADR-0066) moves the same way — `LEEWAY_VALUE_BY_TAG_EQUAL`
→ `LW_VALUE_BY_TAG_EQUAL`, `LEEWAY_LU_*` → `LW_LU_*` — and identsql's
`LW_ID_*` (ADR-0106) does not move at all: it already had the shape the rest
now follow, `LW_` then a family segment then the operation. §SD2 and the §SD6
roster above are rewritten rather than left stale.

`Version` is bumped to **3**. Same bodies, same semantics, same dependency
order, same macro-inlining properties.

What the namespace buys, beyond a reader not having to remember which
package a function came from: `WHERE name LIKE 'LW\_%'` is now the whole
leeway vocabulary on a server. That is one query for the drift check
[ADR-0171](./0171-leeway-sql-read-surface.md) §SD2 wants, and it is what
lets a client say *installed* or *missing* per function rather than only
listing what it happens to find — which is what play's vocabulary panel
([ADR-0174](./0174-play-sql-vocabulary-panel.md)) does with it.

**The stale-function problem from the previous Update is now handled in
code.** `Install` drops the names this repository has withdrawn, from an
append-only list (`chpack.RetiredNames`) covering both earlier pack rosters,
the read-back family's pre-namespace spellings, and the two functions retired
outright on 2026-08-02. Last time 16 stale functions had to be dropped by
hand; this time 23 names change and none do. The drop is best-effort and runs
last, after the new roster has verified — a server must not be left with
neither.

That list is deliberately not the general reconcile. "Drop every `LW\_%`
function the build does not declare" needs the full declared set, which spans
`chpack`, the read-back family and `identsql`, and no package holds all
three; that reconciler stays ADR-0171 §SD2's, still proposed. The list is the
part that needed no new decision.

**Not renamed:** the trial run artifacts under
`doc/trials/jsonbench-on-facts/runs/`. They record which functions were
installed on a server on a given day, including a retired one that was still
there — the finding that motivated ADR-0171. Rewriting them would falsify the
record; the trial's *protocol* files (`queries-*.sql`) are renamed, since
those are meant to be re-run.

**Verified against a live server.** Play was driven against ClickHouse 26.7
after this change: `LW_PACK_VERSION` reports 3, the renamed roster resolves,
and the pre-rename spellings are gone from the server — `CO_GATHER` does not
appear among its 408 user-defined functions, where the previous rename would
have left it. The drop step is what removed it.

## Update 2026-08-14 — the pack's marker and installer move to the surface

[ADR-0171](./0171-leeway-sql-read-surface.md) §SD2 landed, and it takes two
things this ADR decided.

**`LW_PACK_VERSION` is retired**, replaced by one `LW_SURFACE_VERSION` for
all three leeway families. §SD5's mechanism is unchanged — a version
constant, a marker function, an installer that verifies it afterwards — but
it now covers the pack, the read-back family and `identsql` together, under
the invariant that the marker at revision N means all three are installed at
revision N. Two markers that can disagree are the ambiguity the marker was
introduced to remove, so keeping the pack's own alongside the surface's was
not an option. The name goes on `RetiredNames`, and an install drops it.

**`chpack.Install` is removed**, and provisioning is
`lwsqlsurface.Install`. A pack-only install can no longer verify anything,
and a pack-only install that stamped the surface marker would make the
marker lie. `chpack` keeps `Functions`, `Statement`, `Statements` and
`RetiredNames`: it declares and renders the pack, and no longer installs it.

Unchanged: the roster, every body, the append-only-semantics rule, and the
`RetiredNames` list — which had already outgrown the pack, carrying the
read-back family's pre-`LW_` spellings, and which stays here rather than
moving to the surface package. One append-only list is the point; a second
one would be a second place to forget.

The general reconcile the 2026-08-07 Update left open now exists as
`lwsqlsurface.Reconcile`. It reports undeclared `LW\_%` functions by
default and drops them only when a caller asks — an undeclared name may
belong to a fork or a downstream consumer, and hosts reconcile
automatically at startup.
