package nanopass_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// `columns` is admitted as a table name. The lexer makes it the COLUMNS token
// (for `COLUMNS('re')`), and COLUMNS is not in the keyword rule, so
// `FROM system.columns` — a table every ClickHouse server has — failed grammar1
// while `system.tables` parsed. It is admitted in table position only; the
// keyword rule would make `COLUMNS('re')` ambiguous with a function call.
func TestParse_ColumnsKeywordInTablePosition(t *testing.T) {
	for _, sql := range []string{
		"SELECT 1 FROM system.columns",
		"SELECT 1 FROM system.COLUMNS",
		"SELECT 1 FROM columns",
		"SELECT name FROM system.columns WHERE database = 'system'",
		"SELECT system.columns.name FROM system.columns",
		"SELECT columns.* FROM system.columns",
		"SELECT c.name FROM system.columns AS c JOIN system.tables AS t ON c.table = t.name",
	} {
		t.Run(sql, func(t *testing.T) {
			pr, err := nanopass.Parse(sql)
			require.NoError(t, err, "grammar1 must accept `columns` as a table name")
			require.NotNil(t, pr.Tree)
		})
	}
}

// The admission must not change how the dynamic column selector parses.
func TestParse_ColumnsSelectorStillDynamic(t *testing.T) {
	pr, err := nanopass.Parse("SELECT COLUMNS('^a') FROM system.columns")
	require.NoError(t, err)
	dyn := nanopass.FindAll(pr.Tree, func(c antlr.ParserRuleContext) bool {
		_, ok := c.(*grammar1.DynamicColumnSelectionContext)
		return ok
	})
	assert.Len(t, dyn, 1, "COLUMNS('^a') stays a dynamic column selection")
	fn := nanopass.FindAll(pr.Tree, func(c antlr.ParserRuleContext) bool {
		_, ok := c.(*grammar1.ColumnExprFunctionContext)
		return ok
	})
	assert.Empty(t, fn, "COLUMNS('^a') must not become a function call")
	out, err := passes.CanonicalizeFull(10).Run("SELECT COLUMNS('^a') FROM system.columns")
	require.NoError(t, err)
	assert.Contains(t, out, "COLUMNS('^a')")
}

// The scope builder and the table inventory name the table by its token text
// rather than dereferencing a nil Identifier child.
func TestScopes_ColumnsTableNamed(t *testing.T) {
	pr, err := nanopass.Parse("SELECT 1 FROM system.columns")
	require.NoError(t, err)
	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	require.Len(t, scopes[0].Tables, 1)
	assert.Equal(t, "columns", scopes[0].Tables[0].Table)
	assert.Equal(t, "system", scopes[0].Tables[0].Database)

	refs := analysis.ExtractTables(pr)
	require.Len(t, refs, 1)
	assert.Equal(t, "columns", refs[0].Table)
	assert.Equal(t, "system", refs[0].Database)
}

// QualifyTables prepends the default database to an unqualified `columns`.
func TestQualifyTables_ColumnsTable(t *testing.T) {
	out, err := passes.QualifyTables("system").Run("SELECT 1 FROM columns")
	require.NoError(t, err)
	assert.Contains(t, out, "system.columns")
}

// A buffer reading system.columns classifies as a plain read, as a buffer
// reading system.tables does.
func TestSecurity_ColumnsTableIsRead(t *testing.T) {
	pr, err := nanopass.Parse("SELECT name FROM system.columns")
	require.NoError(t, err)
	class, _, err := analysis.ClassifyQuerySecurity(pr)
	require.NoError(t, err)
	assert.Equal(t, analysis.QuerySecurityRead, class)
}

// Canonicalisation keeps the name's case and quotes it, so the canonical form
// names the same table and parses under grammar2, whose table position takes
// IDENTIFIER only.
func TestCanonicalize_ColumnsTable(t *testing.T) {
	for sql, want := range map[string]string{
		"SELECT name FROM system.columns":         `FROM "system"."columns"`,
		"select name from columns":                `FROM "columns"`,
		"SELECT columns.name FROM system.columns": `SELECT "columns"."name"`,
	} {
		t.Run(sql, func(t *testing.T) {
			out, err := passes.CanonicalizeFull(10).Run(sql)
			require.NoError(t, err)
			assert.Contains(t, out, want)
			_, err = nanopass.ParseCanonical(out)
			require.NoError(t, err, "grammar2 must accept the canonical form: %s", out)
		})
	}
}
