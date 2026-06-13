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
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
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
