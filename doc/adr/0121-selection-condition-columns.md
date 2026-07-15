---
type: adr
status: accepted
date: 2026-07-15
reviewed-by: "@spx"
reviewed-date: 2026-07-15
---

> **Status: accepted (2026-07-15).** Built and verified; see the Status section.

# ADR-0121: selection conditions — reporting which part of a retrieval query's WHERE admitted each row

## Status

Accepted. Implemented: the `ExposeSelectionConditions` pass and the
`ConditionNamerI` seam in `public/db/clickhouse/dsl/nanopass/passes`, the leeway
condition namer on `lwsql.Resolver` in `public/semistructured/leeway/lwsql`, and
play's opt-in wiring (`Client.SetExposeConditions`, the top-bar toggle, the
binding bundle). It is deliberately absent from the standard pass set (§SD7).

## Context

[ADR-0117](./0117-passthrough-table-classifier.md) gives us a mechanical answer
to "does this query return a table's stored rows 1:1?" — the passthrough
classifier. It deliberately says nothing about *which rows*: its own
non-guarantees section is explicit that it "constrains the projection, not the
predicate", and that a passthrough query may carry an arbitrary `WHERE`.

That leaves the predicate the opaque half of an otherwise transparent query. For
a query already classified as information retrieval, the remaining question an
auditor or a user asks is *why is this row here?* — which part of the `WHERE`
admitted it. Today that is answerable only by reading the SQL and re-evaluating
it by hand against each row.

This ADR makes the predicate structure explicit in the query's own output: each
maximal OR-free part of the `WHERE` becomes a named column, and the `WHERE` is
rebuilt from those names. The result set then carries, per row, which part of the
query admitted it — its **selection conditions**, exposed as columns.

This is a query-side notion, distinct from the data-provenance family; where it
sits in that field, and which published work it draws on, is set out in
[§Relation to the literature](#relation-to-the-literature).

## Decision

A nanopass pass, `ExposeSelectionConditions`, gated on the ADR-0117 classifier.
For a query it accepts:

```sql
SELECT a, b FROM tt WHERE c = 1 AND d IN (SELECT t FROM u)
```
becomes
```sql
SELECT a, b, (c = 1 AND d IN (SELECT t FROM u)) AS cond_1
FROM tt WHERE cond_1
```

ClickHouse permits a `SELECT` alias to be referenced from `WHERE` and
substitutes its expression, so the rewrite is semantics-preserving: it returns
the same rows.

### SD1 — A condition is a maximal OR-free part of the predicate

The unit is not the boolean leaf and not the top-level `AND` conjunct: it is the
**maximal OR-free subtree**. Any part of the predicate with no `OR` in it becomes
one condition whole; only a part that does contain an `OR` is recursed through
its connectives (`AND`, `OR`, `NOT`, parentheses). Each condition is then **replaced in
place** by its name — the connectives, parentheses, and trivia around it are
never rebuilt:

```sql
WHERE (a = 1 AND b = 2) OR c = 3      →  WHERE cond_1 OR cond_2
WHERE c = 1 AND d IN (SELECT t …)     →  WHERE cond_1
WHERE NOT (a = 5) AND (b = 2 OR c = 3) →  WHERE cond_1 AND (cond_2 OR cond_3)
```

Substituting an identifier for a subtree cannot change precedence (an identifier
is an atom, binding tighter than every operator), so structure is preserved
without a precedence engine. A condition is parenthesised in the projection
(`(c = 1) AS cond_1`) so nothing in it can bind into the `AS` — unless it is
already a parenthesised group, which would otherwise emit `((a = 1 AND b = 2))`.

**Why conjunctions group.** A conjunction has exactly one way to be satisfied, so
splitting it buys nothing: every conjunct is constant-true on every returned row
— necessarily, since the `WHERE` still requires all of them. The disjuncts are
what discriminate, and grouping keeps the answer at the granularity of the
question "which alternative admitted this row?":

| a | b | c | cond_1 (`a=1 AND b=2`) | cond_2 (`c=3`) |
|---|---|---|---|---|
| 1 | 2 | 3 | 1 | 1 |
| 1 | 9 | 3 | 0 | 1 |
| 4 | 9 | 3 | 0 | 1 |

for `WHERE (a=1 AND b=2) OR c=3` — the second and third rows are in the result
*because* `c=3`, and the query now says so. This is the same boundary the
literature's near-silence on query-side attribution of *present* answers implies
(see §Relation to the literature): under conjunction the question is degenerate,
under disjunction it is not.

**But an `AND` is not simply atomic**, which is why the rule is phrased over
OR-freeness rather than "group each conjunction". `NOT (a=5) AND (b=2 OR c=3)` is
a conjunction; grouping it whole would emit one opaque, constant-true condition and
throw away the inner `OR` — precisely the structure worth reporting. Under the
OR-free rule it keeps three conditions, and only the OR-free conjunct
(`NOT (a=5)`) is constant-true. A pure conjunction still yields exactly one
condition, always true: that case is kept rather than skipped, because the column
still names the predicate in the result schema, and because "sometimes rewritten,
sometimes not" is a worse contract than "always rewritten".

**"Contains an `OR`" means the predicate's own boolean skeleton** — ORs reachable
through connectives alone. The `OR` in `d IN (SELECT t FROM u WHERE x OR y)`
belongs to the subquery's structure, not to this `WHERE`'s; counting it would
split a conjunction that must group. The OR-scan therefore mirrors the descent
exactly, and both stop at a leaf.

**Both spellings of a connective are recognised**, and this is not a nicety. In
Grammar1, `NOT (a = 5)` — a `NOT` followed by a parenthesis, which is how it is
normally written — parses as a **function call** named `NOT`
(`ColumnExprFunction`), not as `ColumnExprNot`; only the paren-free `NOT a = 5`
takes the operator branch. Treating call forms as leaves would therefore have
made the *common* spelling of `NOT` opaque while the rare one recursed. So
`and` / `or` / `not` are matched by normalised call name too. Any other call —
and a parametric one, a `DISTINCT` one, or one taking a lambda, none of which a
connective ever is — is a condition whole.

### SD2 — The gate: exactly one passthrough table, one SELECT, one WHERE

The pass rewrites only when all of:

- `analysis.ExtractPassthroughTables` reports **exactly one** table. Non-empty is
  the ADR-0117 information-retrieval signal; exactly one is what makes the
  condition-naming target unambiguous.
- The top level is a **single SELECT** — no `UNION` chain (SD3).
- That SELECT has a `WHERE`. No predicate, nothing to report on.
- The schema provider **knows the table's columns**, which the collision check
  (SD4) requires.

Any of these failing means the pass returns the query unchanged. The gate
inherits ADR-0117's conservatism wholesale: an unparseable query, a join, an
aggregate, a table function, or a recursive CTE never reaches the rewrite.

### SD3 — `UNION` chains are out of scope, because they cannot work

ADR-0117 classifies `UNION ALL` branches and unions their tables, so it is
natural to expect the rewrite to follow. It cannot. Each branch has its own
predicate and therefore its own condition count, and `UNION ALL` requires every
branch to have the same number of columns:

```
Code: 258. DB::Exception: Different number of columns in UNION_ALL elements:
a, cond_1 and a, cond_1, cond_2. (UNION_ALL_RESULT_STRUCTURES_MISMATCH)
```

Padding the narrower branches would mean inventing condition columns for
predicates that do not exist in them, which is worse than declining. A `UNION`
chain therefore classifies as information retrieval but is left unrewritten. This
is a descope, recorded rather than gated on: aligning conditions across branches
is a real design question (do two branches' unrelated predicates share a column?)
and does not need answering to ship the single-SELECT case.

### SD4 — Condition names are collision-checked, and a collision is an error

A condition alias silently shadows a stored column of the same name. On a table
carrying a real `cond_1` column, `SELECT a, cond_1 FROM tt WHERE c = 1` returns
the stored value, while the naive rewrite

```sql
SELECT a, cond_1, (c = 1) AS cond_1 FROM tt WHERE cond_1
```

returns the condition under that name instead — twice, with no error from
ClickHouse. The rewrite would have quietly changed the query's meaning.

So every candidate condition name is checked against **all** of the table's columns
(not only the projected ones — the `WHERE` reference binds to the alias either
way), and a collision **fails the pass** rather than renaming around it. A pass
serving a security lens should decline rather than quietly emit something the
author did not write. `StagePreExecute` is best-effort, so the practical effect
of the error is that the original query ships unrewritten and the failure is
logged.

Checking against the table's columns is the reason the pass needs a schema
provider even for the ordinary, non-leeway path.

### SD5 — Leeway tables get a declared `conditions` section

For a leeway table the conditions are not `cond_1` but full physical column names
in a section of their own, so they are part of the data model rather than bolted
beside it:

```
tv:conditions:c1:val:b:0:0:0:0::
```

— the tagged-value prefix, the configured section name, the condition column name,
`ColumnRoleValue`, canonical type `b` (boolean; no width, per the CT rules), zero
encoding hints / use-aspects / value-semantics, the table's own `tableRowConfig`,
and no co-section or streaming group.

This is what "properly declared" cashes out to: feeding the result's column names
back through `DiscoverTableFromColumnNames` reconstructs a genuine
`conditions` tagged section beside the table's real ones, so everything
keyed off leeway naming works without further change — play's Table tab labels
the columns `conditions:c1` (`lwsql.BuildLabels`), and `` `conditions:*` ``
resolves as an ADR-0116 handle.

Three details are forced rather than chosen:

- **The separator is the table's**, via `lwsql`'s existing `detectSeparator` — a
  dumped table may join components with `_` instead of `:`, and the synthesized
  names must match or they will not parse back.
- **The section name folds to `LowerSpinalCase`** (ADR-0116's rule), so a
  configured `whySelected` / `why_selected` / `conditions` are one name.
  The fold also removes the hazard that a configured `why_selected` would
  contain a `_` separator and corrupt the name grammar; a folded name that still
  contains the separator is rejected.
- **`tableRowConfig` is copied from the table's existing columns**, not fixed at
  zero — it is a table-wide property, and discovery reads it from every column.

The section name is configurable and defaults to `conditions`.

### SD6 — Seam: a domain-agnostic namer, the leeway policy in `lwsql`

Following the ADR-0116 separation exactly. `nanopass/passes` gains

```go
type ConditionNamerI interface {
    NameConditions(dbName string, tableName string, n int) (names []string, ok bool, err error)
}
```

`ok=false` means "not my table" and the pass falls back to plain
`<prefix><n>` naming; a non-nil `err` refuses the rewrite. The leeway
implementation — the physical-name composition and the section policy — lives in
`leeway/lwsql`, so the SQL framework keeps no leeway dependency.

`*lwsql.Resolver` implements it, rather than a separate type — it already holds
the schema provider, the cached per-table classification, and the leeway-or-not
verdict, so it *is* a client's leeway schema knowledge.

### SD7 — Opt-in per host; not in the standard pre-execute set

The rewrite changes a query's **result schema**. That makes it the wrong thing to
put in `defaults.RegisterStandard`, where every `StagePreExecute` consumer would
inherit it and every retrieval query would silently grow columns its author did
not ask for. It is therefore not registered at all: a host that wants it applies
it itself.

In play that is a top-bar checkbox, **default off**, pushed to `Client`, which
applies the pass in `buildResidual` **after** the registry stage — so a
`` `geoPoint:pointLat` `` handle in the `WHERE` is already resolved to a physical
name by the time a condition is lifted into the projection. The application is
best-effort like the registry stage: a refusal (§SD4) logs and ships the query as
the user wrote it.

Two consequences of staying out of the registry, stated plainly rather than
discovered later. It does **not** appear in `keelson('sql_passes')` — that
catalog describes the registry, and this is not in it. And the ordering that
matters (after handle resolution) is enforced by where `buildResidual` calls it
rather than by an `Order` number, so a second such opt-in pass would want a
better home than a hand-sequenced call.

The recursion hazard is already closed upstream: the schema probe issues through
a direct client call that bypasses the registry (ADR-0116 §SD3), so the pass
cannot re-enter through its own `system.columns` lookup.

Rejected: registering it as a late-bound `passreg.Factory` at order 300, gated on
the binding supplying a schema. It was the first cut, and it buys catalog
visibility, but the gate it offers is "does this host have a schema?" — not "does
this user want their result schema changed?", which is the actual question. A
default-on schema rewrite is not something a consumer should have to opt *out* of.

## Relation to the literature

Database provenance is a developed field with precise terms; this section places
the pass within it. References: Buneman, Khanna & Tan, *Why and Where: A
Characterization of Data Provenance* (ICDT 2001); Green, Karvounarakis & Tannen,
*Provenance Semirings* (PODS 2007); and the survey Cheney, Chiticariu & Tan,
*Provenance in Databases: Why, How, and Where* (Foundations and Trends in
Databases 1(4), 2009) — cited below by its numbering.

### Query-side attribution of a present answer

The field has two axes. Provenance attributes an output row to part of the
**data** — which input tuples produced it; a second body of work attributes it to
part of the **query**:

|  | present answer | missing answer |
|---|---|---|
| **which input tuples** | lineage, why-, how-, where-provenance | data-based why-not |
| **which part of the query** | ← *this pass* | query-based why-not ("picky" operators/subqueries) |

This pass sits in the query-side / present-answer cell: it reports which part of
the `WHERE` selected each returned row. That cell is thin in the literature for a
principled reason, and it is the same one SD1 reaches empirically — under a
**conjunction**, "which part of the query is responsible" is trivially *all of
it* (every conjunct is constant-true on every returned row); the question carries
information only under **disjunction**. SD1's OR-free rule is therefore the
boundary at which the question becomes non-degenerate, not an arbitrary cut.

### Distinct from the provenance family

The query-side axis is orthogonal to provenance, and the orthogonality is
definitional rather than a matter of degree. In every provenance notion a
selection predicate is a **truth test, not a structure**: why-provenance has
`Why(σ_θ(Q),I,t) = Why(Q,I,t)` when `θ(t)` and `∅` otherwise (Def. 2.4), and the
semiring generalisation of Green et al. — the general model of which lineage,
why-, how- and where-provenance are all specialisations — has
`(σ_θ(R))(t) = R(t) · θ(t)` with θ mapping each tuple to 0 or 1 (Def. 3.1). A
predicate's *structure*, which is exactly what this pass reports, appears in none
of them. For this pass's query class the provenance is degenerate in its own
right besides: ADR-0117 admits a single source with no join, so every
why-provenance witness (a subset of input records, Def. 2.3) is one stored row.
The two notions answer different questions; this one names conditions, on the
query axis.

### Neighbours it draws on

- **Method — Perm / GProM** (Glavic & Alonso, ICDE 2009; Arab, Glavic et al.).
  Computing provenance by **rewriting the query** so the result carries it as
  extra attributes in the same relation is precisely their design, and precisely
  this pass's. The difference is only in *what* the extra attributes hold: they
  carry witness lists of source tuples, this carries the truth of predicate parts.
- **Shape — c-tables / PosBool** (Imieliński & Lipski 1984). A conditional table
  annotates each tuple with a Boolean condition, its semiring being PosBool(B),
  the positive Boolean expressions over B. The rewritten relation is structurally
  that: rows carrying the evaluated atoms of a positive Boolean formula. Two
  differences — c-table atoms range over *unknown values* where these range over
  *query predicates*, and theirs stay symbolic where these are evaluated.
- **The deferred relax-the-`WHERE` mode is query-based why-not provenance**
  (Chapman & Jagadish, *Why Not?*, SIGMOD 2009; Bidoit, Herschel & Tzompanaki,
  *Query-Based Why-Not Provenance with NedExplain*, EDBT 2014), which identifies
  the "picky" operator or predicate that excluded an expected tuple. If we build
  it, build that — it is a named problem with published algorithms, not an idea to
  reinvent.

### Syntactic, not semantic

The pass keys off the literal `OR` structure, so `(a ∨ b) ∧ c` and
`(a ∧ c) ∨ (b ∧ c)` — the same predicate — yield different condition columns. The
equivalence-invariant alternative would require DNF normalisation, which
§Alternatives rejects on cost and on fidelity to the query the author wrote. The
literature documents the same trade on the data side: the witness basis is itself
sensitive to how a query is written, and Buneman et al.'s *minimal* witness basis
is the equivalence-invariant repair (Thm. 2.10, Cor. 2.11). Sitting at the
syntactic point is a deliberate choice, recorded here rather than left for a
reader to infer.

### Naming

Pass `ExposeSelectionConditions` · seam `ConditionNamerI.NameConditions` ·
section `conditions` · columns `cond_1` · play toggle "Conditions". The vocabulary
stays on the query-side axis: a **condition** is a part of the `WHERE`. The
data-side terms — *provenance* (attribution to data) and *witness* (specifically a
subset of input records, Def. 2.3) — name different objects and are not used for
these columns.

## Alternatives considered

- **Relax the `WHERE`** — emit the condition columns but weaken the filter
  (`cond_1 OR cond_2`, or drop it) so rows that fail some conjuncts still return,
  and the columns discriminate even under a pure conjunction. This answers "why is
  this row *not* here?" by running a different query than the author wrote, which
  is why it is not the default. It is **not** a loose idea to reinvent later: it
  is query-based why-not provenance, which has a literature and algorithms — see
  §Relation to the literature. Deferred; nothing here forecloses it.
- **Every boolean leaf its own condition** (`(a=1) AS cond_1, (b=2) AS cond_2` for
  `a=1 AND b=2`). It was the first cut. Rejected: it splits conjunctions into
  columns that are constant-true by construction, so the extra columns carry no
  per-row information — they only make the result wider. SD1's OR-free rule keeps
  the split exactly where it informs.
- **A conjunction is atomic, full stop** — the simpler phrasing of SD1. Rejected:
  it swallows an `OR` nested inside a conjunction, which is the one part of such
  a predicate worth reporting.
- **DNF-normalise the predicate first**, so every condition is a genuine
  alternative and none is constant-true. This is also what would make the notion
  invariant under query equivalence rather than syntactic (see §Relation to the
  literature). It is the only way to remove the
  constant-true case entirely, but it rewrites the user's `WHERE` into something
  they did not write, can blow up exponentially, and would defeat the in-place
  substitution that keeps the query recognisable. Not pursued.
- **Suffix-escalate on a name collision** (`cond_1_`, `cond_1__`) instead of
  erroring. Always succeeds, but the emitted names stop being predictable from
  the query text, and it papers over a table whose schema genuinely collides with
  the configured prefix — better surfaced than absorbed.
- **A prefix rare enough not to collide** instead of checking. Cheaper (no schema
  probe on the plain path), but trades a checked guarantee for a convention, and
  the probe is needed for the leeway fork regardless.
- **Rebuild the `WHERE` from the parsed structure** rather than substituting
  conditions in place. Requires reproducing ClickHouse's precedence ladder — which
  `RemoveRedundantParens` shows is subtle (`BETWEEN` and `?:` bind looser than
  `OR`) — to gain nothing: in-place substitution preserves the structure exactly,
  including comments and whitespace.

## Consequences

- Idempotency is free, and not by a guard. ADR-0117 taints a table out on **any**
  alias, so the classifier rejects this pass's own output: a second `Apply` finds
  no passthrough table and returns the query unchanged. `f(f(x)) == f(x)` holds
  by construction, and the pass declares `Idempotent` with no fixed-point
  wrapper. The coupling is worth stating plainly: were ADR-0117's strict-rows
  fork ever relaxed to admit aliases, this property would silently break, and
  `AssertProperties` is what would catch it.
- The pass consumes the classifier as a **gate**, not as a map. It never uses the
  reported tables to decide *where* to rewrite — only whether to, and (given
  exactly one) what to name the conditions against.
- A query whose `FROM` is a subquery has its conditions named and collision-checked
  against the resolved base table, which may carry columns the subquery does not
  project. The check is therefore over-conservative there — it can refuse a
  rewrite that would have been safe. Fail-closed, and accepted.
- Result schemas change for a rewritten query — which is why it is opt-in (SD7).
  With the toggle off, nothing in play behaves differently from before; with it
  on, consumers that assume a retrieval query returns exactly the stored columns
  will see the condition columns. Anything keyed on leeway physical names keeps working
  (SD5).
- The condition columns are `UInt8` 0/1 in ClickHouse, typed `b` in leeway. A
  condition is constant-true whenever no `OR` is above it (SD1) — a pure
  conjunction yields exactly one such column. That is informative about the
  query, not about the row.
- The toggle is per `Client`, so it applies to every query that client runs,
  including the reactive graph's re-fires and the Diagnostics `EXPLAIN` probe
  (which shares `buildResidual` deliberately, so its verdict keeps matching a
  real Run).

## Validation

- clickhouse-local 26.6, both analyzers (`enable_analyzer` 1 and 0), Memory and
  MergeTree: the rewrite returns rows identical to the original; alias-in-`WHERE`
  substitution holds; a 21-component colon-bearing physical name works as a
  double-quoted alias and as a `WHERE` reference; the `UNION ALL` mismatch of SD3
  and the silent shadowing of SD4 are both reproduced.
- End to end against clickhouse-local, executing the pass's **own output** (not a
  hand-written approximation) over a leeway table built by the real generator:
  `` SELECT `id:id:u64:0:0:0:` FROM facts WHERE `id:id…` = 1 OR `id:id…` = 2 ``
  returns the same two rows, with `tv:conditions:c1:…` / `c2:…` reading `1 0`
  and `0 1` — each row reporting which disjunct admitted it.
- The SD1 granularity, on the engine: `WHERE (a=1 AND b=2) OR c=3` returns the
  same three rows with the grouped conjunction reading `1 0 0` and `c=3` reading
  `1 1 1` — only the first row matched via the conjunction. `WHERE NOT (a=5) AND
  (b=2 OR c=3)` returns its rows with `cond_1=1` (the OR-free conjunct,
  constant-true as SD1 says) and `cond_2`/`cond_3` discriminating. The worked
  example's grouped single condition returns rows identical to the original.
- The leeway round-trip of SD5 is checked against the real convention:
  `DiscoverTableFromColumnNames` over a mixed column set
  (`id:id:u64:2k:0:0:`, `tv:symbol:value:val:s:m:0:24:0::data`, and two
  synthesized conditions) reconstructs the `conditions` section with value
  columns `[c1 c2]` beside the `symbol` section and the `id` plain column.
- Table-driven pass tests: the ADR's worked example; grouping vs. recursion
  through `AND` / `OR` / `NOT` / parens and both connective spellings; an `OR`
  inside a filter subquery not splitting the grouping; a parenthesised condition
  not double-wrapped; every gate (no WHERE, join, aggregate, alias, UNION,
  unknown schema, two tables); the collision error; `SELECT *`; leeway naming and
  its separator/fold/`tableRowConfig` handling; and `nanopass.AssertProperties`
  for the declared idempotency.
- `passreg/defaults` tests pin SD7 from the other side: the standard set contains
  no `WhyProvenance` unit, and a bound `StagePreExecute` stage leaves a retrieval
  query's SELECT and WHERE exactly as written. `apps/play` tests cover the toggle:
  off by default, on → rewritten, off again → verbatim, a refusal shipping the
  original, and a client with no schema install staying inert.
- **Live drive** (headless weston, `EGUI_INSPECTION`, real ClickHouse 26.6 with
  the 241-column `anchor.facts`). `` SELECT `id:id:u64:2k:0:0:` FROM anchor.facts
  WHERE `id:id…` = 1 OR `id:id…` = 2 ``: the **Why** toggle renders in the top
  bar unchecked, "as sent" shows the query verbatim; checking it rewrites the
  wire SQL to the two `tv:conditions:c*` condition columns while the editor buffer
  stays as typed; and a Run returns

  | # | id:id | conditions:c1 | conditions:c2 |
  |---|---|---|---|
  | 1 | 1 | 1 | 0 |
  | 2 | 2 | 0 | 1 |

  — each row reporting which disjunct admitted it. The Detail pane lists
  `tagged · conditions` as a section beside `plain · entity-id`, which is
  §SD5's claim demonstrated end to end: the synthesized columns are not merely
  leeway-shaped, the schema machinery reconstructs them as a real section and
  labels them, with no change to it.

  The drive earned its keep — it caught two wiring defects that every unit test
  passed through. The `previewAsSent` cache keyed only on (buffer, signal
  revision), so the "as sent" view kept showing the previous query while a
  different one shipped; the toggle now keys it too. And the top bar pushed to
  the client only on an observed change (`before != after` around the checkbox),
  which never fires — `SendRespVal` does not write the field synchronously, so
  the comparison always saw equal values and the flag never reached the client.
  It is now pushed unconditionally.
