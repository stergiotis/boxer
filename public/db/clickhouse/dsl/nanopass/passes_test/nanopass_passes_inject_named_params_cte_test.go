package passes_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammarenv"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInjectNamedParamsAsCTE(t *testing.T) {
	rejectKept := func(p grammarenv.Param) bool { return p.Name != "kept" }
	tests := []struct {
		name     string
		pass     nanopass.Pass
		input    string
		expected string
	}{
		{
			name:     "basic",
			pass:     passes.InjectNamedParamsAsCTE(nil),
			input:    "SET my_set = ['a', 'b'];\nSELECT a FROM t WHERE has({my_set: Array(String)}, a)",
			expected: "WITH ['a', 'b'] AS my_set SELECT a FROM t WHERE has(my_set, a)",
		},
		{
			name:     "cast",
			pass:     passes.InjectNamedParamsAsCastCTE(nil),
			input:    "SET my_set = [];\nSELECT a FROM t WHERE hasAny(tags, {my_set: Array(String)})",
			expected: "WITH CAST([], 'Array(String)') AS my_set SELECT a FROM t WHERE hasAny(tags, my_set)",
		},
		{
			name:     "cast type with quotes",
			pass:     passes.InjectNamedParamsAsCastCTE(nil),
			input:    "SET e = 'a';\nSELECT {e: Enum8('a' = 1)}",
			expected: `WITH CAST('a', 'Enum8(\'a\' = 1)') AS e SELECT e`,
		},
		{
			name:     "value under the param_ prefix",
			pass:     passes.InjectNamedParamsAsCTE(nil),
			input:    "SET param_ids = [1, 2];\nSELECT a FROM t WHERE id IN {ids: Array(UInt64)}",
			expected: "WITH [1, 2] AS ids SELECT a FROM t WHERE id IN ids",
		},
		{
			name:     "prepended to an existing WITH",
			pass:     passes.InjectNamedParamsAsCTE(nil),
			input:    "SET s = [1];\nWITH 2 AS two SELECT has({s: Array(UInt8)}, two)",
			expected: "WITH [1] AS s, 2 AS two SELECT has(s, two)",
		},
		{
			name:     "slot is the first WITH definition",
			pass:     passes.InjectNamedParamsAsCTE(nil),
			input:    "SET s = [1];\nWITH {s: Array(UInt8)} AS arr SELECT arr",
			expected: "WITH [1] AS s, s AS arr SELECT arr",
		},
		{
			name:     "definitions in name order",
			pass:     passes.InjectNamedParamsAsCTE(nil),
			input:    "SET b_set = [2];\nSET a_set = [1];\nSELECT has({b_set: Array(UInt8)}, 1), has({ a_set : Array(UInt8) }, 1)",
			expected: "WITH [1] AS a_set, [2] AS b_set SELECT has(b_set, 1), has(a_set, 1)",
		},
		{
			name: "slot without value, value without slot, refused parameter",
			pass: passes.InjectNamedParamsAsCTE(rejectKept),
			input: "SET param_unused = 5;\nSET kept = 'x';\nSET s = [1];\n" +
				"SELECT {s: Array(UInt8)}, {kept: String}, {free: String}",
			expected: "SET kept = 'x';\nSET param_unused = 5;\n" +
				"WITH [1] AS s SELECT s, {kept: String}, {free: String}",
		},
		{
			name:     "keyword name is quoted",
			pass:     passes.InjectNamedParamsAsCTE(nil),
			input:    "SET date = '2024-01-01';\nSELECT {date: Date}",
			expected: `WITH '2024-01-01' AS "date" SELECT "date"`,
		},
		{
			name: "CTE body, lambda, subquery and both UNION ALL branches",
			pass: passes.InjectNamedParamsAsCTE(nil),
			input: "SET s = [1];\n" +
				"WITH c AS (SELECT x FROM t WHERE has({s: Array(UInt8)}, x)) " +
				"SELECT arrayExists(y -> has({s: Array(UInt8)}, y), arr) FROM c WHERE x IN (SELECT {s: Array(UInt8)}) " +
				"UNION ALL SELECT {s: Array(UInt8)}",
			expected: "WITH [1] AS s, c AS (SELECT x FROM t WHERE has(s, x)) " +
				"SELECT arrayExists(y -> has(s, y), arr) FROM c WHERE x IN (SELECT s) " +
				"UNION ALL SELECT s",
		},
		{
			name:     "UNION ALL without WITH",
			pass:     passes.InjectNamedParamsAsCTE(nil),
			input:    "SET s = [1];\nSELECT {s: Array(UInt8)} UNION ALL SELECT {s: Array(UInt8)}",
			expected: "WITH [1] AS s SELECT s UNION ALL SELECT s",
		},
		{
			name:     "parenthesised UNION ALL arms",
			pass:     passes.InjectNamedParamsAsCTE(nil),
			input:    "SET s = [1];\n(SELECT {s: Array(UInt8)}) UNION ALL (SELECT 2)",
			expected: "WITH [1] AS s (SELECT s) UNION ALL (SELECT 2)",
		},
		{
			name:     "slot text in a string or comment is not a slot",
			pass:     passes.InjectNamedParamsAsCTE(nil),
			input:    "SET s = [1];\nSELECT '{s: Array(UInt8)}', {s: Array(UInt8)} /* {s: Array(UInt8)} */",
			expected: "WITH [1] AS s SELECT '{s: Array(UInt8)}', s /* {s: Array(UInt8)} */",
		},
		{
			name:     "no parameters",
			pass:     passes.InjectNamedParamsAsCTE(nil),
			input:    "SELECT  a /* c */ FROM t WHERE b = {free: String}",
			expected: "SELECT  a /* c */ FROM t WHERE b = {free: String}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.pass.Run(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)

			again, err := tt.pass.Run(got)
			require.NoError(t, err)
			assert.Equal(t, got, again, "not idempotent over Run")
		})
	}
}

func TestInjectNamedParamsAsCTEEnv(t *testing.T) {
	e, body, err := grammarenv.Extract("SET param_unused = 5;\nSET s = [1];\nSET free_value = 'v';\nSELECT {s: Array(UInt8)}, {free: String}")
	require.NoError(t, err)
	pass := passes.InjectNamedParamsAsCTE(nil)

	got, err := pass.Apply(e, body)
	require.NoError(t, err)
	assert.Equal(t, "WITH [1] AS s SELECT s, {free: String}", got)
	_, hasS := e.Params["s"]
	assert.False(t, hasS, "injected parameter stays in env")
	assert.Equal(t, "5", e.Params["param_unused"].Raw)
	assert.Equal(t, "String", e.Params["free"].Type)

	again, err := pass.Apply(e, got)
	require.NoError(t, err)
	assert.Equal(t, got, again)
}

func TestInjectNamedParamsAsCTEInvalidBody(t *testing.T) {
	_, err := passes.InjectNamedParamsAsCTE(nil).Run("SET s = [1];\nSELECT {s: Array(UInt8)} FROM")
	require.Error(t, err)
}

// bigStringArray returns an array literal of n strings, larger than
// nanopass.MaxInputBytes for n of 150 000.
func bigStringArray(n int) string {
	var b strings.Builder
	b.Grow(n * 12)
	b.WriteByte('[')
	for i := range n {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("'v")
		b.WriteString(strconv.Itoa(i))
		b.WriteByte('\'')
	}
	b.WriteByte(']')
	return b.String()
}

// TestInjectNamedParamsAsCTELargeValue runs a value larger than Parse's input
// bound through Pass.Run. Parse refuses any text that long before lexing it,
// and every parse in this run is a nanopass.Parse whose error fails the run,
// so success shows that no step parsed text holding the value.
func TestInjectNamedParamsAsCTELargeValue(t *testing.T) {
	value := bigStringArray(150_000)
	require.Greater(t, len(value), nanopass.MaxInputBytes)
	body := "SELECT a FROM t WHERE hasAny(tags, {my_set: Array(String)})"

	got, err := passes.InjectNamedParamsAsCTE(nil).Run("SET my_set = " + value + ";\n" + body)
	require.NoError(t, err)
	want := "WITH " + value + " AS my_set SELECT a FROM t WHERE hasAny(tags, my_set)"
	assert.True(t, got == want, "unexpected output (%d bytes), starts %q", len(got), got[:min(len(got), 80)])

	_, err = nanopass.Parse(got)
	require.Error(t, err, "the output is beyond Parse's bound, so no pass may parse it")
}

// TestInjectNamedParamsAsCTEAfterExprMacros composes the expression macro
// expander with the injection, the expander first: it parses and validates
// the body while it holds the slot, and the value only enters with the last
// pass.
func TestInjectNamedParamsAsCTEAfterExprMacros(t *testing.T) {
	expander := passes.NewExprMacroExpander()
	require.NoError(t, expander.Register("MY_UDF_EXACT", 2, 2, func(args []string) (string, error) {
		return "hasAny(" + args[0] + ", " + args[1] + ")", nil
	}))
	pipeline := nanopass.Sequence("expand", expander.Pass(), passes.InjectNamedParamsAsCTE(nil))

	t.Run("small", func(t *testing.T) {
		got, err := pipeline.Run("SET my_set = ['a', 'b'];\nSELECT a FROM t WHERE MY_UDF_EXACT(col, {my_set: Array(String)})")
		require.NoError(t, err)
		assert.Equal(t, "WITH ['a', 'b'] AS my_set SELECT a FROM t WHERE "+
			marked("MY_UDF_EXACT", "hasAny(col, my_set)"), got)
		_, err = nanopass.Parse(got)
		require.NoError(t, err)
	})
	t.Run("value beyond the parser's bound", func(t *testing.T) {
		value := bigStringArray(150_000)
		got, err := pipeline.Run("SET my_set = " + value + ";\nSELECT a FROM t WHERE MY_UDF_EXACT(col, {my_set: Array(String)})")
		require.NoError(t, err)
		want := "WITH " + value + " AS my_set SELECT a FROM t WHERE " + marked("MY_UDF_EXACT", "hasAny(col, my_set)")
		assert.True(t, got == want, "unexpected output (%d bytes), starts %q", len(got), got[:min(len(got), 80)])
	})
}
