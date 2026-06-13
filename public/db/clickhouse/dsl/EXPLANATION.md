---
type: explanation
audience: DSL maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not verified against the current documentation standard; migrated from `ARCHITECTURE.md`. Do not cite as authoritative.

# ClickHouse SQL DSL

A structured approach to parsing, analyzing, transforming, and generating
ClickHouse SELECT queries in Go.

## Idea

SQL is treated as a **data structure**, not a string. Every query flows through
a pipeline that progressively simplifies its representation:

```
SQL text ──→ Grammar1 CST ──→ normalized SQL ──→ Grammar2 CST ──→ AST ──→ output
              (parse)          (canonicalize)      (validate)    (convert)
```

The output can be SQL text, CBOR bytes, or Go source code — all from the same
AST. The AST can also be constructed programmatically via the builder API,
closing the loop.

## Two Grammars

The DSL uses two ANTLR4 parser grammars sharing one lexer. This embodies
the robustness principle: **accept generously, emit carefully**.

**Grammar1** accepts the full ClickHouse SELECT surface — all syntactic sugar,
keyword permutations, and notational variants that users might write. It serves
as the input grammar for the normalization pipeline.

**Grammar2** accepts only canonical forms. Every syntactic choice has exactly
one representation: one function call form for CASE, one operator for equality,
one quoting style for identifiers, one keyword order for JOINs. Grammar2 serves
as structural validation — if SQL parses with Grammar2, it is guaranteed
canonical. The AST converter only handles Grammar2 context types, so
non-canonical forms are rejected at compile time (the types don't exist).

## Nanopass passes

The `nanopass` package provides the infrastructure for small, composable SQL
transformations. Each **pass** is a function `string → (string, error)` that
reads SQL, parses it into a CST, performs a focused rewrite using ANTLR's
`TokenStreamRewriter`, and emits modified SQL.

Key properties of passes:

- **Single responsibility** — each pass handles one normalization (e.g. CASE
  elimination, identifier quoting, operator canonicalization)
- **Idempotent** — applying a pass to its own output produces the same result
- **Composable** — passes chain via `Pipeline(sql, pass1, pass2, ...)`
- **Convergent** — `FixedPoint(pass, maxIter)` repeats a pass until stable,
  handling nested constructs like `CASE WHEN CASE ... END ... END`

The package provides `Parse()` (Grammar1) and `ParseCanonical()` (Grammar2)
as entry points, with `ValidateCanonical` as a pipeline-terminal pass that
proves the output conforms to Grammar2.

## Canonicalization Pipeline

The `passes` package contains normalization passes that transform Grammar1 SQL
into Grammar2 SQL. The pipeline order matters — identifier quoting runs last
because all other passes emit bare identifiers.

The passes eliminate syntactic sugar (CASE → multiIf/if/caseWithExpression,
DATE/TIMESTAMP → toDate/toDateTime, ternary → if, EXTRACT/SUBSTRING/TRIM →
function calls), canonicalize operators (== → =), normalize JOIN keyword order
(strictness before direction), remove redundant keywords (OUTER), enforce
structural conventions (USING with parentheses, comma joins → CROSS JOIN),
and quote all identifiers consistently (double-quoted).

The pipeline's correctness is verified by comparing `EXPLAIN SYNTAX` output
from a real ClickHouse instance — the strongest available semantic equivalence
test.

## AST

The `ast` package defines a typed, CBOR-serializable abstract syntax tree for
ClickHouse SELECT queries. It is a tagged union: `Expr` has a `Kind` field
(uint8 enum) and exactly one non-nil data pointer matching that kind.

The AST is converted from Grammar2 CSTs, so it never contains non-canonical
forms. There are no CASE, CAST, ternary, or sugar expression kinds — these
are all function calls by the time they reach the AST. All enum fields (join
kind, operator, direction, frame bound, interval unit, etc.) are typed uint8
values, not strings.

The AST supports three output modes:

- **`ToSQL()`** — emits valid ClickHouse SQL with operator precedence handling
- **CBOR serialization** — compact binary format for storage and IPC
- **`ToGoCode()`** — emits Go source code using the builder API, enabling
  code generation and round-trip testing

## Builder API

The `astbuilder` package provides a fluent API for constructing AST nodes
programmatically. It follows squirrel's composability pattern: builders are
immutable values, each method returns a copy, and queries can be forked and
extended independently.

```go
q, err := Select(Col("a"), Func("count", Col("b")).As("cnt")).
    From("db", "orders").
    Where(Col("status").Eq(Lit("completed"))).
    GroupBy(Col("a")).WithTotals().
    OrderBy(NullsLast(Col("cnt").Desc())).
    Limit(50).
    Build()
```

`Build()` returns an `ast.Query` — the same type produced by the CST→AST
converter. This means builder-constructed queries have full access to
`ToSQL()`, CBOR serialization, `ToGoCode()`, and any analysis passes that
operate on the AST.

Errors are deferred: if `Lit()` receives an unsupported Go type, the error
propagates silently through the chain and surfaces at `Build()`. This
preserves the fluent API without forcing error checks at every step.

## Feature Passes

Beyond canonicalization, the `passes` package includes application-level
transformations that operate on Grammar1 CSTs:

- Adding or modifying WHERE/PREWHERE clauses with tenant isolation filters
- Injecting or overriding SETTINGS for resource governance
- Extracting and validating literal values from parsed SQL
- Expanding macros and rewriting function calls
- Overriding FORMAT for output control

These passes use the same nanopass infrastructure (parse → walk → rewrite →
emit) and compose freely with canonicalization passes in pipelines.

## Testing Strategy

Correctness is verified at multiple levels:

- **Unit tests** — explicit input/output pairs per pass
- **Idempotency** — pass(pass(x)) == pass(x) across the corpus
- **Scope preservation** — normalization doesn't change the query's structure
- **Grammar2 compliance** — pipeline output parses with Grammar2
- **EXPLAIN SYNTAX** — ClickHouse confirms semantic equivalence
- **Round-trip** — SQL → AST → ToSQL() → parse must succeed
- **CBOR round-trip** — SQL → AST → CBOR → AST → ToSQL() → parse
- **Generated round-trip** — SQL → AST → ToGoCode() → compile → execute →
  ast.Query → ToSQL() → parse (via `go generate`)
- **Structural round-trip** — SQL → AST → ToSQL() → re-canonicalize → AST
  must be `reflect.DeepEqual` to the first AST. Parseability alone is blind
  to precedence regrouping and dropped clauses (both print valid SQL that
  means something else); structural equality is not. Runs over the whole
  corpus and as the `FuzzAstRoundTrip` invariant.
- **Fuzzing** — `FuzzAstRoundTrip` (ast_test), `FuzzCanonicalizeFull` /
  `FuzzParse` (nanopass_test), and the marshalling escape/scalar codecs.
  These found, among others, the unparser precedence and identifier-fusion
  bugs and the EscapeString non-UTF-8 leak.
- **Server truth** — `clickhouse format` (the server's parser) accepts the
  canonical form and every `ToSQL()` output; `clickhouse local` confirms
  the original query and its round-tripped rendering evaluate identically
  for table-free expressions (skipped when no `clickhouse` binary is on
  PATH). This grounds judgment calls — octal-is-decimal, comma-LIMIT,
  EXTRACT/TRIM canonical functions — against the real server rather than
  belief.

## Limitations and Over-Acceptance Boundaries

Grammar1 is deliberately generous ("accept liberally"); Grammar2 and the
AST are strict ("emit canonically"). The gap is intentional, but it means
some inputs Grammar1 parses have no canonical form or no AST representation.
These are by design — verified against ClickHouse 26.x where noted — and
must not be "fixed" without re-checking the server:

**Grammar surface (unsupported syntax):**

- `FROM t SELECT a` (FROM-first syntax)
- `WITH (SELECT x) AS name` (scalar subquery CTE)
- `EXISTS (SELECT ...)` (EXISTS predicate)
- `* EXCEPT(col)`, `COLUMNS('...') APPLY(func)`, `REPLACE(...)` (column modifiers)
- Map literals in SET (`SET param = {'key': [1,2]}`)

**Grammar1 over-acceptance (parses in G1, rejected by ClickHouse):**

- Empty identifiers (`""`, `` `` ``) — the IDENTIFIER lexer rule allows a
  zero-length quoted name; the server rejects empty identifiers everywhere.
- Param-slot **type** expressions beyond real data types (`{x: A(0 % b(1))}`)
  — the columnTypeExpr param form admits arbitrary expressions; the server
  rejects non-types.
- `INTERVAL <expr> <non-unit>` (`INTERVAL 0 YYYY`) — ANTLR error-recovers a
  non-keyword unit into an interval node; the converter rejects the bogus
  unit. (The server parses `INTERVAL` as a column there instead.)
- Keyword-named param slots (`{date: UInt64}`) parse in Grammar1 but not
  Grammar2 — slot names there are bare IDENTIFIER tokens.

**AST / unparser:**

- WITH-item ordering: CTEs (`name AS (query)`) and scalar aliases
  (`expr AS name`) that interleave in one WITH clause are split into two
  groups and re-emitted CTEs-first; the source interleaving is not preserved
  (semantically irrelevant — names are unique).
- `NOT(x)` parses as a function call (ClickHouse's real `not()`), so it
  canonicalizes and round-trips as `"NOT"(x)` — semantically equivalent.
- The Go-source emitter (`ToGoCode`) does not emit query-level scalar WITH
  items (`Query.With`); the builder API has no primitive for them. CTEs and
  all other clauses are covered.

**Input guards (nanopass):** input must be valid UTF-8 (the rune-based
ANTLR stream would otherwise transcode invalid bytes to U+FFFD and corrupt
string literals on rewrite); bracket/CASE nesting is capped at
`MaxNestingDepth` and total size at `MaxInputBytes`. See `nanopass_guard.go`.
