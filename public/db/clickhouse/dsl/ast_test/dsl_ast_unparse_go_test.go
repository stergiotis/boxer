//go:build llm_generated_opus46

package ast_test

import (
	"go/parser"
	"go/token"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sqlToASTForEmit(t *testing.T, sql string) ast.Query {
	t.Helper()
	normalized, err := passes.CanonicalizeFull(100)(sql)
	require.NoError(t, err)
	pr, err := nanopass.ParseCanonical(normalized)
	require.NoError(t, err)
	query, err := ast.ConvertCSTToAST(pr)
	require.NoError(t, err)
	return query
}

// wrapInGoFile wraps a Go expression in a minimal compilable Go file
// so we can parse-check it with go/parser.
func wrapInGoFile(expr string) string {
	return `package test

import (
	ast "placeholder/ast"
	. "placeholder/chsql"
)

var _ ast.ExprKind

var _ = ` + expr + `.Build()
`
}

// ============================================================================
// ToGoCode output contains expected builder calls
// ============================================================================

func TestToGoCodeBasicSelect(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT a, b FROM t")
	code := q.ToGoCode()
	assert.Contains(t, code, "Select(")
	assert.Contains(t, code, "Col(")
	assert.Contains(t, code, "From(")
}

func TestToGoCodeWhere(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT a FROM t WHERE a > 1")
	code := q.ToGoCode()
	assert.Contains(t, code, "Where(")
	assert.Contains(t, code, ".Gt(")
}

func TestToGoCodeOrderBy(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT a FROM t ORDER BY a DESC NULLS LAST")
	code := q.ToGoCode()
	assert.Contains(t, code, "OrderBy(")
	assert.Contains(t, code, ".Desc()")
	assert.Contains(t, code, "NullsLast(")
}

func TestToGoCodeGroupByTotals(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT a, count(*) FROM t GROUP BY a WITH TOTALS")
	code := q.ToGoCode()
	assert.Contains(t, code, "GroupBy(")
	assert.Contains(t, code, "WithTotals()")
}

func TestToGoCodeFunction(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT count(a) FROM t")
	code := q.ToGoCode()
	assert.Contains(t, code, `Func("count"`)
}

func TestToGoCodeAlias(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT a AS x FROM t")
	code := q.ToGoCode()
	assert.Contains(t, code, `.As("x")`)
}

func TestToGoCodeCTE(t *testing.T) {
	q := sqlToASTForEmit(t, "WITH cte AS (SELECT 1 AS x) SELECT x FROM cte")
	code := q.ToGoCode()
	assert.Contains(t, code, `With("cte"`)
}

func TestToGoCodeUnionAll(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT 1 UNION ALL SELECT 2")
	code := q.ToGoCode()
	assert.Contains(t, code, "UnionAll(")
}

func TestToGoCodeLimit(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT a FROM t LIMIT 10 OFFSET 20")
	code := q.ToGoCode()
	assert.Contains(t, code, "Limit(")
	assert.Contains(t, code, "Offset(")
}

func TestToGoCodeSettings(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT 1 SETTINGS max_threads = 4")
	code := q.ToGoCode()
	assert.Contains(t, code, `Settings("max_threads"`)
}

func TestToGoCodeFormat(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT 1 FORMAT JSONEachRow")
	code := q.ToGoCode()
	assert.Contains(t, code, `Format("JSONEachRow")`)
}

func TestToGoCodeBetween(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT a FROM t WHERE a BETWEEN 1 AND 10")
	code := q.ToGoCode()
	assert.Contains(t, code, ".Between(")
}

func TestToGoCodeIsNull(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT a FROM t WHERE a IS NOT NULL")
	code := q.ToGoCode()
	assert.Contains(t, code, ".IsNotNull()")
}

func TestToGoCodeSubquery(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT a FROM t WHERE a IN (SELECT id FROM t2)")
	code := q.ToGoCode()
	assert.Contains(t, code, "Sub(")
}

func TestToGoCodeStar(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT * FROM t")
	code := q.ToGoCode()
	assert.Contains(t, code, "Star()")
}

func TestToGoCodeQualifiedStar(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT t.* FROM t")
	code := q.ToGoCode()
	assert.Contains(t, code, `Star("t")`)
}

// ============================================================================
// ToGoCodeWithPrefix
// ============================================================================

func TestToGoCodeWithPrefix(t *testing.T) {
	q := sqlToASTForEmit(t, "SELECT a FROM t")
	code := q.ToGoCodeWithPrefix("chsql.")
	assert.Contains(t, code, "chsql.Select(")
	assert.Contains(t, code, "chsql.Col(")
}

// ============================================================================
// ToGoCode produces parseable Go syntax
//
// We wrap the generated expression in a minimal Go file and parse it
// with go/parser. This catches syntax errors (unclosed parens, bad
// string literals, etc.) without compiling.
// ============================================================================

func TestToGoCodeParseableGo(t *testing.T) {
	sqls := []string{
		"SELECT a, b FROM t WHERE a > 1",
		"SELECT count(a), sum(b) FROM t GROUP BY a WITH TOTALS",
		"SELECT a FROM t ORDER BY a DESC NULLS LAST LIMIT 10 OFFSET 5",
		"WITH cte AS (SELECT 1 AS x) SELECT x FROM cte",
		"SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3",
		"SELECT a FROM t WHERE a BETWEEN 1 AND 10 AND b IS NOT NULL",
		"SELECT a FROM t WHERE a IN (SELECT id FROM t2)",
		"SELECT * FROM t SETTINGS max_threads = 4 FORMAT JSONEachRow",
		"SELECT true, false, NULL, 42, 'hello'",
		"SELECT a FROM (SELECT 1 AS a) AS sub",
	}

	for _, sql := range sqls {
		name := sql
		if len(name) > 60 {
			name = name[:60]
		}
		t.Run(name, func(t *testing.T) {
			q := sqlToASTForEmit(t, sql)
			code := q.ToGoCode()
			goFile := wrapInGoFile(code)

			fset := token.NewFileSet()
			_, err := parser.ParseFile(fset, "test.go", goFile, parser.AllErrors)
			assert.NoError(t, err, "ToGoCode produced unparseable Go:\n%s\n\nWrapped:\n%s", code, goFile)
		})
	}
}

// ============================================================================
// Corpus: ToGoCode produces parseable Go for every corpus entry
// ============================================================================

func TestToGoCodeCorpusParseable(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	passed, skipped := 0, 0
	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			normalized, err := passes.CanonicalizeFull(100)(entry.SQL)
			if err != nil {
				skipped++
				t.Skipf("pipeline: %v", err)
			}

			pr, err := nanopass.ParseCanonical(normalized)
			if err != nil {
				skipped++
				t.Skipf("ParseCanonical: %v", err)
			}

			query, err := ast.ConvertCSTToAST(pr)
			if err != nil {
				skipped++
				t.Skipf("ConvertCSTToAST: %v", err)
			}

			code := query.ToGoCode()
			goFile := wrapInGoFile(code)

			fset := token.NewFileSet()
			_, err = parser.ParseFile(fset, "test.go", goFile, parser.AllErrors)
			assert.NoError(t, err,
				"ToGoCode produced unparseable Go for %s:\n%s", entry.Name, code)

			passed++
		})
	}
	t.Logf("corpus GoCode: %d parseable, %d skipped (of %d)", passed, skipped, len(entries))
}
