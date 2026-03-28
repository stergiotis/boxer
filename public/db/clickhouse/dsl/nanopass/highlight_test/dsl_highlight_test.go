package highlight_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/highlight"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper ---

func findSpans(spans []highlight.Span, cat highlight.CategoryE) []highlight.Span {
	var result []highlight.Span
	for _, s := range spans {
		if s.Category == cat {
			result = append(result, s)
		}
	}
	return result
}

func spanTexts(spans []highlight.Span, cat highlight.CategoryE) []string {
	var result []string
	for _, s := range spans {
		if s.Category == cat {
			result = append(result, s.Text)
		}
	}
	return result
}

func hasSpan(spans []highlight.Span, text string, cat highlight.CategoryE) bool {
	for _, s := range spans {
		if s.Text == text && s.Category == cat {
			return true
		}
	}
	return false
}

// --- Lexical highlighting ---

func TestHighlightKeywords(t *testing.T) {
	spans := highlight.Highlight("SELECT a FROM t WHERE x > 1")

	assert.True(t, hasSpan(spans, "SELECT", highlight.CatKeyword))
	assert.True(t, hasSpan(spans, "FROM", highlight.CatKeyword))
	assert.True(t, hasSpan(spans, "WHERE", highlight.CatKeyword))
}

func TestHighlightOperators(t *testing.T) {
	spans := highlight.Highlight("SELECT a FROM t WHERE a > 1 AND b IN (1, 2) OR c LIKE '%x%'")

	assert.True(t, hasSpan(spans, "AND", highlight.CatOperator))
	assert.True(t, hasSpan(spans, "OR", highlight.CatOperator))
	assert.True(t, hasSpan(spans, "IN", highlight.CatOperator))
	assert.True(t, hasSpan(spans, "LIKE", highlight.CatOperator))
}

func TestHighlightLiterals(t *testing.T) {
	spans := highlight.Highlight("SELECT 42, 3.14, 'hello', NULL")

	assert.True(t, hasSpan(spans, "42", highlight.CatNumberLit))
	assert.True(t, hasSpan(spans, "'hello'", highlight.CatStringLit))
	assert.True(t, hasSpan(spans, "NULL", highlight.CatKeyword))

	// 3.14 may be a FLOATING_LITERAL or split into tokens — just verify number tokens exist
	numSpans := findSpans(spans, highlight.CatNumberLit)
	assert.GreaterOrEqual(t, len(numSpans), 1)
}

func TestHighlightComments(t *testing.T) {
	spans := highlight.Highlight("SELECT a -- inline comment\nFROM t /* block */")

	assert.True(t, hasSpan(spans, "SELECT", highlight.CatKeyword))

	comments := spanTexts(spans, highlight.CatComment)
	require.GreaterOrEqual(t, len(comments), 2)
}

func TestHighlightPunctuation(t *testing.T) {
	spans := highlight.Highlight("SELECT (a, b) FROM t")

	assert.True(t, hasSpan(spans, "(", highlight.CatPunctuation))
	assert.True(t, hasSpan(spans, ")", highlight.CatPunctuation))
	assert.True(t, hasSpan(spans, ",", highlight.CatPunctuation))
}

func TestHighlightWhitespace(t *testing.T) {
	spans := highlight.Highlight("SELECT  a  FROM  t")

	ws := findSpans(spans, highlight.CatWhitespace)
	assert.Greater(t, len(ws), 0)
}

// --- Semantic: table names ---

func TestHighlightTableName(t *testing.T) {
	spans := highlight.Highlight("SELECT a FROM orders")

	assert.True(t, hasSpan(spans, "orders", highlight.CatTableName))
}

func TestHighlightQualifiedTable(t *testing.T) {
	spans := highlight.Highlight("SELECT a FROM prod.orders")

	assert.True(t, hasSpan(spans, "prod", highlight.CatDatabaseName))
	assert.True(t, hasSpan(spans, "orders", highlight.CatTableName))
}

func TestHighlightJoinTables(t *testing.T) {
	spans := highlight.Highlight("SELECT * FROM orders JOIN customers ON orders.id = customers.id")

	tables := spanTexts(spans, highlight.CatTableName)
	assert.Contains(t, tables, "orders")
	assert.Contains(t, tables, "customers")
}

// --- Semantic: table aliases ---

func TestHighlightTableAlias(t *testing.T) {
	spans := highlight.Highlight("SELECT o.id FROM orders AS o")

	assert.True(t, hasSpan(spans, "orders", highlight.CatTableName))
	assert.True(t, hasSpan(spans, "o", highlight.CatTableAlias))
}

// --- Semantic: column names ---

func TestHighlightColumnName(t *testing.T) {
	spans := highlight.Highlight("SELECT id, name FROM t")

	columns := spanTexts(spans, highlight.CatColumnName)
	assert.Contains(t, columns, "id")
	assert.Contains(t, columns, "name")
}

func TestHighlightQualifiedColumn(t *testing.T) {
	spans := highlight.Highlight("SELECT t.id FROM t")

	// t in t.id should be table, id should be column
	assert.True(t, hasSpan(spans, "id", highlight.CatColumnName))
}

// --- Semantic: column aliases ---

func TestHighlightColumnAlias(t *testing.T) {
	spans := highlight.Highlight("SELECT sum(a) AS total FROM t")

	assert.True(t, hasSpan(spans, "total", highlight.CatColumnAlias))
	assert.True(t, hasSpan(spans, "sum", highlight.CatFunctionName))
}

func TestHighlightImplicitAlias(t *testing.T) {
	spans := highlight.Highlight("SELECT a alias1 FROM t")

	assert.True(t, hasSpan(spans, "alias1", highlight.CatColumnAlias))
}

// --- Semantic: function names ---

func TestHighlightFunctionName(t *testing.T) {
	spans := highlight.Highlight("SELECT count(*), sum(a), avg(b) FROM t")

	fns := spanTexts(spans, highlight.CatFunctionName)
	assert.Contains(t, fns, "count")
	assert.Contains(t, fns, "sum")
	assert.Contains(t, fns, "avg")
}

func TestHighlightNestedFunction(t *testing.T) {
	spans := highlight.Highlight("SELECT toDate(toString(now()))")

	fns := spanTexts(spans, highlight.CatFunctionName)
	assert.Contains(t, fns, "toDate")
	assert.Contains(t, fns, "toString")
	assert.Contains(t, fns, "now")
}

// --- Semantic: CTE names ---
func TestHighlightCTEName(t *testing.T) {
	spans := highlight.Highlight("WITH cte AS (SELECT 1) SELECT * FROM cte")

	ctes := spanTexts(spans, highlight.CatCTEName)
	// Should appear at least twice: definition and reference
	assert.GreaterOrEqual(t, len(ctes), 2, "CTE spans: %v", ctes)
	for _, c := range ctes {
		assert.Equal(t, "cte", c)
	}
}

// --- Semantic: parameter slots ---

func TestHighlightParamSlot(t *testing.T) {
	spans := highlight.Highlight("SELECT {a: UInt32}, {b: String}")

	ps := findSpans(spans, highlight.CatParamSlot)
	assert.Greater(t, len(ps), 0)
}

// --- Complex queries ---

func TestHighlightComplexQuery(t *testing.T) {
	sql := `
WITH
    monthly AS (
        SELECT
            toStartOfMonth(created) AS month,
            count(*) AS cnt
        FROM prod.events
        WHERE tenant_id = 42
        GROUP BY month
    )
SELECT
    month,
    cnt,
    sum(cnt) OVER (ORDER BY month) AS running_total
FROM monthly
ORDER BY month
SETTINGS max_threads = 4
`
	spans := highlight.Highlight(sql)

	// Verify key semantic categories are present
	assert.True(t, hasSpan(spans, "monthly", highlight.CatCTEName), "CTE name 'monthly'")
	assert.True(t, hasSpan(spans, "events", highlight.CatTableName), "table 'events'")
	assert.True(t, hasSpan(spans, "prod", highlight.CatDatabaseName), "database 'prod'")
	assert.True(t, hasSpan(spans, "toStartOfMonth", highlight.CatFunctionName), "function 'toStartOfMonth'")
	assert.True(t, hasSpan(spans, "count", highlight.CatFunctionName), "function 'count'")
	assert.True(t, hasSpan(spans, "sum", highlight.CatFunctionName), "function 'sum'")

	assert.True(t, hasSpan(spans, "running_total", highlight.CatColumnAlias), "alias 'running_total'")
	assert.True(t, hasSpan(spans, "month", highlight.CatColumnAlias) || hasSpan(spans, "month", highlight.CatColumnName), "month appears")

	// Keywords
	assert.True(t, hasSpan(spans, "SELECT", highlight.CatKeyword))
	assert.True(t, hasSpan(spans, "WITH", highlight.CatKeyword))
	assert.True(t, hasSpan(spans, "FROM", highlight.CatKeyword))
	assert.True(t, hasSpan(spans, "GROUP", highlight.CatKeyword))
	assert.True(t, hasSpan(spans, "ORDER", highlight.CatKeyword))
	assert.True(t, hasSpan(spans, "SETTINGS", highlight.CatKeyword))

	// Literals
	assert.True(t, hasSpan(spans, "42", highlight.CatNumberLit))
	assert.True(t, hasSpan(spans, "4", highlight.CatNumberLit))

	// Ensure no gaps
	assertCoverage(t, sql, spans)
}

func TestHighlightUNIONALL(t *testing.T) {
	sql := "SELECT a FROM t1 UNION ALL SELECT b FROM t2"
	spans := highlight.Highlight(sql)

	tables := spanTexts(spans, highlight.CatTableName)
	assert.Contains(t, tables, "t1")
	assert.Contains(t, tables, "t2")
}

func TestHighlightSubquery(t *testing.T) {
	sql := "SELECT * FROM (SELECT id, name FROM users) AS sub"
	spans := highlight.Highlight(sql)

	assert.True(t, hasSpan(spans, "users", highlight.CatTableName))
	assert.True(t, hasSpan(spans, "sub", highlight.CatTableAlias))
}

// --- Fallback: invalid SQL ---

func TestHighlightInvalidSQL(t *testing.T) {
	// Should produce lexical-only highlighting, no panic
	spans := highlight.Highlight("SELECT FROM WHERE")
	require.Greater(t, len(spans), 0)

	assert.True(t, hasSpan(spans, "SELECT", highlight.CatKeyword))
	assert.True(t, hasSpan(spans, "FROM", highlight.CatKeyword))
	assert.True(t, hasSpan(spans, "WHERE", highlight.CatKeyword))
}

func TestHighlightEmpty(t *testing.T) {
	spans := highlight.Highlight("")
	assert.Empty(t, spans)
}

func TestHighlightWhitespaceOnly(t *testing.T) {
	spans := highlight.Highlight("   \t\n  ")
	for _, s := range spans {
		assert.Equal(t, highlight.CatWhitespace, s.Category)
	}
}

// --- Coverage: every byte is accounted for ---

func assertCoverage(t *testing.T, sql string, spans []highlight.Span) {
	t.Helper()

	// Reconstruct the full text from spans
	var sb strings.Builder
	for _, s := range spans {
		sb.WriteString(s.Text)
	}
	reconstructed := sb.String()

	assert.Equal(t, sql, reconstructed,
		"spans do not cover the full input — gap or overlap detected")
}

func TestHighlightCoverage(t *testing.T) {
	sqls := []string{
		"SELECT 1",
		"SELECT a, b, c FROM t",
		"SELECT a FROM t WHERE x > 1 AND y < 2",
		"SELECT count(*) FROM t GROUP BY a HAVING count(*) > 1",
		"SELECT a FROM t1 JOIN t2 ON t1.id = t2.id",
		"WITH cte AS (SELECT 1) SELECT * FROM cte",
		"SELECT a FROM db.t SETTINGS max_threads = 4",
		"SELECT sum(a) AS total, count(*) AS cnt FROM t",
		"SELECT * FROM (SELECT 1 AS x) AS sub",
		"-- comment\nSELECT 1 /* block */",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("coverage_%d", i), func(t *testing.T) {
			spans := highlight.Highlight(sql)
			assertCoverage(t, sql, spans)
		})
	}
}

// --- ANSI rendering ---

func TestRenderANSI(t *testing.T) {
	spans := highlight.Highlight("SELECT count(*) FROM orders")
	ansi := highlight.RenderANSI(spans)

	// Should contain ANSI escape codes
	assert.Contains(t, ansi, "\033[")
	// Should contain reset codes
	assert.Contains(t, ansi, "\033[0m")
	// Should contain the original text
	assert.Contains(t, ansi, "SELECT")
	assert.Contains(t, ansi, "count")
	assert.Contains(t, ansi, "orders")

	t.Logf("ANSI output:\n%s", ansi)
}

func TestRenderANSIPreservesText(t *testing.T) {
	sql := "SELECT a, b FROM t WHERE x > 1"
	spans := highlight.Highlight(sql)
	ansi := highlight.RenderANSI(spans)

	// Strip ANSI codes and verify text is preserved
	stripped := stripANSI(ansi)
	assert.Equal(t, sql, stripped)
}

func stripANSI(s string) string {
	var sb strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == '\033' {
			// Skip until 'm'
			for i < len(s) && s[i] != 'm' {
				i++
			}
			i++ // skip 'm'
		} else {
			sb.WriteByte(s[i])
			i++
		}
	}
	return sb.String()
}

// --- HTML rendering ---

func TestRenderHTML(t *testing.T) {
	spans := highlight.Highlight("SELECT count(*) FROM orders")
	html := highlight.RenderHTML(spans)

	assert.True(t, strings.HasPrefix(html, `<code class="hl-sql">`))
	assert.True(t, strings.HasSuffix(html, `</code>`))

	// Should contain CSS class spans
	assert.Contains(t, html, `class="hl-kw"`)  // keyword
	assert.Contains(t, html, `class="hl-fn"`)  // function
	assert.Contains(t, html, `class="hl-tbl"`) // table

	// Should contain the text
	assert.Contains(t, html, "SELECT")
	assert.Contains(t, html, "count")
	assert.Contains(t, html, "orders")

	t.Logf("HTML output:\n%s", html)
}

func TestRenderHTMLEscaping(t *testing.T) {
	spans := highlight.Highlight("SELECT a FROM t WHERE a > 1 AND b < 2")
	html := highlight.RenderHTML(spans)

	// < and > should be escaped
	assert.Contains(t, html, "&gt;")
	assert.Contains(t, html, "&lt;")
	assert.NotContains(t, html, "> 1") // raw > should not appear in HTML context
}

func TestRenderHTMLPreservesText(t *testing.T) {
	sql := "SELECT a, b FROM t WHERE x > 1"
	spans := highlight.Highlight(sql)
	html := highlight.RenderHTML(spans)

	// Strip HTML tags and unescape entities, should get original text
	stripped := stripHTML(html)
	assert.Equal(t, sql, stripped)
}

func stripHTML(s string) string {
	// Remove tags
	var sb strings.Builder
	inTag := false
	for _, c := range s {
		if c == '<' {
			inTag = true
		} else if c == '>' {
			inTag = false
		} else if !inTag {
			sb.WriteRune(c)
		}
	}
	result := sb.String()
	// Unescape entities
	result = strings.ReplaceAll(result, "&amp;", "&")
	result = strings.ReplaceAll(result, "&lt;", "<")
	result = strings.ReplaceAll(result, "&gt;", ">")
	result = strings.ReplaceAll(result, "&quot;", "\"")
	return result
}

// --- CSS ---

func TestHighlightCSS(t *testing.T) {
	css := highlight.HighlightCSS()

	assert.Contains(t, css, ".hl-sql")
	assert.Contains(t, css, ".hl-kw")
	assert.Contains(t, css, ".hl-fn")
	assert.Contains(t, css, ".hl-tbl")
	assert.Contains(t, css, ".hl-col")
	assert.Contains(t, css, ".hl-str")
	assert.Contains(t, css, ".hl-num")
	assert.Contains(t, css, ".hl-cmt")
	assert.Contains(t, css, "prefers-color-scheme: dark")
}

// --- CategoryName ---

func TestCategoryName(t *testing.T) {
	assert.Equal(t, "keyword", highlight.CategoryName(highlight.CatKeyword))
	assert.Equal(t, "table", highlight.CategoryName(highlight.CatTableName))
	assert.Equal(t, "column", highlight.CategoryName(highlight.CatColumnName))
	assert.Equal(t, "function", highlight.CategoryName(highlight.CatFunctionName))
	assert.Equal(t, "string", highlight.CategoryName(highlight.CatStringLit))
	assert.Equal(t, "comment", highlight.CategoryName(highlight.CatComment))
}

// --- Visual demo (run with -v to see output) ---

func TestHighlightDemo(t *testing.T) {
	sqls := []string{
		"SELECT id, name, sum(amount) AS total FROM prod.orders AS o JOIN customers AS c ON o.customer_id = c.id WHERE o.status = 'active' GROUP BY id, name ORDER BY total DESC LIMIT 10",
		"WITH daily AS (SELECT toDate(ts) AS day, count(*) AS cnt FROM events GROUP BY day) SELECT day, cnt, avg(cnt) OVER (ORDER BY day ROWS BETWEEN 6 PRECEDING AND CURRENT ROW) AS moving_avg FROM daily",
		"SELECT {user_id: UInt64}, arrayJoin([1, 2, 3]) AS x -- parameter example",
	}

	for i, sql := range sqls {
		t.Run(fmt.Sprintf("demo_%d", i), func(t *testing.T) {
			spans := highlight.Highlight(sql)

			t.Logf("=== SQL ===")
			t.Logf("%s", sql)
			t.Logf("")
			t.Logf("=== Spans ===")
			for _, s := range spans {
				if s.Category != highlight.CatWhitespace && s.Category != highlight.CatPunctuation {
					t.Logf("  %-15s %q", highlight.CategoryName(s.Category), s.Text)
				}
			}
			t.Logf("")
			t.Logf("=== ANSI ===")
			t.Logf("%s", highlight.RenderANSI(spans))
			t.Logf("")
			t.Logf("=== HTML ===")
			t.Logf("%s", highlight.RenderHTML(spans))

			assertCoverage(t, sql, spans)
		})
	}
}
func TestDebugCTERef(t *testing.T) {
	t.Skip("diagnostic only")
	sql := "WITH cte AS (SELECT 1) SELECT * FROM cte"
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if tid, ok := ctx.(*grammar.TableIdentifierContext); ok {
			parent := tid.GetParent()
			t.Logf("TableIdentifierContext text=%q parent=%T", tid.GetText(), parent)
			if parent != nil {
				if gp := parent.(antlr.RuleNode); gp != nil {
					t.Logf("  grandparent=%T", gp.GetParent())
				}
			}
		}
		return true
	})
}
