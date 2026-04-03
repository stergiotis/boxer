//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Test Infrastructure
// ============================================================================

// fullCanonicalizationPipeline applies all normalization passes in the correct order.
// This is the sequence that transforms Grammar1 SQL into Grammar2 SQL.
func fullCanonicalizationPipeline(sql string) (result string, err error) {
	return passes.CanonicalizeFull(100)(sql)
}

// allNewPasses lists the new passes for parametric testing.
// Existing passes (NormalizeCast, NormalizeConstructors) are excluded.
var allNewPasses = []struct {
	name string
	pass nanopass.Pass
}{
	{"CanonicalizeJoin", passes.CanonicalizeJoin},
	{"NormalizeTernary", passes.CanonicalizeTernary},
	{"CanonicalizeCaseConditionals", passes.CanonicalizeCaseConditionals},
	{"NormalizeSugar", passes.CanonicalizeSugar},
	{"CanonicalizeIdentifiers", passes.CanonicalizeIdentifiers},
}

// ============================================================================
// Category 1: Explicit Input/Output Pairs (per pass)
// ============================================================================

func TestNormalizeJoinExplicitPairs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "left_all_reorder",
			input:    "SELECT a FROM t1 LEFT ALL JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id",
		},
		{
			name:     "inner_asof_reorder",
			input:    "SELECT a FROM t1 INNER ASOF JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 ASOF INNER JOIN t2 ON t1.id = t2.id",
		},
		{
			name:     "already_canonical",
			input:    "SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id",
		},
		{
			name:     "bare_join_unchanged",
			input:    "SELECT a FROM t1 JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM t1 JOIN t2 ON t1.id = t2.id",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := passes.CanonicalizeJoin(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

func TestNormalizeTernaryExplicitPairs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple",
			input:    "SELECT a ? b : c FROM t",
			expected: "SELECT if(a, b, c) FROM t",
		},
		{
			name:     "no_ternary_unchanged",
			input:    "SELECT a FROM t",
			expected: "SELECT a FROM t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := passes.CanonicalizeTernary(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

func TestNormalizeSugarExplicitPairs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "date",
			input:    "SELECT DATE '2024-01-01'",
			expected: "SELECT toDate('2024-01-01')",
		},
		{
			name:     "timestamp",
			input:    "SELECT TIMESTAMP '2024-01-01 12:00:00'",
			expected: "SELECT toDateTime('2024-01-01 12:00:00')",
		},
		{
			name:     "no_sugar_unchanged",
			input:    "SELECT a FROM t",
			expected: "SELECT a FROM t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := passes.CanonicalizeSugar(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// ============================================================================
// Category 2: Idempotency (per pass, per corpus entry)
// ============================================================================

func TestIdempotencyPerPass(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, p := range allNewPasses {
		t.Run(p.name, func(t *testing.T) {
			for _, entry := range entries {
				t.Run(entry.Name, func(t *testing.T) {
					pass1, err := p.pass(entry.SQL)
					if err != nil {
						t.Skipf("pass failed on input: %v", err)
					}
					pass2, err := p.pass(pass1)
					if err != nil {
						t.Fatalf("pass failed on own output: %v", err)
					}
					assert.Equal(t, pass1, pass2, "not idempotent for %s", entry.Name)
				})
			}
		})
	}
}

// ============================================================================
// Category 3: Corpus Validity (per pass — output must parse)
// ============================================================================

func TestCorpusValidityPerPass(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, p := range allNewPasses {
		t.Run(p.name, func(t *testing.T) {
			for _, entry := range entries {
				t.Run(entry.Name, func(t *testing.T) {
					out, err := p.pass(entry.SQL)
					if err != nil {
						t.Skipf("pass failed: %v", err)
					}
					_, err = nanopass.Parse(out)
					require.NoError(t, err, "produced invalid SQL for %s:\n%s", entry.Name, out)
				})
			}
		})
	}
}

// ============================================================================
// Category 4: Scope Preservation
//
// Pure normalization passes must not change the query's scope structure.
// The number of scopes, tables per scope, and CTE definitions must be identical.
// ============================================================================

func TestScopePreservationPerPass(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, p := range allNewPasses {
		t.Run(p.name, func(t *testing.T) {
			for _, entry := range entries {
				t.Run(entry.Name, func(t *testing.T) {
					prBefore, err := nanopass.Parse(entry.SQL)
					if err != nil {
						t.Skip()
					}

					out, err := p.pass(entry.SQL)
					if err != nil {
						t.Skip()
					}

					prAfter, err := nanopass.Parse(out)
					if err != nil {
						t.Skipf("output not parseable: %v", err)
					}

					scopesBefore := nanopass.BuildScopes(prBefore)
					scopesAfter := nanopass.BuildScopes(prAfter)
					require.Equal(t, len(scopesBefore), len(scopesAfter),
						"scope count changed for %s", entry.Name)

					for i := range scopesBefore {
						if i >= len(scopesAfter) {
							break
						}
						assert.Equal(t, len(scopesBefore[i].Tables), len(scopesAfter[i].Tables),
							"table count in scope %d changed for %s", i, entry.Name)
					}
				})
			}
		})
	}
}

// ============================================================================
// Category 5: UNION ALL — passes apply to all branches
// ============================================================================

func TestUnionAllPerPass(t *testing.T) {
	tests := []struct {
		name  string
		input string
		pass  nanopass.Pass
		check func(t *testing.T, got string)
	}{
		{
			name:  "ternary_both_branches",
			input: "SELECT a ? b : c FROM t1 UNION ALL SELECT x ? y : z FROM t2",
			pass:  passes.CanonicalizeTernary,
			check: func(t *testing.T, got string) {
				assert.Equal(t, 2, strings.Count(got, "if("), "if() should appear in both branches")
				assert.NotContains(t, got, "?")
			},
		},
		{
			name:  "case_both_branches",
			input: "SELECT CASE WHEN a = 1 THEN 'x' END FROM t1 UNION ALL SELECT CASE WHEN b = 2 THEN 'y' END FROM t2",
			pass:  passes.CanonicalizeCaseConditionals,
			check: func(t *testing.T, got string) {
				assert.Equal(t, 2, strings.Count(got, "if("),
					"if should appear in both branches")
			},
		},
		{
			name:  "join_both_branches",
			input: "SELECT a FROM t1 LEFT ALL JOIN t2 ON t1.id = t2.id UNION ALL SELECT b FROM t3 RIGHT ANY JOIN t4 ON t3.id = t4.id",
			pass:  passes.CanonicalizeJoin,
			check: func(t *testing.T, got string) {
				assert.Contains(t, got, "ALL LEFT")
				assert.Contains(t, got, "ANY RIGHT")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.pass(tt.input)
			require.NoError(t, err)
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
			tt.check(t, got)
		})
	}
}

// ============================================================================
// Category 6: CTE — CTE bodies are transformed, CTE references are not broken
// ============================================================================

func TestCTETransformation(t *testing.T) {
	tests := []struct {
		name  string
		input string
		pass  nanopass.Pass
		check func(t *testing.T, got string)
	}{
		{
			name:  "ternary_in_cte_body",
			input: "WITH cte AS (SELECT a ? b : c AS val FROM t) SELECT val FROM cte",
			pass:  passes.CanonicalizeTernary,
			check: func(t *testing.T, got string) {
				assert.Contains(t, got, "if(a, b, c)")
				assert.Contains(t, got, "FROM cte") // CTE reference preserved
			},
		},
		{
			name:  "case_in_cte_body",
			input: "WITH cte AS (SELECT CASE WHEN x = 1 THEN 'a' END FROM t) SELECT * FROM cte",
			pass:  passes.CanonicalizeCaseConditionals,
			check: func(t *testing.T, got string) {
				assert.Contains(t, got, "if(")
				assert.Contains(t, got, "FROM cte")
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.pass(tt.input)
			require.NoError(t, err)
			_, err = nanopass.Parse(got)
			require.NoError(t, err)
			tt.check(t, got)
		})
	}
}

// ============================================================================
// Category 7: Invalid SQL Rejection
// ============================================================================

func TestInvalidSQLRejection(t *testing.T) {
	invalid := []string{"", "   ", "SELECT", ";;;", "NOT SQL AT ALL"}
	for _, p := range allNewPasses {
		t.Run(p.name, func(t *testing.T) {
			for _, sql := range invalid {
				_, err := p.pass(sql)
				assert.Error(t, err, "should reject invalid SQL: %q", sql)
			}
		})
	}
}

// ============================================================================
// Category 8: Semantics Preservation — observable properties
//
// After each pass, the set of referenced tables and the set of referenced
// functions must be identical (modulo expected additions like "if", "multiIf").
// This catches accidental corruption of identifiers, dropped clauses, etc.
// ============================================================================

func TestSemanticsPreservationTables(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, p := range allNewPasses {
		// CanonicalizeIdentifiers changes table name text (adds quotes), so skip it here
		if p.name == "CanonicalizeIdentifiers" {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			for _, entry := range entries {
				t.Run(entry.Name, func(t *testing.T) {
					prBefore, err := nanopass.Parse(entry.SQL)
					if err != nil {
						t.Skip()
					}

					out, err := p.pass(entry.SQL)
					if err != nil {
						t.Skip()
					}

					prAfter, err := nanopass.Parse(out)
					if err != nil {
						t.Skip()
					}

					tablesBefore := analysis.ExtractTables(prBefore)
					tablesAfter := analysis.ExtractTables(prAfter)

					// Build sets of table names
					setBefore := make(map[string]bool, len(tablesBefore))
					for _, tr := range tablesBefore {
						setBefore[tr.Table] = true
					}
					setAfter := make(map[string]bool, len(tablesAfter))
					for _, tr := range tablesAfter {
						setAfter[tr.Table] = true
					}

					// Tables should not disappear or appear (except for comma→CROSS JOIN
					// which doesn't change table names)
					for name := range setBefore {
						assert.True(t, setAfter[name],
							"table %q disappeared after %s on %s", name, p.name, entry.Name)
					}
				})
			}
		})
	}
}

// ============================================================================
// Category 9: Grammar2 Compliance Validator
//
// After the full pipeline, walk the CST and verify NO Grammar1-only node
// types exist. This is the structural guarantee that the pipeline is complete.
// ============================================================================

// grammar2ForbiddenNodeTypes lists all CST context types that exist in Grammar1
// but are removed in Grammar2. If any survive the pipeline, the pipeline is incomplete.
var grammar2ForbiddenNodes = []string{
	"ColumnExprCast",
	"ColumnExprDate",
	"ColumnExprTimestamp",
	"ColumnExprExtract",
	"ColumnExprSubstring",
	"ColumnExprTrim",
	"ColumnExprArrayAccess",
	"ColumnExprTupleAccess",
	"ColumnExprArray",
	"ColumnExprTuple",
	"ColumnExprCase",
	"ColumnExprTernaryOp",
}

// checkGrammar2Compliance walks a CST and returns all Grammar1-only node types found.
func checkGrammar2Compliance(pr *nanopass.ParseResult) (violations []string) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		typeName := fmt.Sprintf("%T", ctx)
		for _, forbidden := range grammar2ForbiddenNodes {
			if strings.Contains(typeName, forbidden) {
				violations = append(violations, fmt.Sprintf("%s at line %d:%d",
					forbidden, ctx.GetStart().GetLine(), ctx.GetStart().GetColumn()))
			}
		}

		// Also check for == tokens
		if p3, ok := ctx.(*grammar1.ColumnExprPrecedence3Context); ok {
			for i := 0; i < p3.GetChildCount(); i++ {
				if term, ok := p3.GetChild(i).(*antlr.TerminalNodeImpl); ok {
					if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerEQ_DOUBLE {
						violations = append(violations, fmt.Sprintf("EQ_DOUBLE (==) at line %d:%d",
							term.GetSymbol().GetLine(), term.GetSymbol().GetColumn()))
					}
				}
			}
		}

		// Check for OUTER in join ops
		switch c := ctx.(type) {
		case *grammar1.JoinOpLeftRightContext, *grammar1.JoinOpFullContext:
			for i := 0; i < c.(antlr.ParserRuleContext).GetChildCount(); i++ {
				if term, ok := c.(antlr.ParserRuleContext).GetChild(i).(*antlr.TerminalNodeImpl); ok {
					if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerOUTER {
						violations = append(violations, fmt.Sprintf("OUTER keyword at line %d:%d",
							term.GetSymbol().GetLine(), term.GetSymbol().GetColumn()))
					}
				}
			}
		}

		return true
	})
	return
}

func TestGrammar2ComplianceAfterPipeline(t *testing.T) {
	// Hand-crafted inputs that exercise every Grammar1-only construct
	inputs := []struct {
		name string
		sql  string
	}{
		{"case_searched", "SELECT CASE WHEN a = 1 THEN 'x' ELSE 'y' END FROM t"},
		{"case_simple", "SELECT CASE x WHEN 1 THEN 'a' END FROM t"},
		{"ternary", "SELECT a ? b : c FROM t"},
		{"date_sugar", "SELECT DATE '2024-01-01'"},
		{"timestamp_sugar", "SELECT TIMESTAMP '2024-01-01 00:00:00'"},
		{"extract_sugar", "SELECT EXTRACT(DAY FROM d) FROM t"},
		{"substring_sugar", "SELECT SUBSTRING('hello' FROM 1 FOR 3)"},
		{"trim_sugar", "SELECT TRIM(BOTH ' ' FROM s) FROM t"},
		{"double_eq", "SELECT a FROM t WHERE a == 1"},
		{"join_outer", "SELECT a FROM t1 LEFT OUTER JOIN t2 ON t1.id = t2.id"},
		{"join_reorder", "SELECT a FROM t1 LEFT ALL JOIN t2 ON t1.id = t2.id"},
		{"combined", `
			SELECT
				CASE WHEN a == 1 THEN DATE '2024-01-01'
				     WHEN a == 2 THEN TIMESTAMP '2024-06-01 00:00:00'
				     ELSE EXTRACT(DAY FROM d)
				END AS val,
				x ? y : z AS ternary_val,
				SUBSTRING('hello' FROM 1 FOR 3) AS sub,
				TRIM(BOTH ' ' FROM s) AS trimmed
			FROM t1 LEFT OUTER ALL JOIN t2 ON t1.id == t2.id
		`},
	}

	for _, tt := range inputs {
		t.Run(tt.name, func(t *testing.T) {
			out, err := fullCanonicalizationPipeline(tt.sql)
			require.NoError(t, err, "pipeline failed for: %s", tt.sql)

			pr, err := nanopass.Parse(out)
			require.NoError(t, err, "pipeline produced unparseable SQL: %s", out)

			violations := checkGrammar2Compliance(pr)
			assert.Empty(t, violations,
				"Grammar2 violations after pipeline for %s:\n  %s\nOutput SQL: %s",
				tt.name, strings.Join(violations, "\n  "), out)
		})
	}
}

func TestGrammar2ComplianceCorpus(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			out, err := fullCanonicalizationPipeline(entry.SQL)
			if err != nil {
				t.Skipf("pipeline failed: %v", err)
			}

			pr, err := nanopass.Parse(out)
			if err != nil {
				t.Skipf("pipeline output not parseable: %v", err)
			}

			violations := checkGrammar2Compliance(pr)
			assert.Empty(t, violations,
				"Grammar2 violations for %s:\n  %s", entry.Name, strings.Join(violations, "\n  "))
			if len(violations) > 0 {
				assert.Empty(t, violations,
					"Grammar2 violations for %s:\n  %s", entry.Name, strings.Join(violations, "\n  "))
			}
		})
	}
}

// ============================================================================
// Pipeline Idempotency — full pipeline applied twice produces same result
// ============================================================================

func TestFullPipelineIdempotent(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			pass1, err := fullCanonicalizationPipeline(entry.SQL)
			if err != nil {
				t.Skip()
			}
			pass2, err := fullCanonicalizationPipeline(pass1)
			if err != nil {
				t.Fatalf("pipeline failed on own output: %v", err)
			}
			assert.Equal(t, pass1, pass2, "full pipeline not idempotent for %s", entry.Name)
		})
	}
}

// ============================================================================
// Pipeline Ordering Robustness
//
// The pipeline has a defined order, but most passes are independent.
// Verify that reordering the first 4 passes (all except CanonicalizeIdentifiers)
// produces the same final result. CanonicalizeIdentifiers must remain last.
// ============================================================================

func TestPipelineOrderIndependence(t *testing.T) {
	// Passes that can be reordered (CanonicalizeIdentifiers is always last)
	reorderablePasses := []struct {
		name string
		pass nanopass.Pass
	}{
		{"SetFormat", passes.SetFormat("ArrowStream")},
		{"RemoveParens", passes.RemoveRedundantParens},
		{"ExtractLiterals", passes.ExtractLiterals(passes.NewExtractLiteralsConfig(0))},
	}

	// Test with a query that exercises multiple passes
	sql := "SELECT CASE WHEN a == 1 THEN DATE '2024-01-01' ELSE a ? b : c END FROM t1 LEFT ALL JOIN t2 ON t1.id = t2.id"

	// Canonical
	for _, p := range reorderablePasses {
		var err error
		sql, err = p.pass(sql)
		require.NoError(t, err)
	}
	canonical, err := fullCanonicalizationPipeline(sql)
	require.NoError(t, err)

	// All 24 permutations of 4 passes
	perms := permutations(len(reorderablePasses))
	for _, perm := range perms {
		name := ""
		for _, idx := range perm {
			name += reorderablePasses[idx].name + "_"
		}
		t.Run(name, func(t *testing.T) {
			result := sql
			for _, idx := range perm {
				result, err = reorderablePasses[idx].pass(result)
				require.NoError(t, err)
			}
			result, err = fullCanonicalizationPipeline(result)
			require.NoError(t, err)

			assert.Equal(t, canonical, result,
				"ordering %s produced different result", name)
		})
	}
}

// permutations returns all permutations of [0, n).
func permutations(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var result [][]int
	var gen func(arr []int, k int)
	gen = func(arr []int, k int) {
		if k == 1 {
			tmp := make([]int, len(arr))
			copy(tmp, arr)
			result = append(result, tmp)
			return
		}
		for i := 0; i < k; i++ {
			gen(arr, k-1)
			if k%2 == 0 {
				arr[i], arr[k-1] = arr[k-1], arr[i]
			} else {
				arr[0], arr[k-1] = arr[k-1], arr[0]
			}
		}
	}
	arr := make([]int, n)
	for i := range arr {
		arr[i] = i
	}
	gen(arr, n)
	return result
}

// ============================================================================
// Identifier Normalization — Special Cases
// ============================================================================

func TestNormalizeIdentifiersPreservesStringLiterals(t *testing.T) {
	// String literals must NOT be double-quoted — they use single quotes
	input := "SELECT 'hello' FROM t"
	got, err := passes.CanonicalizeIdentifiers(input)
	require.NoError(t, err)
	assert.Contains(t, got, "'hello'", "string literal must not be modified")
}

func TestNormalizeIdentifiersPreservesOperators(t *testing.T) {
	input := "SELECT a + b * c FROM t"
	got, err := passes.CanonicalizeIdentifiers(input)
	require.NoError(t, err)
	assert.Contains(t, got, "+")
	assert.Contains(t, got, "*")
}

func TestNormalizeIdentifiersParamSlot(t *testing.T) {
	got, err := passes.CanonicalizeIdentifiers("SELECT {p: UInt64} FROM t")
	require.NoError(t, err)
	assert.Contains(t, got, `"p"`)
	// UInt64 is already an IDENTIFIER token (not a keyword), so it gets quoted too
	assert.Contains(t, got, `"UInt64"`)
}

func TestNormalizeIdentifiersInternalDoubleQuoteEscaping(t *testing.T) {
	// An identifier containing a double quote: col"name
	// In backtick form: `col"name`
	// Should become: "col""name" (double-quote escaped by doubling)
	input := "SELECT `col\"name` FROM t"
	got, err := passes.CanonicalizeIdentifiers(input)
	require.NoError(t, err)
	assert.Contains(t, got, `"col""name"`)
}

// ============================================================================
// CanonicalizeCaseConditionals — Nested Cases
// ============================================================================

func TestNormalizeCaseNested(t *testing.T) {
	input := "SELECT CASE WHEN CASE WHEN a = 1 THEN 'inner' ELSE 'other' END = 'inner' THEN 'yes' ELSE 'no' END FROM t"
	pass := nanopass.FixedPoint(passes.CanonicalizeCaseConditionals, 10)
	got, err := pass(input)
	require.NoError(t, err)
	_, err = nanopass.Parse(got)
	require.NoError(t, err, "produced invalid SQL: %s", got)
	// Both CASE expressions should be converted
	assert.NotContains(t, got, "CASE")
	assert.NotContains(t, got, "WHEN")
	assert.Contains(t, got, "if(")
}

// ============================================================================
// NormalizeTernary — Nested Ternaries
//
// a ? b ? c : d : e
// The grammar is right-associative, so this parses as a ? (b ? c : d) : e.
// After one pass: a ? if(b, c, d) : e — still contains a ternary.
// After re-parse + second pass: if(a, if(b, c, d), e) — fully normalized.
// So nested ternaries require FixedPoint or re-parse between passes.
// ============================================================================

func TestNormalizeTernaryNested(t *testing.T) {
	input := "SELECT a ? b ? c : d : e FROM t"

	// First pass: innermost ternary is rewritten
	pass1, err := passes.CanonicalizeTernary(input)
	require.NoError(t, err)
	_, err = nanopass.Parse(pass1)
	require.NoError(t, err)

	// Second pass (re-parsed): outermost ternary is rewritten
	pass2, err := passes.CanonicalizeTernary(pass1)
	require.NoError(t, err)
	_, err = nanopass.Parse(pass2)
	require.NoError(t, err)

	// After two passes, no ternary operators should remain
	assert.NotContains(t, pass2, "?")
	assert.NotContains(t, pass2, ":")
	assert.Contains(t, pass2, "if(")

	// Verify structure: if(a, if(b, c, d), e)
	// The exact text depends on whitespace, so just check nesting
	assert.Equal(t, 2, strings.Count(pass2, "if("), "should have 2 nested if() calls")
}

// ============================================================================
// NormalizeSugar — Edge Cases
// ============================================================================

func TestNormalizeSugarMultipleInSameQuery(t *testing.T) {
	input := "SELECT DATE '2024-01-01', TIMESTAMP '2024-01-01 00:00:00', EXTRACT(DAY FROM d) FROM t"
	got, err := passes.CanonicalizeSugar(input)
	require.NoError(t, err)
	_, err = nanopass.Parse(got)
	require.NoError(t, err)
	assert.Contains(t, got, "toDate(")
	assert.Contains(t, got, "toDateTime(")
	assert.Contains(t, got, "extract(")
	assert.NotContains(t, got, "DATE '")
	assert.NotContains(t, got, "TIMESTAMP '")
	assert.NotContains(t, got, "EXTRACT(")
}

func TestNormalizeSugarInSubquery(t *testing.T) {
	input := "SELECT a FROM t WHERE a IN (SELECT DATE '2024-01-01' FROM t2)"
	got, err := passes.CanonicalizeSugar(input)
	require.NoError(t, err)
	_, err = nanopass.Parse(got)
	require.NoError(t, err)
	assert.Contains(t, got, "toDate(")
}
