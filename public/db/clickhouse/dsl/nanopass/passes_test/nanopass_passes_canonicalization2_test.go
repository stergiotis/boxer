//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper ---

func mustProduceValidSQL(t *testing.T, pass func(string) (string, error), sql string) string {
	t.Helper()
	got, err := pass(sql)
	require.NoError(t, err, "pass failed for: %s", sql)
	_, err = nanopass.Parse(got)
	require.NoError(t, err, "produced invalid SQL: %s", got)
	return got
}

// ============================================================================
// CanonicalizeJoin
// ============================================================================

func TestNormalizeJoinKeywordOrder(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "left_all_to_all_left",
			input:    "SELECT a FROM t1 LEFT ALL JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id",
		},
		{
			name:     "right_any_to_any_right",
			input:    "SELECT a FROM t1 RIGHT ANY JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 ANY RIGHT JOIN t2 ON t1.id = t2.id",
		},
		{
			name:     "inner_all_to_all_inner",
			input:    "SELECT a FROM t1 INNER ALL JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 ALL INNER JOIN t2 ON t1.id = t2.id",
		},
		{
			name:     "full_any_to_any_full",
			input:    "SELECT a FROM t1 FULL ANY JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 ANY FULL JOIN t2 ON t1.id = t2.id",
		},
		{
			name:     "already_canonical_all_left",
			input:    "SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id",
		},
		{
			name:     "bare_inner_unchanged",
			input:    "SELECT a FROM t1 INNER JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 INNER JOIN t2 ON t1.id = t2.id",
		},
		{
			name:     "bare_left_unchanged",
			input:    "SELECT a FROM t1 LEFT JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 LEFT JOIN t2 ON t1.id = t2.id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustProduceValidSQL(t, passes.CanonicalizeJoin, tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestNormalizeJoinRemoveOuter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
		excludes string
	}{
		{
			name:     "left_outer",
			input:    "SELECT a FROM t1 LEFT OUTER JOIN t2 ON t1.id = t2.id",
			contains: "LEFT",
			excludes: "OUTER",
		},
		{
			name:     "full_outer",
			input:    "SELECT a FROM t1 FULL OUTER JOIN t2 ON t1.id = t2.id",
			contains: "FULL",
			excludes: "OUTER",
		},
		{
			name:     "right_outer_any",
			input:    "SELECT a FROM t1 RIGHT OUTER ANY JOIN t2 ON t1.id = t2.id",
			contains: "ANY RIGHT",
			excludes: "OUTER",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustProduceValidSQL(t, passes.CanonicalizeJoin, tt.input)
			assert.Contains(t, got, tt.contains)
			assert.NotContains(t, got, tt.excludes)
		})
	}
}

func TestNormalizeJoinCommaToGross(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeJoin, "SELECT a FROM t1, t2")
	assert.Contains(t, got, "CROSS JOIN")
	assert.NotContains(t, got, ",")
}

func TestNormalizeJoinUsingParens(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeJoin, "SELECT a FROM t1 JOIN t2 USING id")
	assert.Contains(t, got, "USING (")
	assert.Contains(t, got, ")")
}

func TestNormalizeJoinUsingParensAlreadyPresent(t *testing.T) {
	input := "SELECT a FROM t1 JOIN t2 USING (id)"
	got := mustProduceValidSQL(t, passes.CanonicalizeJoin, input)
	assert.Contains(t, got, "USING (id)")
}

func TestNormalizeJoinIdempotent(t *testing.T) {
	sqls := []string{
		"SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id",
		"SELECT a FROM t1 CROSS JOIN t2",
		"SELECT a FROM t1 JOIN t2 USING (id)",
		"SELECT a FROM t WHERE a = 1",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass1 := mustProduceValidSQL(t, passes.CanonicalizeJoin, sql)
			pass2 := mustProduceValidSQL(t, passes.CanonicalizeJoin, pass1)
			assert.Equal(t, pass1, pass2, "not idempotent")
		})
	}
}

// ============================================================================
// CanonicalizeCaseConditionals
// ============================================================================

func TestNormalizeCaseSearched(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "simple_searched",
			input:    "SELECT CASE WHEN a = 1 THEN 'one' ELSE 'other' END FROM t",
			contains: "if(",
		},
		{
			name:     "searched_no_else",
			input:    "SELECT CASE WHEN a = 1 THEN 'one' END FROM t",
			contains: "if(",
		},
		{
			name:     "searched_multiple_whens",
			input:    "SELECT CASE WHEN a = 1 THEN 'one' WHEN a = 2 THEN 'two' ELSE 'other' END FROM t",
			contains: "multiIf(",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustProduceValidSQL(t, passes.CanonicalizeCaseConditionals, tt.input)
			assert.Contains(t, got, tt.contains)
			assert.NotContains(t, got, "CASE")
			assert.NotContains(t, got, "WHEN")
			assert.NotContains(t, got, "THEN")
			assert.NotContains(t, got, "END")
		})
	}
}

func TestNormalizeCaseSimple(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "simple_with_operand",
			input:    "SELECT CASE x WHEN 1 THEN 'one' WHEN 2 THEN 'two' END FROM t",
			contains: "caseWithExpression(",
		},
		{
			name:     "simple_with_else",
			input:    "SELECT CASE x WHEN 1 THEN 'one' ELSE 'other' END FROM t",
			contains: "caseWithExpression(",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustProduceValidSQL(t, passes.CanonicalizeCaseConditionals, tt.input)
			assert.Contains(t, got, tt.contains)
			assert.NotContains(t, got, "CASE")
		})
	}
}

func TestNormalizeCaseNoCaseUnchanged(t *testing.T) {
	input := "SELECT a FROM t WHERE a > 1"
	got := mustProduceValidSQL(t, passes.CanonicalizeCaseConditionals, input)
	assert.Equal(t, input, got)
}

func TestNormalizeCaseIdempotent(t *testing.T) {
	sqls := []string{
		"SELECT multiIf(a = 1, 'one', 'other') FROM t",
		"SELECT multiIf((x) = 1, 'one', 'other') FROM t",
		"SELECT a FROM t",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass1 := mustProduceValidSQL(t, passes.CanonicalizeCaseConditionals, sql)
			pass2 := mustProduceValidSQL(t, passes.CanonicalizeCaseConditionals, pass1)
			assert.Equal(t, pass1, pass2, "not idempotent")
		})
	}
}

// ============================================================================
// NormalizeSugar
// ============================================================================

func TestNormalizeSugarDate(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeSugar, "SELECT DATE '2024-01-01'")
	assert.Contains(t, got, "toDate('2024-01-01')")
	assert.NotContains(t, got, "DATE '")
}

func TestNormalizeSugarTimestamp(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeSugar, "SELECT TIMESTAMP '2024-01-01 00:00:00'")
	assert.Contains(t, got, "toDateTime('2024-01-01 00:00:00')")
}

func TestNormalizeSugarExtract(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeSugar, "SELECT EXTRACT(DAY FROM d) FROM t")
	assert.Contains(t, got, "extract(")
	assert.Contains(t, got, "'DAY'")
	assert.NotContains(t, got, "EXTRACT(")
}

func TestNormalizeSugarSubstring(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeSugar, "SELECT SUBSTRING('hello' FROM 1 FOR 3)")
	assert.Contains(t, got, "substring(")
	assert.NotContains(t, got, "SUBSTRING(")
	assert.NotContains(t, got, "FROM")
	assert.NotContains(t, got, "FOR")
}

func TestNormalizeSugarSubstringNoFor(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeSugar, "SELECT SUBSTRING('hello' FROM 2)")
	assert.Contains(t, got, "substring(")
}

func TestNormalizeSugarTrim(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{
			name:     "trim_both",
			input:    "SELECT TRIM(BOTH ' ' FROM s) FROM t",
			contains: "trimBoth(",
		},
		{
			name:     "trim_leading",
			input:    "SELECT TRIM(LEADING ' ' FROM s) FROM t",
			contains: "trimLeading(",
		},
		{
			name:     "trim_trailing",
			input:    "SELECT TRIM(TRAILING ' ' FROM s) FROM t",
			contains: "trimTrailing(",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mustProduceValidSQL(t, passes.CanonicalizeSugar, tt.input)
			assert.Contains(t, got, tt.contains)
			assert.NotContains(t, got, "TRIM(")
		})
	}
}

func TestNormalizeSugarIdempotent(t *testing.T) {
	sqls := []string{
		"SELECT toDate('2024-01-01')",
		"SELECT toDateTime('2024-01-01 00:00:00')",
		"SELECT extract(d, 'DAY') FROM t",
		"SELECT substring('hello', 1, 3)",
		"SELECT trimBoth(s, ' ') FROM t",
		"SELECT a FROM t",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass1 := mustProduceValidSQL(t, passes.CanonicalizeSugar, sql)
			pass2 := mustProduceValidSQL(t, passes.CanonicalizeSugar, pass1)
			assert.Equal(t, pass1, pass2, "not idempotent")
		})
	}
}

// ============================================================================
// NormalizeTernary
// ============================================================================

func TestNormalizeTernary(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeTernary, "SELECT a ? b : c FROM t")
	assert.Contains(t, got, "if(a, b, c)")
	assert.NotContains(t, got, "?")
	assert.NotContains(t, got, ":")
}

func TestNormalizeTernaryComplex(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeTernary, "SELECT (x > 0) ? x : -x FROM t")
	assert.Contains(t, got, "if(")
}

func TestNormalizeTernaryInWhere(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeTernary, "SELECT a FROM t WHERE (a > 1 ? a : 0) > 5")
	assert.Contains(t, got, "if(")
	assert.NotContains(t, got, "?")
}

func TestNormalizeTernaryNoTernaryUnchanged(t *testing.T) {
	input := "SELECT a FROM t WHERE a > 1"
	got := mustProduceValidSQL(t, passes.CanonicalizeTernary, input)
	assert.Equal(t, input, got)
}

func TestNormalizeTernaryIdempotent(t *testing.T) {
	sqls := []string{
		"SELECT if(a, b, c) FROM t",
		"SELECT a FROM t",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass1 := mustProduceValidSQL(t, passes.CanonicalizeTernary, sql)
			pass2 := mustProduceValidSQL(t, passes.CanonicalizeTernary, pass1)
			assert.Equal(t, pass1, pass2, "not idempotent")
		})
	}
}

// ============================================================================
// CanonicalizeIdentifiers
// ============================================================================

func TestNormalizeIdentifiersBare(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, "SELECT a FROM t")
	assert.Contains(t, got, `"a"`)
	assert.Contains(t, got, `"t"`)
}

func TestNormalizeIdentifiersBacktick(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, "SELECT `col` FROM `tbl`")
	assert.Contains(t, got, `"col"`)
	assert.Contains(t, got, `"tbl"`)
	assert.NotContains(t, got, "`")
}

func TestNormalizeIdentifiersAlreadyDoubleQuoted(t *testing.T) {
	input := `SELECT "a" FROM "t"`
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, input)
	assert.Equal(t, input, got)
}

func TestNormalizeIdentifiersQualified(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, "SELECT t.col FROM db.t")
	assert.Contains(t, got, `"t"."col"`)
	assert.Contains(t, got, `"db"."t"`)
}

func TestNormalizeIdentifiersFunctionName(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, "SELECT count(a) FROM t")
	assert.Contains(t, got, `"count"`)
	assert.Contains(t, got, `"a"`)
}

func TestNormalizeIdentifiersAlias(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, "SELECT a AS x FROM t")
	assert.Contains(t, got, `"a"`)
	assert.Contains(t, got, `"x"`)
}

func TestNormalizeIdentifiersCTE(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, "WITH cte AS (SELECT 1) SELECT * FROM cte")
	assert.Contains(t, got, `"cte"`)
}

func TestNormalizeIdentifiersSettings(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, "SELECT a FROM t SETTINGS max_threads = 4")
	assert.Contains(t, got, `"max_threads"`)
}

func TestNormalizeIdentifiersKeywordsUntouched(t *testing.T) {
	// Structural keywords (SELECT, FROM, WHERE) must NOT be quoted
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, "SELECT a FROM t WHERE a > 1")
	assert.Contains(t, got, "SELECT")
	assert.Contains(t, got, "FROM")
	assert.Contains(t, got, "WHERE")
}

func TestNormalizeIdentifiersBooleanLiteralsUntouched(t *testing.T) {
	// true/false are JSON_TRUE/JSON_FALSE tokens, not identifiers — must NOT be quoted
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, "SELECT true, false")
	// true and false should not become "true" and "false"
	assert.NotContains(t, got, `"true"`)
	assert.NotContains(t, got, `"false"`)
}

func TestNormalizeIdentifiersNullUntouched(t *testing.T) {
	got := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, "SELECT NULL")
	assert.NotContains(t, got, `"NULL"`)
}

func TestNormalizeIdentifiersIdempotent(t *testing.T) {
	sqls := []string{
		`SELECT "a" FROM "t"`,
		`SELECT "a" FROM "db"."t" WHERE "a" > 1`,
		`SELECT "count"("a") FROM "t"`,
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass1 := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, sql)
			pass2 := mustProduceValidSQL(t, passes.CanonicalizeIdentifiers, pass1)
			assert.Equal(t, pass1, pass2, "not idempotent")
		})
	}
}

// ============================================================================
// Pipeline: Grammar1 → Grammar2 completeness
// ============================================================================

func TestFullPipelineProducesCanonicalSQL(t *testing.T) {
	// This test verifies that applying all normalization passes to Grammar1 SQL
	// produces SQL that contains no Grammar1-only constructs.
	// It does NOT re-parse with Grammar2 (that requires a separate parser),
	// but checks for absence of non-canonical patterns.

	inputs := []struct {
		name string
		sql  string
	}{
		{"bare_identifiers", "SELECT a, b FROM t WHERE a > 1"},
		{"backtick_idents", "SELECT `col` FROM `db`.`tbl`"},
		{"case_searched", "SELECT CASE WHEN a = 1 THEN 'x' ELSE 'y' END FROM t"},
		{"case_simple", "SELECT CASE x WHEN 1 THEN 'a' WHEN 2 THEN 'b' END FROM t"},
		{"date_sugar", "SELECT DATE '2024-01-01'"},
		{"timestamp_sugar", "SELECT TIMESTAMP '2024-01-01 00:00:00'"},
		{"extract_sugar", "SELECT EXTRACT(DAY FROM d) FROM t"},
		{"substring_sugar", "SELECT SUBSTRING('hello' FROM 1 FOR 3)"},
		{"trim_sugar", "SELECT TRIM(BOTH ' ' FROM s) FROM t"},
		{"ternary", "SELECT a ? b : c FROM t"},
		{"join_order", "SELECT a FROM t1 LEFT ALL JOIN t2 ON t1.id = t2.id"},
		{"join_outer", "SELECT a FROM t1 LEFT OUTER JOIN t2 ON t1.id = t2.id"},
		{"using_no_parens", "SELECT a FROM t1 JOIN t2 USING id"},
		{"comma_join", "SELECT a FROM t1, t2"},
		{"combined", "SELECT CASE WHEN a = 1 THEN DATE '2024-01-01' ELSE TIMESTAMP '2024-06-01 00:00:00' END AS d FROM t1 LEFT OUTER ALL JOIN t2 USING id"},
	}

	pipeline := func(sql string) (string, error) {
		var err error
		// Order matters: identifiers last (after all structural rewrites)
		sql, err = passes.CanonicalizeJoin(sql)
		if err != nil {
			return "", err
		}
		sql, err = passes.CanonicalizeTernary(sql)
		if err != nil {
			return "", err
		}
		sql, err = passes.CanonicalizeCaseConditionals(sql)
		if err != nil {
			return "", err
		}
		sql, err = passes.CanonicalizeSugar(sql)
		if err != nil {
			return "", err
		}
		// CanonicalizeIdentifiers runs last — all other passes may introduce bare identifiers
		sql, err = passes.CanonicalizeIdentifiers(sql)
		if err != nil {
			return "", err
		}
		return sql, nil
	}

	for _, tt := range inputs {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pipeline(tt.sql)
			require.NoError(t, err)

			// Verify parseable
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "pipeline produced invalid SQL: %s", got)

			// Verify no Grammar1-only constructs remain
			assert.NotContains(t, got, "CASE ", "CASE should be normalized")
			assert.NotContains(t, got, "==", "== should be normalized to =")
			assert.NotContains(t, got, "OUTER", "OUTER should be removed")
			assert.NotContains(t, got, " ? ", "ternary should be normalized")
			// Note: cannot check for bare identifiers easily since structural keywords
			// like SELECT, FROM, WHERE are still bare. The identifier pass only quotes
			// identifiers in identifier positions.
		})
	}
}
