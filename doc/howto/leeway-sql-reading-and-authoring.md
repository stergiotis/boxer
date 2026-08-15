---
type: how-to
audience: SQL practitioner reading or producing leeway tables
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to read and author leeway tables in SQL

The task-oriented walk through leeway's SQL surface: install it, read an
attribute, name a membership, filter soundly, and mint new leeway columns —
without typing a physical column name at any step. What each piece *is* and
why it is shaped that way lives in
[leeway-sql-read-surface](../explanation/leeway-sql-read-surface.md); this
page is the order to do things in, and what the failures look like.

One distinction carries the whole page. The **server-side** names (`LW_CO_*`,
`LW_RAGGED_*`, `LW_LU_*`, `LW_ASPECT_*`, `LW_VALUE_BY_TAG_EQUAL`,
`LW_LIST_BY_TAG_EQUAL`, `LW_ID_*`) are SQL user-defined functions and work
from any client once installed. The **client-side** names — column handles,
`LW_GET*`, `LW_PLAIN`/`LW_TV*` — are rewritten before the statement ships and
exist only where a host runs the nanopass pipeline: `play`, or any Go host
that registers the standard pre-execute set (ADR-0108 §SD7). In a bare
`clickhouse-client` session the client-side names are unknown functions;
that is not a provisioning problem, it is the wrong side of the split.

## 1. Install the surface

The three server families install together under one version marker, from the
CLI or as a plain DDL script through `clickhouse-client`:

```sh
boxer leeway sqlsurface install --url http://localhost:8123/
# or, offline / air-gapped:
boxer leeway sqlsurface print | clickhouse-client -n
```

Prefer `print` over the older per-family scripts (`readback.HelperUDFsSQL()`,
`leeway id udf`) — only `print` stamps the marker. Check any endpoint:

```sql
SELECT LW_SURFACE_VERSION()   -- unknown function ⇒ nothing is provisioned here
```

`boxer leeway sqlsurface status` reports what a server carries against what
this build declares (`--fail-on-drift` for a deployment check), and
`drop-undeclared --confirm` is the separate, deliberate cleanup command.
`play` reconciles its endpoint at startup and shows the per-function verdicts
in the **Vocabulary** tab.

## 2. Name columns with handles, not physical names

A leeway column's physical name encodes its whole schema
([leeway-column-names](../explanation/leeway-column-names.md)); nobody should
type one. A backtick-quoted handle — `` `section:column` `` for tagged
sections, `` `id:id` `` and friends for the backbone — resolves to the
physical name before the statement ships (ADR-0116). Support lanes have
handles too (`` `symbol:lr` ``, `` `symbol:lrcard` ``).

```sql
SELECT `id:naturalKey`, `symbol:value`[1] AS event_type
FROM anchor.facts
```

Two rules keep resolution predictable. Handles rewrite per `SELECT`, against
the tables that `SELECT` reads from the catalog — a CTE or subquery has no
catalog schema, so put the handle read where the table is in scope and export
aliases outward. And an *unaliased* handle keeps the physical name as its
result column (the output stays leeway-shaped); an alias makes it a plain
column.

## 3. Read one attribute (`LW_GET`)

A tagged section stores attributes in parallel arrays, located by membership
tag. `LW_GET('section', 'membership')` expands into the read-back call over
the section's value, membership and cardinality lanes — the same expression
the generator emits, handling non-uniform cardinality correctly (the
hand-written gather the
[jsonbench trial](../trials/jsonbench-on-facts/README.md) started with was
~2.4× slower *and wrong* on exactly those rows).

```sql
SELECT LW_GET('symbol', 'ticker') AS ticker
FROM events
WHERE LW_GET('symbol', 'ticker') != ''
```

An extraction is an ordinary expression — projection, `WHERE`, `GROUP BY`
alike, as above. The variants encode absence three ways:

- `LW_GET` — the type default when the tag is absent (`''`, `0`).
- `LW_GET_NULL` — `NULL` when absent, telling absent from
  present-with-the-default.
- `LW_GET_LIST` — for a section whose values are arrays or sets; `[]` when
  absent.

A section with several value columns or several membership channels is
genuinely ambiguous; the call errors listing the candidates, and a token picks
one:

```sql
SELECT LW_GET('geoPoint', 'here', 'chan:low-card-verbatim', 'col:lat') FROM events
```

The channel vocabulary is `low-card-ref`, `low-card-verbatim`,
`high-card-ref`, `high-card-verbatim`. Mixed and parametrized channels are out
of scope for extraction (ADR-0181 §SD3), as they are for the read-back
generator.

## 4. Name a membership instead of its id

A *verbatim* channel carries names; nothing extra is needed. A *ref* channel
carries a `uint64` from the process's membership registry, and both directions
are covered (ADR-0171 §SD4):

- **Writing:** `LW_GET('metric', 'cpuLoad')` on a ref channel resolves the
  name at expansion time, so the shipped SQL still carries a constant. This
  needs the host to bind a registry (play does); without one the call says so
  and a decimal id always works. The id may be written as a plain number —
  `LW_GET('metric', 6917529027641081861)` — which is the same call as the
  quoted form and needs no registry. A verbatim channel takes only the
  quoted form, because a bare number can only be an id.
- **Reading:** join a numeric result column against the registry table —

  ```sql
  SELECT name FROM keelson('memberships') WHERE id = {id:UInt64}
  ```

The table publishes the **folded** spelling (`naturalKey` lists as
`natural-key`), so join predicates use that form — `LW_GET` accepts either.
`virtual` rows are grouping nodes that never appear on a lane; matching
against one returns nothing by design.

## 5. Filter soundly

Prefer the cheap necessary condition: `has()` over a membership or value lane
prunes granules through a skip index; `indexOf(...) != 0` and `countEqual`
never do. When a guard is sound, what absence means, and when a `SELECT`
still produces a leeway table are the three contracts of
[leeway-dql-contracts](../explanation/leeway-dql-contracts.md) — read that
before trusting a guard, not this page.

```sql
SELECT LW_GET('symbol', 'ticker') FROM events
WHERE has(`symbol:lv`, 'ticker')   -- sargable guard on the membership lane
```

## 6. Regroup lanes (`LW_RAGGED_*`, `LW_CO_*`)

For work below the attribute grain — nesting a flat stream by its counts,
per-run reductions, co-lane lookups — use the installed pack rather than
hand-written array arithmetic (~3× and silent truncation were the measured
costs of open-coding it):

```sql
SELECT `id:naturalKey`, event, tags
FROM anchor.facts
ARRAY JOIN
  `symbol:value` AS event,
  LW_RAGGED_NEST(`symbol:lr`, `symbol:lrcard`) AS tags
```

Prefer the fused forms (`LW_RAGGED_REDUCE`, `LW_RAGGED_EXISTS`,
`LW_RAGGED_COUNT`) over nest-then-operate; nesting copies the stream
(ADR-0162 §SD4). The executable kernel behind the pack, for when you do want
to write it by hand, is the
[array-idioms how-to](./leeway-clickhouse-array-idioms.md).

## 7. Decode a column name you do not recognise

The aspects a physical name encodes are readable in SQL (ADR-0182): `SEG_*`
takes the full name, `NAMES_*` renders a segment as kebab-cased aspect names,
`HAS_*` asks for one:

```sql
SELECT
  name,
  LW_ASPECT_NAMES_SEM(LW_ASPECT_SEG_SEM(name)) AS value_semantics,
  LW_ASPECT_NAMES_ENC(LW_ASPECT_SEG_ENC(name)) AS encoding_hints
FROM system.columns
WHERE database = 'anchor' AND table = 'facts'
```

## 8. Author new leeway columns

A computed column with an ordinary alias breaks leeway closure — the result
stops being a leeway table. The constructors re-admit it: each wraps an
expression and mints the physical name client-side, expanding into
`<expr> AS "<physical name>"`, so any endpoint runs the output.

```sql
SELECT
  `id:id`,
  `id:naturalKey`,
  LW_PLAIN(length(`symbol:value`), 'event-count', 'u64', 'item:oq')
FROM anchor.facts
```

`LW_PLAIN(expr, 'name', 'type', tokens…)` mints a plain column; the
`item:` token is mandatory (the item kind is not defaulted — `item:oq` is the
common opaque case). `LW_TV(expr, 'section', 'column', 'type', tokens…)`
mints a tagged-value lane, and the section only stays *readable* if its
membership lanes ride along:

```sql
SELECT
  LW_TV(arrayMap(x -> upper(x), `symbol:value`), 'symbolUpper', 'value', 's'),
  LW_TV_MEMB(`symbol:lr`, 'symbolUpper', 'low-card-ref'),
  LW_TV_SUPPORT(`symbol:lrcard`, 'symbolUpper', 'lrcard')
FROM anchor.facts
```

Rules the pass enforces, loudly and before anything ships:

- A constructor call is a **projection item and nothing else** — not aliased
  (the minted name *is* the alias), not nested in another expression, not in
  `WHERE`/`GROUP BY`. Subquery projections are fine.
- Aspect tokens are vocabulary-prefixed (`enc:`, `sem:`, `use:`, `item:`),
  and `use:` aspects are section-level: every minted lane of a section must
  agree, and membership/support mints carry none — a `use:`-bearing section
  cannot be fully minted per-column today (ADR-0181 §SD8).
- Types are canonical-type tokens (`u64`, `s`, `u64h`, …), names are stylable
  names minted in the folded spelling (`symbolUpper` lands as
  `symbol-upper`, the same fold the membership registry applies); an unknown
  token errors with the candidates.

`LwShapeCheck` (opt-in pass) verifies a statement's output *is* a leeway
table — names parse plus the vertical-subset rule — which catches the quiet
closure breaks, like `SELECT "…" AS x` renaming a physical column.

Two authoring paths deliberately stay outside the pipeline: skip-index DDL is
table-level `TableOptions`, composed by `boxer leeway ddl compose` (ADR-0181
§SD4), and `INSERT … SELECT` / `CREATE TABLE … AS SELECT` wrappers are written
by hand around an expanded `SELECT` (§SD8).

## Reading list

- [leeway-sql-read-surface](../explanation/leeway-sql-read-surface.md) — the
  map: five layers, where each name runs, known gaps.
- [leeway-dql-contracts](../explanation/leeway-dql-contracts.md) — guard
  soundness, absence semantics, closure.
- [leeway-column-names](../explanation/leeway-column-names.md) — what a
  physical name encodes, and handles.
- [array-idioms how-to](./leeway-clickhouse-array-idioms.md) — the executable
  kernel under the pack.
- [ADR-0181](../adr/0181-leeway-dql-authoring-surface.md) — constructors,
  extraction, shape check, skip indexes.
- [ADR-0171](../adr/0171-leeway-sql-read-surface.md) — the surface as one
  named thing: version handshake, membership naming, deferrals.
