# nanopass — ClickHouse SQL Transformation Framework

A Go library for composable SQL→SQL transformations of ClickHouse SELECT statements. Each transformation is a self-contained **pass** that parses SQL, walks the concrete syntax tree (CST), rewrites tokens, and emits valid SQL. Passes are chained into pipelines — no shared mutable state, no custom AST, no partial parses.

## Architecture

```
SQL string
  → Parse (ANTLR4 lexer + parser → CST)
  → Walk CST + TokenStreamRewriter
  → Emit modified SQL string
  → next pass re-parses from scratch
```

Every pass receives valid SQL and must return valid SQL. This invariant is enforced by re-parsing from scratch at each step, which eliminates corruption accumulation at the cost of repeated parsing (negligible for typical query sizes).

The framework operates on ANTLR4's concrete syntax tree directly — there is no intermediate AST. Whitespace and comments are preserved on the hidden channel (`channel(HIDDEN)` in the lexer grammar), enabling lossless round-trip fidelity.

## Core Components

**`parse.go`** — `Parse(sql) → (*ParseResult, error)`. Wraps the ANTLR4 ClickHouse grammar. Any syntax error is fatal (no error recovery). Returns the CST root, token stream, lexer, and parser.

**`walk.go`** — `WalkCST`, `FindAll`, `FindFirst`. Depth-first CST traversal with predicate-based node filtering. The callback returns `false` to skip a subtree.

**`rewrite.go`** — Convenience functions over `antlr.TokenStreamRewriter`: `ReplaceNode`, `DeleteNode`, `InsertBefore`, `InsertAfter`, `ReplaceToken`, `DeleteToken`, `NodeText`, `GetText`. Also provides `TrackedRewriter` which detects and logs overlapping token modifications.

**`pipeline.go`** — `Pass` type (`func(string) (string, error)`), `Pipeline` (sequential chaining), `FixedPoint` (repeat until stable), `FixedPointPipeline` (repeat a full pipeline until stable), `Validate` (parse-only pass), `LoggingPass` (debug wrapper).

**`scope.go`** — `BuildScopes(pr) → []*SelectScope`. Walks the CST and builds a lexical scope tree:
- Enumerates all UNION ALL / EXCEPT / INTERSECT branches
- Resolves table aliases in FROM/JOIN
- Tags CTE references vs real tables
- Links FROM subqueries and expression subqueries (WHERE, SELECT list, HAVING) to their inner scopes
- `SelectScope.ResolveAlias(name)` — alias → table lookup
- `SelectScope.ResolveCTE(name)` — CTE name resolution with ancestor traversal
- `SelectScope.AllScopes()` — flattened depth-first collection of all descendant scopes

**`macro.go`** — `MacroExpander` with a registry of `MacroFuncI` functions. Matches `ColumnExprFunctionContext` nodes by name (case-insensitive), extracts constant/literal arguments, and replaces the call with expanded SQL. Non-literal arguments cause the macro to be silently skipped. Nested macros are resolved via `FixedPoint`.

## Included Passes

| Pass | Package | Description |
|------|---------|-------------|
| `NormalizeKeywordCase` | `passes` | Uppercases all SQL keywords |
| `NormalizeWhitespace` | `passes` | Collapses whitespace, preserves newlines |
| `NormalizeWhitespaceSingleLine` | `passes` | Collapses all whitespace to single spaces |
| `StripComments` | `passes` | Removes single-line and multi-line comments |
| `RemoveRedundantParens` | `passes` | Removes unnecessary parentheses based on operator precedence |
| `QualifyTables(db)` | `passes` | Adds default database prefix to unqualified table references |
| `AddWhereCondition(pred)` | `passes` | Injects/ANDs a WHERE predicate into every UNION ALL branch |
| `AddSettings(entries)` | `passes` | Appends SETTINGS clause to the outermost query |
| `RewriteFunctionNames(map)` | `passes` | Renames function calls by mapping (case-insensitive) |

All structural passes (`QualifyTables`, `AddWhereCondition`) are scope-aware: they handle UNION ALL branches, skip CTE references, and recurse into subqueries.

## Analysis Functions

| Function | Package | Returns |
|----------|---------|---------|
| `ExtractTables(pr)` | `analysis` | `[]TableRef` — all table references (excluding column qualifiers) |
| `ExtractColumns(pr)` | `analysis` | `[]ColumnRef` — all column references with optional table qualifier |
| `ExtractFunctions(pr)` | `analysis` | `[]FunctionRef` — all function calls (regular, parametric, window) |

## Usage Example

```go
sql := "select /* debug */ a, (b * c) from t where (x > 1)"

result, err := nanopass.Pipeline(sql,
    passes.StripComments,
    passes.NormalizeKeywordCase,
    passes.RemoveRedundantParens,
    passes.QualifyTables("production"),
    passes.AddWhereCondition("tenant_id = 42"),
    passes.AddSettings([]passes.SettingEntry{
        {Key: "max_threads", Value: "4"},
    }),
    passes.NormalizeWhitespaceSingleLine,
    nanopass.Validate,
)
// result: "SELECT a, b * c FROM production.t WHERE (x > 1) AND tenant_id = 42 SETTINGS max_threads = 4"
```

### Macro Expansion

```go
expander := nanopass.NewMacroExpander()
expander.Register("jsonCol", func(args []nanopass.LiteralArg) (string, error) {
    return "JSONExtractString(payload, " + args[0].Value + ")", nil
})

result, err := nanopass.Pipeline(sql,
    expander.Pass(),
    nanopass.Validate,
)
```

### Fixed-Point Iteration

```go
// Nested macros: outer_m(5) → inner_m(5) → 5 + 1
pass := nanopass.FixedPoint(expander.Pass(), 10)
```

## Grammar Modifications

The upstream ClickHouse ANTLR4 grammar has one required modification: the lexer rules for `WHITESPACE`, `SINGLE_LINE_COMMENT`, and `MULTI_LINE_COMMENT` must use `-> channel(HIDDEN)` instead of `-> skip`. Without this change, the `TokenStreamRewriter` cannot preserve original formatting because whitespace tokens are discarded by the lexer.

## Known Grammar Limitations

These ClickHouse SQL features are not supported by the current grammar:

- **FROM-first syntax** (`FROM t SELECT a`) — requires `fromClause?` before `projectionClause` in `selectStmt`
- **Scalar subquery CTEs** (`WITH (SELECT x) AS name, ...`) — requires extending `namedQuery` to accept `columnExpr AS identifier`
- **Complex SET literals** (`SET param = {'key': [1,2]}`) — `settingExpr` only accepts scalar literals
- **EXISTS predicate** (`WHERE EXISTS (SELECT ...)`) — not in the grammar's `columnExpr` alternatives

## Test Corpus

SQL files in `testdata/corpus/` cover SELECT features from simple literals to complex CTEs with window functions, UNION ALL, parametric aggregates, JSON functions, ARRAY JOIN, PREWHERE, SETTINGS, and FORMAT clauses. The corpus is loaded via `embed.FS` and used across all test suites.

## Test Strategy

Every pass is tested with four categories:
1. **Explicit input/output pairs** — specific expected transformations
2. **Idempotency** — `pass(pass(x)) == pass(x)`
3. **Corpus validity** — every corpus entry produces parseable SQL after the pass
4. **Scope preservation** — pure passes (case, whitespace, comments, parens) don't change the scope structure

Additional robustness tests: round-trip fidelity (no-op rewrite reproduces input exactly), pipeline ordering permutations (all 24 orderings of 4 passes produce valid SQL), invalid SQL rejection (empty strings, comments only, incomplete statements), and full corpus × all passes cross-product.