---
name: clickhouse-nanopass
description: "Use this skill when writing ClickHouse SQL transformation passes, pipelines, macro expanders, or analysis functions using the nanopass framework. Triggers include: any mention of 'nanopass', 'SQL pass', 'SQL transformation', 'SQL rewrite', 'ClickHouse pass', 'macro expansion', 'qualify tables', 'add WHERE', or requests to manipulate ClickHouse SQL programmatically in Go. Also use when the user wants to parse ClickHouse SQL, walk a CST, rewrite tokens, build scope-aware transformations, or compose SQL→SQL pipelines. Do NOT use for general SQL querying, ClickHouse client usage, or ORM-based database access."
---

# ClickHouse SQL Nanopass Framework

## Critical Rules — Read Before Writing Any Code

1. **Every pass re-parses from scratch.** A pass receives a `string` and returns a `string`. Never share a `ParseResult` or `TokenStreamRewriter` across passes.
2. **Every pass must return syntactically valid SQL.** If you cannot guarantee this, add `nanopass.Validate` after your pass in the pipeline.
3. **Use `BuildScopes` for any pass that touches tables, WHERE clauses, or needs UNION ALL awareness.** Never use `FindAll`/`FindFirst` for table references — use scopes instead.
4. **Never use `FindAll` to locate `TableIdentifierContext` directly.** `TableIdentifier` appears both in FROM clauses and inside `ColumnIdentifier` (as column qualifiers like `t1.id`). Scopes handle this correctly.
5. **The grammar parses `NOT(x)` as a function call, not logical NOT.** Only `NOT x` (without parens) produces `ColumnExprNotContext`.
6. **Whitespace is on the hidden channel.** The lexer uses `-> channel(HIDDEN)` for whitespace and comments. The `TokenStreamRewriter` preserves them automatically.

## Module Layout

```
github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/
├── parse.go          # Parse(sql) → *ParseResult
├── walk.go           # WalkCST, FindAll, FindFirst
├── rewrite.go        # NewRewriter, ReplaceNode, DeleteNode, InsertBefore, InsertAfter, etc.
│                     # TrackedRewriter for overlap detection
├── pipeline.go       # Pass type, Pipeline, FixedPoint, FixedPointPipeline, Validate
├── scope.go          # BuildScopes → []*SelectScope, table alias resolution, CTE tracking
├── macro.go          # MacroExpander, ExtractLiteralArgs, LiteralArg, LiteralTypeE
├── analysis/
│   ├── tables.go     # ExtractTables
│   ├── columns.go    # ExtractColumns
│   └── functions.go  # ExtractFunctions
├── passes/
│   ├── normalize_case.go
│   ├── normalize_whitespace.go
│   ├── strip_comments.go
│   ├── remove_parens.go
│   ├── qualify_tables.go
│   ├── add_where.go
│   ├── add_settings.go
│   ├── rewrite_functions.go
│   └── helpers.go    # findOutermostSelectStmt, findLastSelectStmtClause
└── testdata/
    ├── corpus.go     # LoadCorpus() via embed.FS
    └── corpus/       # 56 .sql files
```

## Dependencies and Imports

```go
import (
    "github.com/antlr4-go/antlr/v4"
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
    "github.com/stergiotis/boxer/public/observability/eh"
    "github.com/rs/zerolog"
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)
```

## Coding Standards (Mandatory)

- **Named return values** on all functions/methods
- **Naked returns** after setting `err`
- **No `if err := func(); err != nil`** — always assign then check
- **Receiver name always `inst`**
- **Interfaces end with `I`**, enums end with `E`
- **Use `eh.Errorf` with `%w`** for error wrapping
- **Pre-allocate slices/maps** when size is known
- **Explicit anonymous blocks** for scoping within large functions

## How to Write a New Pass

### Template: Simple Token-Level Pass

For passes that operate on individual tokens (keywords, whitespace, comments):

```go
package passes

import (
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
    "github.com/stergiotis/boxer/public/observability/eh"
    "github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

func MyTokenPass(sql string) (result string, err error) {
    pr, err := nanopass.Parse(sql)
    if err != nil {
        err = eh.Errorf("MyTokenPass: %w", err)
        return
    }
    rw := nanopass.NewRewriter(pr)

    for i := 0; i < pr.TokenStream.Size(); i++ {
        tok := pr.TokenStream.Get(i)
        tokenType := tok.GetTokenType()
        // Operate on tokens here
        // Use: nanopass.ReplaceToken(rw, tok.GetTokenIndex(), newText)
        // Use: nanopass.DeleteToken(rw, tok.GetTokenIndex())
        _ = tokenType
    }

    result = nanopass.GetText(rw)
    return
}
```

### Template: CST Node Pass (No Scope Needed)

For passes that match specific CST node types but don't need table/CTE awareness:

```go
func MyNodePass(sql string) (result string, err error) {
    pr, err := nanopass.Parse(sql)
    if err != nil {
        err = eh.Errorf("MyNodePass: %w", err)
        return
    }
    rw := nanopass.NewRewriter(pr)

    nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
        switch c := ctx.(type) {
        case *grammar.SomeSpecificContext:
            // Operate on the node
            nanopass.ReplaceNode(rw, c, "new text")
            return false // don't descend into replaced node
        }
        return true // continue walking
    })

    result = nanopass.GetText(rw)
    return
}
```

### Template: Scope-Aware Pass

For passes that touch tables, WHERE clauses, or need UNION ALL awareness:

```go
func MyStructuralPass(param string) nanopass.Pass {
    return func(sql string) (result string, err error) {
        pr, err := nanopass.Parse(sql)
        if err != nil {
            err = eh.Errorf("MyStructuralPass: %w", err)
            return
        }
        rw := nanopass.NewRewriter(pr)

        scopes := nanopass.BuildScopes(pr)
        for _, scope := range scopes {
            applyToScope(rw, scope, param)
        }

        result = nanopass.GetText(rw)
        return
    }
}

func applyToScope(rw *antlr.TokenStreamRewriter, scope *nanopass.SelectScope, param string) {
    // Access scope.Node (*grammar.SelectStmtContext) for clauses:
    //   scope.Node.WhereClause()
    //   scope.Node.FromClause()
    //   scope.Node.ProjectionClause()
    //   scope.Node.OrderByClause()
    //   scope.Node.GroupByClause()
    //   scope.Node.HavingClause()
    //   scope.Node.LimitClause()
    //   scope.Node.SettingsClause()

    // Access scope.Tables for FROM/JOIN table sources:
    for _, ts := range scope.Tables {
        if ts.IsCTE || ts.IsSubquery {
            continue
        }
        // ts.Table, ts.Database, ts.Alias, ts.Node
    }

    // Resolve aliases:
    // source, found := scope.ResolveAlias("alias_or_table_name")

    // Check CTE names:
    // def, found := scope.ResolveCTE("cte_name")

    // Recurse into CTE bodies:
    for _, cte := range scope.CTEDefs {
        if cte.Scope != nil {
            applyToScope(rw, cte.Scope, param)
        }
    }

    // Recurse into subqueries:
    for _, sub := range scope.Subqueries {
        applyToScope(rw, sub, param)
    }

    // Recurse into FROM subquery scopes:
    for _, ts := range scope.Tables {
        if ts.IsSubquery && ts.Scope != nil {
            applyToScope(rw, ts.Scope, param)
        }
    }
}
```

### Template: Parameterized Pass (Returns `nanopass.Pass`)

When a pass needs configuration, return a closure:

```go
func QualifyTables(defaultDB string) nanopass.Pass {
    return func(sql string) (result string, err error) {
        // ... implementation using defaultDB
    }
}
```

### Template: Macro Registration

```go
expander := nanopass.NewMacroExpander()
expander.Register("macroName", func(args []nanopass.LiteralArg) (string, error) {
    // args[i].Type: LiteralTypeString, LiteralTypeInt, LiteralTypeFloat, LiteralTypeNull
    // args[i].Value: raw text (strings include quotes, e.g. "'hello'")
    return "expanded SQL fragment", nil
})
// Use: expander.Pass() returns a nanopass.Pass
// For nested macros: nanopass.FixedPoint(expander.Pass(), 10)
```

## How to Write Tests for a Pass

Every pass MUST have these four test categories:

### 1. Explicit Input/Output Pairs

```go
func TestMyPass(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {name: "basic", input: "SELECT a FROM t", expected: "SELECT a FROM t"},
        // Add 5-10 cases covering the transformation
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := passes.MyPass(tt.input)
            require.NoError(t, err)
            assert.Equal(t, tt.expected, got)

            // ALWAYS verify output is parseable
            _, err = nanopass.Parse(got)
            require.NoError(t, err, "produced invalid SQL: %s", got)
        })
    }
}
```

### 2. Idempotency

```go
func TestMyPassIdempotent(t *testing.T) {
    sqls := []string{
        "SELECT a FROM t",
        "SELECT a FROM t WHERE x > 1",
        // Include cases that exercise the transformation
    }
    for i, sql := range sqls {
        t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
            pass1, err := passes.MyPass(sql)
            require.NoError(t, err)
            pass2, err := passes.MyPass(pass1)
            require.NoError(t, err)
            assert.Equal(t, pass1, pass2, "not idempotent")
        })
    }
}
```

### 3. Corpus Validity

```go
func TestMyPassOutputValidity(t *testing.T) {
    entries, err := testdata.LoadCorpus()
    require.NoError(t, err)

    for _, entry := range entries {
        t.Run(entry.Name, func(t *testing.T) {
            out, err := passes.MyPass(entry.SQL)
            if err != nil {
                t.Skipf("pass failed: %v", err)
            }
            _, err = nanopass.Parse(out)
            require.NoError(t, err, "produced invalid SQL for %s:\n%s", entry.Name, out)
        })
    }
}
```

### 4. Scope Preservation (for pure passes only)

Pure passes (case normalization, whitespace, comments, parens) must not change the query structure:

```go
func TestMyPassPreservesScopes(t *testing.T) {
    entries, err := testdata.LoadCorpus()
    require.NoError(t, err)

    for _, entry := range entries {
        t.Run(entry.Name, func(t *testing.T) {
            prBefore, err := nanopass.Parse(entry.SQL)
            if err != nil { t.Skip() }

            out, err := passes.MyPass(entry.SQL)
            if err != nil { t.Skip() }

            prAfter, err := nanopass.Parse(out)
            require.NoError(t, err)

            scopesBefore := nanopass.BuildScopes(prBefore)
            scopesAfter := nanopass.BuildScopes(prAfter)
            require.Equal(t, len(scopesBefore), len(scopesAfter))
        })
    }
}
```

### Additional: UNION ALL Tests (for structural passes)

```go
func TestMyPassUnionAll(t *testing.T) {
    // Always test with 2+ UNION ALL branches
    sql := "SELECT a FROM t1 UNION ALL SELECT b FROM t2"
    got, err := passes.MyPass(sql)
    require.NoError(t, err)
    // Verify the transformation applied to BOTH branches
}
```

### Additional: CTE Tests (for structural passes)

```go
func TestMyPassCTEs(t *testing.T) {
    // Verify CTE references are not accidentally modified
    sql := "WITH cte AS (SELECT a FROM real_table) SELECT x FROM cte"
    got, err := passes.MyPass(sql)
    require.NoError(t, err)
    // Verify: "FROM cte" unchanged, "FROM real_table" was transformed
}
```

### Additional: Invalid SQL Rejection

```go
func TestMyPassRejectsInvalid(t *testing.T) {
    invalid := []string{"", "   ", "SELECT", ";;;"}
    for _, sql := range invalid {
        _, err := passes.MyPass(sql)
        assert.Error(t, err)
    }
}
```

## ANTLR4 Go Runtime API Quick Reference

```go
// Parsing
pr, err := nanopass.Parse(sql)
// pr.Tree        — *grammar.QueryStmtContext (root)
// pr.TokenStream — *antlr.CommonTokenStream
// pr.Parser      — *grammar.ClickHouseParser

// Token access
tok := pr.TokenStream.Get(i)     // antlr.Token
tok.GetTokenType()                // int
tok.GetText()                     // string
tok.GetTokenIndex()               // int
tok.GetChannel()                  // int (0=default, 1=hidden)
pr.TokenStream.Size()             // int (total tokens including hidden + EOF)

// Rewriter
rw := nanopass.NewRewriter(pr)
nanopass.ReplaceNode(rw, ctx, "new text")
nanopass.DeleteNode(rw, ctx)
nanopass.InsertBefore(rw, ctx, "prefix ")
nanopass.InsertAfter(rw, ctx, " suffix")
nanopass.ReplaceToken(rw, tokenIndex, "new text")
nanopass.DeleteToken(rw, tokenIndex)
result := nanopass.GetText(rw)

// CST navigation
ctx.GetStart().GetTokenIndex()    // first token index
ctx.GetStop().GetTokenIndex()     // last token index
ctx.GetParent()                   // antlr.Tree
ctx.GetChildCount()               // int
ctx.GetChild(i)                   // antlr.Tree
ctx.GetRuleIndex()                // int
```

## Key Grammar Types

### Token Type Constants

```go
// Keywords: grammar.ClickHouseLexerADD (1) through grammar.ClickHouseLexerJSON_TRUE (199)
// IDENTIFIER = 200
// STRING_LITERAL = 205
// WHITESPACE = 240 (on hidden channel)
// MULTI_LINE_COMMENT, SINGLE_LINE_COMMENT (on hidden channel)
```

### CST Context Types for Type-Switching

**Column expressions** (alternatives of `columnExpr` rule, all have `GetRuleIndex() == RULE_columnExpr`):

```go
*grammar.ColumnExprOrContext           // a OR b
*grammar.ColumnExprAndContext          // a AND b
*grammar.ColumnExprNotContext          // NOT a (only without parens after NOT)
*grammar.ColumnExprIsNullContext       // a IS [NOT] NULL
*grammar.ColumnExprPrecedence3Context  // =, !=, <, >, <=, >=, [NOT] IN, [NOT] LIKE, GLOBAL [NOT] IN
*grammar.ColumnExprBetweenContext      // a [NOT] BETWEEN b AND c
*grammar.ColumnExprPrecedence2Context  // +, -, ||
*grammar.ColumnExprPrecedence1Context  // *, /, %
*grammar.ColumnExprNegateContext       // -a (unary)
*grammar.ColumnExprTernaryOpContext    // a ? b : c
*grammar.ColumnExprLiteralContext      // 42, 'hello', NULL
*grammar.ColumnExprIdentifierContext   // column_name
*grammar.ColumnExprFunctionContext     // func(args) — includes NOT(x)!
*grammar.ColumnExprParensContext       // (expr) — single expression in parens
*grammar.ColumnExprTupleContext        // (expr, expr, ...) — 2+ expressions
*grammar.ColumnExprSubqueryContext     // (SELECT ...)
*grammar.ColumnExprCaseContext         // CASE WHEN ... END
*grammar.ColumnExprCastContext         // CAST(x AS T)
*grammar.ColumnExprAliasContext        // expr AS alias
*grammar.ColumnExprArrayContext        // [1, 2, 3]
*grammar.ColumnExprArrayAccessContext  // arr[i]
*grammar.ColumnExprTupleAccessContext  // t.1
*grammar.ColumnExprWinFunctionContext  // func() OVER (...)
*grammar.ColumnExprParamSlotContext    // {name: Type}
```

**Table expressions** (alternatives of `tableExpr` rule):

```go
*grammar.TableExprIdentifierContext  // table_name or db.table_name
*grammar.TableExprAliasContext       // tableExpr AS alias
*grammar.TableExprSubqueryContext    // (SELECT ...) in FROM
*grammar.TableExprFunctionContext    // table_function(args)
```

**Join expressions** (alternatives of `joinExpr` rule):

```go
*grammar.JoinExprTableContext        // single table source
*grammar.JoinExprOpContext           // left JOIN right ON condition
```

**Key accessor patterns on SelectStmtContext:**

```go
stmt.FromClause()        // IFromClauseContext or nil
stmt.WhereClause()       // IWhereClauseContext or nil
stmt.ProjectionClause()  // IProjectionClauseContext
stmt.GroupByClause()     // IGroupByClauseContext or nil
stmt.HavingClause()      // IHavingClauseContext or nil
stmt.OrderByClause()     // IOrderByClauseContext or nil
stmt.LimitClause()       // ILimitClauseContext or nil
stmt.LimitByClause()     // ILimitByClauseContext or nil
stmt.SettingsClause()    // ISettingsClauseContext or nil
stmt.PrewhereClause()    // IPrewhereClauseContext or nil
stmt.ArrayJoinClause()   // IArrayJoinClauseContext or nil
stmt.WindowClause()      // IWindowClauseContext or nil
```

All clause accessors return interfaces. Cast to concrete types: `stmt.WhereClause().(*grammar.WhereClauseContext)`.

## SelectScope Reference

```go
type SelectScope struct {
    Node       *grammar.SelectStmtContext  // the SELECT statement
    Tables     []TableSource               // FROM/JOIN sources
    Parent     *SelectScope                // enclosing scope (nil for outermost)
    CTEDefs    []CTEDef                    // WITH clause definitions
    UnionPeers []*SelectScope              // all branches including self
    Subqueries []*SelectScope              // expression subqueries (WHERE, SELECT list, HAVING)
}

type TableSource struct {
    Node       antlr.ParserRuleContext     // TableIdentifierContext or TableExprSubqueryContext
    Database   string                      // empty if unqualified
    Table      string                      // table name
    Alias      string                      // AS alias, empty if none
    IsCTE      bool                        // references a CTE name
    IsSubquery bool                        // FROM (SELECT ...)
    Scope      *SelectScope                // inner scope for subqueries
}

// Key methods:
scope.ResolveAlias(name) → (TableSource, bool)  // alias or unaliased table name lookup
scope.ResolveCTE(name) → (CTEDef, bool)          // walks ancestors
scope.AllScopes() → []*SelectScope               // depth-first flattened
```

## Operator Precedence Table

Used by `RemoveRedundantParens`. Higher number = tighter binding.

| Level | Operators | CST Context |
|-------|-----------|-------------|
| 1 | OR | `ColumnExprOr` |
| 2 | AND | `ColumnExprAnd` |
| 3 | NOT (unary) | `ColumnExprNot` |
| 4 | IS [NOT] NULL | `ColumnExprIsNull` |
| 5 | =, !=, <, >, IN, LIKE, BETWEEN | `ColumnExprPrecedence3`, `ColumnExprBetween` |
| 6 | +, -, \|\| | `ColumnExprPrecedence2` |
| 7 | *, /, % | `ColumnExprPrecedence1` |
| 8 | unary - | `ColumnExprNegate` |
| 9 | ?: ternary | `ColumnExprTernaryOp` |
| 99 | atoms | literals, identifiers, functions, etc. |

## Common Pitfalls

1. **Don't use `rw.GetTextDefault()` directly.** Use `nanopass.GetText(rw)` which wraps it.
2. **Don't modify the CST.** Only use the `TokenStreamRewriter`. The CST is read-only after parsing.
3. **`WalkCST` returns `false` to skip a subtree**, not to stop the walk. There is no "stop early" mechanism — set a flag and check it.
4. **`GetChild(i)` returns `antlr.Tree`**, not `antlr.ParserRuleContext`. Always type-assert: `if stmt, ok := node.GetChild(i).(*grammar.SelectStmtContext); ok { ... }`.
5. **Go generics don't work reliably with ANTLR types** for `findFirstChild`-style helpers. Use explicit `GetChild` + type assertion loops.
6. **`IN (expr)` with a single value** — the `(expr)` is parsed as `ColumnExprParens`, not `ColumnExprTuple`. If your pass removes parens, guard against removing IN's right-operand parens.
7. **`ColumnExprAliasContext`** can appear anywhere in the select list. Don't confuse it with `TableExprAliasContext`.
8. **The token stream includes EOF** as the last token with type `-1`. Always check `tok.GetTokenType() == antlr.TokenEOF` or `tok.GetTokenType() == -1` when iterating.
9. **Overlapping `ReplaceDefault` calls** — the later one silently wins. Use `TrackedRewriter` during development to detect this.
10. **The `NamedQueryContext` inside CTEs** has an `Identifier()` method for the CTE name and a `Query()` for the body. Access the body's `SelectUnionStmtContext` by walking children of the `QueryContext`.

## Pipeline Composition

```go
// Sequential pipeline
result, err := nanopass.Pipeline(sql,
    passes.StripComments,
    passes.NormalizeKeywordCase,
    passes.QualifyTables("mydb"),
    passes.AddWhereCondition("tenant_id = 1"),
    nanopass.Validate,
)

// Fixed-point (repeat until stable)
pass := nanopass.FixedPoint(passes.RemoveRedundantParens, 10)

// Fixed-point pipeline
pass := nanopass.FixedPointPipeline(5,
    expander.Pass(),
    passes.NormalizeKeywordCase,
)

// Logging wrapper
debugPass := nanopass.LoggingPass(logger, "my_pass", passes.MyPass)
```

## Known Grammar Limitations

These ClickHouse features are NOT supported by the parser:

- `FROM t SELECT a` (FROM-first syntax)
- `WITH (SELECT x) AS name` (scalar subquery CTE)
- `SET param = {'key': [1,2]}` (complex SET literal values)
- `EXISTS (SELECT ...)` (EXISTS predicate)

Queries using these features will fail at `Parse()` with a syntax error. Add them to the corpus as `.sql` files if/when grammar support is added.