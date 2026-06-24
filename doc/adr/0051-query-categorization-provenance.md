---
type: adr
status: proposed
date: 2026-04-21
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0051: Query Categorization via Perm-Style Provenance-Shape Rewrite

## Context

[ADR-0050](0050-clickhouse-observability-pipeline.md) fixes the transport
and archive surface: ClickHouse produces progress, query_log, and result
Arrow IPC streams; a bridge publishes them on NATS subjects; a canonical
`facts` table plus opaque-blob archive together absorb durable storage.
Unresolved there: what *shape* a given query's result has with respect
to `facts`, and how that shape should drive which archive path applies.

Three result shapes were enumerated in discussion ahead of this ADR:

1. **(a) Opaque.** Output rows do not correspond to `facts` rows —
   scalar aggregates, synthetic columns, computed summaries. Archive
   target: opaque-blob result archive (per ADR-0050).
2. **(b) Data mart.** Output rows are 1:1 with existing `facts` rows,
   keyed by `facts_id`. Archive target: `facts` directly (enriching
   existing rows via `ReplacingMergeTree` semantics).
3. **(c) Analytical with lineage.** Output rows are derived from *n*
   `facts` rows via aggregation; each output row carries a set of
   contributing `facts_id` values. Archive target: new `facts` rows
   (synthetic id) **plus** normalized edges in `ch_lineage`.

The question this ADR answers: **given an arbitrary user SELECT, decide
the category statically, without catalog access, and emit that decision
in a form a reviewer can audit against a published rule.**

Constraints:

- **Lexical only.** No access to CH system catalogs at categorization
  time. The categorizer sees SQL text, nothing else.
- **Bounded nanopass complexity.** The pipeline runs on
  [`public/db/clickhouse/dsl/nanopass`][boxer-nanopass] —
  CST-level, ANTLR4-parsed, pass-composed. Adding passes is cheap;
  adding *rules per pass* is the complexity we cap.
- **Auditable.** Every category decision must cite the published rule
  that justifies it (operator-level rewrite rule or theorem).
- **`facts`-superpower holds.** Per ADR-0050, `facts` accommodates any
  result schema, extended on demand; categorization only picks which
  archive path applies, not whether archival is possible.

The analytical content — "which `facts` tuples does each output tuple
depend on?" — is the subject of a mature literature on
*database provenance*. Foundations were laid by
[Buneman-Khanna-Tan 2001][bkt01] and
[Green-Karvounarakis-Tannen 2007][gkt07] (the "provenance semirings"
framework); a canonical survey is
[Cheney-Chiticariu-Tan 2009][cct09]. Two operational systems —
[Perm][perm-icde09] (Glavic & Alonso, ICDE 2009; [Glavic 2010
thesis][glavic-thesis]) and [ProvSQL][provsql-vldb18]
([Senellart et al. VLDB 2018][provsql-vldb18],
[Sen-Maniu-Senellart arXiv:2504.12058][provsql-arxiv25]) — implement
provenance by SQL rewriting. The hard case of aggregate provenance is
treated by [Amsterdamer-Deutch-Tannen PODS 2011][adt11].

This ADR borrows their theoretical and operational results to turn the
categorization problem into a one-shot symbolic fold — *without* having
to compute provenance at runtime.

## Design space (QOC)

**Question.** How should we classify arbitrary user SELECT statements
into (a) opaque, (b) data mart, (c) analytical-with-lineage at
submission time, given lexical-only access and auditability?

**Options.**

- **O1 — Ad-hoc heuristics.** Hand-written predicates like "FROM
  `facts` with no GROUP BY ⇒ (b)." Cheap; fragile; gets
  `SELECT uniq(id) FROM facts` wrong.
- **O2 — Cardinality-class analysis.** Annotate each SELECT layer with
  `preserving` / `reducing-scalar` / `reducing-grouped` / `expanding`.
  Resolves the aggregate case correctly but has no formal grounding;
  rules are invented per corner case.
- **O3 — Full semiring-annotated rewrite.** Implement ProvSQL-style
  N[X] / B[X] semiring rewriting with the m-semiring monus for EXCEPT
  and the [Amsterdamer-Deutch-Tannen][adt11] K⊗M tensor for aggregates.
  Formally complete; oversized for a classifier.
- **O4 — Perm-style local rewrite folded to a provenance-shape
  lattice.** *(chosen)* Apply the
  [Perm rules R1–R9][perm-icde09] symbolically, abstracted to three
  equivalence classes of annotation (`CONST` / `VAR` / `POLY`);
  combine with a single lexical side-predicate (`facts_id` traced
  through the projection).
- **O5 — User-declared category via pragma.** The caller annotates
  each query with `/* @category: b */`. Zero analysis; zero
  verification; trust-based. Useful only as a reviewer override, not
  as the primary mechanism.

**Criteria.**

- **C1 — Formal backing.** Can every rule in the pass cite a
  published rule, theorem, or proposition?
- **C2 — Lexical purity.** Does the pass avoid catalog access?
- **C3 — Rule count (nanopass size).** How many rules per pass?
- **C4 — Corner-case coverage.** Aggregates, DISTINCT, EXCEPT, outer
  joins, subqueries.
- **C5 — Extensibility.** Cost of adding a new CH-specific SQL feature.
- **C6 — Auditability.** Can a reviewer map the decision back to a
  published rule without re-deriving the theory?
- **C7 — Runtime overhead.** Does the decision itself execute SQL?

**Assessment.** `++` strong positive, `+` positive, `−` negative,
`−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | −  | ++ | ++ | −− |
| C2 | ++ | ++ | ++ | ++ | ++ |
| C3 | ++ | +  | −− | +  | ++ |
| C4 | −  | +  | ++ | +  | −− |
| C5 | −  | +  | −  | +  | ++ |
| C6 | −− | −  | +  | ++ | −− |
| C7 | ++ | ++ | +  | ++ | ++ |

O4 is Pareto-optimal on (C1, C6, C3): it reuses a peer-reviewed,
operationally-validated rule set from Perm (Fig. 3 of
[ICDE 2009][perm-icde09]), but abstracts the output domain from full
polynomials in N[X] down to a three-element lattice, cutting rule count
to what classification actually requires. O3 dominates on completeness
but pays complexity we do not need; O1/O2/O5 fail on auditability.

## Decision

Adopt **O4**: apply [Perm][perm-icde09]'s rewrite rules R1–R9
symbolically, folded through a shape homomorphism into a three-element
provenance-shape lattice, combined with a lexical `facts_id`-trace
predicate, to decide the category. The categorizer is a fixed pipeline
of passes in the boxer nanopass framework; its output is a
`CategorizationReportLw` record usable by ADR-0050's SQL-rewrite phase.

### Formal setup

**Annotation semiring.** Per [Green-Karvounarakis-Tannen 2007][gkt07]
(Definition 3.1 and Proposition 4.6): for the positive relational
algebra RA⁺ (selection σ, projection π, renaming ρ, cross product ×,
natural join ⋈, union ∪), annotations live in the **positive-integer
polynomial semiring** N[X], where X is the set of formal variables
`{ x_t : t is a tuple in any base relation }`. For each input tuple t,
x_t is a fresh indeterminate. For each output tuple t' of query Q, the
annotation `ann(t', Q) ∈ N[X]` is computed inductively.

**Inductive definition of ann**, following [GKT 2007][gkt07] §4:

```
ann(t, R)               = x_t                                  for t ∈ R
ann(t, σ_C(Q))          = ann(t, Q) · [C(t)]                   [C(t)] = 1 if C holds, 0 otherwise
ann(t, π_A(Q))          = Σ { ann(t', Q) : π_A(t') = t }        set-projection sum
ann(t, ρ(Q))            = ann(ρ⁻¹ t, Q)
ann(t₁·t₂, Q₁ × Q₂)     = ann(t₁, Q₁) ⊗ ann(t₂, Q₂)             semiring product
ann(t, Q₁ ∪ Q₂)         = ann(t, Q₁) ⊕ ann(t, Q₂)              semiring sum
ann(t, Q₁ ⋈_C Q₂)       = ann(t, σ_C(Q₁ × Q₂))                  join = filtered product
```

Aggregation γ is treated per [Amsterdamer-Deutch-Tannen 2011][adt11]
Proposition 3.2 and §3: for a grouped aggregate γ_{G, f}(Q) with
grouping key G and aggregate function f, the annotation of an output
row is the N[X]-sum of annotations of all input rows in the same group.
Scalar aggregate (no GROUP BY) is the degenerate case of a single
global group.

Selection-difference (set minus, EXCEPT) requires moving from a
semiring to an **m-semiring** per [ProvSQL 2025 §III][provsql-arxiv25],
Rule R4. We treat EXCEPT conservatively (see *Limits* below) and do
not extend the semiring here.

**Shape lattice.** Define the abstraction
`σ̂ : N[X] → Sh`, where `Sh = {CONST, VAR, POLY}`, by:

```
σ̂(p) = CONST   iff  p ∈ N                               (p contains no variables)
σ̂(p) = VAR     iff  p = c · x_i for some c ∈ N₊, i ∈ X   (single indeterminate, positive coefficient)
σ̂(p) = POLY    otherwise
```

`Sh` carries two operations induced by the semiring:

**Product ⊗̂ : Sh × Sh → Sh** (for × and ⋈):

| ⊗̂      | CONST | VAR  | POLY |
|--------|-------|------|------|
| CONST  | CONST | VAR  | POLY |
| VAR    | VAR   | POLY | POLY |
| POLY   | POLY  | POLY | POLY |

*(Derivation: `c₁ · c₂` ∈ N ⇒ CONST; `c · (c'·x)` = `(cc')·x` ⇒ VAR;
`(c·x) · (c'·y)` with x ≠ y is degree-2 monomial ⇒ POLY; everything
else is POLY by closure.)*

**Sum ⊕̂ : Sh × Sh → Sh** (for ∪ and γ):

| ⊕̂      | CONST | VAR  | POLY |
|--------|-------|------|------|
| CONST  | CONST | POLY | POLY |
| VAR    | POLY  | VAR* | POLY |
| POLY   | POLY  | POLY | POLY |

*(VAR ⊕̂ VAR = VAR only when both operands name the same variable,
undecidable under lexical analysis; conservatively POLY. The table
column marked VAR* records the theoretically-permissible case; the
implementation always emits POLY.)*

**Proposition (lattice homomorphism).** For any positive-RA query Q,
`σ̂(ann(t', Q))` is computable from the operator-tree of Q by local
application of `⊗̂` and `⊕̂`, *without* evaluating ann itself. Proof:
structural induction on Q, using the inductive definition above; each
operator's case corresponds to an entry in `⊗̂` or `⊕̂`. This is the
Perm-style symbolic fold applied to the shape quotient of N[X].

### Restricted dialect

The categorizer is a *gatekeeper* as well as a classifier. By default —
for non-whitelisted queries — the accepted SQL subset is a star-schema
dialect: one FROM target, dimensional lookups via ClickHouse
dictionaries (`dictGet` family), set-membership filters via IN/NOT IN,
all other SELECT mechanics allowed. This mirrors the
[Kimball star-schema][kimball-toolkit] access pattern and matches the
facts-and-dimensions model the `facts` superpower (per ADR-0050) is
built around.

**Motivation.** Under unrestricted SQL, the shape fold's
`VAR ⊗̂ VAR = POLY` rule correctly — but uninformatively — classifies
every fact-to-dimension join as (a), even when the join is genuinely
1:1 on a foreign key. Rather than reach for a catalog to prove
uniqueness, we ban the ambiguous constructs and route their workload
through `dictGet` (lookup-preserving) or through the whitelist
(§Whitelist below) for genuine multi-relation queries. The dialect is
a *productive* restriction: compliant queries classify precisely,
non-compliant queries fail loudly.

**Banned constructs** (pass `reject_disallowed_constructs`):

- `JOIN` in any form: `INNER`, `LEFT`, `RIGHT`, `FULL`, `CROSS`,
  `SEMI`, `ANTI`, `ANY`, `ASOF`, `PASTE`, and natural-join variants.
- Comma-join in FROM: `FROM a, b`.
- `ARRAY JOIN` / `LEFT ARRAY JOIN` — row-expanding, breaks VAR shape.
- `WITH RECURSIVE` — unbounded fixpoint; semantic-POLY by definition.
- Correlated subqueries — any subquery whose free variables resolve
  against an outer SELECT's scope per `BuildScopes`. Scalar-constant
  subqueries (no free variables) remain allowed.
- `UNION [ALL]` / `INTERSECT` / `EXCEPT` across branches that do not
  each independently satisfy the restricted dialect. A union of two
  `FROM facts` branches is legal; a union mixing `FROM facts` with
  `FROM other_table` is rejected.

**Allowed constructs:**

- `FROM <tbl|cte-name|(inner-restricted-select)>` — exactly one FROM
  target per SELECT layer.
- `dictGet` / `dictGetOrDefault` / `dictHas` / `dictGetHierarchy` and
  the full ClickHouse dictionary-lookup family, anywhere scalar
  expressions are permitted.
- `WHERE expr [NOT] IN (<inner-restricted-select> | <value-list>)` —
  non-correlated only.
- Non-correlated scalar subqueries in `WHERE` / `SELECT` returning a
  single row × single column.
- `WITH` (non-recursive CTEs); each CTE body must itself satisfy the
  restricted dialect.
- `GROUP BY`, `HAVING`, `ORDER BY`, `LIMIT [n] [WITH TIES]`, `OFFSET`.
- Window functions, treated per §Window functions below.

#### CTE handling

Scope resolution uses
[`nanopass_scope.go`][boxer-nanopass]'s `BuildScopes`, which
distinguishes `TableSource.IsCTE` from real tables. The categorizer
processes CTEs in dependency order (topological within each WITH block)
and propagates each CTE's `(shape, facts_id_traced)` pair to its
reference sites as if inlined.

- **Single-reference CTE.** Equivalent to inlining the CTE body at
  the reference site; shape and trace flow through unchanged.
- **Multiply-referenced CTE.** Each reference independently carries
  the CTE's `(shape, trace)`. The restricted dialect limits each
  SELECT layer to one FROM target, so two references from the same
  outer FROM (e.g., `FROM cte a JOIN cte b`) are caught by the JOIN
  ban; references split across FROM and IN are legal.
- **CTE shadowing a real table.** `WITH facts AS (...)` in an outer
  query causes inner `FROM facts` to resolve to the CTE, not the real
  base relation. `facts_id_traced` fires **only** when FROM resolves
  to the real configured `FactsRelationName`, never a shadowing CTE.
  `BuildScopes`'s `IsCTE` flag is authoritative.
- **Unused CTE.** A CTE defined but never referenced contributes
  nothing to shape or trace. The reject pass still validates its body
  against the dialect (so dead code cannot smuggle banned constructs).

#### IN-subquery handling

`WHERE expr [NOT] IN (SELECT …)` is selection σ per
[Perm R3][perm-icde09]: the inner subquery is analyzed recursively
(it must itself satisfy the restricted dialect, modulo the filter-only
relaxation below) but its shape does **not** flow to the outer query.
Correlated IN (inner references an outer-scope column or alias) is
rejected.

`WHERE col OP (SELECT …)` with scalar subquery is treated identically:
the subquery returns a value, which is a constant for outer σ.

#### Filter-only CTE relaxation

A CTE referenced *only* as the source of an IN / NOT IN subquery (or
a scalar subquery) contributes to the outer's selection predicate
`[C(t)]` but not to its annotation polynomial. Per [Perm R3][perm-icde09],
`ann(t, σ_C(Q)) = ann(t, Q) · [C(t)]` where `[C(t)] ∈ {0, 1}`: the
subquery's internal annotations multiply *the constant 0 or 1*, not
the outer's polynomial. The outer's shape is therefore independent
of the CTE's internal structure. Joins, ARRAY JOIN, EXCEPT, mixed-source
UNION — constructs dialect-banned in value-contributing positions —
may appear inside a filter-only CTE without disturbing categorization.

**Explicit opt-in.** The relaxation is not implicit; the author marks
the CTE with a pragma:

```sql
WITH
  /* @filter-only */
  overlap_cohorts AS (
      SELECT DISTINCT f.cohort_id
      FROM facts f JOIN dim_enriched d ON f.tag = d.tag    -- legal here
      WHERE d.tier = 'premium'
  )
SELECT facts_id, value
FROM facts
WHERE cohort_id IN (SELECT cohort_id FROM overlap_cohorts)
```

**Verification protocol** (pass `classify_filter_only_ctes`):

1. For each CTE marked `/* @filter-only */`, enumerate all references
   via the scope graph from [`BuildScopes`][boxer-nanopass].
2. Classify each reference as
    - *filter-only*: inside an `IN (SELECT …)` / `NOT IN (SELECT …)`
      subquery's FROM, or inside a scalar-subquery FROM;
    - *value-contributing*: outer FROM, JOIN, ARRAY JOIN, UNION branch,
      CROSS JOIN — any position where the CTE's tuples flow into the
      outer's annotation polynomial.
3. Compute the filter-only closure as a fixpoint over CTEs: a CTE is
   filter-only iff every reference is either filter-only directly or
   is inside a CTE that is itself filter-only. Monotonic; terminates
   in at most |CTEs| iterations.
4. If any reference of an opt-in CTE is value-contributing, reject
   with a diagnostic naming the reference site; do not silently fall
   back to the strict dialect (the pragma's presence is a statement
   of intent that must either verify or fail).

Inside a verified filter-only CTE, `reject_disallowed_constructs`
skips its standard bans for nodes whose enclosing CTE chain reaches
a filter-only classification.

**Still banned, even inside filter-only CTEs:**

- `WITH RECURSIVE` — unbounded fixpoint; an operational concern
  orthogonal to shape.
- Correlated subqueries — CH-planner hazard; the shape argument does
  not cover correlation semantics, and correlation inside a filter-only
  CTE is a common source of ambiguity.
- Large / unbounded `ARRAY JOIN` — DoS / planner concern. Bounded
  ARRAY JOIN (on small fixed-size array columns) may be re-admitted
  in a later ADR.

**What filter-only CTEs may do.** All standard ClickHouse SELECT
constructs otherwise permitted at CH dialect level: JOIN in every
form, comma-join, EXCEPT / INTERSECT, mixed-source UNION, arbitrary
subquery nesting (modulo the three bans above). The body is *not*
further shape-classified — it is treated as an opaque set-producing
expression whose type is `Set<tuple>` for IN membership.

**Audit trail.** `CategorizationReportLw` records each filter-only
CTE via entries in its `filter_only_ctes` list (see §Auditability):
each entry names the CTE, the pragma's CST offset, the verified
reference sites, and the admitted constructs. A reviewer can walk
this list to confirm the relaxation was applied correctly and no
value-contributing reference slipped through.

#### Window functions

Row-preserving window functions retain VAR through the outer
projection; value-producing window aggregates behave like γ and lift
the outer layer to POLY.

| construct | shape propagation | rule |
|--|--|--|
| `row_number() OVER (…)`, `rank() OVER (…)`, `dense_rank() OVER (…)` | passthrough | row-preserving per [GKT 2007 §4][gkt07] |
| `lag(x) OVER (…)`, `lead(x) OVER (…)`, `first_value` / `last_value` | passthrough | row-preserving |
| `agg(x) OVER ()`, `agg(x) OVER (ORDER BY …)`, `agg(x) OVER (PARTITION BY …)` for value-producing aggregates (`sum`, `avg`, `count`, etc.) | POLY | [Perm R5][perm-icde09]; aggregate semantics per [ADT11 §3][adt11] |

Synthetic columns from row-preserving window functions do not disrupt
`facts_id_traced`: the predicate walks outer-projection columns and
succeeds if any of them resolves to `facts.facts_id`.

### Whitelist for new-fact-producing data products

Queries that legitimately require banned constructs — genuine JOINs,
self-joins for as-of-time reasoning, cross-stream correlations — are
registered as named **data products** in a static whitelist.
Whitelisted queries always classify as (c) analytical-with-lineage;
they must produce new `facts` rows with explicit provenance to source
facts rows. There is no path from whitelist admission to (a) or (b).

#### Registry format

The registry is a YAML file under version control
(`public/.../categorizer/whitelist.yaml` by default). Entries:

```yaml
- id: daily_cohort_rollup              # unique, human-chosen identifier
  version: 1                           # append-and-supersede, never edit-in-place
  query_sha256: abc123…                # sha256 of canonicalized SQL text
  declared_lineage_column: __lineage__ # must match the actual alias in the query
  declared_source_relations: [facts]   # base relations the query reads; lineage targets
  owner: p@stergiotis
  rationale: >
    Cross-fact cohort join for daily rollup; produces new fact rows
    with facts_id provenance to both cohort and comparison rows.
  created_at: 2026-04-21
```

Adding or modifying entries is a reviewed PR; the `rationale` field is
the human-readable justification the reviewer signs off on.

#### Activation pragma

A leading SQL comment declares the data-product identity:

```sql
-- @data-product: daily_cohort_rollup
WITH last_month AS (
    SELECT facts_id, cohort, ts, value
    FROM facts
    WHERE ts > now() - INTERVAL 30 DAY
)
SELECT a.cohort AS cohort,
       groupArray(a.facts_id)   AS __lineage__,
       avg(b.value - a.value)    AS avg_delta
FROM last_month a
JOIN last_month b ON a.cohort = b.cohort AND b.ts > a.ts
GROUP BY cohort
```

#### Verification protocol

On seeing `-- @data-product: <id>`, the categorizer:

1. Strips the `@data-product` pragma itself; canonicalizes the
   remainder via the `canonicalize_*` pass chain.
2. Computes `sha256(canonicalized_text)`.
3. Looks up `<id>` in the whitelist registry.
4. Verifies `computed_sha256 == registry[id].query_sha256`. A mismatch
   (query text has drifted since registration) yields a diagnostic
   naming the expected and computed hashes and halts.
5. On match: sets `whitelist_mode=true`, which causes
   `reject_disallowed_constructs` to pass the query through
   unchanged. Shape, trace, and lineage passes then proceed normally.
6. After shape analysis: rejects with diagnostic if
    - final shape is not POLY (whitelisted queries that do not
      aggregate are suspicious — probably should not have been
      whitelisted), or
    - `has_valid_lineage_column` is false (whitelisted queries must
      carry lineage), or
    - the lineage column's underlying expression does not reference
      any of `declared_source_relations` (prevents lineage-to-nowhere).
7. Emits category (c) with `whitelist_id`, `whitelist_version`, and
   `query_sha256` recorded in `CategorizationReportLw`.

#### Governance

Whitelist entries are the principal privileged operation in the
categorizer. Reviewing a PR that adds or supersedes a whitelist entry
is equivalent to admitting a new data product: the reviewer verifies
the query's correctness, confirms the lineage claim matches the
query's semantics, and weighs whether the resulting new facts are
worth the `facts`-schema extension they may require (per ADR-0050's
schema-extension discipline).

Whitelist mutations are **append-and-supersede**, never edit-in-place.
To revise a data product's query, register `daily_cohort_rollup v2`
with the new hash; leave `v1` in place until no archived outputs
reference it. This preserves audit traceability for historical `facts`
rows to the exact query that produced them.

### Facts-trace predicate

Shape alone doesn't distinguish (b) from (a) when multiple base
relations are involved: `SELECT x_t FROM other_table` has shape VAR
but does not reach `facts`. We augment with a **Boolean lexical
predicate**:

```
facts_id_traced(AST) = true iff
    there exists an output-projected column c such that
    c's syntactic lineage through inlined subqueries terminates in
    the identifier `facts_id` resolved against a FROM table whose
    canonical name is `facts` (defaultDB-resolved per boxer
    nanopass BuildScopes).
```

This predicate is purely syntactic (scope resolution from
[`public/db/clickhouse/dsl/nanopass/nanopass_scope.go`][boxer-nanopass];
no catalog). Aliases, CTE references, and subquery-derived tables
are resolved through the scope tree.

**Lineage predicate** (for (c)): we detect lineage columns by explicit
user annotation rather than type-shape analysis (which would require
schema). An output column is a *lineage column* iff either:

- it is aliased `__lineage__` (reserved convention), **or**
- a preceding SQL comment declares `-- @lineage: <column-alias>`,
  and `<column-alias>` names the column.

The lineage predicate additionally verifies that the column's
syntactic form is `groupArray(facts.facts_id)` or
`groupUniqArray(facts.facts_id)` (or same against a resolved
`facts` alias). Any other form invalidates the lineage claim and
downgrades categorization to (a).

### Decision rule

```
categorize(ast, whitelist_mode) :=
  let shape   = σ̂(ast)                         # shape-lattice fold
  let traced  = facts_id_traced(ast)           # lexical predicate
  let lineage = has_valid_lineage_column(ast)  # lexical predicate

  if whitelist_mode:
    # strict: whitelisted queries must aggregate with declared lineage
    require shape == POLY                      # else reject (§Whitelist)
    require lineage                            # else reject (§Whitelist)
    → (c) analytical-with-lineage

  match (shape, traced, lineage):
    (VAR,  true,  _    ) → (b) data mart
    (POLY, _,     true ) → (c) analytical-with-lineage
    (_,    _,     _    ) → (a) opaque
```

The mapping is deliberately coarse: VAR without `facts_id` traced is
(a) (we cannot key into `facts`); POLY without a lineage column is
(a) (we have no provenance to record). No query is silently promoted.
Whitelist mode never downgrades — it either admits the query as (c)
or rejects it outright.

### Worked examples

All examples below are in the restricted dialect (single FROM, no
JOINs) unless explicitly marked as whitelisted.

| # | Query | shape fold | traced | lineage | category | rule cited |
|--|--|--|--|--|--|--|
| 1 | `SELECT facts_id, name FROM facts` | VAR (R1+R2) | true | — | (b) | Perm R1, R2 |
| 2 | `SELECT facts_id, name FROM facts WHERE cohort='x'` | VAR | true | — | (b) | Perm R3 |
| 3 | `SELECT facts_id, dictGet('dept_dim','region',dept_id) AS region FROM facts` | VAR | true | — | (b) | Perm R2 (dictGet is a scalar fn, introduces no variable) |
| 4 | `SELECT facts_id, value FROM facts WHERE region IN (SELECT name FROM region_whitelist)` | VAR | true | — | (b) | Perm R3 (σ with IN subquery) |
| 5 | `SELECT facts_id FROM facts WHERE value > (SELECT avg(value) FROM facts)` | VAR | true | — | (b) | Perm R3 (scalar subquery = constant for outer σ) |
| 6 | `WITH cohort AS (SELECT facts_id, segment FROM facts WHERE cohort_id='x') SELECT facts_id, value FROM facts WHERE facts_id IN (SELECT facts_id FROM cohort)` | VAR | true | — | (b) | Perm R1, R2, R3; CTE inlining (§CTE handling) |
| 7 | `SELECT uniq(id) FROM facts` | POLY (γ scalar) | false | false | (a) | ADT11 §3; Perm R5 degenerate |
| 8 | `SELECT bucket, count() FROM facts GROUP BY bucket` | POLY | false | false | (a) | Perm R5 |
| 9 | `SELECT bucket, groupArray(facts_id) AS __lineage__, count() FROM facts GROUP BY bucket` | POLY | false | true | (c) | Perm R5 + §Lineage predicate |
| 10 | `WITH daily AS (SELECT toDate(ts) AS day, sum(value) AS total FROM facts GROUP BY day) SELECT day, total FROM daily` | POLY (γ inside CTE) | false | false | (a) | Perm R5 propagated via CTE inlining |
| 11 | `SELECT 1` | CONST | false | false | (a) | trivial |
| 12 | `SELECT facts_id, e.bonus FROM facts f JOIN enrich e ON f.id=e.id` | — | — | — | **rejected** | `reject_disallowed_constructs` on JOIN (§Restricted dialect) |
| 13 | `-- @data-product: daily_cohort_rollup`<br>`WITH lm AS (SELECT facts_id, cohort, ts, value FROM facts WHERE ts > now() - INTERVAL 30 DAY)`<br>`SELECT a.cohort, groupArray(a.facts_id) AS __lineage__, avg(b.value - a.value) AS avg_delta FROM lm a JOIN lm b ON a.cohort=b.cohort AND b.ts>a.ts GROUP BY cohort` | POLY (γ over self-join) | — | true | (c) | Whitelist admit + Perm R5 + §Lineage |
| 14 | `WITH /* @filter-only */ overlap AS (SELECT DISTINCT f.cohort_id FROM facts f JOIN dim_enriched d ON f.tag=d.tag WHERE d.tier='premium')`<br>`SELECT facts_id, value FROM facts WHERE cohort_id IN (SELECT cohort_id FROM overlap)` | VAR (outer σ passthrough) | true | — | (b) | Perm R3 + §Filter-only CTE relaxation |
| 15 | `WITH /* @filter-only */ overlap AS (…joins…)`<br>`SELECT o.cohort_id FROM overlap o` | — | — | — | **rejected** | `classify_filter_only_ctes`: opt-in CTE referenced in outer FROM (value-contributing) |

Notes:

- Rows 3–6 are the practical star-schema patterns (dimensional lookup,
  cohort filter, scalar-aggregate threshold, CTE-prep filter). All
  classify (b) deterministically.
- Row 10 shows the aggregate-collapse-through-CTE case: the CTE body
  aggregates, the outer passes through, and the final category is (a)
  — correctly, because daily totals are not `facts` rows.
- Row 12 is the former "JOIN fallback to (a)" case; under the
  restricted dialect the query is rejected at `reject_disallowed_constructs`
  rather than silently archived as (a). The author must either
  restructure via `dictGet` / `IN` (row 4 / row 14), register the
  query as a data product (row 13), or mark the enrichment as a
  filter-only CTE (row 14).
- Row 13 shows the whitelist path: a self-join over a CTE
  materializing a facts window, aggregated, with declared lineage.
  Admits as (c); emits new `facts` rows tagged with provenance edges
  to both `a.facts_id` and `b.facts_id` per the lineage column.
- Row 14 shows the filter-only CTE relaxation: the CTE contains a
  JOIN that would normally be rejected, but because the CTE is used
  *only* as the source of an `IN (SELECT …)` predicate, its shape
  does not influence the outer annotation polynomial. Outer remains
  VAR → (b). The `classify_filter_only_ctes` pass verifies the
  reference pattern before `reject_disallowed_constructs` stands down.
- Row 15 is the failure mode for the opt-in: the author declared
  `@filter-only` but referenced the CTE as an outer FROM target.
  The pragma's intent contradicts the actual usage; rejection is
  immediate, not a silent fall-through to the strict dialect.

### Algorithm (nanopass pipeline)

Passes compose at the `public/db/clickhouse/dsl/nanopass` layer.
All passes are CST-level (ANTLR4); they return valid SQL or a terminal
report.

| # | Pass | Kind | Input | Output | Rule source |
|---|------|------|-------|--------|-------------|
| 1 | `parse` | existing | SQL text | `ParseResult` (ANTLR4 CST) | nanopass framework |
| 2 | `canonicalize_*` | existing | CST | CST | nanopass framework (sugar removal, paren removal, keyword case) |
| 3 | `build_scopes` | existing | CST | `[]SelectScope` | nanopass framework (`nanopass_scope.go`) |
| 4 | `check_whitelist` | new | CST + `-- @data-product:<id>` pragma | CST + `whitelist_mode : Bool`, `whitelist_id`, `whitelist_version`, `query_sha256` | this ADR §Whitelist |
| 5 | `classify_filter_only_ctes` | new | CST + scopes + `/* @filter-only */` pragmas | CST + per-CTE `filter_only : Bool` with verified reference sites; rejection on mismatched opt-in | this ADR §Filter-only CTE relaxation |
| 6 | `reject_disallowed_constructs` | new | CST + `whitelist_mode` + `filter_only` tags | CST unchanged, or terminal rejection | this ADR §Restricted dialect |
| 7 | `tag_base_refs` | new | CST + scopes | CST + annotation: each `FROM` item tagged with resolved base-relation name (or CTE body) | — |
| 8 | `tag_shape` | new | tagged CST | CST + per-node shape ∈ `Sh` | Perm R1–R9 + `⊗̂`/`⊕̂` |
| 9 | `tag_facts_trace` | new | shape-tagged CST | CST + `facts_id_traced : Bool` | this ADR §Facts-trace |
| 10 | `tag_lineage` | new | trace-tagged CST | CST + `has_valid_lineage_column : Bool` | this ADR §Lineage |
| 11 | `categorize` | new | fully-tagged CST + `whitelist_mode` | `CategorizationReportLw` (terminal) | this ADR §Decision rule |
| 12 | `rewrite` | new | `CategorizationReportLw` + CST | emitted SQL per ADR-0050 staging | per-category (see ADR-0050 §Decision) |

**Rule count bounds.** Pass 8 (`tag_shape`) implements the
Perm-derived fold. Rows marked *(whitelist only)* are unreachable in
default-dialect queries because `reject_disallowed_constructs` fires
first; they remain in the table for the whitelist path and for the
bodies of verified filter-only CTEs (where the reject pass also
stands down).

| Perm rule | CH construct | Shape propagation | Reachable in |
|-----------|--------------|-------------------|--------------|
| R1 base relation | `FROM tbl` | `VAR` | both |
| R2 projection π | `SELECT exprs` | passthrough of inner shape; `CONST` if all exprs are literals/constants | both |
| R3 selection σ | `WHERE ...` (incl. `IN (subq)`, scalar subq) | passthrough | both |
| R4 cross product × | `FROM a, b`, `CROSS JOIN`, comma-join | `⊗̂` on operand shapes | *(whitelist only)* |
| R4 inner/left/right join | `INNER/LEFT/RIGHT JOIN ... ON` | `⊗̂` (LEFT/RIGHT treated identically — null-padding is value-level not shape-level) | *(whitelist only)* |
| R5 aggregation γ | `SELECT agg(...) [GROUP BY ...]` | `POLY` (always, including scalar aggregate) | both |
| R5 window aggregate | `agg(x) OVER (…)` value-producing | `POLY` | both |
| — window row-preserving | `row_number / rank / lag / lead OVER (…)` | passthrough | both |
| R6 union ∪ | `UNION ALL`, `UNION` | `⊕̂` | both (branch-dialect-checked) |
| R7 intersection ∩ | `INTERSECT` | `⊗̂` (conservative: behaves like bag-join) | *(whitelist only; banned in dialect)* |
| R8/R9 difference − | `EXCEPT`, `MINUS` | `⊕̂` (conservative over-approximation; see *Limits*) | *(whitelist only; banned in dialect)* |
| — DISTINCT ε | `SELECT DISTINCT`, `uniqExact()` | `POLY` if operand is `VAR` over non-key, else passthrough (conservative) | both |
| — `arrayJoin` / `UNNEST` | CH-specific row expansion | `POLY` | *(whitelist only; banned in dialect)* |
| — CTE reference | `WITH name AS (...)` reference | passthrough of the CTE body's final shape | both |
| — Subquery in FROM | `FROM (SELECT ...)` | passthrough of the inner SELECT's final shape | both |
| — Scalar subquery in WHERE/SELECT | `WHERE x = (SELECT …)` non-correlated | scoped out — inner does not affect outer shape | both |
| — IN subquery | `WHERE x [NOT] IN (SELECT …)` non-correlated | outer = σ passthrough; inner analyzed recursively | both |

**Pass budget.** Sixteen shape rules in `tag_shape` (of which nine
map to Perm R1–R9 plus seven CH-specific extras). Default-dialect
execution exercises roughly half of them (the non-whitelist-only
rows); the remainder are reachable only via `-- @data-product` or
inside a verified `/* @filter-only */` CTE. Three gatekeeper passes —
`check_whitelist` (#4), `classify_filter_only_ctes` (#5),
`reject_disallowed_constructs` (#6) — all thin. Eight passes new in
total.

### Auditability contract

Every `CategorizationReportLw` record produced by `categorize` includes:

```
record CategorizationReportLw {
    query_id: string;
    category: enum { opaque, data_mart, analytical_with_lineage };
    final_shape: enum { CONST, VAR, POLY };
    facts_id_traced: bool;
    lineage_column: optional string;
    whitelist_id: optional string;              // set iff admitted via §Whitelist
    whitelist_version: optional uint32;         // registry version at admission
    query_sha256: optional []byte;              // 32 bytes; canonicalized-text hash
    dialect_mode: enum { restricted, whitelisted };
    filter_only_ctes: []FilterOnlyCteLw;        // opt-in CTEs verified filter-only
    shape_derivation: []ShapeDerivationStepLw;  // one per CST node
    rule_citations: []RuleCitationLw;           // Perm R#, ADT11 §, this ADR §
}

record ShapeDerivationStepLw {
    cst_node_id: string;                       // position in CST (token offsets)
    operator: string;                          // SELECT, JOIN, GROUP BY, etc.
    child_shapes: []string;                    // from child nodes
    emitted_shape: string;
    rule_id: string;                           // "Perm.R4", "ADT11.Prop3.2", "ADR-0051.§Facts-trace"
}

record FilterOnlyCteLw {
    cte_name: string;                          // as declared in WITH
    pragma_cst_offset: string;                 // location of /* @filter-only */
    reference_sites: []string;                 // CST offsets of each reference, all verified filter-only
    admitted_constructs: []string;             // e.g., "INNER JOIN at <offset>", "EXCEPT at <offset>"
}

record RuleCitationLw {
    rule_id: string;
    source: string;                            // "Glavic-Alonso ICDE 2009, Fig. 3"
    justification: string;                     // one-liner for reviewer
}
```

A reviewer can walk `shape_derivation` in CST order, verify each
`rule_id` against the paper cited in `RuleCitationLw`, and independently
re-derive `category`. No step is opaque; no step depends on runtime
state.

## Alternatives

The QOC matrix carries the comparative assessment. Nuance:

- **O1 — Ad-hoc heuristics.** Fails on
  `SELECT uniq(id) FROM facts`: the heuristic "FROM facts and
  selects from facts columns" labels it (b), but the scalar aggregate
  collapses the row cardinality and the result is not a `facts` row.
  The semiring fold (O4) gives the right answer because R5 maps
  aggregation to POLY regardless of the FROM clause. Rejected.
- **O2 — Cardinality-class analysis.** Disambiguates the aggregate
  case but has no theoretical backing; extensions for EXCEPT, outer
  joins, and nested subqueries accrete per-case rules with no unifying
  framework. Rejected as "correct until it's not, and we can't tell
  when that is."
- **O3 — Full semiring-annotated rewrite.** Implementing the
  [ADT11][adt11] tensor K⊗M and [ProvSQL 2025][provsql-arxiv25]
  m-semiring for EXCEPT gives a complete provenance annotation
  algebra; if we ever wanted to *compute* provenance at runtime
  (rather than classify), this is the path. For classification, the
  shape lattice is a sound abstraction of the full algebra — all
  theoretically-interesting distinctions between polynomial forms
  collapse into the single POLY equivalence class. Rejected as
  over-specification until a use case for runtime provenance appears.
  [ProvSQL 2018][provsql-vldb18] and [Perm][perm-icde09] remain the
  upgrade path.
- **O5 — User-declared pragma.** Shifts the auditability burden to
  the caller with no verification. Accepted as a reviewer-override
  mechanism layered *on top of* O4 (a pragma can force-downgrade a
  categorization to (a) for paranoid operators), but not as the
  primary mechanism.

## Consequences

### Positive

- Every category decision is traceable to a named, peer-reviewed rule.
  A reviewer opening `CategorizationReportLw` can read the
  `shape_derivation` and confirm each step against
  [Glavic-Alonso 2009][perm-icde09] Fig. 3, [GKT 2007][gkt07] §4, or
  this ADR's §Facts-trace / §Lineage.
- Nanopass rule count is bounded at twelve (nine Perm + three CH
  extras) for the shape fold, plus two auxiliary predicate passes.
  Extending to new CH SELECT features means one new row in the shape
  table or one new CST pattern in the trace predicate.
- Categorization does not execute SQL. No CH round trip, no planner
  invocation. The pipeline is a pure function of SQL text.
- The `CONST` / `VAR` / `POLY` abstraction is a sound semiring
  homomorphism (proposition above) — its decisions are conservative
  but never wrong: a POLY-classified query that is "really" VAR (same
  variable on both sides of a union) is safely routed to (a) or (c),
  never to (b) with a false 1:1 claim.
- Escape hatch is a small pragma, not a re-architecture. Operators
  and query authors can force-downgrade via `-- @category: opaque`
  without touching the categorizer.
- The `/* @filter-only */` CTE relaxation lets authors define
  multi-table filter conditions (joins to dimensions, EXCEPT
  against cohort blacklists) without triggering a whitelist review.
  The outer query classifies (b) deterministically; the extra
  expressiveness is confined to set-producing positions where Perm R3
  says the inner tuples can't affect the outer shape. This addresses
  the most common "why does my legitimate (b) query need a whitelist
  entry?" complaint without weakening auditability — the opt-in
  pragma + `classify_filter_only_ctes` verification keeps the
  relaxation traceable.

### Negative

- The restricted dialect bans JOIN, ARRAY JOIN, comma-join, and
  correlated subqueries outright rather than silently archiving them
  as (a). Authors of enrichment-join queries must either restructure
  with `dictGet` + IN (the common case under a star schema) or
  register the query as a whitelisted data product. The friction is
  intentional — it surfaces non-star access patterns for review
  rather than hiding them in the (a) bucket.
- Whitelist admission is manual. Each genuinely-join-requiring data
  product is a PR + owner sign-off. For a project that produces
  many ad-hoc analytical queries, this may become a bottleneck;
  mitigation is to expose a self-service admission flow for
  well-understood query templates, but that is out of scope for this
  ADR.
- The `facts_id_traced` predicate is name-based. Renames of the
  `facts` primary-key column require a coordinated change to the
  categorizer config. This is a small but real coupling.
- Lineage detection requires explicit user annotation (reserved alias
  or pragma). Queries that would be (c) by shape but omit the
  annotation are classified (a). Intentional: we refuse to infer
  lineage from `Array(UInt64)` shape without schema.
- The POLY equivalence class is broad. Inside a whitelisted query, a
  self-join of `facts` on a key that is genuinely 1:1
  (`facts ⋈ facts ON facts_id`) folds to POLY via the ⊗̂ table;
  without tracking variable identity we cannot recover that the
  result is actually VAR. Whitelist admission routes the query to
  (c) regardless, so the coarseness is harmless in practice.
- Dictionary lookups via `dictGet` sidestep the shape fold (they are
  scalar functions, not relations), but their returned values depend
  on the dictionary's state at query time. For full audit / replay,
  the archive layer must record the dictionary version hash per query
  touched. Earmarked for a follow-up ADR on dictionary versioning.

### Neutral

- Leeway will define `CategorizationReportLw`, `ShapeDerivationStepLw`,
  and `RuleCitationLw` (Arrow-schema-bound audit records). These are
  categorization outputs; they do not flow to NATS and do not appear
  in the hot data plane of ADR-0050.
- The rewrite pass (#12) is a separable concern. It consumes the
  categorization report and emits the category-specific
  `INSERT INTO staging_result` SQL per ADR-0050. If the staging-table
  design in ADR-0050 changes, only pass 12 is affected; passes 1–11
  are category-semantics-only.
- EXCEPT / INTERSECT are banned in the restricted dialect (they need
  ProvSQL's m-semiring monus for precise semantics and would
  conservatively fold to POLY anyway). Users who want 1:1 set
  subtraction rewrite with `NOT IN` (which stays in the restricted
  dialect as non-correlated σ). Whitelist admission retains the
  constructs for data-product use; their shape in whitelist mode is
  `⊕̂`/`⊗̂` per the propagation table.
- The restricted dialect is intentionally narrower than the full
  ClickHouse SELECT grammar. Introducing new allowed constructs is
  cheap (one table row + optional pass rule) but is an ADR-scope
  decision, not a silent evolution. Supersede with ADR-00XX when the
  dialect's scope needs to grow.

## Limits of the theory (explicit cautions)

The ADR's soundness rests on four non-trivial results in the cited
literature. Each deserves an auditable citation in the codebase and
in training material for reviewers:

- **Aggregate impossibility.** [ADT11 Prop. 3.2][adt11]: there is no
  K-relation semantics for SUM/MAX/MIN aggregates that is
  simultaneously set-compatible, commutes with semiring homomorphisms,
  and factors through N[X]. *Our use:* we classify aggregate outputs
  as POLY unconditionally, which ADT11 §3 confirms is sound — we
  never attempt to compute the aggregate's provenance value, so the
  tensor K⊗M construction is unnecessary.
- **Set-difference semantics.** [ProvSQL 2025 §III][provsql-arxiv25]
  notes that the standard bag EXCEPT ALL has no complete semiring
  semantics; ProvSQL resolves via m-semiring monus, which we skip.
  *Our use:* EXCEPT / INTERSECT are dialect-banned in the default
  path (users rewrite with `NOT IN` / `IN`), and in whitelist mode
  fold conservatively to POLY via `⊕̂` / `⊗̂`. The precision loss
  applies only to whitelisted data products and is acceptable there.
- **Nested aggregates.** [ADT11 §4][adt11] handles nested aggregates
  via iterated tensor products. *Our use:* always POLY; no
  finer-grained analysis. Nested aggregates are rare in observability
  queries and the worst case is mis-classification to (a), which is
  safe (correct archival, just not category-optimal).
- **Outer-join null padding.** Perm R8/R9 ([Glavic 2010
  thesis][glavic-thesis] Ch. 4) handle NULL-padded tuples on the
  preserved side with a left-outer-join trick in the rewrite. *Our
  use:* NULL padding is a value-level concern, not a shape-level
  one; LEFT/RIGHT OUTER JOIN are treated identically to INNER JOIN
  for shape propagation (`⊗̂`). Soundness argument: null-padded
  tuples carry the same annotation structure as the preserved-side
  tuple they extend, so the shape class is preserved.

## Implementation outline

> Informational appendix. Implementation details may evolve without
> superseding this ADR provided the decision above stands. When a
> detail below turns into a cross-cutting choice of its own, spin it
> out as a new ADR and link back here.

### Module layout

- `public/.../categorizer/` — new Go package.
  - `categorizer.go` — pipeline wiring; `Categorize(sql string) (*CategorizationReportLw, error)`.
  - `shape.go` — `Sh` type, `⊗̂`, `⊕̂` tables, propagation.
  - `whitelist_registry.go` — YAML registry loader; hash verification.
  - `canonicalize.go` — canonicalization helper that hashes SQL text identically to the registry pre-image (strips `@data-product` pragma, runs `canonicalize_*`, re-serializes).
  - `pass_check_whitelist.go` — #4.
  - `pass_classify_filter_only_ctes.go` — #5.
  - `pass_reject_disallowed_constructs.go` — #6.
  - `pass_tag_base_refs.go` — #7.
  - `pass_tag_shape.go` — #8.
  - `pass_tag_facts_trace.go` — #9.
  - `pass_tag_lineage.go` — #10.
  - `pass_categorize.go` — #11.
  - `pass_rewrite.go` — #12 (emits ADR-0050 staging SQL).
- `public/.../categorizer/whitelist.yaml` — registry of
  admitted data products.
- `public/.../categorizer/testdata/` — golden-file corpus of
  SQL → expected `CategorizationReportLw`, checked against published
  rules. Must include: every restricted-dialect example above, one
  rejected-by-`reject_disallowed_constructs` case per banned
  construct, at least one whitelisted data product with its registry
  entry, and a hash-mismatch case that exercises the drift
  diagnostic.

### Interaction with `public/db/clickhouse/dsl/nanopass`

The nanopass framework treats every pass as SQL → SQL. Our passes
#4 (`check_whitelist`), #5 (`classify_filter_only_ctes`),
#6 (`reject_disallowed_constructs`), and #7–#10 (the `tag_*` chain)
annotate the CST with their findings; in the nanopass model, this
metadata rides as structured SQL comments inserted at pass #4 and
read by subsequent passes. Pass #11 (`categorize`) is a terminal:
it consumes the annotated SQL and emits `CategorizationReportLw`
out-of-band (returning the unchanged SQL so that a downstream
rewrite pass or a dead-end pass can proceed).

If the annotation-via-comments approach proves unergonomic, an
alternative is to fork the nanopass pipeline API to allow passes that
produce side-channel artifacts in addition to the SQL string. This
is a framework change; out of scope for this ADR.

### Configuration

- `FactsRelationName` (default `"facts"`) — the canonical fact-table
  name against which `facts_id_traced` resolves.
- `FactsIdColumnName` (default `"facts_id"`) — the primary-key column
  used for VAR→(b) promotion.
- `LineageAliasConvention` (default `"__lineage__"`) — reserved column
  alias for lineage.
- `LineagePragmaPattern` (default `"^--\\s*@lineage:\\s*(\\w+)\\s*$"`) —
  regex for the pragma form.
- `OverridePragmaPattern` (default `"^--\\s*@category:\\s*(opaque|data_mart|analytical_with_lineage)\\s*$"`) —
  O5-style user override; when present, `categorize` emits the
  declared category and sets `shape_derivation` to a single
  `RuleCitationLw` pointing at this ADR §Alternatives O5.
- `DataProductPragmaPattern` (default `"^--\\s*@data-product:\\s*(\\w+)\\s*$"`) —
  regex matched by `check_whitelist`.
- `WhitelistRegistryPath` (default `"whitelist.yaml"` relative to the
  categorizer package) — file path to the data-product registry.
- `WhitelistHashAlgorithm` (fixed to `"sha256"`) — the canonicalized-text
  hash algorithm; fixed rather than configurable to keep the
  registry's hash values portable.
- `FilterOnlyPragmaPattern` (default `"^\\s*/\\*\\s*@filter-only\\s*\\*/\\s*$"`) —
  regex for the CTE-level opt-in pragma matched by
  `classify_filter_only_ctes`.

### Golden-file test corpus

Each file: `{name}.sql` + `{name}.report.json`. The categorizer runs
on the SQL and the emitted `CategorizationReportLw` is diffed against
the golden JSON. A CI check fails on any mismatch. Corpus must
include:

- Every query in §Worked examples above (rows 1–13).
- One example per Perm rule R1–R9.
- Every CH construct in the shape-propagation table (both
  always-reachable and whitelist-only rows).
- One rejected-by-`reject_disallowed_constructs` example per banned
  construct (JOIN, ARRAY JOIN, comma-join, correlated subquery,
  `WITH RECURSIVE`, mixed-source union).
- At least one example where the override pragma changes the category.
- At least one whitelisted data product with its registry entry, and
  a hash-mismatch variant exercising the drift diagnostic.
- At least one CTE-shadowing-`facts` case confirming the real-table
  trace does not fire on a CTE named `facts`.
- At least one verified `/* @filter-only */` CTE admitting a JOIN in
  its body (row 14), and one mismatched-opt-in case where the CTE is
  referenced value-contributingly and `classify_filter_only_ctes`
  rejects (row 15).

## Status

Proposed — 2026-04-21. Awaiting review by `p@stergiotis`.
Implementation to follow once accepted.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

### Theory

- <a id="gkt07"></a>**[gkt07]** Green, T.J., Karvounarakis, G., Tannen, V.
  "[Provenance Semirings](https://doi.org/10.1145/1265530.1265535),"
  PODS 2007 (DOI:10.1145/1265530.1265535). Foundational N[X]
  provenance algebra; §3 defines K-relations, §4 defines ann by
  induction over RA⁺. *Load-bearing for this ADR:* the inductive
  definition of ann and Proposition 4.6 (faithfulness of N[X]).
- <a id="bkt01"></a>**[bkt01]** Buneman, P., Khanna, S., Tan, W.C.
  "[Why and Where: A Characterization of Data Provenance](https://doi.org/10.1007/3-540-44503-X_20),"
  ICDT 2001 (DOI:10.1007/3-540-44503-X_20). Origin of the
  why/where-provenance distinction that motivates the VAR vs. POLY
  split.
- <a id="cct09"></a>**[cct09]** Cheney, J., Chiticariu, L., Tan, W.C.
  "[Provenance in Databases: Why, How, and Where](https://doi.org/10.1561/1900000006),"
  Foundations and Trends in Databases, 2009 (DOI:10.1561/1900000006).
  Canonical survey; Chapter 3 covers the semiring framework at
  reviewer-friendly depth.
- <a id="adt11"></a>**[adt11]** Amsterdamer, Y., Deutch, D., Tannen, V.
  "[Provenance for Aggregate Queries](https://doi.org/10.1145/1989284.1989302),"
  PODS 2011 (DOI:10.1145/1989284.1989302;
  [arXiv:1101.1110](https://arxiv.org/abs/1101.1110)).
  *Load-bearing:* Proposition 3.2 (impossibility) and §3 (simple
  aggregate semantics) justify our blanket POLY classification of
  aggregates.

### Operational systems

- <a id="perm-icde09"></a>**[perm-icde09]** Glavic, B., Alonso, G.
  "[Perm: Processing Provenance and Data on the Same Data Model through Query Rewriting](https://doi.org/10.1109/ICDE.2009.113),"
  ICDE 2009 (DOI:10.1109/ICDE.2009.113). *Load-bearing:* Figure 3 is
  the rule set R1–R9 cited throughout this ADR. Figure 4 shows a
  fully-rewritten example. §5 gives TPC-H overhead numbers (relevant
  only to runtime provenance, not this ADR's classification use).
- <a id="glavic-thesis"></a>**[glavic-thesis]** Glavic, B.
  "[Perm: Efficient Provenance Support for Relational Databases](http://cs.iit.edu/~dbgroup/assets/pdfpubls/G10a.pdf),"
  PhD thesis, University of Zurich, 2010. Full treatment of R1–R9
  including outer-join and set-difference cases; distinguishes PI-CS
  (influence) from C-CS (copy) provenance. *Load-bearing:* Chapter 4
  §4.3 on outer joins justifies our shape-level treatment of LEFT/RIGHT.
- <a id="provsql-vldb18"></a>**[provsql-vldb18]** Senellart, P., Jachiet, L.,
  Maniu, S., Ramusat, Y.
  "[ProvSQL: Provenance and Probability Management in PostgreSQL](https://doi.org/10.14778/3229863.3236253),"
  VLDB 2018 (DOI:10.14778/3229863.3236253;
  [HAL](https://inria.hal.science/hal-01851538)).
  Original operational description of ProvSQL.
- <a id="provsql-arxiv25"></a>**[provsql-arxiv25]** Sen, D., Maniu, S.,
  Senellart, P.
  "[ProvSQL: A General System for Keeping Track of the Provenance and Probability of Data](https://arxiv.org/abs/2504.12058),"
  arXiv:2504.12058, April 2025. Current canonical write-up; §III
  covers the extended algebra, §IV.C gives rewrite rules R1–R5, §VI
  gives benchmarks. *Load-bearing:* R4 (monus) justifies our
  conservative EXCEPT treatment.
- [ProvSQL GitHub repository](https://github.com/PierreSenellart/provsql)
  — reference implementation for probabilistic-DB upgrade path.

### Framework

- <a id="boxer-nanopass"></a>**[boxer-nanopass]**
  `public/db/clickhouse/dsl/nanopass/README.md` — the
  CST-based pass framework this ADR's passes plug into. Passes
  receive and return valid SQL; scope resolution is provided by
  `BuildScopes`.
- Keep, A.W., Dybvig, R.K.
  "[A Nanopass Framework for Commercial Compiler Development](https://doi.org/10.1145/2500365.2500618),"
  ICFP 2013 (DOI:10.1145/2500365.2500618). Original nanopass paper;
  referenced for pipeline-composition discipline.

### Data modeling

- <a id="kimball-toolkit"></a>**[kimball-toolkit]** Kimball, R.,
  Ross, M. *The Data Warehouse Toolkit: The Definitive Guide to
  Dimensional Modeling*, 3rd ed., Wiley, 2013 (ISBN 978-1118530801).
  Foundational reference for star-schema dimensional modeling; the
  fact-and-dimension pattern this ADR's restricted dialect enforces
  lexically. *Load-bearing:* Chapters 1–3 motivate the
  one-fact-one-query constraint; Chapter 2's dimension-enrichment
  pattern maps to our `dictGet` allowance.

### Internal

- [ADR-0050](0050-clickhouse-observability-pipeline.md) —
  transport and archive decisions this ADR's rewrite pass (#12)
  targets.

[boxer-nanopass]: #boxer-nanopass
[gkt07]: #gkt07
[bkt01]: #bkt01
[cct09]: #cct09
[adt11]: #adt11
[perm-icde09]: #perm-icde09
[glavic-thesis]: #glavic-thesis
[provsql-vldb18]: #provsql-vldb18
[provsql-arxiv25]: #provsql-arxiv25
[kimball-toolkit]: #kimball-toolkit
