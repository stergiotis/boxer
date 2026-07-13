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

Proposed. Implemented: the generic `ResolveColumnNames` pass and
`ColumnResolverI` in `public/db/clickhouse/dsl/nanopass/passes`, the leeway
resolver and label builder in `public/semistructured/leeway/lwsql`, and the play
wiring (`system.columns` probe, per-client factory binding, display-time labels)
in `apps/play`. The pre-execute pass is registered as a late-bound
`passreg.Factory` (ADR-0108 §SD7); an earlier cut used a per-client
`passreg.Registry` (see SD6).

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

### SD1 — Handle syntax: `section:column` or `section:*`, colon-always

The colon is the **sole** marker of a leeway handle; a bare identifier is always
ordinary SQL. That single rule is what makes the client-side warnings (SD8)
false-positive-free: a `:` cannot occur in a bare SQL identifier, so anything
carrying one is unambiguously a handle to resolve or warn about.

- `` `section:column` `` — one column. Sections are the tagged sections
  (`geoPoint`, `symbol`, …) plus six plain/backbone sections derived from the
  physical item type — `id`, `routing`, `timestamp`, `lifecycle`, `transaction`,
  `opaque` — so the backbone columns are reachable too (`` `id:id` ``,
  `` `routing:naturalKey` ``), with no bare-name exception.
- `` `section:*` `` — all of the section's value columns.
- Both are **quoted** (a colon-bearing identifier is a single identifier), and
  both sides fold to `LowerSpinalCase`, so `geoPoint:pointLat` and
  `geo_point:point-lat` are one handle.

A bare unquoted `geoPoint:pointLat` cannot parse — in grammar1 a lone `COLON` is
only the ternary tail `cond ? a : b`, and the pass works on the parse tree — so
the handle must be quoted; zero grammar change. An unquoted colon form is a
deferred grammar extension.

Rejected: keeping **bare section names** for single-value sections (a shorter
`symbol` for the common case). It reintroduces an overlap between handles and
ordinary columns, which reopens the false-positive problem SD8 relies on the
colon to close; the plain-section names above make the backbone columns
reachable without it.

### SD2 — A generic substitution pass, leeway-agnostic

`ResolveColumnNames(resolver, defaultDatabase, sink)` lives in `nanopass/passes`.
It is domain-agnostic: `ColumnResolverI.Resolve(db, table, handle) ResolveResult`
is the only seam — a verdict (`NotAHandle` / `OK` with physical name(s) /
`UnknownSection` / `UnknownColumn` with candidates) — and the leeway
implementation, including the colon policy, lives elsewhere. This keeps the SQL
framework free of any leeway dependency — the same separation `SchemaProviderI`
already models, and the `identsql.ExpandPass` precedent (a domain pass registered
through `passreg`, ADR-0106/0108).

The pass is **scope-aware** (`BuildScopes`) and rewrites column references
**wherever a column appears** — projection, `WHERE`, `GROUP BY`, `ORDER BY`,
`HAVING`, `ARRAY JOIN`, nested expressions — not only the SELECT list. It
substitutes identifiers rather than wrapping them in `COLUMNS(…)` (see
Alternatives), and is one-directional (input only). An `OK` verdict splices in
its physical name(s): one for `section:column`, several — a comma-separated list
— for `section:*`, so a `:*` expands co-positionally inside `ARRAY JOIN` as well
as the projection (ClickHouse validates positions where a list is illegal). A
bare handle that resolves in exactly one in-scope table wins; several is
ambiguous (left untouched). A qualified `alias.handle` resolves against that
alias's table, and a qualified `:*` prefixes the alias onto every expanded
column.

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

### SD4 — Every column resolves by `section:column`; `:*` is value columns

A single `classifyColumns` helper parses the physical names once and is the
shared authority for the resolver and the label builder. It maps each column to
its section — the tagged section, or, for a plain column, the item-type section
(SD1) — and marks value columns via `TaggedValuesSections[*].ValueColumnNames`
(plain columns are value columns; support columns — length, ref, cardinality,
named after their role like `tv:blobArray:lr:…` — are not). Two consequences:

- `` `section:column` `` resolves **any** column, value or support, so a
  specific handle never mis-warns "no such column" on real machinery.
- `` `section:*` `` and the "did you mean …?" candidates use the **value**
  columns only — the data, not the membership machinery.

### SD5 — Result labels are display-time, physical-on-hover

The SQL sent to ClickHouse keeps physical names; there is no `AS` aliasing and no
data-model rename. The Table tab maps each result column's physical name to its
friendly label (`lwsql.BuildLabels`) and renders the label as the header, with
the physical name on hover. Keeping physical names the ground truth means nothing
downstream that keys on them — the reactive query graph, re-fusing, the Schema
tab — is disturbed.

Every leeway column labels as `section:column` — value, support, and the six
plain/backbone sections (`id:id`, `routing:naturalKey`) alike — so a header
reads exactly as a user would type it. The display vocabulary is thus symmetric
with the input vocabulary (both cover every column; only `:*` and candidate
suggestions narrow to value columns), and the raw `SELECT *` on a leeway table,
dominated by support columns, reads friendly throughout.

### SD6 — Wiring: a late-bound factory at `StagePreExecute`

The resolver's pass closes over this client's live endpoint (its schema provider
probes `system.columns` there), so it cannot be a process-global `Pass` value.
It is registered as a `passreg.Factory` — the "second entry kind" of ADR-0108
§SD7 — in the standard set (`defaults.RegisterStandard`), ordered after
`identsql` (100 → 200) so names resolve on already-macro-expanded SQL. The
factory's `Build` type-asserts its binding to `passes.ColumnResolverI`, keeping
the standard set leeway-free; play supplies that binding *per client* via
`Client.passBinding` (set by `installLeewayNameResolution`), and
`Client.buildResidual` applies the stage with `ApplyBestEffortBound`.
`Client.passes` itself stays `passreg.Default`.

The descriptor being process-global while the binding stays per client is what
lets the resolver show in `keelson('sql_passes')` — as a `late_bound` row. Only
the *binding* is wired per host (the `identsql` precedent): the standalone CLI in
`play_cli.go` and the carousel launcher in `PlayLauncher.Mount`; the factory
itself rides each host's existing `RegisterDefaults` call.

Rejected: giving each `Client` its own `passreg.Registry` (the standard set plus
a concrete resolver pass). It was the first cut, but a client-scoped registry
never reaches `passreg.Default`, so the resolver was invisible to the
`sql_passes` catalog — the factory keeps the per-client *binding* while restoring
catalog visibility, and removes the duplicated standard-set registration.

### SD7 — Scope cull

- **Membership discrimination is out of scope.** Distinguishing `droneStatus`
  from `cyberType` inside a shared `symbol` column is a row-level predicate, not
  a name, and is absent from the physical name by construction of the chosen
  catalog source. Multi-membership packing (ADR-0109) is why this is inherent,
  not an oversight.
- **Bare unquoted colon** is deferred (SD1).
- **A derived `AS` alias for a `:*` expanded inside `ARRAY JOIN`** is deferred —
  the columns array-join co-positionally without one for now.

Whole-section expansion is *not* descoped: `section:*` is built (SD1/SD2/SD4) and
expands as a plain comma-separated column list wherever the identifier sits.

### SD8 — Client-side resolution warnings in Diagnostics

Because the colon marks intent unambiguously (SD1), a handle that names no known
section, or a known section's non-existent column, is a confident error worth
flagging **before** the query runs. `ResolveColumnNames` takes an optional
`sink func(ColumnDiagnostic)`; the execution-path pass passes nil (silent
rewrite), while play's Diagnostics pane runs the resolver with a collecting sink
on the debounced buffer — off the render thread, since the schema probe may hit
the network the first time (cached thereafter; latest-wins, polled like the
EXPLAIN probe) — and lists the unresolved handles, with candidate suggestions
for an unknown column. Bare identifiers are never flagged (ordinary SQL). The
only cost is the cached `system.columns` probe; the user's actual query never
runs.

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
- `StagePreExecute` passes run best-effort, so on the execution path an
  unresolved handle still just passes through to the server. The Diagnostics pane
  now warns about the confident cases client-side first (SD8), so this is far
  less silent than it was. Remaining follow-up, not a gate: reset the resolver
  cache on endpoint switch (a `Reset` exists, unwired). The resolver *is* listed
  by the `keelson('sql_passes')` catalog (ADR-0108/0094) — as a `late_bound`
  factory row (ADR-0108 §SD7) — now that its descriptor lives in
  `passreg.Default` rather than a per-client registry.

## Validation

- `ResolveColumnNames` unit tests (`nanopass/passes`): colon handles resolved
  everywhere (projection, `WHERE`/`GROUP BY`/`ORDER BY`), bare identifiers left
  untouched, `:*` expansion in the projection *and* in `ARRAY JOIN`,
  alias-preserving qualified refs (and per-column alias on a qualified `:*`),
  diagnostics for unknown-section and unknown-column (with candidates),
  cross-join ambiguity, idempotence, and the bare-colon parse error.
- `lwsql` unit tests: a known table built through the manipulator, its real
  generated physical names driving folding, `section:column` and `section:*`,
  plain/backbone sections (`id:id`), unknown-section / unknown-column verdicts
  with candidates, non-leeway passthrough, and a label↔resolve round-trip over
  every column.
- `go build` and `go test` pass for the touched packages including `apps/play`.
- `passreg`/`defaults` tests cover the factory path: registration and the shared
  `(stage, name)` namespace, `ApplyBestEffortBound` merging entries and factories
  in `(Order, Name)` order while skipping a declined binding, `Catalog()` marking
  the resolver `late_bound`, and `RegisterStandard` placing `ResolveColumnNames`
  at order 200 with a `Build` that accepts a `ColumnResolverI` and declines
  anything else. The `sql_passes` provider test asserts the `late_bound` column.
- Live drive (GPU-less `headless_svg` host, one-shot `BOXER_PLAY_SCREENSHOT` →
  SVG → PNG) against a running ClickHouse with `anchor.facts`:
  `` SELECT `id:id`, `geoPoint:*` … `` rewrites in the "as sent" view to the id
  column plus geoPoint's three value columns, the Table headers read `id:id`,
  `geoPoint:pointLat`, `geoPoint:pointLng`, and rows return;
  `` SELECT `geoPoint:lat`, `nope:x` … `` shows the Diagnostics "Column
  resolution" warnings client-side before the query runs — "section geoPoint has
  no column lat — did you mean: pointLat, pointLng, h3?" and "unknown leeway
  section nope". (Two defects an earlier drive surfaced were fixed en route:
  `PlayLauncher.Mount` never installed the resolver, and `CachingSchemaProvider`
  reported not-found on every first/cache-miss lookup — a latent `ExpandColumns`
  bug too.)
