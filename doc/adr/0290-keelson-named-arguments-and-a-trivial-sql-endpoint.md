---
type: adr
status: proposed
date: 2026-10-07
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0290: keelson tables with named arguments, and a trivial SQL endpoint for hosts without ClickHouse

## Context

A keelson table ([ADR-0094](./0094-keelson-introspection-tables.md)) is
computed by a Go provider and read with SQL: `keelson('name')` is expanded to a
temporary table that the introspection engine snapshots and hands to
clickhouse-local. Two limits follow from that shape, and each costs something
on its own.

**A provider cannot be asked for less than everything.** `keelson()` takes one
argument, the table name, so a provider snapshots all of its rows and the
caller narrows them in SQL afterwards. For most tables that is cheap. For some
the question has a natural input that the provider could use to compute less:
the functions of one package in `coverage_funcs`, the symbols of one package in
`go_symbols`, the text of one ADR in `adrcontent`, the runtime events since a
moment in `runtime_events`. A table whose content is *defined* by an input — a
window of a precomputed grid, a profile by its id — cannot be expressed at all.

**Reading a keelson table needs ClickHouse.** Even `SELECT * FROM
keelson('env')` goes through clickhouse-local. A host without it gets no SQL
access to its own data: a browser tab
([ADR-0263](./0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md),
[ADR-0278](./0278-tab-mode-for-downstream-apps.md)) served as static files, a
small headless binary, a test that wants to read a provider through the same
surface a user would. Much of what such hosts ask is a whole table, or a whole
table chosen by its inputs; that needs no engine.

ClickHouse's own conventions already cover both: query parameters as
`{name:Type}` placeholders bound by `param_<name>` over HTTP
([ADR-0133](./0133-chhttp-server-dialect-and-param-binding.md) §SD2), and named
table-function arguments in the style of `remote(…, table = '…')`. Following
them keeps one statement text valid in every host.

## Design space (QOC)

**Q — How does a host without ClickHouse read keelson data, and how does a
computed table take inputs?**

| Option | C1 one statement text in every host | C2 no ClickHouse semantics re-implemented | C3 callers unchanged | C4 cost |
| --- | --- | --- | --- | --- |
| O1 a Go evaluator for a growing SQL subset (filters, aggregates, subqueries) | yes | **no** — each consumer widens it | yes | weeks, and parity forever |
| O2 per-consumer Go paths that bypass SQL | **no** | yes | **no** — a second data path per consumer | small each, repeated |
| O3 named arguments on `keelson()`, and a trivial evaluator for `SELECT *` over keelson calls | yes | yes | yes | moderate, once |
| O4 ClickHouse compiled for the browser | yes | yes | yes | none exists; DuckDB-wasm is another dialect and a third-party JavaScript engine (P1, P2) |

O3 is taken.

## Decision

We will let a keelson table take **named arguments** whose values are literals or
ClickHouse query-parameter slots, and answer `[WITH a AS (SELECT * FROM
keelson(…))] SELECT * FROM keelson(…)` without ClickHouse with a **trivial
evaluator**, reachable in a tab behind an HTTP round-tripper that speaks the
subset of ClickHouse's HTTP interface play uses. The same statement text runs
natively through the introspection engine.

### Subsidiary design decisions

- **SD1 — Named arguments on `keelson()`.** `keelson('name', key = value, …)`:
  the first argument is the table name as today; every further argument is
  `identifier = value`, where value is a literal, a `{slot:Type}`
  placeholder or a WITH constant (SD3). A provider declares its arguments (name, ClickHouse type,
  required or defaulted); an undeclared, missing or ill-typed argument fails
  naming it, before anything is snapshotted. `keelson('name')` with no
  arguments is unchanged. nanopass already parses the form, slots included.

- **SD2 — Arguments resolve where the parameters are.** A placeholder takes its
  value from the request's `param_<slot>`, as ClickHouse would bind it, and the
  provider is called with typed values. Natively, `keelsonsql` expands each
  distinct call — name plus resolved arguments — to its own TEMPORARY table, so
  two calls with different arguments in one statement are two tables; the
  introspection engine snapshots it with the arguments. A provider without
  arguments is snapshotted as before. A call with arguments is snapshotted
  whole: column pruning keys on table names, and the call's table is not one.
  The paths that rewrite without the query's parameters — the `keelson.query`
  gate ([ADR-0253](./0253-introspection-table-reads-as-a-bus-capability.md)) and the
  HTTP source's `url()` rewrite for an external ClickHouse — refuse a call with
  arguments, naming the engine as the path that resolves them (Q4).

- **SD3 — The trivial evaluator, by shape.** It accepts, parsed by nanopass:
  optional `SET param_<name>` preludes; an optional `WITH` list of constants
  (`<constant> AS c`) and queries (`q AS (SELECT <list> FROM <source>)`, each
  able to name the queries before it); then `SELECT <list>`, with or without
  `FROM <source>`; then optional `LIMIT n [OFFSET m]` and `FORMAT f`. A
  source is a keelson call, `values()` over constants, or a WITH query; a
  list is `*` and constants, each with an optional alias; a constant is a
  literal, `true`, `false`, a `{slot:Type}` parameter or a WITH constant —
  never an operator or a function, which a server evaluates. Anything else
  is refused with an error that says the statement needs
  ClickHouse, in the `chhttp` error envelope a client already reads for any
  server error — without an engine code, since inventing one would be worse
  than leaving it out. It evaluates nothing: it resolves arguments (SD2),
  calls the provider and encodes the record.

  What it does answer, it answers as clickhouse-local does, and what it
  cannot answer that way it refuses rather than approximates:
  - **Formats** are a closed set: ArrowStream, TabSeparated and CSV (each
    also `WithNames`), and JSONEachRow, matched as ClickHouse matches a
    format name. The text formats render a value as clickhouse-local renders
    the same Arrow column — integers, floats, Bool, strings and binaries, and
    arrays and dictionaries of those — and refuse a column of any other type
    (a timestamp, a decimal, a tuple), whose text depends on settings this
    evaluator does not model. ArrowStream carries every type, with the
    provider's own Arrow types where ClickHouse would map them (a String
    comes back LargeBinary): equal in values, not in bytes. Other formats
    are refused; one is added when a client needs it and a parity test can
    hold it to ClickHouse's bytes.
  - **Settings** — a `SET`, a `SETTINGS` clause, a query-string key — are
    accepted only when they change neither the rows nor their encoding
    (`log_comment`, `readonly`, the query-cache flags, resource limits), and
    every other one is refused: ignoring `limit`, `max_result_rows` or an
    `output_format_*` setting would hand back other rows or other bytes.
  - **Literals and parameters** decode as ClickHouse decodes them: a string
    literal's backslash escapes, and a parameter's value read in the escaped
    text format. This is shared with SD2, so the native path reads them the
    same way.
  - **Constants are typed as ClickHouse types them** — an integer as the
    smallest UInt holding it, or Int when negative, a number with a point or
    an exponent as Float64, a parameter as its declared type — and an
    unaliased one takes ClickHouse's default column name. `values()` converts
    its rows to a declared structure, or without one finds the least type
    every row fits, as ClickHouse does; what ClickHouse rejects there is an
    error of the statement. A shape whose ClickHouse answer is not modelled
    is refused: NULL as a column, an unaliased parameter (named `_CAST(…)`),
    a name given twice, a WITH constant named like a column, a `values()`
    type outside the scalars above.
  - **A WITH constant is a keelson() argument value** as a literal is, on
    both paths (SD2), so `WITH 4 AS k SELECT * FROM keelson('t', n = k)`
    runs the same natively.

- **SD4 — In a tab, the endpoint is a round-tripper, not a client change.** In
  a tab, Go's default HTTP transport is already the host's fetch. The endpoint
  is an `http.RoundTripper` in front of it that answers one dedicated origin
  in-process and passes every other request through: `query` in the body or
  URL, `param_*` pairs, `default_format` or a `FORMAT` clause, errors in the
  `chhttp` envelope. The evaluator is handed the query string's settings and
  judges them as SD3 does. A handler that panics fails its request, as a
  dropped connection would, instead of the tab. A tab binary that wants it
  sets `CLICKHOUSE_URL` to that origin; play's client, executor and panes do
  not know the difference.

### Deferred and open

- **Q1 — What else of play runs on the trivial endpoint.** Panes and helpers
  that send their own SQL — completion, docs, the schema reads — are refused by
  SD3. Which of them a tab should hide, and which merely show the refusal, is
  settled by the first tab that uses the endpoint.
- **Q2 — FORMATs.** Settled in SD3: a closed set, each held to
  clickhouse-local's output by a parity test.
- **Q3 — Outside a tab.** SD4 is the tab's transport. Whether a headless binary
  or a test reaches the evaluator through the same round-tripper or by calling
  it directly is left to the first such user.
- **Q4 — Arguments over `url()`.** An external ClickHouse reads a keelson
  table through `url('<base>/table/<name>', …)`, and it does not substitute
  query parameters inside a string literal, so a placeholder cannot travel in
  that URL as written. Whether the HTTP source takes arguments from the URL's
  query string, with the statement building it from parameters, is left until
  an external reader needs it; the `keelson.query` gate follows whichever
  answer the engine path gives.
- Progress headers (ADR-0115 plane A) are not emitted; a reply is in memory and
  immediate.

### Milestones

- **M1 — Named arguments, natively.** SD1 and SD2: named arguments in
  `keelsonsql` and the introspection engine, a provider declaring arguments,
  tests over a toy provider.
- **M2 — The trivial endpoint.** SD3 and SD4: the trivial evaluator and the
  round-tripper, with a tab that runs `SELECT * FROM keelson('env')` in play's
  Table pane.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `keelson()` table-function macro | extended: named arguments with literal or placeholder values | `keelsonsql` expansion; the introspection engine's snapshot |
| `introspect.Provider` | extended: an optional argument declaration and an argument-taking snapshot | providers without arguments unchanged |
| Trivial evaluator | added: `SELECT *` over keelson calls, refusing every other shape | — |
| Tab HTTP transport | added: a round-tripper answering one origin in-process | `CLICKHOUSE_URL` of a tab binary that uses it |

## Alternatives

- **O1 — a growing SQL subset.** Rejected on C2: it is a query engine with
  ClickHouse's semantics, widened by every consumer; the first consumer alone,
  the vector field pane, sends statements with ~25 functions and aggregates
  ([ADR-0291](./0291-a-precomputed-vector-field-family-of-keelson-tables.md)).
- **O2 — per-consumer Go paths.** Rejected on C1 and C3: each consumer would
  show data that no SQL produced, through a second path of its own.
- **Positional arguments** (`keelson('x', 3, 2, …)`). Rejected: a statement's
  meaning would depend on argument order, and named arguments are already the
  form ClickHouse's own table functions accept.

## Consequences

### Positive

- Providers can compute only what a statement asks for, and tables defined by an
  input become expressible.
- Any host without ClickHouse — a static tab, a headless binary, a test — reads
  keelson tables through the same SQL and HTTP surface; play runs in a static
  tab with no change to play.
- Named arguments work natively too, so a statement written for a tab runs
  unchanged on the desktop.

### Negative

- A tab endpoint that refuses most SQL makes play's other panes show refusals;
  a tab has to choose what to show (Q1).

### Neutral

- The trivial evaluator is deliberately not a stepping stone to an engine; a
  statement it refuses needs ClickHouse.

## Migration — Tier 1

None. `keelson('x')` without arguments and every host that reaches ClickHouse
behave as before.

## Verification plan — Tier 1

- M1: unit tests of argument parsing, typing and refusal; an engine test where
  one statement reads two calls of one table with different arguments.
- M2: a tab test that runs play's Table pane on `keelson('env')`, and a refusal
  test for a statement outside the shape.

## Status

Proposed 2026-10-07.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.

## Updates

### 2026-10-07 — M1 built

Named arguments work natively: `introspect` gains `ArgsProviderI`
(`Args()` declaring name, type, required or default; `SnapshotArgs`), with
`ResolveArgs` typing values and `SnapshotCall` as the one entry for a call
with or without arguments; `keelsonsql.ExpandWithArgs` resolves literals and
placeholders against the run's parameters and renames each distinct call to a
TEMPORARY table named after the provider and a digest of the resolved values;
the introspection engine snapshots those calls under their names. Alias
rewriting keeps a call's arguments. Verified with unit tests of typing,
refusal and naming, and an engine test through clickhouse-local in which one
statement reads one table twice with different arguments, one bound from a
query parameter.

### 2026-10-07 — M2 built

`trivialsql` answers the shape of SD3 — CTEs only as aliases of keelson calls,
`SET param_*` preludes bound as ClickHouse binds them, `LIMIT n`, `LIMIT n
OFFSET m` and `LIMIT m, n` — in the formats SD3 lists, and refuses everything
else with `ErrNeedsClickHouse`; a bad call (an unknown table, a missing
argument) is reported as that, not as a refusal. A parity test runs the same
statements through it and through clickhouse-local, over a table holding a
column of each type the text formats render and the values whose spelling is
easiest to get wrong, and over constant rows, constants beside `*`, WITH
constants and `values()` tables, and finds the text bodies byte-equal and the
ArrowStream records equal in values. `introspecthttp.InProcess` serves the existing introspection
handler to a client without a socket, and `tabhost.Services.KeelsonSQL` installs
it in a tab at `http://keelson.invalid/query`, a name that cannot resolve.
`imzero2tab` turns it on over the static tables. In a tab with no ClickHouse,
play pointed there runs `SELECT * FROM keelson('env') LIMIT 12` into its Table
and Detail panes; what it refused at start was its SQL-surface install, the
first instance of Q1.

## References

- [ADR-0094](./0094-keelson-introspection-tables.md) — keelson introspection tables and the `keelson()` macro.
- [ADR-0133](./0133-chhttp-server-dialect-and-param-binding.md) — ClickHouse HTTP dialect; `{name:Type}` parameters bound by bare name.
- [ADR-0263](./0263-imzero2-browser-both-modules-in-one-worker-mesh-to-the-painter.md) — the browser tab.
- [ADR-0278](./0278-tab-mode-for-downstream-apps.md) — tab mode and the Pages demo.
- [ADR-0291](./0291-a-precomputed-vector-field-family-of-keelson-tables.md) — the first consumer: a vector field family of keelson tables.
