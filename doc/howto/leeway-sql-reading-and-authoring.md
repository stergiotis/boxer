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
`high-card-ref`, `high-card-verbatim`, and the two **mixed** channels,
`low-card-ref-high-card-params` and `low-card-verbatim-high-card-params`.
Parametrized channels are still out of scope: a parametrized membership is one
opaque blob carrying identity and parameters together, with no shared codec
saying how it is laid out, so there is no literal you could pass.

A mixed channel spends a second lane on the high-cardinality half of the tag —
the array index the canonical JSON mapping elides from a path, so that
`/tags/0` and `/tags/1` share the tag `/tags/_` and differ only in the
parameter. Name it with `param:`:

```sql
SELECT LW_GET('string', '/tags/_', 'param:0001') FROM docs
```

**On a mixed channel `param:` is required for a single-attribute read**, and
that is not a restriction so much as the shape of the question. The tag alone
names a *set* there — that is what the parameter lane is for — so a singular
read without one would hand back an arbitrary member of it. Reading every
attribute that carries the tag is the next section.

The parameter is the blob as stored. For the canonical form, that is
fixed-width lowercase hex, four digits per index, `.`-separated in path order:
`/tags/1` is `'0001'`, `/a/12/b/3` is `'000c.0003'`, and a path with no elided
index carries `''`.

## 4. Read every attribute carrying a tag (`LW_SEL`)

`LW_GET` locates *one* attribute. When a tag is carried by several — the
normal case on a mixed channel, and common enough elsewhere — the question is
plural, and the answer is a **selector**: `LW_SEL` returns the positions the
tag occupies, and you project any lane you like through them.

```sql
SELECT
  LW_CO_GATHER(`string:value`, LW_SEL_ATTRS('string', '/tags/_')) AS values,
  LW_CO_GATHER(`string:mvhp`,  LW_SEL('string', '/tags/_'))       AS indices
FROM docs
```

This is "argwhere + gather": select positions once, then every further lane —
including a co-section's — projects through the same selector. It needs no
`ARRAY JOIN`, so the row grain does not change and two tags can be read side
by side in one statement.

The pair is the point. `LW_SEL` indexes the **membership** lanes (the tag lane
and, on a mixed channel, the parameter lane); `LW_SEL_ATTRS` indexes the
**attribute** lanes (the values). They are co-indexed with each other, so both
pass to one lambda and stay aligned:

```sql
SELECT arrayMap((p, a) -> (`string:mvhp`[p], `string:value`[a]),
                LW_SEL('string', '/tags/_'), LW_SEL_ATTRS('string', '/tags/_'))
FROM docs
```

`param:` is **optional** here, and narrows the selection to the pair when
given — the mirror of the rule in §3, and the same rule underneath: the
parameter is required exactly when the answer must be unique. An absent tag
selects nothing, and every consumer of an empty selector — `arrayMap`,
`arrayFilter`, `LW_CO_GATHER`, `length` — answers empty without a special
case.

Two sharp edges. A selector returns indices, so `col:` is rejected rather than
ignored — gather the column you want through the selector instead. And the
selector itself does not prune: a multi-lane `arrayFilter` is opaque to index
analysis, so keep a `has()` guard in `WHERE` as the pruner, exactly as §6
says.

## 5. Name a membership instead of its id

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

## 6. Filter soundly

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

## 7. Regroup lanes (`LW_RAGGED_*`, `LW_CO_*`)

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

## 8. Decode a column name you do not recognise

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

## 9. Author new leeway columns

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

## 9. Write it back (`INSERT … SELECT`)

The `INSERT INTO <table> [(columns)] SELECT …` wrapper flows through the
pipeline like any statement (ADR-0181 §SD8): handles, `LW_GET` and the
constructors expand inside the SELECT source, the target is a scope sink no
handle binds against, and — with the destination known — constructor mints
**adopt the target's own physical names**, spelling and aspect hints
included, instead of composing fresh ones. `LwShapeCheckTarget` is the
opt-in proof that the SELECT's output matches the destination's columns
(and, when a column list is written, position by position).

```sql
INSERT INTO anchor.silver
SELECT `id:id`, `id:naturalKey`,
       LW_TV(arrayMap(x -> upper(x), `symbol:value`), 'symbol', 'value', 's'),
       LW_TV_MEMB(`symbol:lr`, 'symbol', 'low-card-ref'),
       LW_TV_SUPPORT(`symbol:lrcard`, 'symbol', 'lrcard')
FROM anchor.facts
```

Hosts expand everywhere but gate execution: play (and every play-engined
host, sqlapplet included) refuses to Run a write until
`BOXER_PLAY_ALLOW_WRITES=1` is set, and ships an INSERT without the
`FORMAT` clause a read gets. Create the destination with
`boxer leeway ddl compose` first — an in-pipeline CTAS is deliberately
absent, because a table it minted could carry neither codecs nor the
`TableOptions` skip indexes of §SD4; create-with-compose, fill-with-INSERT
is the sanctioned flow, and `VALUES` sources stay outside the grammar.

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
