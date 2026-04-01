# CST → AST Converter: Assumptions and Limitations

## Scope

The converter transforms a ClickHouse ANTLR4 CST into a canonical AST.
The grammar (`ClickHouseParser.g4`) is **SELECT-only**: `query → setStmt* ctes? selectUnionStmt`.
No DDL (CREATE, ALTER, DROP) or INSERT is supported.

## Pre-normalization Requirements

The converter assumes all preceding normalization passes have been applied.
It returns an error if it encounters a non-canonical CST node.

| Non-canonical form | Required normalization | Error on encounter |
|---|---|---|
| `expr::Type` | → `CAST(expr, 'Type')` function call | ✅ |
| `CAST(expr AS Type)` | → `CAST(expr, 'Type')` function call | ✅ |
| `[1, 2, 3]` | → `array(1, 2, 3)` | ✅ |
| `(1, 2, 3)` tuple | → `tuple(1, 2, 3)` | ✅ |
| `a[i]` | → `arrayElement(a, i)` | ✅ |
| `t.1` | → `tupleElement(t, 1)` | ✅ |
| `DATE 'str'` | → `toDate('str')` | ✅ |
| `TIMESTAMP 'str'` | → `toDateTime('str')` | ✅ |
| `EXTRACT(DAY FROM e)` | → `extract(e, 'DAY')` | ✅ |
| `SUBSTRING(e FROM n FOR m)` | → `substring(e, n, m)` | ✅ |
| `TRIM(BOTH s FROM e)` | → `trimBoth(e, s)` / `trimLeading` / `trimTrailing` | ✅ |

These normalizations must be implemented as nanopass passes applied **before** the
CST → AST conversion.

## Grammar Ambiguity: Keyword-as-Alias Absorption

### The Problem

The ClickHouse grammar allows most keywords to be used as aliases via the
`keywordForAlias` rule. This creates ambiguity when keywords appear in positions
where both "alias" and "keyword with special meaning" interpretations are valid.

The ANTLR parser resolves ambiguity by rule priority — `ColumnExprAlias` has higher
priority than keyword-specific alternatives in several contexts.

### Affected Keywords and Contexts

**ORDER BY direction keywords:**
```
ORDER BY a DESC NULLS LAST
```
The parser may produce `ColumnExprAlias(ColumnExprAlias(ColumnExprAlias(a, DESC), NULLS), LAST)`
instead of `OrderExpr(a, dir=DESC, nulls=LAST)`.

**Workaround implemented:** `unwrapOrderExprAlias` iteratively peels off alias nodes
whose names match `ASC`, `ASCENDING`, `DESC`, `DESCENDING`, `NULLS`, `FIRST`, `LAST`
and reconstructs the ORDER BY direction and nulls fields.

**Window frame keywords:**
```
OVER (ORDER BY a ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)
OVER (PARTITION BY g ROWS BETWEEN ...)
```
`ROWS` and `RANGE` are in `keywordForAlias`. When they follow an expression in
ORDER BY or PARTITION BY, the parser treats them as aliases: `a ROWS` → `ColumnExprAlias(a, ROWS)`.
The entire `BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW` then becomes a BETWEEN expression.

**No workaround implemented.** The CST is too mangled to reconstruct reliably.

**Affected test:** `TestWindowFrameWithOrderBy` — skipped.

**Unaffected cases:** Window frames work correctly when there is no preceding
ORDER BY or PARTITION BY clause, e.g. `OVER (ROWS BETWEEN ...)`.

### Boolean Literals

```
SELECT true, false
```
`true` and `false` are `JSON_TRUE` / `JSON_FALSE` tokens. The parser produces
`ColumnExprIdentifierContext` (not `ColumnExprLiteralContext`).

**Workaround implemented:** `convertColumnIdentifier` checks for `true`/`false`
and produces `KindLiteral` instead of `KindColumnRef`.

### ORDER BY COLLATE

```
ORDER BY a COLLATE 'en'
```
`COLLATE` is in `keywordForAlias`. The parser may parse `a COLLATE` as
`ColumnExprAlias(a, COLLATE)`. The string literal `'en'` is then orphaned.

**Status:** Not investigated. May require similar unwrapping logic.

### Completelist of problematic keywords

All keywords in `keywordForAlias` that also have special meaning in specific
contexts are potentially problematic:

| Keyword | Special meaning | Risk |
|---------|----------------|------|
| `ASC` / `ASCENDING` | ORDER BY direction | ✅ Workaround exists |
| `DESC` / `DESCENDING` | ORDER BY direction | ✅ Workaround exists |
| `NULLS` | ORDER BY nulls handling | ✅ Workaround exists |
| `FIRST` / `LAST` | ORDER BY nulls position | ✅ Workaround exists |
| `ROWS` | Window frame unit | ❌ No workaround |
| `RANGE` | Window frame unit | ❌ No workaround |
| `PRECEDING` | Window frame bound | ❌ Absorbed inside frame |
| `FOLLOWING` | Window frame bound | ❌ Absorbed inside frame |
| `UNBOUNDED` | Window frame bound | ❌ Absorbed inside frame |
| `COLLATE` | ORDER BY collation | ⚠️ Not investigated |
| `FORMAT` | Top-level output format | Low risk (end of query) |

## Identifier Handling

### Quote Stripping

The converter strips backtick and double-quote delimiters from identifiers:
- `` `col` `` → `col`
- `"col"` → `col`
- `col` → `col`

Escape sequences inside quoted identifiers (e.g. `` `col\`name` ``) are **not**
processed. The content between quotes is taken verbatim after stripping the outer
quotes.

### Three-Part Names: `a.b.c`

The grammar rule `columnIdentifier: (tableIdentifier DOT)? nestedIdentifier`
with `tableIdentifier: (databaseIdentifier DOT)? identifier` means:

| SQL | Parsed as | AST |
|-----|-----------|-----|
| `col` | `nestedIdentifier(col)` | `ColRef{Column: "col"}` |
| `t.col` | `tableIdentifier(t) DOT nestedIdentifier(col)` | `ColRef{Table: "t", Column: "col"}` |
| `db.t.col` | `tableIdentifier(db.t) DOT nestedIdentifier(col)` | `ColRef{Database: "db", Table: "t", Column: "col"}` |

**There is no syntactic distinction between `table.column` and `column.nested_field`.**
Both `t.col` and `col.nested` parse as `tableIdentifier(t/col) DOT nestedIdentifier(col/nested)`.
Disambiguation requires schema information.

The `ColRef.Nested` field is only populated when `nestedIdentifier` contains a DOT,
which requires a four-part name like `db.t.col.nested` — but `tableIdentifier`
consumes at most two parts (`db.t`), so in practice `Nested` is only set in
edge cases involving the grammar's identifier resolution.

## Schema-Dependent Features

The following features require a `SchemaResolver` interface (not yet implemented):

| Feature | AST node if unresolved |
|---------|----------------------|
| `SELECT *` | `AsteriskExpr{Table: ""}` |
| `SELECT t.*` | `AsteriskExpr{Table: "t"}` |
| `COLUMNS('regex')` | `DynColumnExpr{Pattern: "regex"}` |
| Table database qualification | `ColRef.Database` may be empty |

Without a schema resolver, these nodes are preserved as-is in the AST.

## Structural Decisions

### Parentheses

`ColumnExprParens` (`(expr)`) is always unwrapped. Precedence is encoded in the
tree structure — the AST never contains redundant parentheses.

### FunctionCall Unification

All syntactic sugar that normalizes to function form shares the `FuncCallData` node:

| `FuncCallData.Name` | Represents |
|---------------------|-----------|
| `CAST` | Type cast (2nd arg is string literal with type name) |
| `array` | Array construction |
| `tuple` | Tuple construction |
| `arrayElement` | Array subscript |
| `tupleElement` | Tuple field/index access |
| `toDate` | Date literal |
| `toDateTime` | Timestamp literal |
| `extract` | Date part extraction |
| `substring` | String substring |
| `trimBoth` / `trimLeading` / `trimTrailing` | String trim |

`CAST` retains uppercase to distinguish from a hypothetical user function named `cast`.

### Parametric Functions

`f(params)(args)` is detected by counting LPAREN/RPAREN groups via `collectParenGroups`.
Two groups → first is `Params`, second is `Args`. One group → `Args` only.

**Risk:** If the grammar nests things differently for some edge case, the paren
counting could mis-split. This is tested for `quantile(0.9)(x)` and `quantiles(0.5, 0.9, 0.99)(x)`.

### Scoping

Scope is structural (implicit in tree nesting). No symbol table is built.

| Scope boundary | AST location | Visibility |
|---|---|---|
| Top-level CTE | `Query.CTEs` | Entire `Query.Body` |
| Inline WITH | `Select.With` | Enclosing `Select` |
| Subquery | `SubqueryData.Query` | Inner query only (may reference outer = correlated) |
| Lambda | `LambdaData.Params` | `LambdaData.Body` only |
| JOIN | `JoinOpData.Left/Right` | Both sides visible in constraint |
| Alias | `AliasData.Name` | Forward-visible in same SELECT list (ClickHouse semantics) |

### JOIN Tree

JOINs form a left-associative binary tree:
```
t1 JOIN t2 ON ... JOIN t3 ON ...
→ JoinOp(left=JoinOp(left=t1, right=t2), right=t3)
```

### Table Function Arguments

Table function arguments (`tableArgExpr`) are currently serialized as literal text
(`LiteralData{SQL: fullText}`), not fully parsed. This is because `tableArgExpr`
can be a `nestedIdentifier`, `tableFunctionExpr`, or `literal` — a heterogeneous
set that doesn't map cleanly to `Expr`.

## Test Coverage Summary

### Well Tested (high confidence)
- All 17 expression kinds
- All 10 non-canonical rejections
- All binary operators (14 arithmetic/comparison + 4 IN + 4 LIKE)
- SELECT clause presence (10 clauses)
- UNION/EXCEPT/INTERSECT (5 cases)
- CTE structure (3 cases)
- FROM/JOIN (10 cases + strictness + GLOBAL)
- Parentheses unwrapping and precedence
- Identifier quote stripping
- ORDER BY with ASC/DESC/NULLS unwrapping
- Window frames (without ORDER BY)
- GROUP BY modifiers (CUBE/ROLLUP/TOTALS)
- Lambda expressions
- CASE (searched, simple, nested, many WHENs)

### Tested but with Known Limitations
- Window frames after ORDER BY/PARTITION BY (grammar ambiguity)
- Nested identifiers (grammar ambiguity with table.column)
- COLLATE in ORDER BY (potential alias absorption)

### Not Tested
- ORDER BY WITH FILL / INTERPOLATE
- FORMAT clause
- INTO OUTFILE clause
- Deeply nested CTEs (CTE referencing another CTE)
- ASOF JOIN with inequality constraint
- Multiple WINDOW clauses