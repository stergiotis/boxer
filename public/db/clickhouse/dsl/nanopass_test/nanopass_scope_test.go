//go:build llm_generated_opus46

package nanopass_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildScopesSimple(t *testing.T) {
	sql := "SELECT a FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	scope := scopes[0]
	require.Len(t, scope.Tables, 1)
	assert.Equal(t, "t", scope.Tables[0].Table)
	assert.Equal(t, "", scope.Tables[0].Database)
	assert.Equal(t, "", scope.Tables[0].Alias)
	assert.False(t, scope.Tables[0].IsCTE)
	assert.False(t, scope.Tables[0].IsSubquery)
	assert.Nil(t, scope.Parent)
}

func TestBuildScopesQualifiedTable(t *testing.T) {
	sql := "SELECT a FROM db.t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)
	require.Len(t, scopes[0].Tables, 1)
	assert.Equal(t, "db", scopes[0].Tables[0].Database)
	assert.Equal(t, "t", scopes[0].Tables[0].Table)
}

func TestBuildScopesAlias(t *testing.T) {
	sql := "SELECT a.x FROM t AS a"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)
	require.Len(t, scopes[0].Tables, 1)
	assert.Equal(t, "t", scopes[0].Tables[0].Table)
	assert.Equal(t, "a", scopes[0].Tables[0].Alias)

	source, found := scopes[0].ResolveAlias("a")
	assert.True(t, found)
	assert.Equal(t, "t", source.Table)
}

func TestBuildScopesJoin(t *testing.T) {
	sql := "SELECT * FROM t1 AS a JOIN t2 AS b ON a.id = b.id"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)
	require.Len(t, scopes[0].Tables, 2)

	assert.Equal(t, "t1", scopes[0].Tables[0].Table)
	assert.Equal(t, "a", scopes[0].Tables[0].Alias)
	assert.Equal(t, "t2", scopes[0].Tables[1].Table)
	assert.Equal(t, "b", scopes[0].Tables[1].Alias)
}

func TestBuildScopesUnionAll(t *testing.T) {
	sql := "SELECT a FROM t1 UNION ALL SELECT b FROM t2 UNION ALL SELECT c FROM t3"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 3)

	assert.Equal(t, "t1", scopes[0].Tables[0].Table)
	assert.Equal(t, "t2", scopes[1].Tables[0].Table)
	assert.Equal(t, "t3", scopes[2].Tables[0].Table)

	// All branches should reference each other as union peers
	assert.Len(t, scopes[0].UnionPeers, 3)
	assert.Len(t, scopes[1].UnionPeers, 3)
	assert.Equal(t, scopes[0].UnionPeers, scopes[1].UnionPeers)
}

func TestBuildScopesCTE(t *testing.T) {
	sql := "WITH cte AS (SELECT 1 AS x FROM t_real) SELECT x FROM cte"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	scope := scopes[0]

	// The outer SELECT references "cte" — should be marked as CTE
	require.Len(t, scope.Tables, 1)
	assert.Equal(t, "cte", scope.Tables[0].Table)
	assert.True(t, scope.Tables[0].IsCTE)

	// CTE definitions should be tracked
	require.Len(t, scope.CTEDefs, 1)
	assert.Equal(t, "cte", scope.CTEDefs[0].Name)

	// CTE body scope should reference the real table
	cteScope := scope.CTEDefs[0].Scope
	require.NotNil(t, cteScope)
	require.Len(t, cteScope.Tables, 1)
	assert.Equal(t, "t_real", cteScope.Tables[0].Table)
	assert.False(t, cteScope.Tables[0].IsCTE)
}

func TestBuildScopesCTEResolution(t *testing.T) {
	sql := "WITH cte AS (SELECT 1 AS x) SELECT x FROM cte"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	def, found := scopes[0].ResolveCTE("cte")
	assert.True(t, found)
	assert.Equal(t, "cte", def.Name)

	_, found = scopes[0].ResolveCTE("nonexistent")
	assert.False(t, found)
}

func TestBuildScopesSubquery(t *testing.T) {
	sql := "SELECT * FROM (SELECT a FROM t)"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	// Outer SELECT should have one subquery source
	require.Len(t, scopes[0].Tables, 1)
	assert.True(t, scopes[0].Tables[0].IsSubquery)
}

func TestBuildScopesNoFrom(t *testing.T) {
	sql := "SELECT 1"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)
	assert.Empty(t, scopes[0].Tables)
}

func TestBuildScopesMultipleCTEs(t *testing.T) {
	sql := "WITH a AS (SELECT 1 AS x), b AS (SELECT 2 AS y) SELECT * FROM a, b"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	scope := scopes[0]
	require.Len(t, scope.CTEDefs, 2)
	assert.Equal(t, "a", scope.CTEDefs[0].Name)
	assert.Equal(t, "b", scope.CTEDefs[1].Name)

	// Both table references should be marked as CTE
	require.Len(t, scope.Tables, 2)
	assert.True(t, scope.Tables[0].IsCTE)
	assert.True(t, scope.Tables[1].IsCTE)
}

func TestResolveAlias(t *testing.T) {
	sql := "SELECT * FROM orders AS o JOIN customers AS c ON o.cid = c.id"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	{ // Resolve by alias
		source, found := scopes[0].ResolveAlias("o")
		assert.True(t, found)
		assert.Equal(t, "orders", source.Table)
	}

	{ // Resolve by alias
		source, found := scopes[0].ResolveAlias("c")
		assert.True(t, found)
		assert.Equal(t, "customers", source.Table)
	}

	{ // Resolve by table name fails when alias is set
		_, found := scopes[0].ResolveAlias("orders")
		assert.False(t, found)
	}

	{ // Unknown alias
		_, found := scopes[0].ResolveAlias("z")
		assert.False(t, found)
	}
}
func TestDebugScopeTree(t *testing.T) {
	t.Skip("diagnostic")
	sql := "SELECT a FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	t.Logf("Root type: %T", pr.Tree)
	t.Logf("Root children: %d", pr.Tree.GetChildCount())
	for i := 0; i < pr.Tree.GetChildCount(); i++ {
		child := pr.Tree.GetChild(i)
		t.Logf("  child[%d]: %T", i, child)
		if rc, ok := child.(antlr.ParserRuleContext); ok {
			t.Logf("    ruleIndex: %d", rc.GetRuleIndex())
			for j := 0; j < rc.GetChildCount(); j++ {
				gc := rc.GetChild(j)
				t.Logf("    grandchild[%d]: %T", j, gc)
				if gc2, ok := gc.(antlr.ParserRuleContext); ok {
					t.Logf("      ruleIndex: %d", gc2.GetRuleIndex())
					for k := 0; k < gc2.GetChildCount(); k++ {
						ggc := gc2.GetChild(k)
						t.Logf("      greatgrandchild[%d]: %T", k, ggc)
					}
				}
			}
		}
	}
}
func TestDebugUnionMethods(t *testing.T) {
	t.Skip("diagnostic")
	sql := "SELECT a FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	query := pr.Tree.GetChild(0).(*grammar1.QueryContext)
	union := query.GetChild(0).(*grammar1.SelectUnionStmtContext)

	t.Logf("union type: %T", union)
	t.Logf("union children: %d", union.GetChildCount())
	for i := 0; i < union.GetChildCount(); i++ {
		t.Logf("  child[%d]: %T", i, union.GetChild(i))
	}
}
func TestDebugFromClause(t *testing.T) {
	t.Skip("diagnostic")
	sqls := []string{
		"SELECT a FROM t",
		"SELECT a FROM t AS x",
		"SELECT * FROM t1 JOIN t2 ON t1.id = t2.id",
	}
	for _, sql := range sqls {
		t.Logf("--- SQL: %s", sql)
		pr, err := nanopass.Parse(sql)
		require.NoError(t, err)
		nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			switch ctx.(type) {
			case *grammar1.FromClauseContext,
				*grammar1.JoinExprContext,
				*grammar1.TableExprContext:
				t.Logf("  %T (rule=%d) text=%q", ctx, ctx.GetRuleIndex(), ctx.GetText())
				for i := 0; i < ctx.GetChildCount(); i++ {
					t.Logf("    child[%d]: %T", i, ctx.GetChild(i))
				}
			}
			return true
		})
	}
}
func TestDebugJoinExpr(t *testing.T) {
	t.Skip("diagnostic")
	sqls := []string{
		"SELECT a FROM t",
		"SELECT a FROM t AS x",
		"SELECT * FROM t1 AS a JOIN t2 AS b ON a.id = b.id",
		"SELECT * FROM (SELECT 1 AS x)",
	}
	for _, sql := range sqls {
		t.Logf("--- SQL: %s", sql)
		pr, err := nanopass.Parse(sql)
		require.NoError(t, err)
		nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			typeName := fmt.Sprintf("%T", ctx)
			if strings.Contains(typeName, "JoinExpr") || strings.Contains(typeName, "TableExpr") {
				t.Logf("  %T text=%q", ctx, ctx.GetText())
				for i := 0; i < ctx.GetChildCount(); i++ {
					t.Logf("    child[%d]: %T text=%q", i, ctx.GetChild(i), ctx.GetChild(i))
				}
			}
			return true
		})
	}
}
func TestDebugUnionAll(t *testing.T) {
	t.Skip("diagnostic")
	sql := "SELECT a FROM t1 UNION ALL SELECT b FROM t2 UNION ALL SELECT c FROM t3"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	query := pr.Tree.GetChild(0).(*grammar1.QueryContext)
	for i := 0; i < query.GetChildCount(); i++ {
		t.Logf("query child[%d]: %T", i, query.GetChild(i))
	}

	union := query.GetChild(0).(*grammar1.SelectUnionStmtContext)
	t.Logf("union children: %d", union.GetChildCount())
	for i := 0; i < union.GetChildCount(); i++ {
		child := union.GetChild(i)
		t.Logf("  union child[%d]: %T text=%q", i, child, child)
	}
}
func TestBuildScopesSubqueryInFrom(t *testing.T) {
	sql := "SELECT * FROM (SELECT a FROM t_inner)"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	outer := scopes[0]
	require.Len(t, outer.Tables, 1)
	assert.True(t, outer.Tables[0].IsSubquery)
	require.NotNil(t, outer.Tables[0].Scope)

	inner := outer.Tables[0].Scope
	require.Len(t, inner.Tables, 1)
	assert.Equal(t, "t_inner", inner.Tables[0].Table)
	assert.Equal(t, outer, inner.Parent)
}

func TestBuildScopesSubqueryInWhere(t *testing.T) {
	sql := "SELECT a FROM t1 WHERE a IN (SELECT b FROM t2)"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	outer := scopes[0]
	require.Len(t, outer.Tables, 1)
	assert.Equal(t, "t1", outer.Tables[0].Table)

	// Subquery in WHERE should be captured
	require.Len(t, outer.Subqueries, 1)
	inner := outer.Subqueries[0]
	require.Len(t, inner.Tables, 1)
	assert.Equal(t, "t2", inner.Tables[0].Table)
	assert.Equal(t, outer, inner.Parent)
}

func TestBuildScopesNestedSubqueries(t *testing.T) {
	sql := "SELECT * FROM (SELECT * FROM (SELECT a FROM t_deep))"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	outer := scopes[0]
	require.Len(t, outer.Tables, 1)
	require.NotNil(t, outer.Tables[0].Scope)

	mid := outer.Tables[0].Scope
	require.Len(t, mid.Tables, 1)
	require.NotNil(t, mid.Tables[0].Scope)

	deep := mid.Tables[0].Scope
	require.Len(t, deep.Tables, 1)
	assert.Equal(t, "t_deep", deep.Tables[0].Table)
}

func TestBuildScopesAllScopes(t *testing.T) {
	sql := "WITH cte AS (SELECT a FROM t1) SELECT * FROM cte WHERE x IN (SELECT b FROM t2)"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	all := scopes[0].AllScopes()

	// Should include: outer scope, CTE body scope, WHERE subquery scope
	require.Len(t, all, 3)
}

func TestQualifyTablesSubqueries(t *testing.T) {
	pass := passes.QualifyTables("mydb")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "from_subquery",
			input:    "SELECT * FROM (SELECT a FROM t)",
			expected: "SELECT * FROM (SELECT a FROM mydb.t)",
		},
		{
			name:     "nested_subquery",
			input:    "SELECT * FROM (SELECT * FROM (SELECT a FROM t))",
			expected: "SELECT * FROM (SELECT * FROM (SELECT a FROM mydb.t))",
		},
		{
			name:     "where_subquery",
			input:    "SELECT a FROM t1 WHERE a IN (SELECT b FROM t2)",
			expected: "SELECT a FROM mydb.t1 WHERE a IN (SELECT b FROM mydb.t2)",
		},
		{
			name:     "mixed_cte_subquery",
			input:    "WITH cte AS (SELECT a FROM t1) SELECT * FROM cte WHERE x IN (SELECT b FROM t2)",
			expected: "WITH cte AS (SELECT a FROM mydb.t1) SELECT * FROM cte WHERE x IN (SELECT b FROM mydb.t2)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}
func TestBuildScopesAliasedSubqueryInJoin(t *testing.T) {
	sql := "SELECT * FROM t1 JOIN (SELECT b FROM t2) AS sub ON t1.id = sub.id"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	outer := scopes[0]
	require.Len(t, outer.Tables, 2)

	// First table is t1
	assert.Equal(t, "t1", outer.Tables[0].Table)
	assert.False(t, outer.Tables[0].IsSubquery)

	// Second table is the subquery with alias "sub"
	assert.True(t, outer.Tables[1].IsSubquery)
	assert.Equal(t, "sub", outer.Tables[1].Alias)
	require.NotNil(t, outer.Tables[1].Scope)

	inner := outer.Tables[1].Scope
	require.Len(t, inner.Tables, 1)
	assert.Equal(t, "t2", inner.Tables[0].Table)
}

func TestBuildScopesScalarSubqueryInSelectList(t *testing.T) {
	sql := "SELECT (SELECT max(x) FROM t2) AS mx, a FROM t1"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	outer := scopes[0]
	require.Len(t, outer.Tables, 1)
	assert.Equal(t, "t1", outer.Tables[0].Table)

	// Scalar subquery in SELECT list should be captured
	require.Len(t, outer.Subqueries, 1)
	inner := outer.Subqueries[0]
	require.Len(t, inner.Tables, 1)
	assert.Equal(t, "t2", inner.Tables[0].Table)
}

func TestBuildScopesExistsSubquery(t *testing.T) {
	sql := "SELECT a FROM t1 WHERE a IN (SELECT 1 FROM t2 WHERE t2.id = t1.id)"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	outer := scopes[0]
	require.Len(t, outer.Tables, 1)
	assert.Equal(t, "t1", outer.Tables[0].Table)

	require.Len(t, outer.Subqueries, 1)
	inner := outer.Subqueries[0]
	require.Len(t, inner.Tables, 1)
	assert.Equal(t, "t2", inner.Tables[0].Table)
	assert.Equal(t, outer, inner.Parent)
}

func TestBuildScopesGlobalInSubquery(t *testing.T) {
	sql := "SELECT a FROM t1 WHERE a GLOBAL IN (SELECT b FROM t2)"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	outer := scopes[0]
	require.Len(t, outer.Tables, 1)
	assert.Equal(t, "t1", outer.Tables[0].Table)

	require.Len(t, outer.Subqueries, 1)
	inner := outer.Subqueries[0]
	require.Len(t, inner.Tables, 1)
	assert.Equal(t, "t2", inner.Tables[0].Table)
}

func TestBuildScopesNestedCTEWithSubqueryInWhere(t *testing.T) {
	sql := "WITH cte AS (SELECT a FROM t1 WHERE b IN (SELECT c FROM t2)) SELECT * FROM cte"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	outer := scopes[0]
	require.Len(t, outer.CTEDefs, 1)
	assert.Equal(t, "cte", outer.CTEDefs[0].Name)

	// CTE body scope
	cteScope := outer.CTEDefs[0].Scope
	require.NotNil(t, cteScope)
	require.Len(t, cteScope.Tables, 1)
	assert.Equal(t, "t1", cteScope.Tables[0].Table)

	// Subquery inside CTE WHERE clause
	require.Len(t, cteScope.Subqueries, 1)
	inner := cteScope.Subqueries[0]
	require.Len(t, inner.Tables, 1)
	assert.Equal(t, "t2", inner.Tables[0].Table)
}

func TestBuildScopesAllScopesDeep(t *testing.T) {
	sql := "WITH cte AS (SELECT a FROM t1 WHERE b IN (SELECT c FROM t2)) SELECT * FROM cte WHERE x IN (SELECT d FROM t3)"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	all := scopes[0].AllScopes()
	// outer, CTE body, CTE WHERE subquery, outer WHERE subquery
	require.Len(t, all, 4)

	// Collect all table names across all scopes
	var allTables []string
	for _, s := range all {
		for _, ts := range s.Tables {
			if !ts.IsSubquery && !ts.IsCTE {
				allTables = append(allTables, ts.Table)
			}
		}
	}
	assert.Contains(t, allTables, "t1")
	assert.Contains(t, allTables, "t2")
	assert.Contains(t, allTables, "t3")
}
func TestBuildScopesDefaultDatabase(t *testing.T) {
	sql := "SELECT a FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr, "mydb")
	require.Len(t, scopes, 1)

	scope := scopes[0]
	assert.Equal(t, "mydb", scope.DefaultDatabase)

	require.Len(t, scope.Tables, 1)
	assert.Equal(t, "", scope.Tables[0].Database) // not explicitly qualified
	assert.Equal(t, "mydb", scope.Tables[0].ResolvedDatabase(scope))
}

func TestBuildScopesExplicitDatabaseOverridesDefault(t *testing.T) {
	sql := "SELECT a FROM otherdb.t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr, "mydb")
	require.Len(t, scopes, 1)

	require.Len(t, scopes[0].Tables, 1)
	assert.Equal(t, "otherdb", scopes[0].Tables[0].Database)
	assert.Equal(t, "otherdb", scopes[0].Tables[0].ResolvedDatabase(scopes[0]))
}

func TestBuildScopesDefaultDatabasePropagates(t *testing.T) {
	sql := "WITH cte AS (SELECT a FROM t_inner) SELECT * FROM cte"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr, "mydb")
	require.Len(t, scopes, 1)

	// Outer scope has default database
	assert.Equal(t, "mydb", scopes[0].DefaultDatabase)

	// CTE body scope inherits default database
	require.Len(t, scopes[0].CTEDefs, 1)
	cteScope := scopes[0].CTEDefs[0].Scope
	require.NotNil(t, cteScope)
	assert.Equal(t, "mydb", cteScope.DefaultDatabase)

	// Unqualified table in CTE resolves to default database
	require.Len(t, cteScope.Tables, 1)
	assert.Equal(t, "mydb", cteScope.Tables[0].ResolvedDatabase(cteScope))
}

func TestBuildScopesDefaultDatabaseInSubquery(t *testing.T) {
	sql := "SELECT * FROM (SELECT a FROM t)"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr, "mydb")
	require.Len(t, scopes, 1)

	require.Len(t, scopes[0].Tables, 1)
	require.NotNil(t, scopes[0].Tables[0].Scope)

	innerScope := scopes[0].Tables[0].Scope
	assert.Equal(t, "mydb", innerScope.DefaultDatabase)
	require.Len(t, innerScope.Tables, 1)
	assert.Equal(t, "mydb", innerScope.Tables[0].ResolvedDatabase(innerScope))
}

func TestBuildScopesDefaultDatabaseUnionAll(t *testing.T) {
	sql := "SELECT a FROM t1 UNION ALL SELECT b FROM db2.t2"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr, "mydb")
	require.Len(t, scopes, 2)

	// First branch: unqualified → resolves to default
	assert.Equal(t, "mydb", scopes[0].DefaultDatabase)
	assert.Equal(t, "mydb", scopes[0].Tables[0].ResolvedDatabase(scopes[0]))

	// Second branch: explicitly qualified → explicit wins
	assert.Equal(t, "mydb", scopes[1].DefaultDatabase)
	assert.Equal(t, "db2", scopes[1].Tables[0].ResolvedDatabase(scopes[1]))
}

func TestBuildScopesNoDefaultDatabase(t *testing.T) {
	sql := "SELECT a FROM t"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	// No default database — backward compatible
	scopes := nanopass.BuildScopes(pr)
	require.Len(t, scopes, 1)

	assert.Equal(t, "", scopes[0].DefaultDatabase)
	assert.Equal(t, "", scopes[0].Tables[0].ResolvedDatabase(scopes[0]))
}

func TestBuildScopesMixedDatabases(t *testing.T) {
	sql := "SELECT * FROM t1 JOIN db2.t2 ON t1.id = t2.id"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	scopes := nanopass.BuildScopes(pr, "mydb")
	require.Len(t, scopes, 1)

	require.Len(t, scopes[0].Tables, 2)
	// t1 is unqualified → resolves to default
	assert.Equal(t, "mydb", scopes[0].Tables[0].ResolvedDatabase(scopes[0]))
	// db2.t2 is qualified → uses explicit
	assert.Equal(t, "db2", scopes[0].Tables[1].ResolvedDatabase(scopes[0]))
}
