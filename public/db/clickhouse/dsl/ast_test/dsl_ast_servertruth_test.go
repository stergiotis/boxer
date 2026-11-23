package ast_test

// Server-truth harness: validates the DSL's notion of ClickHouse SQL
// against a real ClickHouse binary instead of against beliefs.
//
//   - Acceptance: `clickhouse format` is the server's parser without table
//     resolution — every canonicalised corpus entry and every ToSQL output
//     must be accepted by it.
//   - Semantics: `clickhouse local` evaluates table-free expressions — the
//     pipeline's judgment calls (octal literals, comma-LIMIT, quoted slot
//     names, escape handling) are asserted against server output, and
//     selected expressions are checked for end-to-end equivalence: the
//     original query and the canonicalise→AST→ToSQL output must evaluate
//     to identical results.
//
// The tests skip when no `clickhouse` binary is on PATH.

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireClickhouse(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("clickhouse"); err != nil {
		t.Skip("clickhouse binary not on PATH — server-truth tests skipped")
	}
}

// chFormatAccepts runs sql through `clickhouse format -n` (the server's
// parser; no table resolution) and returns the parse error, if any.
func chFormatAccepts(sql string) error {
	cmd := exec.Command("clickhouse", "format", "-n")
	cmd.Stdin = strings.NewReader(sql)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &serverParseError{sql: sql, output: string(out)}
	}
	return nil
}

type serverParseError struct {
	sql    string
	output string
}

func (e *serverParseError) Error() string {
	return "clickhouse format rejected:\n  sql: " + e.sql + "\n  " + firstLine(e.output)
}

func firstLine(s string) string {
	if before, _, ok := strings.Cut(s, "\n"); ok {
		return before
	}
	return s
}

// chLocal evaluates a query with `clickhouse local` and returns trimmed
// TSV output. extraArgs precede --query (e.g. --param_x=5).
func chLocal(t *testing.T, query string, extraArgs ...string) (string, error) {
	t.Helper()
	args := append(append([]string{"local"}, extraArgs...), "--query", query)
	out, err := exec.Command("clickhouse", args...).CombinedOutput()
	if err != nil {
		return "", &serverParseError{sql: query, output: string(out)}
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// TestServerTruthCorpusAcceptance: the canonical form of every corpus
// entry, and the ToSQL rendering of its AST, must be accepted by the
// server's parser.
func TestServerTruthCorpusAcceptance(t *testing.T) {
	requireClickhouse(t)
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			canonical, err := fullPipeline(entry.SQL)
			if err != nil {
				t.Skipf("pipeline: %v", err)
			}
			assert.NoError(t, chFormatAccepts(canonical), "canonical form rejected by server parser")

			pr, err := nanopass.ParseCanonical(canonical)
			require.NoError(t, err)
			query, err := ast.ConvertCSTToAST(pr)
			require.NoError(t, err)
			assert.NoError(t, chFormatAccepts(query.ToSQL()), "ToSQL output rejected by server parser")
		})
	}
}

// TestServerTruthJudgments pins the pipeline's semantic judgment calls to
// server behaviour (ClickHouse 26.x verified):
func TestServerTruthJudgments(t *testing.T) {
	requireClickhouse(t)

	t.Run("octal_is_decimal", func(t *testing.T) {
		// UnmarshalScalarLiteral treats 0777 as decimal — so does the server.
		out, err := chLocal(t, "SELECT 0777")
		require.NoError(t, err)
		assert.Equal(t, "777", out)
	})

	t.Run("comma_limit_offset_first", func(t *testing.T) {
		// LIMIT m, n ≡ LIMIT n OFFSET m — the AST converter relies on this.
		out, err := chLocal(t, "SELECT number FROM system.numbers LIMIT 5, 3")
		require.NoError(t, err)
		assert.Equal(t, "5\n6\n7", out)
	})

	t.Run("quoted_not_is_a_function", func(t *testing.T) {
		// Grammar1 parses NOT(x) as a function call; the canonical form
		// "NOT"(x) resolves on the server.
		out, err := chLocal(t, `SELECT "NOT"(0)`)
		require.NoError(t, err)
		assert.Equal(t, "1", out)
	})

	t.Run("param_slot_names_must_be_bare", func(t *testing.T) {
		// CanonicalizeIdentifiers must not quote slot names: the server
		// rejects quoted spellings ("Expected substitution name").
		out, err := chLocal(t, "SELECT {x: UInt64}", "--param_x=5")
		require.NoError(t, err)
		assert.Equal(t, "5", out)

		_, err = chLocal(t, `SELECT {"x": "UInt64"}`, "--param_x=5")
		assert.Error(t, err, "quoted slot name must be rejected by the server")
	})

	t.Run("escape_string_bytes", func(t *testing.T) {
		// EscapeString output must denote exactly the original bytes.
		val := "it's\na\tb\\x"
		lit := marshalling.EscapeString(val)
		out, err := chLocal(t, "SELECT hex("+lit+")")
		require.NoError(t, err)
		expected := strings.ToUpper(hexOf(val))
		assert.Equal(t, expected, out)
	})
}

func hexOf(s string) string {
	const digits = "0123456789abcdef"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteByte(digits[s[i]>>4])
		b.WriteByte(digits[s[i]&0xf])
	}
	return b.String()
}

// TestServerTruthSemanticEquivalence: for table-free expressions, the
// original query and its canonicalise→AST→ToSQL rendering must evaluate to
// identical results on the server — end-to-end semantics, not just
// acceptance. The shapes mirror the precedence and conversion defects the
// reviews found.
func TestServerTruthSemanticEquivalence(t *testing.T) {
	requireClickhouse(t)

	queries := []string{
		"SELECT 10 - (4 - 3)",
		"SELECT 100 / (10 / 2)",
		"SELECT -(1 + 2)",
		"SELECT 1 - (-5)",
		"SELECT NOT (1 OR 0) AND 1",
		"SELECT (5 BETWEEN 1 AND 3) OR 1",
		"SELECT 5 BETWEEN (0 AND 1) AND 9",
		"SELECT (1 = 2) IS NULL",
		"SELECT 0777 + 1",
		"SELECT CASE WHEN 1 = 1 THEN 'a' ELSE 'b' END",
		"SELECT CASE 2 WHEN 1 THEN 'one' WHEN 2 THEN 'two' END",
		"SELECT (1, 2) IN ((1, 2), (3, 4))",
		"SELECT [1, 2, 3][2]",
		"SELECT CAST(42 AS String), 7::Float64",
		"SELECT SUBSTRING('abcdef' FROM 2 FOR 3)",
		"SELECT EXTRACT(DAY FROM DATE '2024-01-15')",
		"SELECT 1 = 1 ? 'yes' : 'no'",
		"SELECT number FROM system.numbers LIMIT 5, 3",
		"SELECT number FROM system.numbers WHERE number > 1 LIMIT 2 OFFSET 1",
		"SELECT 'it\\'s', length('déjà')",
	}

	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			want, err := chLocal(t, sql)
			require.NoError(t, err, "original rejected by server — fix the probe, not the pipeline")

			canonical, err := fullPipeline(sql)
			require.NoError(t, err)
			pr, err := nanopass.ParseCanonical(canonical)
			require.NoError(t, err)
			query, err := ast.ConvertCSTToAST(pr)
			require.NoError(t, err)
			rendered := query.ToSQL()

			got, err := chLocal(t, rendered)
			require.NoError(t, err, "ToSQL output rejected by server:\n  rendered: %s", rendered)
			assert.Equal(t, want, got,
				"server evaluates original and ToSQL output differently:\n  original: %s\n  rendered: %s", sql, rendered)
		})
	}
}

// chFormat returns `clickhouse format -n` of sql: the server's parse of it,
// printed.
func chFormat(sql string) (formatted string, err error) {
	cmd := exec.Command("clickhouse", "format", "-n")
	cmd.Stdin = strings.NewReader(sql)
	out, err := cmd.CombinedOutput()
	if err != nil {
		err = &serverParseError{sql: sql, output: string(out)}
		return
	}
	formatted = string(out)
	return
}

// TestServerTruthAggregateFilter: the server's parser reads an aggregate
// filter as the If call CanonicalizeSugar writes for it — both format to the
// same text — and the original, its canonical form and the canonical AST's
// rendering evaluate alike.
func TestServerTruthAggregateFilter(t *testing.T) {
	requireClickhouse(t)

	queries := []string{
		"SELECT quantile(0.05)(number) FILTER (WHERE number BETWEEN 3 AND 15) AS p05 FROM numbers(20)",
		"SELECT quantiles(0.05, 0.95)(number) FILTER (WHERE number BETWEEN 3 AND 15), quantileExactWeighted(0.5)(number, number + 1) FILTER (WHERE number > 2), topK(3, 2, 'counts')(number % 4) FILTER (WHERE number > 1) FROM numbers(20)",
		"SELECT sum(number) FILTER (WHERE number > 5), avg(number) filter (where number % 2 = 0) FROM numbers(20)",
		"SELECT count() FILTER (WHERE number > 4), count(*) FILTER (WHERE number > 4), count(number, *) FILTER (WHERE number > 4) FROM numbers(20)",
		"SELECT count(DISTINCT number % 3) FILTER (WHERE number % 4), uniq(DISTINCT number % 3, number % 5) FILTER (WHERE number > 2) FROM numbers(20)",
		"SELECT quantile(0.5)(DISTINCT number % 7) FILTER (WHERE number > 2) FROM numbers(20)",
		"SELECT sumArray([number, 1]) FILTER (WHERE number > 5), sumOrNull(number) FILTER (WHERE number > 100), finalizeAggregation(sumState(number) FILTER (WHERE number > 5)) FROM numbers(20)",
		"SELECT argMax(number, number % 7) FILTER (WHERE number < 15), topK(2)(number % 4) FILTER (WHERE number > 1) FROM numbers(20)",
		"SELECT number, sum(number) FILTER (WHERE number > 5) OVER (ORDER BY number) AS rs FROM numbers(20) ORDER BY number",
		"SELECT number, count(*) FILTER (WHERE number % 2 = 1) OVER w AS c FROM numbers(20) WINDOW w AS (ORDER BY number) ORDER BY number",
		"SELECT sum(sum(number) FILTER (WHERE number > 5)) OVER () FROM numbers(20)",
		"SELECT sum(number) FILTER (WHERE number IN (SELECT number * 2 FROM numbers(10))) FROM numbers(20)",
		"SELECT number % 3 AS k, sum(number) FILTER (WHERE number > 5) AS s FROM numbers(20) GROUP BY k HAVING sum(number) FILTER (WHERE number > 10) > 0 ORDER BY k",
		"SELECT `sum`(number) FILTER (WHERE number > 5 AS c), any(number) FILTER (WHERE number > 7) FROM numbers(20)",
	}

	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			sugar, err := nanopass.FixedPoint(passes.CanonicalizeSugar, 16).Run(sql)
			require.NoError(t, err)
			wantFormat, err := chFormat(sql)
			require.NoError(t, err)
			gotFormat, err := chFormat(sugar)
			require.NoError(t, err)
			assert.Equal(t, wantFormat, gotFormat, "the server parses the filter and its If call differently:\n  sugar: %s", sugar)

			want, err := chLocal(t, sql)
			require.NoError(t, err)
			canonical, err := fullPipeline(sql)
			require.NoError(t, err)
			got, err := chLocal(t, canonical)
			require.NoError(t, err)
			assert.Equal(t, want, got, "canonical: %s", canonical)

			pr, err := nanopass.ParseCanonical(canonical)
			require.NoError(t, err)
			query, err := ast.ConvertCSTToAST(pr)
			require.NoError(t, err)
			rendered := query.ToSQL()
			got, err = chLocal(t, rendered)
			require.NoError(t, err)
			assert.Equal(t, want, got, "rendered: %s", rendered)
		})
	}
}

// TestServerTruthExists: the server's parser reads EXISTS, in either
// spelling, as the call CanonicalizeSugar writes for it — both format to the
// same text — and the original, its canonical form and the canonical AST's
// rendering evaluate alike.
func TestServerTruthExists(t *testing.T) {
	requireClickhouse(t)

	queries := []string{
		"SELECT exists(SELECT 1 FROM numbers(3) WHERE number > 5) AS a, EXISTS (SELECT 1 FROM numbers(3)) AS b",
		"SELECT number FROM numbers(6) AS o WHERE exists(SELECT 1 FROM numbers(3) AS i WHERE i.number = o.number) ORDER BY number",
		"SELECT number FROM numbers(6) AS o WHERE NOT EXISTS (SELECT 1 FROM numbers(3) AS i WHERE i.number = o.number) ORDER BY number",
		"SELECT exists((SELECT 1 WHERE 0)), EXISTS ((SELECT 1)), exists((SELECT 1 WHERE 0) UNION ALL (SELECT 2))",
		"SELECT exists(SELECT 1 WHERE 0 UNION ALL SELECT 2 WHERE 0), exists(WITH 3 AS x SELECT x WHERE x > 2)",
		"SELECT exists(SELECT * FROM numbers(0)), exists(SELECT 1, 2 FROM numbers(0)), exists(SELECT number FROM numbers(1))",
		"SELECT Exists (SELECT 1 WHERE EXISTS (SELECT 2 WHERE 0)), EXISTS/* c */(SELECT 1) + 1",
		"SELECT number, EXISTS (SELECT 1 FROM numbers(3) AS i WHERE i.number = o.number) AS e FROM numbers(5) AS o ORDER BY number",
		"SELECT count() FILTER (WHERE EXISTS (SELECT 1 FROM numbers(3) WHERE number = 2)), count() FILTER (WHERE exists(SELECT 1 WHERE 0)) FROM numbers(10)",
		"SELECT number % 2 AS k FROM numbers(10) GROUP BY k HAVING exists(SELECT 1 FROM numbers(1)) ORDER BY k",
	}

	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			sugar, err := nanopass.FixedPoint(passes.CanonicalizeSugar, 16).Run(sql)
			require.NoError(t, err)
			require.NotEqual(t, sql, sugar)
			wantFormat, err := chFormat(sql)
			require.NoError(t, err)
			gotFormat, err := chFormat(sugar)
			require.NoError(t, err)
			assert.Equal(t, wantFormat, gotFormat, "the server parses EXISTS and its call differently:\n  sugar: %s", sugar)

			want, err := chLocal(t, sql)
			require.NoError(t, err)
			canonical, err := fullPipeline(sql)
			require.NoError(t, err)
			got, err := chLocal(t, canonical)
			require.NoError(t, err)
			assert.Equal(t, want, got, "canonical: %s", canonical)

			pr, err := nanopass.ParseCanonical(canonical)
			require.NoError(t, err)
			query, err := ast.ConvertCSTToAST(pr)
			require.NoError(t, err)
			rendered := query.ToSQL()
			got, err = chLocal(t, rendered)
			require.NoError(t, err)
			assert.Equal(t, want, got, "rendered: %s", rendered)
		})
	}
}

// TestServerTruthMaterializedCTE: the server prints a definition marked
// MATERIALIZED with its marker, as the canonical form and the canonical AST's
// rendering keep it, and the three evaluate alike, with the setting that
// makes the server materialize off (its default) and on.
func TestServerTruthMaterializedCTE(t *testing.T) {
	requireClickhouse(t)

	queries := []string{
		"WITH a AS MATERIALIZED (SELECT number AS k FROM numbers(10)), b AS MATERIALIZED (SELECT k, k * 2 AS d FROM a) SELECT count() AS n, sum(d) AS s FROM b",
		"WITH a AS materialized (SELECT number AS k FROM numbers(4)) SELECT count() FROM a AS x CROSS JOIN a AS y WHERE x.k < y.k",
		"WITH a(v) AS MATERIALIZED (SELECT number FROM numbers(5)), 3 AS materialized SELECT sum(v) + materialized FROM a",
		"SELECT * FROM (WITH a AS MATERIALIZED (SELECT 1 AS k UNION ALL SELECT 2) SELECT sum(k) FROM a)",
		"WITH a AS MATERIALIZED (SELECT number AS k FROM numbers(6)), b AS (SELECT k FROM a WHERE k % 2 = 0) SELECT (SELECT count() FROM a), (SELECT count() FROM b), exists(SELECT 1 FROM a WHERE k > 4)",
	}

	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			formatted, err := chFormat(sql)
			require.NoError(t, err)
			assert.Contains(t, formatted, "AS MATERIALIZED")
			canonical, err := fullPipeline(sql)
			require.NoError(t, err)
			assert.Contains(t, canonical, "AS MATERIALIZED")
			pr, err := nanopass.ParseCanonical(canonical)
			require.NoError(t, err)
			query, err := ast.ConvertCSTToAST(pr)
			require.NoError(t, err)
			rendered := query.ToSQL()
			assert.Contains(t, rendered, "AS MATERIALIZED")
			renderedFormat, err := chFormat(rendered)
			require.NoError(t, err)
			assert.Equal(t, formatted, renderedFormat, "the server parses the original and the AST's rendering differently:\n  rendered: %s", rendered)

			for _, setting := range []string{"0", "1"} {
				want, err := chLocal(t, sql, "--enable_materialized_cte", setting)
				require.NoError(t, err)
				got, err := chLocal(t, canonical, "--enable_materialized_cte", setting)
				require.NoError(t, err)
				assert.Equal(t, want, got, "canonical: %s", canonical)
				got, err = chLocal(t, rendered, "--enable_materialized_cte", setting)
				require.NoError(t, err)
				assert.Equal(t, want, got, "rendered: %s", rendered)
			}
		})
	}
}

// TestServerTruthArrayJoins: the server prints several ARRAY JOIN clauses as
// the clauses they are, the canonical form and the canonical AST's rendering
// keep them apart in their order, and the three evaluate alike. The row
// counts show why: two clauses multiply, one clause walks in step.
func TestServerTruthArrayJoins(t *testing.T) {
	requireClickhouse(t)

	queries := []string{
		"SELECT number, T, tau FROM numbers(2) ARRAY JOIN [1.0, 1.5, 2.0] AS T ARRAY JOIN [0.5, 0.7] AS tau ORDER BY number, T, tau",
		"SELECT count() FROM numbers(2) ARRAY JOIN [1, 2, 3] AS a ARRAY JOIN [4, 5] AS b",
		"SELECT count() FROM numbers(2) ARRAY JOIN [1, 2, 3] AS a, [4, 5, 6] AS b",
		"SELECT number, a, b FROM numbers(3) LEFT ARRAY JOIN range(number) AS a ARRAY JOIN [10, 20] AS b ORDER BY number, a, b",
		"SELECT number, a, b FROM numbers(3) ARRAY JOIN [10, 20] AS b LEFT ARRAY JOIN range(number) AS a ORDER BY number, a, b",
		"SELECT a, b, c FROM numbers(1) INNER ARRAY JOIN [1, 2] AS a ARRAY JOIN arrayMap(i -> i * a, [1, 2]) AS b ARRAY JOIN [b, b + 1] AS c WHERE c > 2 ORDER BY a, b, c",
	}

	for _, sql := range queries {
		t.Run(sql, func(t *testing.T) {
			formatted, err := chFormat(sql)
			require.NoError(t, err)
			canonical, err := fullPipeline(sql)
			require.NoError(t, err)
			pr, err := nanopass.ParseCanonical(canonical)
			require.NoError(t, err)
			query, err := ast.ConvertCSTToAST(pr)
			require.NoError(t, err)
			rendered := query.ToSQL()
			assert.Equal(t, strings.Count(formatted, "ARRAY JOIN"), strings.Count(canonical, "ARRAY JOIN"), "canonical: %s", canonical)
			assert.Equal(t, strings.Count(formatted, "ARRAY JOIN"), strings.Count(rendered, "ARRAY JOIN"), "rendered: %s", rendered)

			want, err := chLocal(t, sql)
			require.NoError(t, err)
			got, err := chLocal(t, canonical)
			require.NoError(t, err)
			assert.Equal(t, want, got, "canonical: %s", canonical)
			got, err = chLocal(t, rendered)
			require.NoError(t, err)
			assert.Equal(t, want, got, "rendered: %s", rendered)
		})
	}
	two, err := chLocal(t, queries[1])
	require.NoError(t, err)
	one, err := chLocal(t, queries[2])
	require.NoError(t, err)
	assert.Equal(t, "12", two, "two clauses over 3 and 2 elements, for 2 rows")
	assert.Equal(t, "6", one, "one clause over two arrays of 3 elements, for 2 rows")
}
