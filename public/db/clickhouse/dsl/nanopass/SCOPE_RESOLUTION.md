# Database Resolution in the Nanopass Framework

## How ClickHouse Resolves Databases

ClickHouse resolves table references to databases using these rules, in order:

1. **Fully qualified** — `db.table` always resolves to database `db`
2. **Connection default** — unqualified `table` resolves to the connection's default database, set by:
   - `database` parameter in the connection string
   - `USE db` statement in the session
   - Falls back to `default` if neither is specified
3. **Independent resolution** — each table reference in a query resolves independently; the database of one table does NOT influence the resolution of another table in the same query

### Key behaviors

- **UNION ALL**: Each branch resolves tables against the same connection default — there is no per-branch database context
- **Subqueries**: Inherit the connection's default database, not the enclosing query's table databases
- **JOINs**: `FROM db1.t1 JOIN t2 ON ...` — `t2` resolves to the connection default, NOT to `db1`
- **CTEs**: CTE names (`WITH cte AS (...)`) are query-scoped aliases, not database-qualified. `FROM cte` references the CTE, not a table named `cte` in any database
- **Table functions**: `remote('host', 'db', 'table')` — the database is an argument to the function, not in the table identifier. The nanopass framework cannot and should not rewrite inside function arguments

## How the Nanopass Framework Models This

### SelectScope.DefaultDatabase

Every `SelectScope` carries a `DefaultDatabase` field set during `BuildScopes`:

```go
scopes := nanopass.BuildScopes(pr, "mydb")  // "mydb" is the connection default
```

This value propagates to all child scopes (CTEs, subqueries, UNION ALL branches) identically, matching ClickHouse's behavior where the connection default applies uniformly.

If no default is provided, `BuildScopes(pr)` sets `DefaultDatabase` to `""`.

### TableSource.ResolvedDatabase

Each `TableSource` has two database-related fields:

- `Database` — the explicit database from the SQL text (populated from `TableIdentifierContext.DatabaseIdentifier()`). Empty if the table is unqualified.
- `ResolvedDatabase(scope)` — returns `Database` if non-empty, otherwise `scope.DefaultDatabase`

```go
for _, ts := range scope.Tables {
    db := ts.ResolvedDatabase(scope)
    // db is "otherdb" for "otherdb.table", or "mydb" for unqualified "table"
}
```

### CTE and Subquery Handling

- `ts.IsCTE == true` — this table source references a CTE name, not a real table. `ResolvedDatabase` still works but is meaningless — CTE references have no database.
- `ts.IsSubquery == true` — this table source is a `FROM (SELECT ...)`. It has no table name or database. Its inner scope (`ts.Scope`) carries the same `DefaultDatabase`.

## Comparison With ClickHouse

| Behavior | ClickHouse | Nanopass Framework | Match? |
|----------|-----------|-------------------|--------|
| `db.table` resolves to `db` | Yes | `ts.Database = "db"` | ✅ |
| Unqualified `table` resolves to connection default | Yes | `ts.ResolvedDatabase(scope)` returns `scope.DefaultDatabase` | ✅ |
| Each table resolves independently | Yes | Each `TableSource` has its own `Database` field | ✅ |
| UNION ALL branches share same default | Yes | All branches get same `DefaultDatabase` from `BuildScopes` | ✅ |
| Subqueries inherit connection default | Yes | `DefaultDatabase` propagated to child scopes | ✅ |
| JOINs — `t2` does NOT inherit from `db1.t1` | Yes | `DefaultDatabase` is external, not inferred from siblings | ✅ |
| CTE references are not database-qualified | Yes | `ts.IsCTE` flag prevents database resolution | ✅ |
| `USE db` changes default mid-session | Yes | Not modeled — we process individual queries, not sessions | N/A |
| Table functions (`remote(...)`) — db inside args | Yes | `TableExprFunctionContext` is skipped entirely | ✅ (correct to ignore) |
| `system.*` / `information_schema.*` tables | Always qualified | No special handling needed — explicit `db.table` works | ✅ |
| Distributed/materialized view indirection | Runtime behavior | Not modeled — this is execution, not query syntax | N/A |

## What Is NOT Modeled (By Design)

### Session-level `USE db`

The framework processes individual SQL queries, not sessions. The `USE` statement changes the default database for subsequent queries in a session — this is connection-level state that must be provided externally via `BuildScopes(pr, "mydb")`.

### Table Functions

`remote('host', 'db', 'table')`, `file('path')`, `numbers(10)`, etc. — these are opaque to the framework. The database parameter (if any) is a function argument, not a table identifier. The framework correctly skips `TableExprFunctionContext` nodes.

### Runtime Indirection

Distributed tables, materialized views, and dictionaries may reference tables in other databases at runtime. This is execution-level behavior invisible in the query text. The framework operates purely on query syntax.

### Database-Qualified Column References

ClickHouse allows `db.table.column` syntax in some contexts. The framework's `ColumnIdentifierContext` tracking handles `table.column` qualifiers but does not track the database part of column references. This would require three-part identifier resolution, which is rare in practice.

## Usage Patterns

### QualifyTables with Default Database

```go
// Qualify unqualified tables with the connection's default database
pass := passes.QualifyTables("production")
// "SELECT a FROM orders" → "SELECT a FROM production.orders"
// "SELECT a FROM staging.orders" → unchanged (already qualified)
```

### ExpandColumns with Schema and Default Database

```go
schema := passes.NewSchemaProvider(map[string][]string{
    "orders": {"id", "amount", "tenant_id"},
})

// BuildScopes with default database for ResolvedDatabase lookups
pr, _ := nanopass.Parse(sql)
scopes := nanopass.BuildScopes(pr, "production")

for _, scope := range scopes {
    for _, ts := range scope.Tables {
        db := ts.ResolvedDatabase(scope)
        // Use db + ts.Table to look up schema
        cols, found := schema.GetColumns(ts.Table) // or use db-qualified lookup
    }
}
```

### EnforceRLS with Database-Aware Policies

```go
// Policy keyed by database.table for cross-database queries
policy := passes.NewRLSPolicy(map[string]string{
    "orders":    "orders.tenant_id = currentUser()",     // default db
    "customers": "customers.visible = 1",                 // default db
})

// The RLS pass uses scope.Tables which has ResolvedDatabase available
pass := passes.EnforceRLS(policy)
```

## Extending Database Resolution

If future requirements need database-aware schema lookup (e.g., same table name in different databases with different columns), extend `SchemaProvider`:

```go
// Current — table name only
schema.GetColumns("orders") → ["id", "amount", ...]

// Future — database-qualified lookup
schema.GetColumns("production", "orders") → ["id", "amount", ...]
schema.GetColumns("staging", "orders") → ["id", "amount", "debug_flag", ...]
```

The `ResolvedDatabase` method on `TableSource` provides the database needed for this lookup. No changes to `SelectScope` or `BuildScopes` would be needed — only the `SchemaProvider` and the passes that use it.

