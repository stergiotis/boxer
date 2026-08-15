---
type: explanation
audience: SQL practitioner reading leeway tables; operator provisioning a ClickHouse endpoint
status: draft
# reviewed-by: "@<handle>"   # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD  # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** This page is the entry point
> [ADR-0171](../adr/0171-leeway-sql-read-surface.md) §SD1 asked for. It names
> the pieces and routes; the pages it links to are where each is explained.
> Where this page and an ADR disagree, the ADR is the record.

# Leeway's SQL read surface

If you are about to write array arithmetic by hand against a leeway table,
this page is the one to read first. Everything it names already exists; the
problem it addresses is that none of it is findable from where a person
querying a leeway table actually stands.

That is not a guess. The
[jsonbench-on-facts trial](../trials/jsonbench-on-facts/README.md) recreated a
published benchmark using native idioms only, and its headline number moved
four times under review — never because the data model was slow, and every
time because the trial had hand-written something the repository already
provided:

| What was written by hand | What existed | Cost of not finding it |
| --- | --- | --- |
| Open-coded lane arithmetic | the query vocabulary | ~3× |
| A per-row path reconstruction | a `MATERIALIZED` column | 7× time, 8× memory |
| A gather over ragged starts | `LW_LIST_BY_TAG_EQUAL` | up to 2.4×, and it silently truncated |
| Physical column names, spelled out | column handles | repetition, no measured cost |

The third row is the one worth pausing on: the hand-written form was not
merely slower, it returned wrong answers on rows where an attribute's
cardinality was not uniform, and nothing said so.

## The layering

Five things compose, bottom to top. Each layer only assumes the one below it.

| Layer | What it knows | Names |
| --- | --- | --- |
| **Lane algebra** ([ADR-0162](../adr/0162-leeway-co-ragged-function-pack.md)) | arrays that travel together, and ragged runs — nothing about leeway | `LW_CO_*`, `LW_RAGGED_*` |
| **Aspect decoding** ([ADR-0182](../adr/0182-leeway-aspects-v2-codec-and-vocabulary.md)) | how a physical name encodes its aspects | `LW_ASPECT_*` |
| **Identity** ([ADR-0106](../adr/0106-identity-fibonacci-tags-build-tag-retirement.md)) | the bit layout of a tagged identifier | `LW_ID_*` |
| **Read-back** ([ADR-0066](../adr/0066-leeway-dql-clickhouse-readback-generator.md)) | leeway's tagged-section layout: locate an attribute by membership, extract its value | `LW_LU_*`, `LW_VALUE_BY_TAG_EQUAL`, `LW_LIST_BY_TAG_EQUAL` |
| **Authoring** ([ADR-0116](../adr/0116-play-leeway-column-handle-resolution.md), [ADR-0181](../adr/0181-leeway-dql-authoring-surface.md)) | how a person names a column or an attribute | handles, `LW_PLAIN`/`LW_TV*`, `LW_GET*` |

The layering is not new — ADR-0162's 2026-08-02 Update records it. What was
missing is a reader arriving from the consumer side finding it stated once.

## Where each name runs, and how it fails

This is the distinction that predicts what to do when something does not
work, and it does not follow package boundaries.

- **Server** — installed SQL user-defined functions. `LW_CO_*`,
  `LW_RAGGED_*`, `LW_ASPECT_*`, `LW_LU_*`, `LW_VALUE_BY_TAG_EQUAL`,
  `LW_LIST_BY_TAG_EQUAL`. Absent means *not provisioned here*; the server
  answers "unknown function", which reads like a typo and is not one.
- **Client** — expanded before the statement ships, so they work against any
  endpoint, including one carrying nothing. Column handles, the `LW_PLAIN` /
  `LW_TV*` constructors, and the `LW_GET*` extraction family. Absent means
  *this host did not register the pass* — the server never saw the call.
- **Both** — `LW_ID_*` is genuinely installable *and* client-expanded.
  Picking one answer for it makes the other wrong.

One asymmetry matters more than it looks: a client-expanded name is portable
only when **what it expands into** is. The `LW_GET*` family expands into
read-back calls, so it needs those installed even though the name itself
never travels. play's Vocabulary tab marks exactly this case.

## Provisioning, and checking

The three server families install together, under one version marker:

```go
err := lwsqlsurface.Install(ctx, conn) // pack + read-back + identity, then verify
```

`LW_SURFACE_VERSION()` reports the revision, and the invariant is that the
marker at revision N means **all three** families are installed at N — which
is why they install together and why none carries a marker of its own
([ADR-0171](../adr/0171-leeway-sql-read-surface.md) §SD2).

```sql
SELECT LW_SURFACE_VERSION()   -- unknown function ⇒ nothing is provisioned here
```

To ask what a server carries against what a build declares — including
functions it has that no build declares — use `lwsqlsurface.Reconcile`. It
**reports** by default and drops only when asked: an undeclared `LW_` name
may belong to someone else. Names this repository itself shipped and
withdrew are on an append-only retired list and are dropped by every
install, which is what keeps a rename from leaving a stale function behind
answering under the old spelling.

Practical paths today, in the order most people will want them:

```sh
boxer leeway sqlsurface install --url http://localhost:8123/   # provision and verify
boxer leeway sqlsurface status                                 # what does this server carry?
boxer leeway sqlsurface print | clickhouse-client -n           # offline, marker included
```

`status` reports and changes nothing. It separates two kinds of leftover,
because they need different actions: a **withdrawn spelling** this repository
once shipped is dropped by the next `install`, while an **undeclared** name
may be a fork's or a downstream consumer's and is left alone until someone
asks — `drop-undeclared --confirm`, a separate command so it cannot happen by
accident. `--fail-on-drift` makes `status` exit non-zero, for a deployment
check.

- **play** reconciles the configured endpoint at startup, and its
  **Vocabulary** tab ([ADR-0174](../adr/0174-play-sql-vocabulary-panel.md))
  lists all three populations marked against that endpoint — including the
  revision skew line and, for a client-expanded name, whether what it
  expands into is present.
- `jsonbench sqlsurface --url …` does the same install for that tool's own
  runs.
- `readback.HelperUDFsSQL()` still returns the read-back script for a
  consumer that wants that family alone, and `leeway id udf` the identity
  statements. Neither stamps the version marker — prefer
  `leeway sqlsurface print`, which does.

## Reading one attribute, end to end

A tagged section stores its attributes in parallel arrays, flattened across
memberships. Reading one attribute means locating it by membership, then
extracting at that position. You do not have to write that:

```sql
SELECT LW_GET('symbol', 'ticker') FROM events
```

expands, client-side, into the read-back call over the section's physical
lanes — the value column, the membership identity column and the
cardinality column, none of which you type. Its siblings are
`LW_GET_NULL` (`NULL` when the membership is absent, rather than the type
default) and `LW_GET_LIST` (for a section whose values are arrays or sets).

Two arguments cover the common case. A section with several value columns,
or several membership channels, is genuinely ambiguous, and the call says so
and lists what it has:

```sql
SELECT LW_GET('geoPoint', 'here', 'col:lat') FROM events
```

`LW_GET` is singular — it locates *the* attribute carrying a membership.
Where a membership is carried by several, the question is plural and the
answer is a **selector**: `LW_SEL` returns the positions the membership
occupies and `LW_SEL_ATTRS` the attribute indices, co-indexed with each
other, so any lane projects through them with the pack's `LW_CO_GATHER`
without an `ARRAY JOIN` changing the row grain.

The two questions divide on one rule: **a parameter is required exactly when
the answer must be unique.** On a mixed channel the membership is shared by
design — the high-cardinality half is what tells its attributes apart — so
`LW_GET` there requires `param:` and refuses without it, while `LW_SEL`
treats the same token as an optional narrowing.

## Naming a membership instead of its id

Memberships come in two spellings. A *verbatim* channel carries the name
itself, so nothing extra is needed. A *ref* channel carries a uint64 from a
registered vocabulary — the numbers that used to ride SQL text as literals
like `6917529027641081861`.

Both directions are reachable ([ADR-0171](../adr/0171-leeway-sql-read-surface.md)
§SD4). Reading, when a result column came back as a number:

```sql
SELECT name FROM keelson('memberships') WHERE id = 6917529027641081861
```

and writing, where `LW_GET` takes the name and resolves it before the
statement ships, so the SQL still carries a constant:

```sql
SELECT LW_GET('metric', 'cpuLoad') FROM events
```

Two things to know. The table publishes the **folded** spelling — a
membership declared as `naturalKey` lists as `natural-key`, because that is
what the registry keeps — so a join predicate must use that form, though
`LW_GET` accepts either. And a `virtual` row is a grouping node that never
appears on a lane: matching against one returns nothing, which is a wrong
question rather than missing data.

Naming only works where the host bound a registry. Without one, `LW_GET`
still takes the id and says so — quoted or as a plain number,
`LW_GET('metric', 6917529027641081861)`, which is the same call. The
unquoted spelling is the id form only: a verbatim channel carries names and
refuses it, naming the fix.

For filtering, prefer the cheap necessary condition — `has()` over a
membership lane prunes granules through a skip index, which `indexOf` and
`countEqual` never do. The
[three DQL contracts](./leeway-dql-contracts.md) is the page that states when
a cheap guard is sound and when it is not; do not guess at that from this
one.

## Known gaps

Stated here rather than discovered later:

- **`MATERIALIZED` projections are not generated** from a leeway schema —
  §SD3, deferred on the cost of a generator that must track physical naming
  across every shape. The trial's largest single lever (3.8–13.8×) is still
  hand-written per path, with physical names inlined and nothing checking
  they still match.
- **The exploded companion table** (§SD5) is deferred too, and may not be
  its own decision: it is the same one-row-per-attribute shape
  [ADR-0025](../adr/0025-pushout-forget-architecture.md) realises as its
  personal-data vault, and settling it twice would mean a migration between
  them.
- **Parametrized membership channels** are out of scope for the extraction
  sugar: a parametrized membership is one opaque blob carrying identity and
  parameters together, with no shared codec saying how it is laid out, so
  there is no literal a caller could match. The **mixed** channels are
  readable — `param:` names the high-cardinality half — but the read-back
  *generator* still does not model them, because its `Plan` front-end maps
  only the four simple channels.
- **Statement wrapping** — `INSERT … SELECT` flows through the pipeline
  (ADR-0181 §SD8, 2026-08-15): the source expands like any SELECT, mints
  adopt the target's names, and hosts gate execution behind
  `BOXER_PLAY_ALLOW_WRITES`. `CREATE TABLE … AS SELECT` stays out
  deliberately — a CTAS-minted table can carry neither codecs nor skip
  indexes, so the flow is create-with-`ddl compose`, fill-with-INSERT.

## Reading list

- [reading-and-authoring how-to](../howto/leeway-sql-reading-and-authoring.md)
  — the task order: install, read an attribute, name a membership, filter,
  mint new columns.
- [three DQL contracts](./leeway-dql-contracts.md) — when a guard is sound,
  what absence means, and when a `SELECT` still produces a leeway table.
- [leeway-column-names](./leeway-column-names.md) — the anatomy of a
  physical name, and handles.
- [array-idioms how-to](../howto/leeway-clickhouse-array-idioms.md) — the
  executable kernel, for when you do want to write it by hand.
- [query algebra](./leeway-query-algebra.md) — the model behind the
  vocabulary.
- [ADR-0171](../adr/0171-leeway-sql-read-surface.md) — the surface as one
  named thing: the version handshake, and the gaps above.
- [ADR-0181](../adr/0181-leeway-dql-authoring-surface.md) — constructors,
  extraction sugar, shape checking, skip-index options.
- [jsonbench-on-facts trial](../trials/jsonbench-on-facts/README.md) — the
  measurements this page opens with.
