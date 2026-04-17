---
type: adr
status: proposed
date: 2026-04-17
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0002: Nanopass Pipeline Discipline — Stateless Passes on CST + Scopes

## Context

The nanopass package ([`public/db/clickhouse/dsl/nanopass`](../../public/db/clickhouse/dsl/nanopass)) performs SQL→SQL transformations on ClickHouse SELECT statements. During its design we faced interlocking choices about:

- Whether each transformation pass parses and emits independently or shares state / trees with neighbours.
- Whether to build an abstract syntax tree (AST) or operate directly on the grammar's concrete syntax tree (CST).
- How to centralize semantic information (scopes, FROM tables, visible columns) so passes consume it consistently.

Observed pressures:

- Every time a pass shared state with another, composability broke and the combined pipeline behaved differently depending on order.
- Every tree abstraction beyond CST + scope required a translation layer that drifted from the ANTLR grammar whenever the grammar changed — and grammar maintenance was already the highest-risk area (we carry a local `channel(HIDDEN)` patch).
- Repeated ad-hoc scope analyses in different passes disagreed about basics like "which columns are in scope here," producing subtle bugs.

This ADR is retrospective: the decision is already embodied in the code. The purpose here is to record *why* the shape is as it is so future contributors resist the forces that would pull the design toward an AST or stateful passes.

## Design space (QOC)

**Question.** How should nanopass structure its passes, tree representation, and semantic information to maximise composability and minimise maintenance cost against an evolving grammar?

**Options.**

- **O1** — Shared mutable state across passes; one persistent CST.
- **O2** — Build a full AST with typed nodes; passes operate on the AST.
- **O3** — CST + per-pass ad-hoc analysis (each pass walks the CST and derives what it needs locally).
- **O4** — CST + centralised `SelectScope` populated once by `BuildScopes`, consumed by every pass *(chosen)*.
- **O5** — Hybrid: CST for syntax, AST for semantics.

**Criteria.**

- **C1 — Composability:** can passes be chained or reordered without contract breakage?
- **C2 — Grammar-drift resistance:** does the representation stay consistent when the ANTLR grammar is regenerated or extended?
- **C3 — Parse / memory overhead:** what is the runtime cost per pipeline invocation?
- **C4 — Single source of truth for scope:** is there exactly one place where "the FROM tables of this SELECT" lives?
- **C5 — Testability:** does the shape support idempotency checks and corpus-based validation naturally?

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | −  | +  | ++ | −  |
| C2 | +  | −− | +  | ++ | −  |
| C3 | ++ | −  | +  | +  | −  |
| C4 | −− | +  | −  | ++ | +  |
| C5 | −  | +  | +  | ++ | +  |

O4 is `++` on every criterion except C3 (where it pays a parse cost per pass relative to shared-state). It is the unique Pareto optimum.

## Decision

We adopt O4. The invariants:

1. **Stateless passes.** Every pass takes valid SQL, returns valid SQL, and shares no tree or scope with the next pass.
2. **No AST.** The CST plus `SelectScope` is the complete representation. Richer tree operations are handled by extending `SelectScope`, not by introducing a new abstraction.
3. **`SelectScope` is the single source of structural truth.** New semantic fields (e.g., "columns in GROUP BY") are added as fields on `SelectScope`, populated by `BuildScopes`, consumed by passes. Parallel analyses are disallowed.
4. **Fixed-point iteration over multi-stage passes.** If a transformation is too complex for one pass, split it into two stateless passes and iterate to a fixed point — never break the stateless invariant for convenience.

## Alternatives

Comparative assessment is in the QOC matrix above; the notes below capture nuance not visible in the ratings.

- **O1 — Shared state.** Eliminates parse overhead but destroys composability; tried informally and abandoned after passes behaved differently depending on ordering.
- **O2 — Full AST.** Adds a translation layer that must track every grammar regeneration. Given that grammar work is already the highest-risk maintenance area, amplifying that risk for a mostly-ergonomic gain is unjustified.
- **O3 — CST + ad-hoc.** Works, but forces every pass to re-derive scope information locally. Produced subtle drift when two passes disagreed on the "visible columns" set.
- **O5 — Hybrid.** Pays the AST maintenance cost without escaping CST-level concerns. No ergonomic win large enough to justify it.

## Consequences

### Positive

- Passes can be chained or reordered freely; composability is a property of the design, not a testing burden.
- Grammar changes propagate through one regeneration plus a full corpus re-run; there is no AST schema to update in parallel.
- `SelectScope` is auditable: every piece of structural information has exactly one definition site (`BuildScopes`).
- Idempotency (`pass(pass(x)) == pass(x)`) becomes a natural per-pass test invariant.

### Negative

- Each pass re-parses the SQL from scratch, paying a measurable CPU cost per stage. Acceptable for the current workload; revisit only if profiling shows parsing dominates runtime.
- `SelectScope` will grow as more passes are added. Contributors must resist tacking on ad-hoc fields elsewhere and instead extend the scope walker.
- The ANTLR grammar carries a local `channel(HIDDEN)` modification; upstream regeneration requires re-applying the patch. Tracked as a separate concern (out of scope for this ADR).

### Neutral

- Grammar maintenance remains the highest-risk area regardless of this decision. The decision does not solve it; it declines to amplify it.

### Derived practices

The following operational heuristics follow from the decision and are recorded here rather than as separate ADRs, so the practices and their rationale stay together:

- **Corpus before features.** Every new pass begins with 5–10 SQL examples covering the target patterns. The corpus is more valuable than the passes themselves; real queries that break a pass are added to the corpus *before* the bug is fixed.
- **Four test categories per pass.**
  (1) Explicit input/expected pairs for the happy path.
  (2) Idempotency (`pass(pass(x)) == pass(x)`).
  (3) Corpus validity (the pass produces parseable SQL for every corpus entry).
  (4) Scope preservation for pure passes (case, whitespace, comments, parens) or a scope-delta check for structural passes (`AddWhereCondition` etc.).
- **Grammar extensions only on real demand.** FROM-first syntax, scalar subquery CTEs, and `EXISTS` are deferred until a user query requires them. Each change requires regeneration, full corpus re-run, and an ambiguity check against existing rules.
- **`WalkCST` is pre-order only.** If bottom-up processing is needed, build a separate post-order walker; do not retrofit pre-order.
- **Sub-packaging threshold.** Flat layout is preferred up to ~25–30 files. Beyond that, split into sub-packages such as `scope`, `macro`, `passes/security` (row-level filtering), `passes/compat` (dialect rewrites). Do not split prematurely.

## Status

Proposed — retrospective ADR documenting decisions already embodied in the nanopass package. Promote to `accepted` and fill `reviewed-by` + `reviewed-date` after review by a code owner of [`public/db/clickhouse/dsl/nanopass`](../../public/db/clickhouse/dsl/nanopass).

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`. ADRs are append-only; supersession is recorded, not deleted.

## References

- [`public/db/clickhouse/dsl/nanopass/README.md`](../../public/db/clickhouse/dsl/nanopass/README.md) — package overview and component inventory.
- [`public/db/clickhouse/dsl/nanopass/SCOPE_RESOLUTION.md`](../../public/db/clickhouse/dsl/nanopass/SCOPE_RESOLUTION.md) — deep dive on database resolution within the scope system.
- [`public/db/clickhouse/dsl/EXPLANATION.md`](../../public/db/clickhouse/dsl/EXPLANATION.md) — DSL-level architecture rationale.
- Source: "OPUS4.6 advice for long-term development" captured in the retired `MAINTENANCE.md` (superseded by this ADR).
