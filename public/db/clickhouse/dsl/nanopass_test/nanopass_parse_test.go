//go:build llm_generated_opus46

package nanopass_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCorpus(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			pr, err := nanopass.Parse(entry.SQL)
			require.NoError(t, err, "failed to parse:\n%s", entry.SQL)
			require.NotNil(t, pr)
			require.NotNil(t, pr.Tree)
		})
	}
}
func TestParseEmptyString(t *testing.T) {
	_, err := nanopass.Parse("")
	assert.Error(t, err)
}

func TestParseWhitespaceOnly(t *testing.T) {
	_, err := nanopass.Parse("   ")
	assert.Error(t, err)
}

func TestParseCommentOnly(t *testing.T) {
	_, err := nanopass.Parse("-- just a comment\n")
	assert.Error(t, err)
}

func TestParseSemicolons(t *testing.T) {
	_, err := nanopass.Parse(";;;")
	assert.Error(t, err)
}

func TestParseIncompleteSelect(t *testing.T) {
	_, err := nanopass.Parse("SELECT")
	assert.Error(t, err)
}

func TestParseIncompleteWhere(t *testing.T) {
	_, err := nanopass.Parse("SELECT a FROM t WHERE")
	assert.Error(t, err)
}

func TestParseTrailingSemicolon(t *testing.T) {
	// ClickHouse allows trailing semicolons — grammar may or may not
	pr, err := nanopass.Parse("SELECT 1;")
	if err != nil {
		t.Logf("trailing semicolon not supported by grammar: %v", err)
		t.Skip()
	}
	require.NotNil(t, pr)
}

// ============================================================================
// Parse() — Grammar1
// ============================================================================

func TestParseGrammar1Basic(t *testing.T) {
	pr, err := nanopass.Parse("SELECT a FROM t")
	require.NoError(t, err)
	require.NotNil(t, pr)
	require.NotNil(t, pr.Tree)
	require.NotNil(t, pr.TokenStream)
	require.NotNil(t, pr.Parser)

	// Tree should be a Grammar1 QueryStmtContext
	_, ok := pr.Tree.(*grammar1.QueryStmtContext)
	assert.True(t, ok, "expected *grammar1.QueryStmtContext, got %T", pr.Tree)
}

func TestParseGrammar1AcceptsSugar(t *testing.T) {
	// Grammar1 accepts all syntactic sugar
	sugars := []string{
		"SELECT CASE WHEN a = 1 THEN 'x' END FROM t",
		"SELECT CAST(a AS UInt64) FROM t",
		"SELECT a::UInt64 FROM t",
		"SELECT [1, 2, 3]",
		"SELECT (1, 2, 3)",
		"SELECT a[1] FROM t",
		"SELECT DATE '2024-01-01'",
		"SELECT TIMESTAMP '2024-01-01 00:00:00'",
		"SELECT EXTRACT(DAY FROM d) FROM t",
		"SELECT SUBSTRING(s FROM 1 FOR 3) FROM t",
		"SELECT TRIM(BOTH ' ' FROM s) FROM t",
		"SELECT a ? b : c FROM t",
		"SELECT a FROM t WHERE a == 1",
		"SELECT a FROM t1 LEFT OUTER JOIN t2 ON t1.id = t2.id",
		"SELECT a FROM t1, t2",
	}
	for _, sql := range sugars {
		t.Run(sql, func(t *testing.T) {
			_, err := nanopass.Parse(sql)
			assert.NoError(t, err, "Grammar1 should accept: %s", sql)
		})
	}
}

func TestParseGrammar1RejectsInvalid(t *testing.T) {
	invalid := []string{"", "   ", "SELECT", ";;;", "NOT SQL AT ALL"}
	for _, sql := range invalid {
		_, err := nanopass.Parse(sql)
		assert.Error(t, err, "Grammar1 should reject: %q", sql)
	}
}

// Grammar1 rejects bare keyword aliases (no keywordForAlias rule)
func TestParseGrammar1RejectsBareKeywordAlias(t *testing.T) {
	// These use keywords as bare aliases — not allowed in Grammar1
	rejected := []string{
		"SELECT a desc FROM t",  // desc is a keyword, not IDENTIFIER
		"SELECT a rows FROM t",  // rows is a keyword
		"SELECT a nulls FROM t", // nulls is a keyword
	}
	for _, sql := range rejected {
		t.Run(sql, func(t *testing.T) {
			_, err := nanopass.Parse(sql)
			assert.Error(t, err, "Grammar1 should reject bare keyword alias: %s", sql)
		})
	}
}

// Grammar1 accepts keyword aliases with AS prefix
func TestParseGrammar1AcceptsASKeywordAlias(t *testing.T) {
	accepted := []string{
		"SELECT a AS desc FROM t",
		"SELECT a AS rows FROM t",
		"SELECT a AS nulls FROM t",
	}
	for _, sql := range accepted {
		t.Run(sql, func(t *testing.T) {
			_, err := nanopass.Parse(sql)
			assert.NoError(t, err, "Grammar1 should accept AS + keyword alias: %s", sql)
		})
	}
}

// ============================================================================
// ParseCanonical() — Grammar2
// ============================================================================

func TestParseCanonicalBasic(t *testing.T) {
	// Canonical SQL: double-quoted identifiers, no sugar
	pr, err := nanopass.ParseCanonical(`SELECT "a" FROM "t"`)
	require.NoError(t, err)
	require.NotNil(t, pr)
	require.NotNil(t, pr.Tree)

	// Tree should be a Grammar2 QueryStmtContext
	_, ok := pr.Tree.(*grammar2.QueryStmtContext)
	assert.True(t, ok, "expected *grammar2.QueryStmtContext, got %T", pr.Tree)
}

func TestParseCanonicalAcceptsCanonicalForms(t *testing.T) {
	canonical := []string{
		`SELECT "a" FROM "t"`,
		`SELECT "count"("a") FROM "t"`,
		`SELECT multiIf("a" = 1, 'one', 'other') FROM "t"`,
		`SELECT if("a" > 0, "a", -"a") FROM "t"`,
		`SELECT caseWithExpression("x", 1, 'one', 'other') FROM "t"`,
		`SELECT "a" FROM "t" ORDER BY "a" DESC NULLS LAST`,
		`SELECT "sum"("x") OVER (ORDER BY "a" ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) FROM "t"`,
		`SELECT true, false, NULL`,
		`SELECT "a" FROM "t1" CROSS JOIN "t2"`,
		`SELECT "a" FROM "t1" ALL LEFT JOIN "t2" ON "t1"."id" = "t2"."id"`,
		`SELECT "a" FROM "t1" JOIN "t2" USING ("id")`,
		`SELECT toDate('2024-01-01')`,
		`SELECT toDateTime('2024-01-01 00:00:00')`,
		`SELECT "a" FROM "t" WHERE "a" = 1`,
		`SELECT INTERVAL "x" DAY FROM "t"`,
	}
	for _, sql := range canonical {
		name := sql
		if len(name) > 50 {
			name = name[:50]
		}
		var err error
		sql, err = passes.CanonicalizeFull(100)(sql)
		require.NoError(t, err)
		t.Run(name, func(t *testing.T) {
			_, err := nanopass.ParseCanonical(sql)
			assert.NoError(t, err, "Grammar2 should accept canonical SQL: %s", sql)
		})
	}
}

func TestParseCanonicalRejectsNonCanonical(t *testing.T) {
	nonCanonical := []struct {
		name string
		sql  string
	}{
		// Syntactic sugar — removed from Grammar2
		{"case", "SELECT CASE WHEN a = 1 THEN 'x' END FROM t"},
		{"cast_as", "SELECT CAST(a AS UInt64) FROM t"},
		{"cast_shorthand", "SELECT a::UInt64 FROM t"},
		{"array_literal", "SELECT [1, 2, 3]"},
		{"tuple_literal", "SELECT (1, 2, 3)"},
		{"array_access", "SELECT a[1] FROM t"},
		{"tuple_access", "SELECT t.1 FROM t"},
		{"date_sugar", "SELECT DATE '2024-01-01'"},
		{"timestamp_sugar", "SELECT TIMESTAMP '2024-01-01 00:00:00'"},
		{"extract_sugar", "SELECT EXTRACT(DAY FROM d) FROM t"},
		{"substring_sugar", "SELECT SUBSTRING(s FROM 1 FOR 3) FROM t"},
		{"trim_sugar", "SELECT TRIM(BOTH ' ' FROM s) FROM t"},
		{"ternary", "SELECT a ? b : c FROM t"},
	}
	for _, tt := range nonCanonical {
		t.Run(tt.name, func(t *testing.T) {
			_, err := nanopass.ParseCanonical(tt.sql)
			assert.Error(t, err, "Grammar2 should reject non-canonical SQL: %s", tt.sql)
		})
	}
}

// ============================================================================
// Validate and ValidateCanonical as pipeline steps
// ============================================================================

func TestValidatePass(t *testing.T) {
	result, err := nanopass.Validate("SELECT a FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t", result)
}

func TestValidateRejectsInvalid(t *testing.T) {
	_, err := nanopass.Validate("NOT SQL")
	assert.Error(t, err)
}

func TestValidateCanonicalPass(t *testing.T) {
	result, err := nanopass.ValidateCanonical(`SELECT "a" FROM "t"`)
	require.NoError(t, err)
	assert.Equal(t, `SELECT "a" FROM "t"`, result)
}

func TestValidateCanonicalRejectsNonCanonical(t *testing.T) {
	_, err := nanopass.ValidateCanonical("SELECT CASE WHEN a = 1 THEN 'x' END FROM t")
	assert.Error(t, err)
}

// ============================================================================
// SharedInfrastructure — WalkCST, NodeText work with both grammars
// ============================================================================

func TestWalkCSTWorksWithGrammar1(t *testing.T) {
	pr, err := nanopass.Parse("SELECT a FROM t")
	require.NoError(t, err)

	nodeCount := 0
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		nodeCount++
		return true
	})
	assert.Greater(t, nodeCount, 0)
}

func TestWalkCSTWorksWithGrammar2(t *testing.T) {
	pr, err := nanopass.ParseCanonical(`SELECT "a" FROM "t"`)
	require.NoError(t, err)

	nodeCount := 0
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		nodeCount++
		return true
	})
	assert.Greater(t, nodeCount, 0)
}

func TestNodeTextWorksWithGrammar1(t *testing.T) {
	pr, err := nanopass.Parse("SELECT a FROM t")
	require.NoError(t, err)

	text := nanopass.NodeText(pr, pr.Tree)
	assert.Contains(t, text, "SELECT")
	assert.Contains(t, text, "FROM")
}

func TestNodeTextWorksWithGrammar2(t *testing.T) {
	pr, err := nanopass.ParseCanonical(`SELECT "a" FROM "t"`)
	require.NoError(t, err)

	text := nanopass.NodeText(pr, pr.Tree)
	assert.Contains(t, text, "SELECT")
	assert.Contains(t, text, "FROM")
}

// ============================================================================
// Pipeline with ValidateCanonical as final step
// ============================================================================

func TestPipelineWithCanonicalValidation(t *testing.T) {
	// A pipeline that ends with ValidateCanonical should pass for canonical SQL
	result, err := nanopass.Pipeline(
		`SELECT "a" FROM "t"`,
		nanopass.ValidateCanonical,
	)
	require.NoError(t, err)
	assert.Equal(t, `SELECT "a" FROM "t"`, result)
}

func TestPipelineWithCanonicalValidationRejects(t *testing.T) {
	// A pipeline that ends with ValidateCanonical should fail for non-canonical SQL
	_, err := nanopass.Pipeline(
		"SELECT CASE WHEN a = 1 THEN 'x' END FROM t",
		nanopass.ValidateCanonical,
	)
	assert.Error(t, err)
}

// ============================================================================
// Cross-grammar: token types are compatible
// ============================================================================

func TestTokenTypesCompatibleAcrossGrammars(t *testing.T) {
	// Parse the same SQL with both grammars (using canonical form for Grammar2)
	pr1, err := nanopass.Parse(`SELECT "a" FROM "t" WHERE "a" > 1`)
	require.NoError(t, err)
	pr2, err := nanopass.ParseCanonical(`SELECT "a" FROM "t" WHERE "a" > 1`)
	require.NoError(t, err)

	// Both token streams should have the same tokens
	require.Equal(t, pr1.TokenStream.Size(), pr2.TokenStream.Size(),
		"token stream sizes differ")

	for i := 0; i < pr1.TokenStream.Size(); i++ {
		t1 := pr1.TokenStream.Get(i)
		t2 := pr2.TokenStream.Get(i)
		assert.Equal(t, t1.GetTokenType(), t2.GetTokenType(),
			"token[%d] type mismatch: %d vs %d (text: %q vs %q)",
			i, t1.GetTokenType(), t2.GetTokenType(), t1.GetText(), t2.GetText())
		assert.Equal(t, t1.GetText(), t2.GetText(),
			"token[%d] text mismatch", i)
	}
}
