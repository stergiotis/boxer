# ClickHouse Canonical AST — Design Document

## Purpose

The Canonical AST is a CBOR-serializable, fully normalized representation of a parsed
ClickHouse SELECT query. It is the output of the final pass in the nanopass pipeline.
Two semantically equivalent queries produce structurally identical ASTs (modulo
schema-dependent normalizations).

## Pipeline Position

```
Input SQL
  → Parse (ANTLR4 CST)
  → NormalizeCast (all casts → expr::Type)
  → ExtractLiterals (literals → SET params)
  → NormalizeCast (reverse: all casts → CAST(expr, 'Type') functional form)
  → CST → Canonical AST (this pass)
  → CBOR serialization
```

Note: NormalizeCast runs twice — first to `::` form for ExtractLiterals (which expects
`::` form for cast detection), then reversed to `CAST(expr, 'Type')` functional form
for the AST export. Alternatively, a single AST conversion pass can handle the
CAST normalization directly during CST→AST translation.

## Normalization Guarantees

The following normalizations are applied during CST → AST conversion:

| Aspect | Normalization | Example |
|--------|--------------|---------|
| Casts | `CAST(expr, 'Type')` functional form | `1::UInt64` → `CAST(1, 'UInt64')` |
| Array literals | `array()` functional form | `[1, 2]` → `array(1, 2)` |
| Tuple literals | `tuple()` functional form | `(1, 2)` → `tuple(1, 2)` |
| Array access | `arrayElement()` functional form | `a[i]` → `arrayElement(a, i)` |
| Tuple access | `tupleElement()` functional form | `t.1` → `tupleElement(t, 1)` |
| DATE sugar | `toDate()` function call | `DATE '2024-01-01'` → `toDate('2024-01-01')` |
| TIMESTAMP sugar | `toDateTime()` function call | `TIMESTAMP '...'` → `toDateTime('...')` |
| EXTRACT sugar | `extract()` function call | `EXTRACT(DAY FROM e)` → `extract(e, 'DAY')` |
| SUBSTRING sugar | `substring()` function call | `SUBSTRING(e FROM n FOR m)` → `substring(e, n, m)` |
| TRIM sugar | `trimBoth/Leading/Trailing()` | `TRIM(BOTH s FROM e)` → `trimBoth(e, s)` |
| Parentheses | Removed (implicit in tree structure) | `(a + b) * c` → tree encodes precedence |
| Comments | Removed | `/* comment */` → absent |
| Keyword casing | All UPPERCASE | `select` → `SELECT` |
| Identifier quoting | All double-quoted | `col` → `"col"` |
| Whitespace | Normalized to single spaces | `a   =   b` → `a = b` |
| Table qualification | `"database"."table"` (requires schema) | `t` → `"default"."t"` |
| Star expansion | Resolved to column list (requires schema) | `*` → `"a", "b", "c"` |
| COLUMNS() | Resolved to column list (requires schema) | `COLUMNS('a.*')` → `"a1", "a2"` |
| Literal extraction | Function/operator args extracted as params | `WHERE x = 'hello'` → `WHERE x = {p: String}` |

```
Between
Date
CASE as caseWithExpression
INTERVAL 1 DAY ?
Ternary Operator

// col.nested_field → columnIdentifier(tableIdentifier(col) DOT nestedIdentifier(nested_field))
// The parser treats "col" as a table qualifier, "nested_field" as the column.
// There's no way to distinguish col.nested from table.col without schema.
e := mustConvertExpr(t, "col.nested_field")
assert.Equal(t, ast.KindColumnRef, e.Kind)
assert.Equal(t, "col", e.ColRef.Table)
assert.Equal(t, "nested_field", e.ColRef.Column)
assert.Equal(t, "", e.ColRef.Nested)

sql := "SELECT sum(x) OVER (ORDER BY a ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) FROM t"
The WindowExprContext has only 1 child: WinOrderByClauseContext. No WinFrameClauseContext at all! The ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW is being consumed by the ORDER BY expression — same alias absorption problem.
The ORDER BY parser sees a ROWS and parses ROWS as an alias for a (since ROWS is in keywordForAlias). Then BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW becomes a BETWEEN expression. The entire frame clause is swallowed into the order expression.
The fix is in unwrapOrderExprAlias — we need to also unwrap ROWS and RANGE as frame-related keywords, not aliases. But the problem is deeper: the BETWEEN expression has already been constructed by the parser.
Actually, this is a grammar ambiguity that the parser resolves "wrong" for our use case. ORDER BY a ROWS BETWEEN ... is parsed as ORDER BY (a ROWS) BETWEEN ... where ROWS is an alias.
This means the window frame with ROWS/RANGE after ORDER BY cannot be parsed correctly by this grammar when the order expression is a simple identifier. This is likely a known limitation.
```

**Schema-dependent normalizations** (table qualification, star expansion, COLUMNS resolution)
are optional. When no `SchemaResolver` is provided, `AsteriskExpr` and `DynamicColumnExpr`
are preserved as-is in the AST, and table identifiers remain unqualified.

## Node Architecture

### Expr — Tagged Union

All expression nodes share a single `Expr` struct with a `Kind` discriminant and
kind-specific data in dedicated sub-structs. This avoids interfaces, enables direct
CBOR serialization, and matches the data-oriented philosophy of the project.

```
Expr
├── Kind: ExprKind (uint8 enum)
├── Literal: *LiteralData        (Kind == KindLiteral)
├── ParamSlot: *ParamSlotData    (Kind == KindParamSlot)
├── ColumnRef: *ColumnRefData    (Kind == KindColumnRef)
├── FunctionCall: *FuncCallData  (Kind == KindFunctionCall)
├── WindowFunc: *WindowFuncData  (Kind == KindWindowFunc)
├── Binary: *BinaryData          (Kind == KindBinary)
├── Unary: *UnaryData            (Kind == KindUnary)
├── Between: *BetweenData        (Kind == KindBetween)
├── IsNull: *IsNullData          (Kind == KindIsNull)
├── Ternary: *TernaryData        (Kind == KindTernary)
├── Case: *CaseData              (Kind == KindCase)
├── Interval: *IntervalData      (Kind == KindInterval)
├── Lambda: *LambdaData          (Kind == KindLambda)
├── Alias: *AliasData            (Kind == KindAlias)
├── Subquery: *SubqueryData      (Kind == KindSubquery)
├── Asterisk: *AsteriskData      (Kind == KindAsterisk)
└── DynColumn: *DynColumnData    (Kind == KindDynColumn)
```

Only one pointer is non-nil at a time, determined by `Kind`.

### Statement Nodes

```
Query
├── Settings: []SettingPair
├── CTEs: []CTE
└── Body: SelectUnion

SelectUnion
├── Head: Select
└── Items: []SelectUnionItem

Select
├── Distinct: bool
├── Top: *TopClause
├── Projection: []Expr
├── ExceptColumns: *ExceptClause
├── With: []Expr               (inline WITH scalar expressions)
├── From: *JoinExpr
├── ArrayJoin: *ArrayJoinClause
├── WindowDef: *WindowDefClause
├── Qualify: *Expr
├── Prewhere: *Expr
├── Where: *Expr
├── GroupBy: *GroupByClause
├── Having: *Expr
├── OrderBy: *OrderByClause
├── LimitBy: *LimitByClause
├── Limit: *LimitClause
└── Settings: []SettingPair
```

### FROM / JOIN Nodes

```
JoinExpr (tagged union)
├── Kind: JoinExprKind
├── Table: *JoinTableData      (leaf: tableExpr + FINAL + sample)
├── Op: *JoinOpData            (binary: lhs JOIN rhs ON/USING)
└── Cross: *JoinCrossData      (binary: lhs CROSS JOIN rhs)
```

### Function Call Normalizations

The `FunctionCall` node serves double duty for regular functions and all syntactic
sugar that normalizes to function form:

| `FuncCallData.Name` | Original syntax | Notes |
|---------------------|----------------|-------|
| User-defined | `f(a, b)` | Regular function call |
| `"CAST"` | `CAST(e AS T)`, `e::T`, `CAST(e, 'T')` | Second arg is string literal with type |
| `"array"` | `[1, 2]`, `array(1, 2)` | Array construction |
| `"tuple"` | `(1, 2)`, `tuple(1, 2)` | Tuple construction |
| `"arrayElement"` | `a[i]` | Array subscript |
| `"tupleElement"` | `t.1`, `t.name` | Tuple field access |
| `"toDate"` | `DATE 'str'` | Date literal sugar |
| `"toDateTime"` | `TIMESTAMP 'str'` | Timestamp literal sugar |
| `"extract"` | `EXTRACT(DAY FROM e)` | Second arg is interval string |
| `"substring"` | `SUBSTRING(e FROM n FOR m)` | Standard SQL SUBSTRING |
| `"trimBoth"` | `TRIM(BOTH s FROM e)` | |
| `"trimLeading"` | `TRIM(LEADING s FROM e)` | |
| `"trimTrailing"` | `TRIM(TRAILING s FROM e)` | |

### CBOR Field Naming

- Statement-level fields: descriptive names (`"projection"`, `"where"`, `"order_by"`)
- Expr fields: short names (`"k"` for kind, `"n"` for name, `"a"` for args)
- Bulk data (args, elements): single-letter names

This balances readability for top-level inspection with compactness for the
expression tree which dominates the output size.

## Scoping Model

Scope is **structural** — implicit in tree nesting. No symbol table.

| Scope boundary | Where in AST | Visibility |
|---------------|-------------|------------|
| CTE (`Query.CTEs`) | `Query` level | Entire `Query.Body` |
| Inline WITH (`Select.With`) | `Select` level | Rest of that `Select` |
| Subquery (`SubqueryData`) | Nested `SelectUnion` | Inner query; can reference outer (correlated) |
| Lambda (`LambdaData`) | `LambdaData.Params` | `LambdaData.Body` only; shadows outer names |
| JOIN | `JoinOpData` | Both sides visible in `ON`; left side visible in right's subqueries |
| Alias | `AliasData` | ClickHouse: forward-visible within same SELECT list |

Consumers who need resolution build their own scope chain by walking the AST.

## Schema Resolver Interface

```go
type SchemaResolver interface {
    ResolveColumns(database, table string) ([]string, error)
    ResolveDatabase(table string) (string, error)
}
```

When `nil`, schema-dependent normalizations are skipped:
- `AsteriskExpr` preserved in projection
- `DynamicColumnExpr` preserved in projection
- Table identifiers may lack database qualifier

## Limitations

1. **SELECT-only grammar** — No DDL, no INSERT. The grammar `query` rule only
produces `setStmt* ctes? selectUnionStmt`.

2. **No type inference** — The AST doesn't track expression result types. CAST
types are preserved as string literals in function arguments.

3. **No correlated subquery detection** — Subqueries that reference outer columns
are structurally nested but not flagged as correlated.

4. **No alias resolution** — Forward alias references in SELECT lists are preserved
as `ColumnRefExpr`; the consumer resolves them.

5. **Parametric functions** — ClickHouse's `f(params)(args)` syntax is represented
as `FunctionCall` with `Params` and `Args` as separate lists.
