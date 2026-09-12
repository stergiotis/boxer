---
type: adr
status: accepted
date: 2026-09-12
reviewed-by: "p@stergiotis"
reviewed-date: 2026-09-12
---

# ADR-0226: `leeway.*` — schema-decode views over `system.tables` and `system.columns`

## Context

A leeway physical column name carries the whole authored structure — prefix,
section, column, role, canonical type, the three aspect segments, row config,
co-section and streaming group — and that self-description is a premise, not a
convenience ([ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md)
rejected moving aspects into column comments precisely because names survive
in result headers, logs and CSV where metadata does not). The consequence is
that every question about a leeway table's shape is answerable from
`system.columns` alone.

Answerable, but not answered. Three things sit near this question and none of
them is it:

- **`LW_ASPECT_*`** ([ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md)
  §SD4) decodes the three aspect segments in SQL, with bodies generated from
  the live Go enum tables. Its stated purpose is to make "every aspect a
  queryable predicate over `system.columns`". It reaches three of the eleven
  segments; the other eight — section, column, role, canonical type, row
  config, co-section group, streaming group — have no SQL reader, so a query
  that wants them splits the name by hand.
- **`boxer.tables_leeway`** ([ADR-0170](./0170-data-catalog-competence.md)) is
  the authoritative classification: a Go batch pass that restores a `TableDesc`
  through `DiscoverTableFromColumnNames`, normalizes it, and relates tables
  pairwise. It is table-grained, snapshot-shaped, and stale between runs.
  Nothing in it reaches a single column.
- **`systemTableColumns`** (the leeway-modelled schema catalog in
  `leeway/common`) is column-grained and push-side: it is populated from an
  `IntermediateTableRepresentation` at describe time, so it describes tables
  this build compiled, not tables a server carries.

So the column-grained question about a *live* server — which columns of this
table carry `delta-encoding`, what does each section's value column cost
compressed, which streaming group does this lane belong to — is answered today
by a person splitting strings in an ad-hoc query, and the eleven-segment layout
is re-derived from memory each time.

[ADR-0170](./0170-data-catalog-competence.md) §Q1 considered SQL over
`system.columns` and killed it: *"requires re-implementing the column-name
grammar and the normalization rules in SQL — a second implementation that
drifts."* That kill-reason is still right about half of what it covers, and
`LW_ASPECT_*` has since demonstrated where the seam is:

- **Restoration and normalization** — canonical-type parsing, membership
  coherence, `TableOperations.Relate` — genuinely cannot be re-expressed in
  SQL without a second implementation. The kill-reason holds, and this ADR does
  not touch it.
- **Grammar decode** — split on the separator, read field *k*, map an enum code
  to its name — is a table lookup, and a table lookup *generated from the Go
  side* is not a second implementation. `LW_ASPECT_*` already ships on exactly
  this basis.

This ADR does not supersede ADR-0170: its chosen option, storage form, grain
and authority all stand, and the column-grained question asked here is one it
does not address. The split above is recorded as a dated entry under that ADR's
`## Updates`, which is where a reader arriving from the catalog side will find
it.

## Design space (QOC)

**Question.** How is a live server's leeway schema made queryable at column
grain, without re-implementing the restoration the Go path owns?

**Options.**

- **O1** — Views over `system.tables` / `system.columns`, bodies generated in
  Go from the naming convention's position data and the enum tables.
- **O2** — More `LW_*` scalar functions (one per remaining segment), leaving
  the relation for each caller to assemble.
- **O3** — Extend the ADR-0170 cataloger to write a column-grained snapshot
  table beside its four.
- **O4** — Populate `systemTableColumns` from a server probe rather than from
  an IR, and query that.

**Criteria.**

- **C1 — Freshness.** Does the answer reflect the server as it is now, or as a
  run left it?
- **C2 — Drift.** Can the SQL disagree with the Go naming convention?
- **C3 — Ergonomics at column grain.** What does "columns of this table
  carrying `delta-encoding`, with their compressed size" cost to write?
- **C4 — Weight.** New surface to install, version, and reconcile.
- **C5 — Claim honesty.** Does the mechanism tempt a caller to read it as the
  authoritative leeway/not-leeway verdict?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | ++ | −− | −  |
| C2 | +  | +  | ++ | ++ |
| C3 | ++ | −  | +  | +  |
| C4 | −  | +  | −  | −− |
| C5 | −  | ++ | +  | +  |

O2 is the cheapest and the most honest — a function claims nothing about a
table — but it leaves every caller writing the same eleven-way `multiIf` and
the same join to `system.tables`, which is the repetition ADR-0171 was written
about. O3 and O4 both trade freshness for a stronger classifier that the
column-grained question does not need. O1 wins on the two criteria the question
is about, and its C5 risk is a naming problem, addressed in §SD2.

## Decision

We will ship **three generated ClickHouse views** — `leeway.columns`,
`leeway.sections`, `leeway.tables` — that decode leeway physical column names
over `system.columns` and `system.tables`, declared by a new package
`leeway/chviews` and installed through the ADR-0171 surface as its fourth
family.

### SD1 — Generated from the naming convention, not written against it

View bodies are rendered in Go from the same declarations the composer and the
parser read: the naming convention's component-position data for both name
arities, and the enum tables for prefixes, plain item types, column roles and
table row configs. No segment index, prefix spelling or enum code is typed into
SQL by hand. The three aspect segments are decoded by calling `LW_ASPECT_DECODE`
and `LW_ASPECT_NAMES_*` rather than by re-rendering their tables — those bodies
are already generated (ADR-0182 §SD4), and a second rendering of the same
vocabulary is the drift ADR-0170 §Q1 named.

The generator reads the layout through an exported accessor on the naming
convention rather than re-deriving field offsets, so a component added to
either arity moves the views with it or fails the build.

### SD2 — The views decode names; they do not classify tables

`DiscoverTableFromColumnNames` remains the only answer to "is this a leeway
table". The views report what a name's *shape* supports and nothing beyond it,
and their columns are named for that: `leeway.columns.layout` is
`foreign | plain | tagged`, and `leeway.tables.name_shape` is
`foreign | mixed | leeway | empty` — a count of layouts, not a verdict. No
column is called `is_leeway`.

This is the discipline that keeps the views inside the seam §Context describes.
A view that claimed the verdict would be asserting the restoration it cannot
perform, and `boxer.tables_leeway` would become the second opinion rather than
the only one.

### SD3 — Three grains, one decode

`leeway.columns` is the decode, one row per column of every table outside the
system databases — foreign columns included, as rows rather than absences, for
the reason ADR-0170 §SD2 gives for its inventory table. `leeway.sections` and
`leeway.tables` are aggregates over it and add nothing the decode does not
already carry, except the `system.tables` passthrough.

Where a property is per-section or per-table by construction (streaming group,
co-section group, table row config) the aggregate reports the value *and* the
distinct count, so a table whose columns disagree is a visible anomaly rather
than an arbitrary `any()`.

`leeway.columns` also carries the `section:column` **handle**
([ADR-0116](./0116-play-leeway-column-handle-resolution.md)) and the **lane
kind** (`value`, `length`, `membership`, …), both taken from the single
in-tree spelling of each. A handle read out of the view is a handle the
playground's resolver accepts; the two cannot disagree because they are one
function.

### SD4 — Installed as the surface's fourth family

`lwsqlsurface` declares the views alongside the three function families,
`Version` goes to 2, and the one-marker invariant is unchanged: the marker at
revision N means *everything* the build declares is installed at revision N.
Reconciliation gains a second listing — views live in `system.tables`, not
`system.functions` — and a missing-views field that counts toward the in-sync
verdict.

Views are reported missing, never dropped, and there is no undeclared-view
field. The asymmetry against the function side is deliberate: the `LW_`
namespace is owned, so a stray function in it is evidence of something, while
the target database may be shared and a view somebody else created there is
not this surface'"'"'s business.

**A view is a snapshot of the vocabulary it was created with.** ClickHouse
expands a SQL UDF *into* a view's stored query at `CREATE` time rather than
resolving it at read time — measured on 26.7, and pinned by the integration
lane. `leeway.columns` therefore carries the whole aspect vocabulary inlined:
the `LW_ASPECT_NAMES_ENC` call is not in its stored query, the kebab names are.
The consequences are all one fact:

- Functions must be installed *before* the views, so an install creates them
  last.
- Replacing a function does not reach a view that already expanded it. Only
  re-creating the view does, which every install does.
- Dropping a function a view expanded is *not* refused, and does not break the
  view. So there is no teardown ordering to defend, and an install replaces the
  views in place rather than dropping them first.
- **A view can be present, selectable, and a vocabulary generation behind** —
  the only drift in this surface that returns data instead of an error.

That last one is what `LW_SURFACE_VERSION` cannot cover: the marker says what
the *functions* are, and an install that put the marker at N while leaving a
view expanded at N−1 would report in sync. So each view records the revision
it was expanded against in its ClickHouse `COMMENT`, and the reconciler
compares it. This is not a second marker — it is how a view answers the
question the marker already asks, for the one object the marker cannot speak
for. `StaleViews` is reported apart from `MissingViews`, and counts toward the
in-sync verdict.

### SD5 — Own database, `:` separator only

The views live in their own `leeway` database, overridable the way the
cataloger's target is, with the default the name a reader will type. This keeps
the always-live decode visibly apart from `boxer.*`, whose tables are
run-stamped snapshots, and makes the family enumerable by database the same way
the `LW_` namespace makes functions enumerable by name (ADR-0162 §SD2).

Scope is the `:` separator, matching `LW_ASPECT_*`'s v0 scope. Every leeway
table in this tree is composed with it. A `_`-separated table decodes as
`foreign` — visibly wrong rather than silently mis-split, since a `_` separator
cannot be told from a snake_case component by position alone.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `lwsqlsurface.Version` | 1 → 2 | every provisioned server: a marker at 1 now reads as skew |
| `lwsqlsurface` declared set | gains views alongside functions | `Reconcile`'s report and the pinned-declared-set test |
| Exported Go API under `public/` | `leeway/chviews` added; `leeway/ddl` gains the layout accessor and `PlainSectionName` | `lwsql`'s plain-section spelling folds into the exported one |
| ClickHouse DDL | three views in a new `leeway` database | the install path, and any deployment that grants `CREATE DATABASE` |

## Alternatives

- **`boxer.leeway_columns` instead of a `leeway` database.** One database for
  everything derived, no new grant — but `boxer.tables_leeway` (snapshot) and
  `boxer.leeway_tables` (live) differing by word order is a trap set for the
  next reader. Rejected.
- **Unqualified `leeway_columns` in the session database.** Works without
  `CREATE DATABASE`, but the family stops being enumerable and collides with
  user tables. Rejected; the target override covers the restricted-grant case.
- **Materialized views or a refreshed table.** Storage and a refresh cadence
  bought nothing: the source is server metadata, and the decode is string
  splitting over a few thousand rows. Rejected.
- **Inlining the aspect tables into the view bodies** instead of calling
  `LW_ASPECT_*`. Removes the install-order and drop-order coupling of §SD4, at
  the cost of a second rendering of the aspect vocabulary on the server.
  Rejected — the coupling is mechanical, the second rendering is the drift.
- **A standalone installer for the views**, leaving the surface marker at 1.
  Cheaper, and a stale view becomes invisible — the exact failure ADR-0171 was
  written about. Rejected.

## Consequences

### Positive

- The eight name segments with no SQL reader gain one, and the aspect
  predicates ADR-0182 §SD4 shipped gain the relation they were meant to be
  selected from.
- Encoding-aspect questions become measurable rather than arguable: aspect,
  role and section join to `system.columns`' compression figures in one query.
- A handle read from SQL is a handle the playground accepts, because both come
  from one function.

### Negative

- A fourth family in a surface whose reconciler was function-shaped. The second
  enumeration and the drop-order rule are permanent weight on a path that was
  a single `system.functions` query.
- A `leeway` database is a grant a deployment may not have. The override is the
  answer, and it is one more thing to configure.
- **`columns` collides with the `COLUMNS('…')` matcher in the client-side
  grammar** (ADR-0108's pre-execute pipeline), as a table name and as a result
  alias alike, so a playground query naming `leeway.columns` unquoted fails to
  parse and ships verbatim — silently skipping handle resolution and every
  other pass. Backticks fix it, the shipped snippets carry them, and
  `system.columns` has always had the same requirement. The name stays: it is
  the parallel that makes the view findable, and the real fix is in the
  grammar, not in the schema. Deferred.
- **The views are snapshots** (§SD4). An aspect added to a vocabulary does not
  reach an installed view until someone re-installs, and the surface now has a
  kind of drift that answers rather than erroring. The stamp makes it visible;
  it does not make it stop happening. This is the strongest argument against
  views and for the scalar-function option O2, which has no such state — it
  was not enough to outweigh C1 and C3, and it is recorded here so a later
  reader can re-weigh it rather than rediscover it.
- Two spellings of "what leeway tables does this server carry" now exist. §SD2
  names which is authoritative; nothing enforces that a reader reads it.
- The decode carries `system.columns`' compression figures, which that table
  computes only for requested columns. The saving depends on the analyzer
  pruning them through the view for a query that does not select them —
  believed to hold, unmeasured here. A query that filters by `database` and
  `table` is cheap either way, and is what a reader should write.

### Neutral

- The views list themselves, and decode as `foreign`. Correct and free, the way
  the ADR-0170 catalog lists its own tables.

## Migration — Tier 1

- **Breaks.** Nothing in the data path. `lwsqlsurface.Version` at 2 makes every
  server provisioned against an earlier build report skew from `Reconcile` until
  it is re-installed — which is the marker working, not a break.
- **Path.** Re-run the surface install against each endpoint. An endpoint whose
  role cannot `CREATE DATABASE` has its functions installed and then fails on
  the view step, which names it; point the install at a database the role owns.
- **Regeneration.** None — the views are rendered at install time, not
  committed as generated files. But note §SD4: an installed view holds an
  expanded copy of the aspect vocabulary, so a vocabulary change is not fully
  deployed until every endpoint is re-installed. `status` names the endpoints
  that still owe one.
- **Old shape.** Nothing removed. `boxer.tables_leeway` and `systemTableColumns`
  keep their jobs (§SD2).

## Verification plan — Tier 1

- **Lane.** Default `go test` for the generated SQL — the layout the generator
  emits is checked against names the real composer produces, so a segment index
  that drifts fails without a server. The `//go:build integration` lane
  installs the surface on a live ClickHouse and selects from all three views,
  including the drop-order case: install, then re-install over an endpoint
  already carrying the views.
- **What would fail.** A component added to either name arity, or an enum
  gaining a member, without the views moving: the field-position check against
  the real composer stops matching. A ClickHouse that stopped inlining UDF
  bodies into views: the pin on that behaviour goes red, and the stamp and the
  create-views-last rule stop being load-bearing — which is worth being told
  about rather than discovering as a silent change of semantics. A reconciler
  that stopped noticing a view from another revision: the stale-view case,
  which fakes a stamp because the claim is the only thing a reconciler can
  check.
- **Gap.** Nothing pins the `columns` backtick requirement — play's help-corpus
  test rejects an unparseable snippet, which catches it in the shipped
  snippets and nowhere else. Projection pruning through the view is not
  asserted, so a release that stopped pruning would cost time without failing
  anything. The
  `_`-separator corpus is out of scope by §SD5 and untested. No
  test asserts that `leeway.tables.name_shape` agrees with
  `boxer.tables_leeway.kind` — by §SD2 they answer different questions, and a
  test that pinned them together would assert the claim this ADR declines to
  make.

## Status

Accepted 2026-09-12.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## Updates

### 2026-09-12 — the views get a book, and the backtick gap closes

`apps/sqlapplet/bookleeway` ships five chapters over the three views — the
leeway-table inventory, a table's section anatomy, the stored bytes as an
icicle and a treemap, what each aspect vocabulary sits on, and the tables that
share a schema as a graph. It sits beside the ADR-0170 catalog book under the
same topic, which is where §SD2's split becomes something a reader can see
rather than a sentence they have to remember: one book says what the names
spell out, the other says whether a table really is leeway.

Two things the book settled that the body above left open.

**The `columns` backtick requirement is now pinned.** §Verification recorded it
as a gap — caught in the shipped play snippets by the help-corpus parse and
nowhere else. `TestLeewayBookBackticksTheColumnsView` asserts it directly, on
every chapter, against the view name rather than a literal.

**The Graphview tab needed classifying before a chapter could name it.** It had
landed in play (ADR-0227) without an entry in sqlapplet's tab policy, which
that policy's own guard had been failing on. It is a result panel, and it is
listed auto-off: its contract is the Network tab's unchanged, so every shape
that would turn it on already turns Network on, and auto-on would put a second
graph tab on those windows by default. `lw-affinity.md` names both and lets the
reader compare the two readings.

The chapters' SQL is verified against a live server by the book's own
integration lane, knobs included — the views are generated SQL, and a Go test
can check the shape of what they emit but not what it means.

## References

- [ADR-0162](./0162-leeway-co-ragged-function-pack.md) — the `LW_` namespace and the pack.
- [ADR-0170](./0170-data-catalog-competence.md) — the authoritative classifier, and the kill-reason §Context re-opens.
- [ADR-0171](./0171-leeway-sql-read-surface.md) — the surface, its marker, and its reconciler.
- [ADR-0182](./0182-leeway-aspects-v2-codec-and-vocabulary.md) — the aspect codec and the `LW_ASPECT_*` family §SD1 builds on.
- [ADR-0116](./0116-play-leeway-column-handle-resolution.md) — handles and labels.
- [doc/explanation/leeway-column-names.md](../explanation/leeway-column-names.md) — the name anatomy in prose.
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md) — the applet book the views ship a corpus for.
