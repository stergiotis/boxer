---
type: adr
status: proposed
date: 2026-07-13
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0116: play resolves friendly leeway column handles to physical names

## Status

Proposed. v1 implemented (uncommitted at time of writing): the generic
`ResolveColumnNames` pass and `ColumnResolverI` in
`public/db/clickhouse/dsl/nanopass/passes`, the leeway resolver and label
builder in `public/semistructured/leeway/lwsql`, and the play wiring
(`system.columns` probe, per-client registry, display-time labels) in
`apps/play`.

## Context

Leeway generates physical column names that carry the whole authored schema —
section, column, role, canonical type, encoding hints, use-aspects,
co-section/streaming groups — as a 21- or 13-component colon-joined string
(the `HumanReadableNamingConvention`, ADR-0066). Real examples from the play
demo table:

```
tv:symbol:value:val:s:m:0:24:0::data
id:id:u64:2k:0:0:
```

A user querying a leeway table in the play playground (ADR-0097) must type
those verbatim, backtick-quoted, in every clause. They are precise and stable,
but not memorable — the friction this ADR addresses.

**What the schema actually guarantees (verified in code).** `TableDesc`
finalization runs `TableValidator.validateNames`
(`public/semistructured/leeway/common/lw_table_validator.go`), which expands
every name into all six `naming.AllNamingStyles` and rejects the table on any
collision. Two properties matter here, and one corrects the naive premise:

- Uniqueness is **reject-on-collision, not auto-dedup** — a colliding table
  simply fails to build; nothing renames it.
- Uniqueness is **scoped**: section names are unique table-wide; tagged-value
  *column* names are unique only within their section (so the column name
  `value` recurs across the `symbol`, `string`, … sections).
- The descriptive token a user would want to type is, in real mappings, often
  the **membership** (`droneStatus`, `goPkgImportPath`) — and membership is
  compiled to row data / aspect encoding, **not** textually present in the
  physical name. Section and column `StylableName`s *are* present.

**Where play stands today.** play only reconstructs a `TableDesc` *after*
execution, from the result's physical column names, via
`DiscoverTableFromColumnNames` (`apps/play/play_card_driver.go`). The point
where SQL is rewritten before it ships — the `passreg.StagePreExecute` seam
(ADR-0108), `apps/play/play_client.go` `buildResidual` — receives a bare string
with no schema. So the rewrite path has no way to know any friendly name.

## Decision

A schema-aware nanopass pass rewrites friendly column handles to physical names
before a query ships; the reverse mapping labels result columns in the UI. No
transformation happens in ClickHouse.

### SD1 — Handle syntax: bare section, or quoted `section:column`, style-folded

- A section with a single value column is named **bare**: `SELECT symbol`.
- A specific column of a multi-value section is named with a colon composite,
  which must be **quoted** because it is a single identifier:
  `` SELECT `geoPoint:lat` ``.
- Both sides are folded to `LowerSpinalCase` before matching (the
  style-independent canonical form; `naming.Compare` uses the same reduction),
  so `` `geoPoint:lat` ``, `` `geo_point:lat` ``, and `` `geo-point:lat` `` are
  one handle.

The colon is deliberate: it is neither SQL's `.` qualification nor `::` cast.
A **bare** `geoPoint:lat` cannot be used because in the grammar (grammar1) a
lone `COLON` only appears as the ternary tail `cond ? a : b`, so `geoPoint:lat`
is a parse error — and the pass works on the parse tree. Quoting sidesteps that
with zero grammar change. An unquoted colon form is a possible future grammar
extension (a `nestedIdentifier COLON nestedIdentifier` alternative), deferred
because it must be proven unambiguous against the ternary and it touches
generated parser code.

### SD2 — A generic substitution pass, leeway-agnostic

`ResolveColumnNames(resolver ColumnResolverI, defaultDatabase string)` lives in
`nanopass/passes`. It is domain-agnostic: `ColumnResolverI.Resolve(db, table,
handle) (physical, ok)` is the only seam, and the leeway implementation lives
elsewhere. This keeps the SQL framework free of any leeway dependency — the same
separation `SchemaProviderI` already models, and the same shape as the
`identsql.ExpandPass` precedent (a domain pass registered through `passreg`,
ADR-0106/0108).

The pass is **scope-aware** (`BuildScopes`) and rewrites bare and
table-qualified column references **wherever a column appears** — projection,
`WHERE`, `GROUP BY`, `ORDER BY`, `HAVING`, nested expressions — not only the
SELECT list. That reach is the reason it substitutes identifiers rather than
wrapping them (see Alternatives). It is one-directional (input only) and never
renames output. A bare handle that resolves in exactly one in-scope table is
rewritten to the quoted physical name; zero or several matches (a real column, a
SELECT alias, or a genuinely ambiguous reference) are left untouched for the
server to interpret. A qualified `alias.handle` resolves against that alias's
table and keeps the `alias.` prefix.

### SD3 — Catalog source: `system.columns` + `DiscoverTableFromColumnNames`

The resolver learns a table's schema from the live endpoint. For each table in
scope it issues `SELECT name FROM system.columns …` (params via the HTTP
`param_*` channel; `database = ''` resolves to `currentDatabase()`), then feeds
the physical names through the *same* `DiscoverTableFromColumnNames` play already
uses post-execution. This introduces no ingestion-side change. Its known limit,
accepted by choosing it: it recovers only what is embedded in the physical name
(section and column), never membership descriptive names.

The probe is **lazy** (first query touching a table), **cached** for the session
(`CachingSchemaProvider`, plus a per-table index cache in the resolver, negatives
included), and issued through a **direct** client call that bypasses the pass
registry — otherwise resolving names *inside* the `system.columns` query would
recurse.

### SD4 — Only value columns are handles

Support columns (length, ref, cardinality) are named after their role
(`tv:blobArray:lr:…`); they are machinery, not something a user names. A single
`classifyColumns` helper parses the physical names once, takes the reconstructed
`TableDesc.TaggedValuesSections[*].ValueColumnNames` as the authority for which
`(section, column)` pairs are value columns, and both the resolver and the label
builder key off it — so the query vocabulary and the display vocabulary are
identical. Plain/backbone columns (`id`, `ts`, `naturalKey`) are named by their
bare name.

### SD5 — Result labels are display-time, physical-on-hover

The SQL sent to ClickHouse keeps physical names; there is no `AS` aliasing and no
data-model rename. The Table tab maps each result column's physical name to its
friendly label (`lwsql.BuildLabels`) and renders the label as the header, with
the physical name on hover. Keeping physical names the ground truth means nothing
downstream that keys on them — the reactive query graph, re-fusing, the Schema
tab — is disturbed.

Labels intentionally **diverge** from the resolver's input vocabulary (SD4).
The resolver resolves value columns only, because those are what a user queries
by name; but a raw `SELECT *` on a leeway table returns many more support
columns (membership refs, cardinalities, lengths) than value columns, so
labelling only value columns leaves the table reading as mostly unlabelled. The
label builder therefore covers **every** leeway column: a value column labels as
its handle form (a section's sole default `value` column as the bare section,
else `section:column`), and a support column labels as `section:role` (e.g.
`symbol:lr`). A support label is display-only — it is not a handle the resolver
accepts — which is sound, since labels never need to round-trip through the
input path.

### SD6 — Wiring: a per-client registry at `StagePreExecute`

play gives its `Client` its own `passreg.Registry` — the standard set plus the
resolver — ordered after `identsql` (100 → 200) so names resolve on
already-macro-expanded SQL. It is client-scoped rather than added to
`passreg.Default` because the schema provider closes over this client's live
endpoint. Both hosts must wire it themselves (the `identsql` precedent): the
standalone CLI in `play_cli.go`, and the carousel-embedded launcher in
`PlayLauncher.Mount`.

### SD7 — Scope cull

- **Membership discrimination is out of scope.** Distinguishing `droneStatus`
  from `cyberType` inside a shared `symbol` column is a row-level predicate, not
  a name, and is absent from the physical name by construction of the chosen
  catalog source. Multi-membership packing (ADR-0109) is why this is inherent,
  not an oversight.
- **Bare unquoted colon** is deferred (SD1).
- **Whole-section expansion** — a bare multi-value section expanding to all its
  columns via `COLUMNS('^tv:section:…')` — is the one place `COLUMNS` is the
  correct construct, but it is not built in v1; a bare multi-value section is
  simply left unresolved (the specific `section:column` handles work).

## Alternatives considered

- **Pure `COLUMNS('regex')` (no catalog).** Wrap each handle as
  `COLUMNS('…')` and let ClickHouse resolve it. Killed on four counts: it matches
  a regex against the *physical name string*, so it can only reach section/column
  substrings and **never** a membership-packed field; it is reliable only in the
  projection, not `WHERE`/`GROUP BY`/`JOIN`; it does not rename output (a
  `COLUMNS('geoPoint')` result column is still `tv:geoPoint:…`); and the server
  resolves it, so play cannot report "unknown" or "ambiguous" before the round
  trip.
- **A pass that inserts `COLUMNS` (the hybrid).** Killed because it pays the full
  schema-awareness cost of substitution yet inherits `COLUMNS`'s projection-only
  and no-rename limits. Once a pass is schema-aware enough to know a token is a
  leeway field, substituting the physical name directly strictly dominates
  wrapping it. `COLUMNS` is kept in mind only for the whole-section case (SD7),
  where multi-column expansion is genuinely what is meant.
- **Query-time views that alias physical → friendly.** Heavier (DDL and view
  lifecycle per table), and ClickHouse view column-aliasing has its own edges.
  Not pursued.

## Consequences

- Readable queries in every clause, readable result headers, and — as a
  byproduct — the first pre-execution schema catalog play has had. The same
  provider can back a future autocomplete / schema browser, which today is
  impossible because all schema is result-derived.
- A new dependency edge: `leeway/lwsql` → `nanopass/passes` + `leeway/ddl`. The
  general framework stays leeway-free; the coupling lives in the leeway-adjacent
  package, following the `identsql` precedent.
- A per-table `system.columns` round trip on first touch (then cached), issued
  inline on the execution path before the main query.
- Because `StagePreExecute` passes run best-effort, a resolver that leaves a
  handle unresolved (ambiguous bare section, unknown handle) surfaces only as the
  server's own error plus the Preview "as sent" view — there is no dedicated
  client-side diagnostic yet. Follow-ups, not gates: surface those in the UI;
  reset the resolver cache on endpoint switch (a `Reset` exists, unwired); and
  the client-scoped pass is not listed by the `keelson('sql_passes')` catalog
  (ADR-0108/0094), which reads `passreg.Default`.

## Validation

- `ResolveColumnNames` unit tests (`nanopass/passes`): resolution in projection
  and in `WHERE`/`GROUP BY`/`ORDER BY`, alias-preserving qualified refs,
  cross-join ambiguity left untouched, subquery scoping, idempotence, and the
  bare-colon parse error.
- `lwsql` unit tests: a known table built through the manipulator, its real
  generated physical names driving folding, single vs multi-value-column
  sections, plain columns, non-leeway passthrough, and a label↔resolve
  round-trip (which caught a labeler bug where a multi-value section's `value`
  column was labelled bare and so did not round-trip).
- `go build` and `go test` pass for the touched packages including `apps/play`.
- Live drive (GPU-less `headless_svg` host, one-shot `BOXER_PLAY_SCREENSHOT` →
  SVG → PNG) against a running ClickHouse with `anchor.facts`: `SELECT * …`
  renders every header friendly (value and support), and `SELECT id, symbol …`
  rewrites to physical names in the "as sent" view and returns rows. The drive
  surfaced two defects, both fixed: `PlayLauncher.Mount` never installed the
  resolver (only the CLI did), and the reused `CachingSchemaProvider` returned
  not-found on every first (cache-miss) lookup — it cached the delegate's
  columns but never surfaced them until the second call, so no table ever
  resolved on first touch (a latent `ExpandColumns` bug too).
