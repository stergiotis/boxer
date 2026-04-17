---
type: reference
audience: DSL maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not yet verified against the current documentation standard. Do not cite as authoritative.

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

Every pass receives valid SQL and must return valid SQL. Re-parsing at each step eliminates corruption accumulation at the cost of repeated parsing (negligible for typical query sizes). Whitespace and comments are preserved on the hidden channel (`channel(HIDDEN)`), enabling lossless round-trip fidelity.

## Core Components

| File | Purpose |
|------|---------|
| `parse.go` | `Parse(sql) → (*ParseResult, error)` — fatal on any syntax error |
| `walk.go` | `WalkCST`, `FindAll`, `FindFirst` — depth-first CST traversal |
| `rewrite.go` | `ReplaceNode`, `DeleteNode`, `InsertBefore`, `InsertAfter`, `TrackedRewriter` |
| `pipeline.go` | `Pass` type, `Pipeline`, `FixedPoint`, `FixedPointPipeline`, `Validate` |
| `scope.go` | `BuildScopes` — lexical scope tree with UNION ALL, CTE, subquery awareness |
| `macro.go` | `MacroExpander` — function-call macro expansion with literal arguments |

## Scope System

`BuildScopes(pr, "defaultDB")` walks the CST and builds a tree of `SelectScope` objects:

- Enumerates all UNION ALL branches
- Resolves table aliases in FROM/JOIN
- Tags CTE references vs real tables (`TableSource.IsCTE`)
- Links FROM subqueries and expression subqueries to inner scopes
- Tracks default database for unqualified table resolution (`TableSource.ResolvedDatabase(scope)`)

```go
scopes := nanopass.BuildScopes(pr, "production")
for _, scope := range scopes {
    for _, ts := range scope.Tables {
        db := ts.ResolvedDatabase(scope) // "production" for unqualified, explicit db for qualified
    }
}
```

Database resolution matches ClickHouse behavior: each table resolves independently against the connection default, with no ambient database inheritance from sibling tables.

## Included Passes

### Lexical Passes (token-level, no structural awareness needed)

| Pass | Description | Idempotent |
|------|-------------|-----------|
| `NormalizeKeywordCase` | Uppercases all SQL keywords | Yes |
| `NormalizeWhitespace` | Collapses whitespace, preserves newlines | Yes |
| `NormalizeWhitespaceSingleLine` | Collapses all whitespace to single spaces | Yes |
| `StripComments` | Removes single-line and multi-line comments | Yes |

### Structural Passes (scope-aware, handle UNION ALL / CTEs / subqueries)

| Pass | Description | Idempotent |
|------|-------------|-----------|
| `QualifyTables(db)` | Adds default database prefix to unqualified tables, skips CTEs | Yes |
| `AddWhereCondition(pred)` | Injects/ANDs a WHERE predicate into every UNION ALL branch | No |
| `EnforceRLS(policy)` | Row-level security — per-table predicates with alias rewriting | No |
| `ExpandColumns(schema)` | Expands `*`, `table.*`, `COLUMNS('regex')` using schema | Yes |
| `ExpandColumnsWithOptions(schema, opts)` | Expand with Go-side EXCEPT/REPLACE/APPLY | Yes |
| `WrapColumnsWithDynamic(pattern)` | Wraps matching column names in `COLUMNS('^name$')` | Yes |

### Expression Passes

| Pass | Description | Idempotent |
|------|-------------|-----------|
| `RemoveRedundantParens` | Removes unnecessary parentheses based on operator precedence | Yes |
| `RewriteFunctionNames(map)` | Renames function calls (case-insensitive) | Yes |
| `CanonicalizeConstructors(form)` | Normalizes tuple/array syntax between literal and function form | Yes |

### Query-Level Passes

| Pass | Description | Idempotent |
|------|-------------|-----------|
| `SetFormat(name)` | Sets, replaces, or removes the FORMAT clause | Yes |
| `AddSettings(entries)` | Appends SETTINGS clause to the outermost query | No |

### Settings Manipulation

| Function | Description |
|----------|-------------|
| `ReadSettings(sql)` | Deserializes SETTINGS to `map[string]any` |
| `WriteSettings(map)` | Serializes `map[string]any` back to SETTINGS clause |
| `ModifySettings(fn)` | Atomic read-modify-write of settings |

Setting values round-trip through Go types: `int64`, `float64`, `string`, `nil`, `bool`, `[]any` (arrays), `*Tuple` (tuples).

### Compile-Time Evaluation

| Component | Description |
|-----------|-------------|
| `MacroExpander` | Expands registered function calls with literal arguments into SQL fragments |
| `FunctionEvaluator` | Evaluates registered functions in Go, with recursive nested evaluation and partial evaluation |

The `FunctionEvaluator` handles three cases in a single pass:
- **Full evaluation**: `myAdd(myAdd(1,2), 3)` → `6` (recursive, in-memory)
- **Partial evaluation**: `myAdd(a, myAdd(1,2))` → `myAdd(a, 3)` (inner evaluated, outer left)
- **Passthrough**: `myAdd(a, b)` → `myAdd(a, b)` (no evaluable args)

### Validation

| Pass | Description |
|------|-------------|
| `ValidateColumnNames(pattern)` | Checks all column names (aliases and inferred) match a regex |
| `ValidateColumnNamesExclude(pattern)` | Checks no column name matches a forbidden regex |
| `DetectUnsupportedColumnSyntax(sql)` | Pre-parse detection of EXCEPT/APPLY/REPLACE modifiers |

### Utilities

| Function | Description |
|----------|-------------|
| `GetFormat(sql)` | Reads the FORMAT value without modifying the query |
| `Validate` | Parse-only check — useful as a pipeline step |
| `FixedPoint(pass, max)` | Repeats a pass until output stabilizes |
| `FixedPointPipeline(max, passes...)` | Repeats an entire pipeline until stable |
| `LoggingPass(logger, name, pass)` | Debug wrapper with zerolog |

## Usage Examples

### Pipeline

```go
result, err := nanopass.Pipeline(sql,
    passes.StripComments,
    passes.NormalizeKeywordCase,
    passes.RemoveRedundantParens,
    passes.QualifyTables("production"),
    passes.EnforceRLS(rlsPolicy),
    passes.AddWhereCondition("tenant_id = 42"),
    passes.AddSettings([]passes.SettingEntry{{Key: "max_threads", Value: "4"}}),
    passes.SetFormat("JSON"),
    passes.NormalizeWhitespaceSingleLine,
    nanopass.Validate,
)
```

### Macro Expansion

```go
expander := nanopass.NewMacroExpander()
expander.Register("jsonCol", func(args []nanopass.LiteralArg) (string, error) {
    return "JSONExtractString(payload, " + args[0].Value + ")", nil
})

result, err := nanopass.Pipeline(sql, expander.Pass(), nanopass.Validate)
```

### Compile-Time Function Evaluation

```go
eval := passes.NewFunctionEvaluator()
eval.RegisterBuiltins() // array(), tuple()
eval.Register("daysInMonth", func(args []any) (any, error) {
    year, month := args[0].(int64), args[1].(int64)
    // ... compute ...
    return days, nil
})

result, err := eval.Pass()("SELECT daysInMonth(2024, 2)")
// result: "SELECT 29"
```

### Row-Level Security

```go
policy := passes.NewRLSPolicy(map[string]string{
    "orders":    "orders.tenant_id = currentUser()",
    "customers": "customers.visible = 1",
})
result, err := passes.EnforceRLS(policy)(sql)
// Injects predicates into every scope, rewrites table qualifiers to match aliases
```

### Column Expansion with Schema

```go
schema := passes.NewSchemaProvider(map[string][]string{
    "prod.orders": {"id", "amount", "tenant_id", "created"},
    "customers":   {"id", "name", "email"},
})
result, err := passes.ExpandColumns(schema, "prod")(
    "SELECT * FROM orders AS o JOIN customers AS c ON o.id = c.id",
)
// Expands * using schema, resolves "orders" to "prod.orders" via default database
```

### Settings Manipulation

```go
pass := passes.ModifySettings(func(settings map[string]any) error {
    if v, ok := settings["max_threads"]; ok {
        settings["max_threads"] = v.(int64) * 2
    }
    settings["optimize_read_in_order"] = int64(1)
    return nil
})
result, err := pass("SELECT a FROM t SETTINGS max_threads = 4")
// result: "SELECT a FROM t SETTINGS max_threads = 8, optimize_read_in_order = 1"
```

## Analysis Functions

| Function | Package | Returns |
|----------|---------|---------|
| `ExtractTables(pr)` | `analysis` | `[]TableRef` — table references (excluding column qualifiers) |
| `ExtractColumns(pr)` | `analysis` | `[]ColumnRef` — column references with optional table qualifier |
| `ExtractFunctions(pr)` | `analysis` | `[]FunctionRef` — function calls (regular, parametric, window) |

## Test Corpus

67+ embedded SQL files in `testdata/corpus/` cover SELECT features from simple literals to complex CTEs with window functions, UNION ALL, parametric aggregates, JSON functions, ARRAY JOIN, PREWHERE, SETTINGS with arrays/tuples, and FORMAT clauses. Loaded via `embed.FS` with `testdata.LoadCorpus()`.

## Test Strategy

Every pass is tested with:

1. **Explicit input/output pairs** — 5-10+ cases covering the transformation
2. **Idempotency** — `pass(pass(x)) == pass(x)` for idempotent passes
3. **Corpus validity** — every corpus entry produces parseable SQL
4. **UNION ALL** — transformations apply to all branches
5. **CTEs** — CTE references not accidentally modified, CTE bodies are transformed
6. **Subqueries** — FROM subqueries, WHERE subqueries, scalar subqueries
7. **Invalid SQL rejection** — empty strings, whitespace, incomplete statements
8. **Pipeline integration** — composes correctly with other passes
9. **Round-trip fidelity** — no-op rewrite reproduces input exactly

Additional robustness tests: pipeline ordering permutations (all 24 orderings of 4 passes), scope structure preservation for pure passes, and full corpus × all passes cross-product.

## Dependencies

- `github.com/antlr4-go/antlr/v4` v4.13.1 — ANTLR4 Go runtime
- `github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar` — Generated ClickHouse lexer/parser
- `github.com/stergiotis/boxer/public/observability/eh` — Error handling
- `github.com/rs/zerolog` — Structured logging
- `github.com/stretchr/testify` — Test assertions

## Grammar Modifications

The upstream ClickHouse ANTLR4 grammar has these required modifications:

- **Whitespace/comments**: `-> skip` changed to `-> channel(HIDDEN)` for `WHITESPACE`, `SINGLE_LINE_COMMENT`, `MULTI_LINE_COMMENT`. Without this, the `TokenStreamRewriter` cannot preserve original formatting.
- **Setting values**: `settingExpr` extended with `settingValue` rule supporting arrays (`[1,2]`), tuples (`(1,2)`), and function-form constructors (`array(1,2)`, `tuple(1,2)`) alongside scalar literals.

## Known Grammar Limitations

- `FROM t SELECT a` (FROM-first syntax)
- `WITH (SELECT x) AS name` (scalar subquery CTE)
- `EXISTS (SELECT ...)` (EXISTS predicate)
- `* EXCEPT(col)`, `COLUMNS('...') APPLY(func)`, `REPLACE(...)` (column modifiers — use Go-side `WithExcept`/`WithApply`/`WithReplace`)
- Map literals in SET (`SET param = {'key': [1,2]}`)
