---
name: clickhouse-nanopass
description: "Use this skill when writing ClickHouse SQL transformation passes, pipelines, macro expanders, or analysis functions using the nanopass framework. Triggers include: any mention of 'nanopass', 'SQL pass', 'SQL transformation', 'SQL rewrite', 'ClickHouse pass', 'macro expansion', 'qualify tables', 'add WHERE', or requests to manipulate ClickHouse SQL programmatically in Go. Also use when the user wants to parse ClickHouse SQL, walk a CST, rewrite tokens, build scope-aware transformations, or compose SQL→SQL pipelines. Do NOT use for general SQL querying, ClickHouse client usage, or ORM-based database access."
type: reference
audience: agent reading this skill
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# ClickHouse SQL Nanopass Framework

## Critical Rules — Read Before Writing Any Code

1. **Every pass re-parses from scratch.** A pass receives a `string` and returns a `string`. Never share a `ParseResult` or `TokenStreamRewriter` across passes.
2. **Every pass must return syntactically valid SQL.** If you cannot guarantee this, add `nanopass.Validate` after your pass in the pipeline.
3. **Use `BuildScopes` for any pass that touches tables, WHERE clauses, or needs UNION ALL awareness.** Never use `FindAll`/`FindFirst` for table references — use scopes instead.
4. **Never use `FindAll` to locate `TableIdentifierContext` directly.** `TableIdentifier` appears both in FROM clauses and inside `ColumnIdentifier` (as column qualifiers like `t1.id`). Scopes handle this correctly.
5. **The grammar parses `NOT(x)` as a function call, not logical NOT.** Only `NOT x` (without parens) produces `ColumnExprNotContext`.
6. **Whitespace is on the hidden channel.** The lexer uses `-> channel(HIDDEN)` for whitespace and comments. The `TokenStreamRewriter` preserves them automatically.
7. **Always run a diagnostic test before writing a pass.** Use `WalkCST` + `fmt.Sprintf("%T", ctx)` to verify which CST node types the grammar actually produces. Assumed type names are wrong more often than right.
8. **ANTLR Go generics don't work.** Never use `findFirstChild[T]` with generics — use explicit `GetChild(i)` + type assertion loops.

## Lessons Learned From Building Passes

These lessons were learned through actual debugging sessions. Each one cost significant time. Read them carefully.

### Lesson 1: Always Verify CST Structure Before Coding

The grammar produces context types that are often surprising. Before writing any pass, run a diagnostic:

```go
func TestDebugMyFeature(t *testing.T) {
    sqls := []string{"SELECT ...", "SELECT ..."}
    for _, sql := range sqls {
        t.Logf("--- SQL: %s", sql)
        pr, err := nanopass.Parse(sql)
        if err != nil {
            t.Logf("  PARSE ERROR: %v", err)
            continue
        }
        nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
            typeName := fmt.Sprintf("%T", ctx)
            if strings.Contains(typeName, "MyKeyword") {
                t.Logf("  %T text=%q", ctx, ctx.GetText())
                for i := 0; i < ctx.GetChildCount(); i++ {
                    t.Logf("    child[%d]: %T", i, ctx.GetChild(i))
                }
            }
            return true
        })
    }
}
```

Real examples of surprises we encountered:
- `NOT(a)` → `ColumnExprFunctionContext` with identifier `NOT`, not `ColumnExprNotContext`
- `SELECT * FROM t` → `ColumnsExprAsteriskContext`, not `ColumnExprAsteriskContext`
- UNION ALL first branch → bare `SelectStmtWithParensContext`, subsequent branches → `SelectUnionStmtItemContext` wrapping `SelectStmtWithParensContext`
- `FROM t AS x` → `JoinExprTableContext` → `TableExprAliasContext` (not `TableExprIdentifierContext` directly)
- FORMAT clause lives on `QueryStmtContext`, not `SelectStmtContext`
- Settings values use `settingValue` rule types (`SettingLiteralContext`, `SettingArrayContext`, etc.), completely separate from `columnExpr` types

### Lesson 2: The Token Stream Determines Everything

The `TokenStreamRewriter` operates on token indices. Key facts:

- The original grammar used `-> skip` for whitespace, which **destroyed** whitespace tokens entirely. We changed it to `-> channel(HIDDEN)`. Without this fix, `GetTextDefault()` produces `SELECTaFROMt`.
- `ReplaceDefault(start, stop, text)` is the correct method — NOT `ReplaceTokenDefaultPos` (which doesn't exist in antlr4-go v4.13.1).
- `DeleteTokenDefaultPos` doesn't exist either. Use `DeleteDefault(start, stop)`.
- `InsertBeforeDefault` and `InsertAfterDefault` take `string`, not `interface{}`.
- When you delete tokens (e.g., parentheses in `RemoveRedundantParens`), whitespace tokens around them survive on the hidden channel. This can leave double spaces. Use `NormalizeWhitespaceSingleLine` as the last pass to clean up.

### Lesson 3: UNION ALL Is The #1 Source of Bugs

Every pass that targets "the SELECT statement" will silently ignore UNION ALL branches if it uses `FindFirst` to locate `SelectStmtContext`. This is the single most common bug.

**Wrong pattern:**
```go
selectStmt := findOutermostSelectStmt(pr)  // finds first branch only!
// ... modify selectStmt
```

**Correct pattern:**
```go
scopes := nanopass.BuildScopes(pr)
for _, scope := range scopes {  // iterates all UNION ALL branches
    applyToScope(rw, scope, ...)
}
```

The UNION ALL tree structure is non-obvious:
```
SelectUnionStmtContext
  ├── SelectStmtWithParensContext        (first branch — direct child)
  ├── SelectUnionStmtItemContext         (second branch — wrapped!)
  │     ├── TerminalNode "UNION"
  │     ├── TerminalNode "ALL"
  │     └── SelectStmtWithParensContext  (actual SELECT)
  └── SelectUnionStmtItemContext         (third branch — also wrapped)
        └── ...
```

### Lesson 4: Scope Recursion Has Four Legs

When writing a scope-aware pass, you must recurse into ALL four scope containers. Missing any one of them silently drops transformations:

```go
func applyToScope(rw *antlr.TokenStreamRewriter, scope *nanopass.SelectScope, ...) (err error) {
    // 1. Process THIS scope's tables/clauses
    for _, ts := range scope.Tables { ... }

    // 2. Recurse into CTE body scopes
    for _, cte := range scope.CTEDefs {
        if cte.Scope != nil {
            err = applyToScope(rw, cte.Scope, ...)
            if err != nil { return }
        }
    }

    // 3. Recurse into FROM subquery scopes
    for _, ts := range scope.Tables {
        if ts.IsSubquery && ts.Scope != nil {
            err = applyToScope(rw, ts.Scope, ...)
            if err != nil { return }
        }
    }

    // 4. Recurse into expression subqueries (WHERE, SELECT list, HAVING)
    for _, sub := range scope.Subqueries {
        err = applyToScope(rw, sub, ...)
        if err != nil { return }
    }
    return
}
```

Forgetting leg 3 (FROM subqueries) means `SELECT * FROM (SELECT a FROM inner_table)` won't be transformed.
Forgetting leg 4 (expression subqueries) means `WHERE x IN (SELECT b FROM inner_table)` won't be transformed.

### Lesson 5: CTE References vs Real Tables

`QualifyTables("mydb")` on `WITH cte AS (SELECT a FROM t) SELECT * FROM cte` must:
- Qualify `t` → `mydb.t` (real table inside CTE body)
- NOT qualify `cte` (it's a CTE reference, not a real table)
- `scope.Tables[i].IsCTE` tells you which is which

### Lesson 6: Alias Rewriting in Predicates

When injecting predicates (like RLS policies) that reference table names, you must substitute aliases:
- Policy says `orders.tenant_id = currentUser()`
- Query says `FROM orders AS o`
- Injected predicate must say `o.tenant_id = currentUser()`

Use `scope.ResolveAlias()` to find the alias, then string-replace the table name prefix.

Watch for word boundaries: `orders.` must not match inside `my_orders.`. Check that the character before the match is not an identifier character.

### Lesson 7: `settingValue` vs `columnExpr` — Two Parallel Worlds

Settings values (`SETTINGS key = value`) use a completely different set of CST types than query expressions:

| Query expression | Setting value |
|-----------------|---------------|
| `ColumnExprTupleContext` | `SettingTupleContext` |
| `ColumnExprArrayContext` | `SettingArrayContext` |
| `ColumnExprFunctionContext` | `SettingFunctionContext` |
| `ColumnExprLiteralContext` | `SettingLiteralContext` |

A pass that canonicalizes tuple/array syntax in expressions must ALSO walk `settingValue` nodes separately if it wants to affect SETTINGS values. One `WalkCST` call handles expressions; a second walk handles settings.

### Lesson 8: Serialization/Deserialization of Setting Values

When reading settings into Go and writing them back:

| ClickHouse | Go type |
|------------|---------|
| Integer | `int64` |
| Float | `float64` |
| String `'hello'` | `string` (without quotes) |
| NULL | `nil` |
| Array `[1, 2]` | `[]any` |
| Tuple `(1, 2)` | `*Tuple` |
| Boolean | `bool` → serialized as `1`/`0` |

Key subtleties:
- String deserialization must strip surrounding quotes
- String serialization must escape `'` → `\'` and `\` → `\\`
- Number parsing: try `int64` first, then `float64`
- Empty array `[]` deserializes to `[]any{}` (empty slice, not nil)
- Empty tuple `tuple()` deserializes to `*Tuple` with length 0
- Tuple literal `()` requires minimum 2 elements — `tuple()` with 0 args has no literal equivalent
- Sort keys when serializing back for deterministic output

### Lesson 9: COLUMNS('regex') and Star Expansion

`*`, `table.*`, and `COLUMNS('regex')` are three distinct CST patterns:
- `*` → `ColumnsExprAsteriskContext` (no `TableIdentifierContext` child)
- `table.*` → `ColumnsExprAsteriskContext` (with `TableIdentifierContext` child)
- `COLUMNS('regex')` → `ColumnsExprColumnContext` → `ColumnExprDynamicContext` → `DynamicColumnSelectionContext`

When expanding `*` across joined tables, use `scope.Tables` iteration order for column ordering.

If ANY table in scope is missing from the schema, leave bare `*` unexpanded (otherwise the column count would be wrong). But `table.*` can still be expanded if that specific table is in the schema.

`EXCEPT`, `APPLY`, and `REPLACE` modifiers are NOT supported by the grammar. Detect them with `DetectUnsupportedColumnSyntax()` and use Go-side `WithExcept`/`WithApply`/`WithReplace` options instead.

Use `regexp.QuoteMeta` for escaping column names into regex patterns — never write your own escape function.

### Lesson 10: FORMAT Clause Location

FORMAT is at `QueryStmtContext` level, NOT inside `SelectStmtContext`:
```
QueryStmtContext
  QueryContext          ← contains the SELECT(s)
  TerminalNode "FORMAT" ← FORMAT keyword
  IdentifierOrNullContext ← format name
  TerminalNode "<EOF>"
```

To add FORMAT, insert after the last non-EOF token. To replace FORMAT, replace the `IdentifierOrNullContext`. To remove FORMAT, delete from the FORMAT keyword through the format name, including preceding whitespace.

### Lesson 11: Idempotency Is Not Automatic

Some passes are naturally idempotent (e.g., `NormalizeKeywordCase` — already-uppercased keywords stay uppercased). Others are NOT:
- `AddWhereCondition` — running twice doubles the predicate
- `EnforceRLS` — running twice injects duplicate policies
- `WrapColumnsWithDynamic` — IS idempotent because `COLUMNS('^name$')` is a `ColumnExprDynamic`, not a `ColumnExprIdentifier`, so the second pass doesn't match it

Document non-idempotent passes explicitly. Use them once in a pipeline, not in `FixedPoint`.

### Lesson 12: FixedPoint for Nested Transformations

Single-pass rewriting replaces CST nodes with text strings. The replacement text is NOT re-parsed within the same pass. This means nested patterns require multiple passes:
- `outer_macro(inner_macro(1))` — first pass expands `outer_macro`, second pass (after re-parse) expands `inner_macro`
- `(((a)))` — `RemoveRedundantParens` may only remove one layer per pass depending on walk order
- Nested `array(tuple(...))` in settings — outer `array()` becomes `[tuple(...)]`, needs second pass to handle inner `tuple()`

Use `nanopass.FixedPoint(pass, maxIterations)` for these cases.

## Module Layout

```
nanopass/
├── parse.go              # Parse(sql) → *ParseResult
├── walk.go               # WalkCST, FindAll, FindFirst
├── rewrite.go            # NewRewriter, TrackedRewriter, ReplaceNode, DeleteNode, etc.
├── pipeline.go           # Pass type, Pipeline, FixedPoint, FixedPointPipeline, Validate
├── scope.go              # BuildScopes, SelectScope, TableSource, CTEDef
├── macro.go              # MacroExpander, ExtractLiteralArgs
├── analysis/
│   ├── tables.go         # ExtractTables
│   ├── columns.go        # ExtractColumns
│   └── functions.go      # ExtractFunctions
├── passes/
│   ├── helpers.go        # findOutermostSelectStmt, findLastSelectStmtClause
│   ├── normalize_case.go
│   ├── normalize_whitespace.go
│   ├── strip_comments.go
│   ├── remove_parens.go
│   ├── qualify_tables.go
│   ├── add_where.go
│   ├── add_settings.go
│   ├── set_format.go
│   ├── rewrite_functions.go
│   ├── enforce_rls.go
│   ├── expand_columns.go
│   ├── expand_columns_options.go
│   ├── wrap_columns.go
│   ├── canonicalize_constructors.go
│   └── settings_manipulation.go
└── testdata/
    ├── corpus.go         # LoadCorpus() via embed.FS
    └── corpus/           # 67+ .sql files
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
- **Use `regexp.QuoteMeta`** for escaping regex literals — never write your own

## How to Write Tests for a Pass

Every pass MUST have these test categories:

### 1. Explicit Input/Output Pairs (5-10 minimum)

```go
func TestMyPass(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected string
    }{
        {name: "basic", input: "SELECT a FROM t", expected: "..."},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := passes.MyPass(tt.input)
            require.NoError(t, err)
            assert.Equal(t, tt.expected, got)
            // ALWAYS verify parseable
            _, err = nanopass.Parse(got)
            require.NoError(t, err, "produced invalid SQL: %s", got)
        })
    }
}
```

### 2. Idempotency

```go
pass1, err := pass(sql)
pass2, err := pass(pass1)
assert.Equal(t, pass1, pass2, "not idempotent")
```

### 3. Corpus Validity (all 67+ corpus entries must produce valid SQL)

### 4. UNION ALL (for structural passes — test 2 and 3 branches)

### 5. CTE (verify CTE references not accidentally modified)

### 6. Subqueries (FROM subquery, WHERE subquery, scalar subquery in SELECT)

### 7. Invalid SQL Rejection (`""`, `"   "`, `"SELECT"`, `";;;"`)

### 8. Pipeline Integration (compose with other passes)

### 9. Round-trip (for serialization passes — read → write → read must produce equal values)

## ANTLR4 Go Runtime API — Verified Method Names (v4.13.1)

```go
rw.ReplaceDefault(startIndex, stopIndex, text)   // NOT ReplaceTokenDefaultPos
rw.DeleteDefault(startIndex, stopIndex)           // NOT DeleteTokenDefaultPos
rw.InsertBeforeDefault(index, text)               // text is string, NOT interface{}
rw.InsertAfterDefault(index, text)                // text is string, NOT interface{}
rw.GetTextDefault()                               // returns modified SQL
```

## CST Type Quick Reference

```go
// Column expressions
*grammar.ColumnExprFunctionContext     // func(args) — also NOT(x), array(x), tuple(x)!
*grammar.ColumnExprParensContext       // (expr) — single expression
*grammar.ColumnExprTupleContext        // (expr, expr) — 2+ expressions
*grammar.ColumnExprArrayContext        // [expr, expr]
*grammar.ColumnExprArrayAccessContext  // arr[i]
*grammar.ColumnExprTupleAccessContext  // t.1
*grammar.ColumnExprSubqueryContext     // (SELECT ...)
*grammar.ColumnExprDynamicContext      // COLUMNS('regex')
*grammar.ColumnExprIdentifierContext   // bare column name
*grammar.ColumnExprLiteralContext      // 42, 'hello', NULL
*grammar.ColumnExprAliasContext        // expr AS alias

// Table expressions (inside FROM/JOIN)
*grammar.TableExprIdentifierContext    // table_name
*grammar.TableExprAliasContext         // table AS alias
*grammar.TableExprSubqueryContext      // (SELECT ...) in FROM
*grammar.TableExprFunctionContext      // table_function(args)

// Join expressions
*grammar.JoinExprTableContext          // single table
*grammar.JoinExprOpContext             // left JOIN right ON ...

// SELECT list items
*grammar.ColumnsExprAsteriskContext    // * or table.*
*grammar.ColumnsExprColumnContext      // single column expression

// Setting values (separate type system!)
*grammar.SettingLiteralContext         // scalar
*grammar.SettingArrayContext           // [val, val]
*grammar.SettingTupleContext           // (val, val)
*grammar.SettingEmptyArrayContext      // []
*grammar.SettingFunctionContext        // func(val, val)
*grammar.SettingFunctionEmptyContext   // func()

// Query-level
*grammar.QueryStmtContext              // root — contains FORMAT
*grammar.QueryContext                  // contains SELECT union
*grammar.IdentifierOrNullContext       // FORMAT name
```

## SelectScope Reference

```go
scope.Node                    // *grammar.SelectStmtContext
scope.Tables                  // []TableSource — FROM/JOIN sources
scope.Parent                  // *SelectScope (nil for outermost)
scope.CTEDefs                 // []CTEDef
scope.UnionPeers              // []*SelectScope — all UNION ALL branches
scope.Subqueries              // []*SelectScope — expression subqueries

scope.ResolveAlias(name)      // → (TableSource, bool)
scope.ResolveCTE(name)        // → (CTEDef, bool) — walks ancestors
scope.AllScopes()             // → []*SelectScope — depth-first flattened

ts.Table                      // table name
ts.Database                   // database qualifier (empty if unqualified)
ts.Alias                      // AS alias (empty if none)
ts.IsCTE                      // true if this is a CTE reference
ts.IsSubquery                 // true if FROM (SELECT ...)
ts.Scope                      // *SelectScope for subqueries
```

## Existing Passes — Don't Reimplement

| Pass | What it does | Idempotent? |
|------|-------------|-------------|
| `NormalizeKeywordCase` | Uppercases SQL keywords | Yes |
| `NormalizeWhitespace` | Collapses whitespace, preserves newlines | Yes |
| `NormalizeWhitespaceSingleLine` | Collapses all whitespace to single spaces | Yes |
| `StripComments` | Removes comments | Yes |
| `RemoveRedundantParens` | Removes unnecessary parens by precedence | Yes |
| `QualifyTables(db)` | Adds default database prefix | Yes |
| `AddWhereCondition(pred)` | Injects/ANDs WHERE predicate into all branches | **No** |
| `AddSettings(entries)` | Appends SETTINGS to outermost query | **No** |
| `SetFormat(name)` | Sets/replaces/removes FORMAT clause | Yes |
| `GetFormat(sql)` | Reads FORMAT value (analysis, not a pass) | N/A |
| `RewriteFunctionNames(map)` | Renames function calls | Yes |
| `EnforceRLS(policy)` | Row-level security with alias rewriting | **No** |
| `ExpandColumns(schema)` | Expands `*`, `table.*`, `COLUMNS('regex')` | Yes |
| `ExpandColumnsWithOptions(schema, opts)` | Expand with EXCEPT/REPLACE/APPLY | Yes |
| `WrapColumnsWithDynamic(pattern)` | Wraps matching columns in `COLUMNS('^name$')` | Yes |
| `CanonicalizeConstructors(form)` | Normalizes tuple/array syntax (literal ↔ function) | Yes |
| `ReadSettings(sql)` | Deserializes settings to `map[string]any` | N/A |
| `WriteSettings(map)` | Serializes `map[string]any` to SETTINGS | Yes |
| `ModifySettings(fn)` | Atomic read-modify-write of settings | Depends |
| `DetectUnsupportedColumnSyntax(sql)` | Pre-parse check for EXCEPT/APPLY/REPLACE | N/A |
| `MacroExpander.Pass()` | Expands registered macro functions | Depends |
| `Validate` | Parse-only check | Yes |
| `FixedPoint(pass, max)` | Repeat until stable | N/A |
| `FixedPointPipeline(max, passes...)` | Repeat pipeline until stable | N/A |
| `LoggingPass(logger, name, pass)` | Debug wrapper | N/A |

## Known Grammar Limitations

- `FROM t SELECT a` (FROM-first syntax)
- `WITH (SELECT x) AS name` (scalar subquery CTE)
- `EXISTS (SELECT ...)` (EXISTS predicate)
- `* EXCEPT(col)`, `COLUMNS('...') APPLY(func)`, `REPLACE(...)` (column modifiers — use Go-side `WithExcept`/`WithApply`/`WithReplace` instead)
- `SET param_d = {'key': [1,2]}` (map literals in SET — arrays and tuples are supported after grammar extension)
