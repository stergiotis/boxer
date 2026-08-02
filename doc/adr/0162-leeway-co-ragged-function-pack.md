---
type: adr
status: proposed
date: 2026-08-02
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not
> implement as if accepted.

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

- **Expression-level vocabulary** (the co*/ragged* functions of SD6) ships
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

### SD2 — Pack conventions follow ClickHouse, with owned prefixes

Names and semantics lean on the built-in function family, which is modern
and consistent enough to extend rather than replace: camelCase, lambda-first
argument order (`raggedExists(f, vals, card)` mirrors
`arrayExists(f, arr)`), 1-based indexing, and ClickHouse's own suffix
patterns as the template for variants (`arrayReduceInRanges` is the
precedent). Two owned prefixes — `co` and `ragged` — group the pack in
`system.functions`, signal leeway semantics (positivity, alignment), and
keep clear of present and future builtin names. Exact-case spelling is
mandatory everywhere (ClickHouse UDF names are case-sensitive; some builtin
lookups are not). The pack wraps **compositions only, never renames**: a
builtin that already is the operation is used directly.

### SD3 — Predicate functions bundle their sargable guards

Every pack predicate that implies a constant-membership condition embeds
the corresponding `has`/`hasAny` conjunct in its own body (e.g.
`coExistsEq2` carries `has(a, x) AND has(b, y)` beside its lambda). Measured
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
(`raggedExists` is range-max over a lifted boolean stream, not
exists-over-nest). `raggedNest` stays in the pack as the explicit
presentation-boundary operation, documented as such. Positivity (both
descriptors are at least 1 on leeway data) means empty-run aggregate
defaults are dead cases; the bodies remain total for foreign data that does
carry zeros.

### SD5 — One Go spec; idempotent install; additive-only evolution

The pack is defined once in Go — name, parameters, body — and emitted as
`CREATE OR REPLACE FUNCTION` statements. Installation is an idempotent
reconcile at connect time (play startup, and appliance provisioning), the
same reconcile-at-startup pattern other ClickHouse-adjacent state uses. A
zero-argument marker function, `leewayPackVersion()`, returns the pack
revision so client/server skew is a query. Shipped names are **append-only
in semantics**: a published function's meaning never changes; changed
behavior gets a new name. Install performs a collision check — if a name
resolves to a builtin (e.g. after a server upgrade), install fails loudly
for that name rather than shadowing or silently skipping.

### SD6 — The v1 roster

Definitions below are denotational; the Go spec is normative, and the
how-to carries executable forms. Everything not listed is deliberately not
in v1.

| function | definition |
|---|---|
| `coLookup(keys, lane, k)` | `lane[indexOf(keys, k)]` |
| `coLookupNull(keys, lane, k)` | `NULL` when `indexOf(keys, k) = 0`, else the lookup |
| `coGather(lane, sel)` | `arrayMap(i -> lane[i], sel)` |
| `coArgSort(keys)` | permutation that sorts `keys` (argsort) |
| `coArgMax(lane, keys)` | `arrayReduce('argMax', lane, keys)` |
| `coExistsEq2(a, x, b, y)` | guarded two-lane equality existence (SD3) |
| `raggedStarts(card)` | run start offsets, 1-based |
| `raggedRanges(card)` | `(start, len)` tuples for `arrayReduceInRanges` |
| `raggedParentIds(card)` | per-element instance index (broadcast carrier) |
| `raggedIota(card)` | per-element position within its run |
| `raggedNest(vals, card)` | per-instance lists; boundary op (SD4) |
| `raggedReduce(agg, vals, card)` | `arrayReduceInRanges(agg, raggedRanges(card), vals)` |
| `raggedExists(f, vals, card)` | range-max over `arrayMap(f, vals)` |
| `raggedCount(f, vals, card)` | range-sum over `arrayMap(f, vals)` |
| `raggedElem(vals, card, i, k)` | k-th value of instance i |

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
  SD6 roster plus `leewayPackVersion()`. Visible to every client via
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
  clients. `leewayPackVersion()` makes the skew observable; provisioning
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
  (`raggedNest`, `raggedExists`), so no committed prose changes on
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
  pack on a live server, asserts `leewayPackVersion()`, and re-runs the
  plan-identity and guard-pruning probes as regressions;
- differential tests of pack calls against their handwritten expansions on
  randomized co/ragged data (positivity respected);
- the install-time feature probe and collision check, each failing loudly.

## Status

Proposed. Pre-acceptance, this document is edited in place; after
acceptance, changes arrive as dated `## Update` sections.

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
