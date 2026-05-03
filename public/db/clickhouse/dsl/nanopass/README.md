---
type: reference
audience: DSL maintainer
status: draft
---

> **Status: draft — pre-human-review.** Not yet verified against the current documentation standard. Do not cite as authoritative.

# nanopass — ClickHouse SQL Transformations

A Go library for composable SQL→SQL transformations of ClickHouse SELECT statements. Each transformation is a self-contained **pass**: a struct value carrying an `Apply` function plus declared `PassProperties`. Passes compose via combinators that take and return passes; the runner threads a shared [`env.Environment`](../env) through the chain so settings, params, and FORMAT are first-class Go values.

The current Pass / Environment design is recorded in [ADR-0006](../../../../../doc/adr/0006-nanopass-environment-and-first-class-pass.md). The substrate (stateless passes on CST + scopes) was decided in [ADR-0002](../../../../../doc/adr/0002-nanopass-discipline.md).

## Architecture

```
SQL string
  → env.Extract           (split SET prelude → Environment, body)
  → Pass.Apply(env, body) (parse, walk CST, rewrite tokens, emit body)
  → env.Integrate(body)   (re-emit SET prelude + body)
  → next pass repeats
```

`Pass.Run(sql)` does the full round-trip. `Sequence` shares the env across child passes — they all observe each other's mutations to settings and params. Re-parsing each Apply preserves composability at the cost of repeated parsing (negligible for typical query sizes). Whitespace and comments are preserved on the hidden channel (`channel(HIDDEN)`), enabling lossless round-trip fidelity.

## Pass — first-class value

```go
type Pass struct {
    Name       string
    Apply      func(*env.Environment, string) (string, error)
    Properties PassProperties
}

type PassProperties struct {
    Idempotent      bool        // f(f(x)) == f(x) — testable via AssertProperties
    NeedsFixedPoint bool        // runner auto-wraps Apply in a fixpoint loop
    Reads, Writes   EnvRegions  // bitset: Body | SessionSettings | StatementSettings | Params | Format
    Requires        []FormTag   // pre/post-condition hints (documentation in v1)
    Produces        []FormTag
}
```

`Idempotent` and `NeedsFixedPoint` are mutually exclusive — flagged by [`AssertProperties`](#assertproperties-machine-checked-annotations).

## Combinators

```go
func Sequence(name string, ps ...Pass) Pass
func FixedPoint(p Pass, maxIter int) Pass
func Validating(g Grammar, p Pass) Pass
func Conditional(name string, pred func(*env.Environment) bool, p Pass) Pass
func LiftBodyPass(name string, fn func(string)(string,error), props PassProperties) Pass
```

The runner auto-wraps any pass with `NeedsFixedPoint: true` in `FixedPoint(p, DefaultFixedPointMaxIter)` (128). Pass authors who need a different cap call `FixedPoint(p, n)` explicitly. `Validating(g, p)` runs `p.Apply` then validates the body against grammar `g`; for pre-validation, insert `nanopass.ValidateGrammar1` as a prior step in a `Sequence`.

## Environment

[`dsl/env`](../env) models the execution context surrounding a SELECT.

```go
type Environment struct {
    SessionSettings   map[string]Setting   // from leading `SET k = v;` (non-param)
    StatementSettings map[string]Setting   // read-only view of inline `SETTINGS k=v`
    Params            map[string]Param     // unified view: SET param_x AND {x: T} slots
    Format            string               // read-only view of trailing `FORMAT X`
}
```

`env.Extract` parses the SET prelude exhaustively: keys with the `param_` prefix or that match a body slot become `Params`; everything else becomes `SessionSettings`. The inline SETTINGS clause and FORMAT clause are populated as **read-only views** — they live in body, and passes that mutate them rewrite the body's CST (which then refreshes the env on the next Extract). `env.Integrate` re-emits SET-prelude lines only.

## Core Components

| File | Purpose |
|------|---------|
| `nanopass_pipeline.go` | `Pass`, `PassProperties`, combinators, `Pass.Run`, `Pass.Apply`, validators |
| `nanopass_assert_properties.go` | `AssertProperties(t, p, corpus)` — corpus-backed contract enforcement |
| `nanopass_parse.go` | `Parse` (Grammar1) and `ParseCanonical` (Grammar2) |
| `nanopass_walk.go` | `WalkCST`, `FindAll`, `FindFirst` — depth-first CST traversal |
| `nanopass_rewrite.go` | `ReplaceNode`, `DeleteNode`, `InsertBefore`, `InsertAfter`, `TrackedRewriter` |
| `nanopass_scope.go` | `BuildScopes` — lexical scope tree with UNION ALL / CTE / subquery awareness |
| `nanopass_macro.go` | `MacroExpander` — function-call macro expansion with literal arguments |

## Scope System

`BuildScopes(pr, "defaultDB")` walks the CST and builds a tree of `SelectScope` objects:

- Enumerates all UNION ALL branches
- Resolves table aliases in FROM/JOIN
- Tags CTE references vs. real tables (`TableSource.IsCTE`)
- Links FROM subqueries and expression subqueries to inner scopes
- Tracks default database for unqualified table resolution (`TableSource.ResolvedDatabase(scope)`)

```go
scopes := nanopass.BuildScopes(pr, "production")
for _, scope := range scopes {
    for _, ts := range scope.Tables {
        db := ts.ResolvedDatabase(scope) // "production" for unqualified, explicit for qualified
    }
}
```

Database resolution matches ClickHouse behavior: each table resolves independently against the connection default, with no ambient database inheritance from sibling tables.

## Included Passes

All passes live in [`passes/`](passes). Properties shown reflect declared `PassProperties` and are corpus-checked by `AssertProperties`.

### Lexical (token-level)

| Pass | Description | Idempotent |
|------|-------------|-----------|
| `StripComments` | Removes single-line and multi-line comments | Yes |
| `CanonicalizeKeywordCase` | Uppercases SQL keywords; preserves identifier case | Yes |
| `CanonicalizeWhitespace` | Collapses whitespace, preserves single newlines | Yes |
| `CanonicalizeWhitespaceSingleLine` | Collapses all whitespace to single spaces | Yes |
| `CanonicalizeEquals` | Replaces `==` with `=` | Yes |
| `CanonicalizeIdentifiers` | Wraps identifiers in double quotes | Yes |

### Structural (scope-aware)

| Pass | Description | Idempotent |
|------|-------------|-----------|
| `QualifyTables(db)` | Adds default database prefix to unqualified tables, skips CTEs | Yes |
| `ExpandColumns(schema, defaultDB)` | Expands `*`, `table.*`, `COLUMNS('regex')` using schema | Yes |
| `WrapColumnsWithDynamic(pattern)` | Wraps matching column names in `COLUMNS('^name$')` | Yes |

### Expression Canonicalisation

| Pass | Description | Properties |
|------|-------------|-----------|
| `CanonicalizeJoin` | Strictness-before-direction, removes OUTER, comma→CROSS, parenthesises USING | Idempotent |
| `CanonicalizeSugar` | DATE/TIMESTAMP/EXTRACT/SUBSTRING/TRIM → function-call form | Idempotent |
| `CanonicalizeCasts` | `expr::Type` and `CAST(expr AS Type)` → `CAST(expr, 'Type')` | NeedsFixedPoint |
| `CanonicalizeCaseConditionals` | CASE → `if`/`multiIf`/`caseWithExpression`; leaf-level | NeedsFixedPoint |
| `CanonicalizeMultiIf` | `multiIf(c, r, d)` (3-arg) → `if(c, r, d)` | Idempotent |
| `CanonicalizeTernary` | `cond ? a : b` → `if(cond, a, b)`; leaf-level | NeedsFixedPoint |
| `CanonicalizeConstructors(form)` | tuple/array between literal and function form | NeedsFixedPoint |
| `RemoveRedundantParens` | Removes parentheses unnecessary given operator precedence | Idempotent |

`CanonicalizeFull(maxIter)` returns a `Sequence` of the above in the canonical order, ending in `CanonicalizeKeywordCase` and `CanonicalizeIdentifiers`.

### Query-Level

| Pass | Description | Properties |
|------|-------------|-----------|
| `SetFormat(name)` | Sets, replaces, or removes the FORMAT clause; mirrors into env.Format | Idempotent |
| `RemoveFormat` | `SetFormat("")` | Idempotent |
| `WriteSettings(map)` | Replaces SETTINGS clause; mirrors into env.StatementSettings | Idempotent |
| `ModifySettings(fn)` | Atomic read-modify-write of SETTINGS | (factory; not idempotent under arbitrary modifier) |

### Param Lifecycle

| Pass | Description | Properties |
|------|-------------|-----------|
| `ExtractLiterals(config)` | Walks body, replaces qualifying literals with `{name: Type}` slots, writes Raw values to `env.Params` | (factory) |
| `InjectParamsAsCTE(prefix, predicate, mapper)` | Selected `env.Params` entries become WITH-clause CTE definitions; their slots become bare references | Idempotent |
| `PruneUnreferencedParams(prefix)` | Drops `env.Params` entries no longer referenced by body | Idempotent |

### Validation

| Pass | Description |
|------|-------------|
| `nanopass.ValidateGrammar1` | Parses body with Grammar1; returns body unchanged or error |
| `nanopass.ValidateGrammar2` | Parses body with Grammar2 (canonical-only); same |
| `ValidateColumnNames(pattern)` | Checks all column names match a regex; returns body unchanged or `*ColumnNameValidationError` |

### Compile-Time Evaluation

| Component | Description |
|-----------|-------------|
| `MacroExpander` | Expands registered function calls with literal arguments into SQL fragments |
| `FunctionEvaluator` | Evaluates registered functions in Go, with recursive nested evaluation, partial evaluation, and `env.Params` slot resolution |

`FunctionEvaluator.Pass()` declares `NeedsFixedPoint: true`. It handles:

- **Full evaluation**: `myAdd(myAdd(1,2), 3)` → `6`
- **Partial evaluation**: `myAdd(a, myAdd(1,2))` → `myAdd(a, 3)`
- **Param resolution**: `{a: UInt64}` slots resolve via `env.Params`. A resolved param contributes its `Value` (unwrapped from `marshalling.ResolvedParamSlot` when `useAny=true`); an unresolved slot makes the outer call non-evaluable, mirroring `marshalling.VerbatimSql` semantics.

## AssertProperties — machine-checked annotations

`nanopass.AssertProperties(t, p, corpus)` enforces declared properties against a corpus:

- `Idempotent: true` ⇒ `p.Apply(p.Apply(b)) == p.Apply(b)` byte-for-byte.
- `NeedsFixedPoint: true` ⇒ at least one corpus entry exhibits non-convergence on the second Apply.
- `Idempotent` and `NeedsFixedPoint` not both set.

`TestAssertProperties` in [`passes_test/`](passes_test/nanopass_passes_assert_properties_test.go) covers every exported pass against the shared `testdata/corpus/`, with a small additional nested-forms corpus for passes that declare `NeedsFixedPoint`.

## Usage Examples

### Pipeline

```go
result, err := nanopass.Sequence("normalize+validate",
    passes.StripComments,
    passes.CanonicalizeKeywordCase,
    passes.RemoveRedundantParens,
    passes.QualifyTables("production"),
    passes.SetFormat("JSON"),
    passes.CanonicalizeWhitespaceSingleLine,
    nanopass.ValidateGrammar1,
).Run(sql)
```

### Macro Expansion

```go
expander := nanopass.NewMacroExpander()
expander.Register("jsonCol", func(args []nanopass.LiteralArg) (string, error) {
    return "JSONExtractString(payload, " + args[0].Value + ")", nil
})

result, err := nanopass.Sequence("expand+validate",
    expander.Pass(),
    nanopass.ValidateGrammar1,
).Run(sql)
```

### Compile-Time Function Evaluation

```go
eval := passes.NewFunctionEvaluator()
eval.RegisterBuiltins() // array(), tuple()
eval.Register("daysInMonth", func(args []any) (any, error) {
    year, month := args[0].(uint64), args[1].(uint64)
    // ... compute ...
    return days, nil
}, true)

result, err := eval.Pass().Run("SELECT daysInMonth(2024, 2)")
// result: "SELECT 29"
```

### Param Lifecycle

```go
// 1) Extract literals; SET param_* lines emitted into env.Params on Run.
result, err := nanopass.Sequence("extract+inject+prune",
    passes.ExtractLiterals(passes.NewExtractLiteralsConfig(0)),
    passes.InjectParamsAsCTE("", nil, nil),
    passes.PruneUnreferencedParams(""),
).Run(sql)
```

### Settings Manipulation

```go
result, err := passes.ModifySettings(func(settings map[string]any) error {
    if v, ok := settings["max_threads"]; ok {
        settings["max_threads"] = v.(int64) * 2
    }
    settings["optimize_read_in_order"] = int64(1)
    return nil
}).Run("SELECT a FROM t SETTINGS max_threads = 4")
```

### Column Expansion with Schema

```go
schema := passes.NewStaticSchemaProvider(map[string][]string{
    "prod.orders": {"id", "amount", "tenant_id", "created"},
    "customers":   {"id", "name", "email"},
})
result, err := passes.ExpandColumns(schema, "prod").Run(
    "SELECT * FROM orders AS o JOIN customers AS c ON o.id = c.id",
)
```

## Analysis Functions

| Function | Package | Returns |
|----------|---------|---------|
| `ExtractTables(pr)` | `analysis` | `[]TableRef` — table references (excluding column qualifiers) |
| `ExtractColumns(pr)` | `analysis` | `[]ColumnRef` — column references with optional table qualifier |
| `ExtractFunctions(pr)` | `analysis` | `[]FunctionRef` — function calls (regular, parametric, window) |

## Test Corpus

Embedded SQL files in `testdata/corpus/` cover SELECT features from simple literals to complex CTEs with window functions, UNION ALL, parametric aggregates, JSON functions, ARRAY JOIN, PREWHERE, SETTINGS with arrays/tuples, and FORMAT clauses. Loaded via `embed.FS` with `testdata.LoadCorpus()`.

## Test Strategy

Every pass is tested with:

1. **Explicit input/output pairs** — 5–10+ cases covering the transformation
2. **Idempotency / fixpoint** — `AssertProperties` corpus check enforces declared `Idempotent` / `NeedsFixedPoint`
3. **Corpus validity** — every corpus entry produces parseable SQL
4. **UNION ALL / CTEs / subqueries** — transformations apply to all branches; CTE references untouched, CTE bodies transformed
5. **Invalid SQL rejection** — empty strings, whitespace, incomplete statements
6. **Pipeline integration** — composes correctly with other passes

Additional robustness tests: pipeline ordering permutations, scope structure preservation for pure passes, full corpus × all passes cross-product.

## Dependencies

- `github.com/antlr4-go/antlr/v4` — ANTLR4 Go runtime
- `github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1`, `grammar2` — Generated ClickHouse lexer/parser
- `github.com/stergiotis/boxer/public/db/clickhouse/dsl/env` — Environment package
- `github.com/stergiotis/boxer/public/observability/eh` — Error handling
- `github.com/stretchr/testify` — Test assertions

## Grammar Modifications

The upstream ClickHouse ANTLR4 grammar has these required modifications:

- **Whitespace/comments**: `-> skip` changed to `-> channel(HIDDEN)` for `WHITESPACE`, `SINGLE_LINE_COMMENT`, `MULTI_LINE_COMMENT`. Without this, the `TokenStreamRewriter` cannot preserve original formatting.
- **Setting values**: `settingExpr` extended with `settingValue` rule supporting arrays (`[1,2]`), tuples (`(1,2)`), and function-form constructors (`array(1,2)`, `tuple(1,2)`) alongside scalar literals.

## Known Grammar Limitations

- `FROM t SELECT a` (FROM-first syntax)
- `WITH (SELECT x) AS name` (scalar subquery CTE)
- `EXISTS (SELECT ...)` (EXISTS predicate)
- `* EXCEPT(col)`, `COLUMNS('...') APPLY(func)`, `REPLACE(...)` (column modifiers)
- Map literals in SET (`SET param = {'key': [1,2]}`)
