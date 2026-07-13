---
type: adr
status: accepted
date: 2026-07-13
reviewed-by: "@spx"
reviewed-date: 2026-07-13
---

> **Status: accepted (2026-07-13).** Built and committed — see the Status section.

# ADR-0117: passthrough-table classifier for query triage

## Status

Accepted. v1 implemented and committed: `ExtractPassthroughTables`
in `public/db/clickhouse/dsl/nanopass/analysis`, with 56 table-driven tests.

## Context

Security review of ad-hoc SQL is tractable for some queries and not others. A
query that returns a table's stored rows unchanged — a projection or filter, no
aggregation or derivation — is a transparent window: whether it is safe reduces
to that table's own row/column policy. A query that aggregates, joins, or
computes columns is opaque; its output no longer corresponds to stored rows, and
that policy reasoning does not carry over.

We want a mechanical triage that names, for one `SELECT`, the base tables it
returns **1:1 as stored**. On that list marks a query as eligible for the
lighter, policy-only review path; off it marks a query for deeper scrutiny. The
classifier is an *input to* that judgement, not the judgement (see Security
properties). Two examples fix intent:

- `SELECT c1, c2 FROM tbl1 UNION ALL SELECT * EXCEPT c3 FROM tbl2` → `{tbl1, tbl2}`.
- `SELECT mycol1 AS c1, mycol2+3 AS c2 FROM othertable` → `{}` (every output
  column renamed or computed).

## Decision

A scope-aware nanopass analysis (`BuildScopes`, no rewrite) reports a base table
iff the `SELECT` reading it is a pure passthrough:

- **one FROM source** — any JOIN / CROSS / comma join yields nothing;
- **every projection item is a bare column (`c`, `t.c`) or a star (`*`, `t.*`,
  bare `* EXCEPT c`)** — any alias (including a pure rename), expression,
  function, CASE, cast, or scalar subquery taints the table out. A column
  *subset* is still 1:1; a value or name change is not;
- **no GROUP BY / HAVING / DISTINCT / ARRAY JOIN / WINDOW / QUALIFY** — WHERE,
  PREWHERE, ORDER BY, and LIMIT are permitted (they restrict or reorder stored
  rows without transforming them);
- **subquery and non-recursive CTE sources are resolved through** to their base
  tables, but only when the inner `SELECT` is itself a passthrough; an impure
  inner layer stops resolution;
- **branches combine only under `UNION ALL`** (result = their union); any
  `UNION DISTINCT` / `EXCEPT` / `INTERSECT` combining a traversed chain makes
  that chain non-1:1.

Each fork resolved to the **conservative** side (Alternatives): the failure that
matters is a query wrongly called passthrough.

## Security properties and non-guarantees

Read this before relying on the output.

**A positive result means:** for table `T` on the list, the output columns drawn
from `T` are `T`'s stored columns, unchanged in name and value — no aggregation
collapses rows, no expression transforms values. The query is a projection or
filter of `T`.

**It does not mean — infer none of these:**

- **Not an authorization or safety verdict.** The analysis is purely structural;
  it consults no catalog, permissions, or data sensitivity. A 1:1 column can
  still be a secret — "exposed as stored" is exactly what makes row/column
  policy *the* remaining control, to be applied, not skipped.
- **It constrains the projection, not the predicate.** A passthrough query may
  carry an arbitrary WHERE, including one that exfiltrates through a blind
  boolean side channel. 1:1 says nothing about which rows are chosen.
- **It is per-table.** `T` on the list vouches for neither sibling tables nor a
  sibling `UNION ALL` branch.

**Conservative by construction.** Every uncertainty resolves to exclusion — an
unparseable query, a join, a set operation other than `UNION ALL`, a table
function, a recursive CTE, or any unrecognised shape yields no table. A caller
**must** treat a query the classifier cannot parse (Consequences) as *not*
passthrough and route it to deeper review, never as trusted-by-default. The
trust boundary is ClickHouse `SELECT` as accepted by Grammar1; anything outside
it does not classify as passthrough.

**Not defeated by nesting.** Resolve-through follows subqueries and non-recursive
CTEs to the real base tables, so a trivial subquery wrapper cannot hide a source,
while a deriving wrapper correctly declassifies it. Set operators are checked
only on chains actually traversed, so a `UNION DISTINCT` inside a `WHERE … IN
(…)` filter — which surfaces no rows — does not spuriously taint an otherwise
1:1 outer table.

## Alternatives

The three forks; the permissive side of each was rejected because over-inclusion
is the dangerous direction (a listed query gets *lighter* scrutiny):

- **Any-verbatim-column** (vs. strict rows). Report `T` if any single column of
  it is verbatim, beside derived siblings. Rejected: it would call
  `SELECT secret, other+1 FROM t` a passthrough of `t`, though the returned row
  is not a stored row. Strict rows matches "rows 1:1".
- **Per-table-within-joins** (vs. single-source). Credit each joined table on its
  own columns. Rejected for v1: a joined row is a composite, not a stored row of
  any single table — exactly the structure triage should push to manual review.
- **Top-level-FROM-only** (vs. resolve-through). Name only outer-FROM tables.
  Rejected: a subquery wrapper would hide the true source, unacceptable when the
  goal is to know what a query reads.

## Consequences

- A cheap, dependency-light primitive: `(pr, defaultDatabase) → sorted
  []TableRef`; `err` only on scope-construction failure, "none" being an empty
  slice. It sits beside the syntactic `ExtractTables` / `ExtractColumns` but is
  scope-aware.
- **Deferred:** the parenthesised modifier forms `* EXCEPT (a, b)`,
  `* REPLACE(…)`, `* APPLY(…)` are absent from Grammar1 and fail to parse (the
  bare `* EXCEPT c` form is handled). Such a query classifies as empty at the
  caller's parse — the conservative outcome, and why the first example uses the
  bare form. `REPLACE` / `APPLY` transform columns and could never be 1:1.
  Adding the parenthesised `EXCEPT` list is a local grammar change carrying
  `EXCEPT`-as-set-operator prediction risk; out of scope here.
- This is advisory triage, not an allow-gate. A caller that maps "listed" to
  "skip review" rather than "eligible for the policy-only path" misreads it.

## Validation

56 table-driven cases in `nanopass_analytics_passthrough_test.go`: both examples;
passthrough shapes; every disqualifier (expression, rename, mixed, aggregate,
window, scalar subquery, GROUP BY, HAVING, DISTINCT, ARRAY JOIN, join, comma
join, table function); filters that preserve 1:1; `UNION ALL` union vs. other
set-op exclusion including a mid-chain `UNION DISTINCT`; resolve-through
subqueries and CTEs with an impure inner layer stopping, a recursive CTE
excluded, and a set operator inside a `WHERE … IN` filter not tainting the outer
table. `go build`, `go vet`, `go test` pass for the touched package.
