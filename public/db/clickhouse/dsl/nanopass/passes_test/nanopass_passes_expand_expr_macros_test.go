package passes_test

import (
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammarenv"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errExprMacroTest = errors.New("macro refuses")

// marked is the text a call of name expands to when its macro returns
// expansion.
func marked(name string, expansion string) string {
	return "/* " + name + " BEGIN */ (" + expansion + ") /* " + name + " END */"
}

func newTestExprMacros(t *testing.T) (inst *passes.ExprMacroExpander) {
	t.Helper()
	inst = passes.NewExprMacroExpander()
	reg := func(name string, minArgs int, maxArgs int, fn passes.ExprMacroFuncI) {
		require.NoError(t, inst.Register(name, minArgs, maxArgs, fn))
	}
	reg("MY_UDF_FALSE_POSITIVE", 3, 3, func(args []string) (string, error) {
		return args[0] + " + " + args[1] + " - " + args[2], nil
	})
	reg("MY_UDF_EXACT", 2, 2, func(args []string) (string, error) {
		return "hasAny(" + args[0] + ", " + args[1] + ")", nil
	})
	reg("jsonField", 1, 1, func(args []string) (string, error) {
		return "JSONExtractString(payload, " + args[0] + ")", nil
	})
	reg("MY_UDF_SUM", 1, -1, func(args []string) (string, error) {
		return strings.Join(args, " + "), nil
	})
	reg("MY_UDF_TWICE", 1, 1, func(args []string) (string, error) {
		return "MY_UDF_SUM(" + args[0] + ", " + args[0] + ")", nil
	})
	reg("MY_UDF_NOT", 1, 1, func(args []string) (string, error) {
		return "NOT " + args[0], nil
	})
	reg("MY_UDF_ANY", 2, 2, func(args []string) (string, error) {
		return "arrayExists(" + args[0] + ", " + args[1] + ")", nil
	})
	reg("MY_UDF_ARGS", 0, 3, func(args []string) (string, error) {
		return "tuple(" + strings.Join(args, ", ") + ")", nil
	})
	reg("MY_UDF_FAIL", 0, -1, func(args []string) (string, error) {
		return "", errExprMacroTest
	})
	reg("MY_UDF_ESCAPE", 0, 0, func(args []string) (string, error) {
		return "a) OR (b", nil
	})
	reg("MY_UDF_TRAILING_COMMENT", 0, 0, func(args []string) (string, error) {
		return "a -- trailing", nil
	})
	return
}

func TestExprMacroExpander(t *testing.T) {
	inst := newTestExprMacros(t)
	pass := inst.Pass()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "basic",
			input:    "SELECT MY_UDF_FALSE_POSITIVE(x, y, z) FROM t",
			expected: "SELECT " + marked("MY_UDF_FALSE_POSITIVE", "x + y - z") + " FROM t",
		},
		{
			name:     "case-insensitive name, registered spelling in markers",
			input:    "SELECT my_udf_exact(a, b) FROM t",
			expected: "SELECT " + marked("MY_UDF_EXACT", "hasAny(a, b)") + " FROM t",
		},
		{
			name:     "double-quoted call name",
			input:    `SELECT "My_Udf_Exact"(a, b) FROM t`,
			expected: "SELECT " + marked("MY_UDF_EXACT", "hasAny(a, b)") + " FROM t",
		},
		{
			name:     "backquoted call name",
			input:    "SELECT `JSONFIELD`('k') FROM t",
			expected: "SELECT " + marked("jsonField", "JSONExtractString(payload, 'k')") + " FROM t",
		},
		{
			name:     "quoted identifier and array literal arguments",
			input:    `SELECT a FROM t WHERE MY_UDF_EXACT("string:lv", ['x', 'y'])`,
			expected: `SELECT a FROM t WHERE ` + marked("MY_UDF_EXACT", `hasAny("string:lv", ['x', 'y'])`),
		},
		{
			name:     "param slot argument",
			input:    "SELECT a FROM t WHERE MY_UDF_EXACT(tags, {wanted: Array(String)})",
			expected: "SELECT a FROM t WHERE " + marked("MY_UDF_EXACT", "hasAny(tags, {wanted: Array(String)})"),
		},
		{
			name:     "OR argument is parenthesised",
			input:    "SELECT a FROM t WHERE MY_UDF_NOT(a OR b)",
			expected: "SELECT a FROM t WHERE " + marked("MY_UDF_NOT", "NOT (a OR b)"),
		},
		{
			name:     "operator arguments are parenthesised",
			input:    "SELECT MY_UDF_FALSE_POSITIVE(a + 1, b * c, d = e) FROM t",
			expected: "SELECT " + marked("MY_UDF_FALSE_POSITIVE", "(a + 1) + (b * c) - (d = e)") + " FROM t",
		},
		{
			name:     "signed literal is parenthesised",
			input:    "SELECT MY_UDF_FALSE_POSITIVE(1, 2, -3) FROM t",
			expected: "SELECT " + marked("MY_UDF_FALSE_POSITIVE", "1 + 2 - (-3)") + " FROM t",
		},
		{
			name:     "atoms stay bare",
			input:    "SELECT MY_UDF_ARGS(t.c, 'lit', (a, b)) , MY_UDF_ARGS((a + b), (SELECT 1), lower(s)) FROM t",
			expected: "SELECT " + marked("MY_UDF_ARGS", "tuple(t.c, 'lit', (a, b))") + " , " + marked("MY_UDF_ARGS", "tuple((a + b), (SELECT 1), lower(s))") + " FROM t",
		},
		{
			name:     "operator-word calls are parenthesised",
			input:    "SELECT MY_UDF_ARGS(not(a), and(a, b)) FROM t",
			expected: "SELECT " + marked("MY_UDF_ARGS", "tuple((not(a)), (and(a, b)))") + " FROM t",
		},
		{
			name:     "zero arguments",
			input:    "SELECT MY_UDF_ARGS() FROM t",
			expected: "SELECT " + marked("MY_UDF_ARGS", "tuple()") + " FROM t",
		},
		{
			name:     "comments inside an argument survive",
			input:    "SELECT jsonField(a /* keep */ || 'x') FROM t",
			expected: "SELECT " + marked("jsonField", "JSONExtractString(payload, (a /* keep */ || 'x'))") + " FROM t",
		},
		{
			name:     "lambda argument is handed over unwrapped",
			input:    "SELECT MY_UDF_ANY(x -> x > 1, col) FROM t",
			expected: "SELECT " + marked("MY_UDF_ANY", "arrayExists(x -> x > 1, col)") + " FROM t",
		},
		{
			name:  "nested call in an argument",
			input: "SELECT MY_UDF_SUM(jsonField('k'), 1) FROM t",
			expected: "SELECT " + marked("MY_UDF_SUM",
				marked("jsonField", "JSONExtractString(payload, 'k')")+" + 1") + " FROM t",
		},
		{
			name:  "nested call in a non-atom argument",
			input: "SELECT MY_UDF_NOT(jsonField('k') = 'v') FROM t",
			expected: "SELECT " + marked("MY_UDF_NOT",
				"NOT ("+marked("jsonField", "JSONExtractString(payload, 'k')")+" = 'v')") + " FROM t",
		},
		{
			name:  "expansion calls another macro",
			input: "SELECT MY_UDF_TWICE(a) FROM t",
			expected: "SELECT " + marked("MY_UDF_TWICE",
				marked("MY_UDF_SUM", "a + a")) + " FROM t",
		},
		{
			name:     "after a slash",
			input:    "SELECT a/jsonField('k') FROM t",
			expected: "SELECT a/ " + marked("jsonField", "JSONExtractString(payload, 'k')") + " FROM t",
		},
		{
			name:     "after a minus",
			input:    "SELECT a-jsonField('k') FROM t",
			expected: "SELECT a-" + marked("jsonField", "JSONExtractString(payload, 'k')") + " FROM t",
		},
		{
			name:     "aliased call",
			input:    "SELECT jsonField('k') AS k FROM t",
			expected: "SELECT " + marked("jsonField", "JSONExtractString(payload, 'k')") + " AS k FROM t",
		},
		{
			name: "every clause",
			input: "SELECT jsonField('p') FROM t PREWHERE MY_UDF_NOT(a) WHERE MY_UDF_NOT(b) " +
				"GROUP BY jsonField('g') HAVING MY_UDF_NOT(c) ORDER BY jsonField('o')",
			expected: "SELECT " + marked("jsonField", "JSONExtractString(payload, 'p')") +
				" FROM t PREWHERE " + marked("MY_UDF_NOT", "NOT a") +
				" WHERE " + marked("MY_UDF_NOT", "NOT b") +
				" GROUP BY " + marked("jsonField", "JSONExtractString(payload, 'g')") +
				" HAVING " + marked("MY_UDF_NOT", "NOT c") +
				" ORDER BY " + marked("jsonField", "JSONExtractString(payload, 'o')"),
		},
		{
			name:     "inside a lambda",
			input:    "SELECT arrayExists(x -> MY_UDF_NOT(x), col) FROM t",
			expected: "SELECT arrayExists(x -> " + marked("MY_UDF_NOT", "NOT x") + ", col) FROM t",
		},
		{
			name: "CTE body, subquery and both UNION ALL branches",
			input: "WITH c AS (SELECT jsonField('a') AS v FROM t WHERE MY_UDF_NOT(b)) " +
				"SELECT v FROM c WHERE v IN (SELECT jsonField('b') FROM u) " +
				"UNION ALL SELECT jsonField('c') FROM (SELECT * FROM w WHERE MY_UDF_NOT(d))",
			expected: "WITH c AS (SELECT " + marked("jsonField", "JSONExtractString(payload, 'a')") +
				" AS v FROM t WHERE " + marked("MY_UDF_NOT", "NOT b") + ") " +
				"SELECT v FROM c WHERE v IN (SELECT " + marked("jsonField", "JSONExtractString(payload, 'b')") + " FROM u) " +
				"UNION ALL SELECT " + marked("jsonField", "JSONExtractString(payload, 'c')") +
				" FROM (SELECT * FROM w WHERE " + marked("MY_UDF_NOT", "NOT d") + ")",
		},
		{
			name:     "unregistered calls are left alone",
			input:    "SELECT otherFn(x), MY_UDF_EXACTLY(x),  lower( s ) /* MY_UDF_NOT(a) */ FROM t -- jsonField('k')",
			expected: "SELECT otherFn(x), MY_UDF_EXACTLY(x),  lower( s ) /* MY_UDF_NOT(a) */ FROM t -- jsonField('k')",
		},
		{
			name:     "names in table-function and SETTINGS position are left alone",
			input:    "SELECT a FROM jsonField('k') SETTINGS max_threads = 1",
			expected: "SELECT a FROM jsonField('k') SETTINGS max_threads = 1",
		},
		{
			name:     "slot is untouched",
			input:    "SELECT {jsonField: String} FROM t",
			expected: "SELECT {jsonField: String} FROM t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass.Run(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
			if got != tt.input {
				assert.False(t, inst.Mentions(got), "expanded output still mentions a macro: %s", got)
			}
		})
	}
}

func TestExprMacroExpanderSetPrelude(t *testing.T) {
	inst := newTestExprMacros(t)
	ids := make([]string, 0, 200)
	for i := range 200 {
		ids = append(ids, "'id"+strings.Repeat("x", i%7)+"'")
	}
	prelude := "SET param_wanted = [" + strings.Join(ids, ", ") + "];\n"
	input := prelude + "SELECT a FROM t WHERE MY_UDF_EXACT(tags, {wanted: Array(String)})"

	got, err := inst.Pass().Run(input)
	require.NoError(t, err)
	assert.Equal(t, prelude+"SELECT a FROM t WHERE "+marked("MY_UDF_EXACT", "hasAny(tags, {wanted: Array(String)})"), got)

	e, _, err := grammarenv.Extract(got)
	require.NoError(t, err)
	assert.Equal(t, "["+strings.Join(ids, ", ")+"]", e.Params["param_wanted"].Raw)
	assert.Equal(t, "Array(String)", e.Params["wanted"].Type)

	// A statement without a call keeps its body byte for byte.
	plain := prelude + "SELECT a FROM t WHERE has(tags, {wanted: Array(String)})"
	gotPlain, err := inst.Pass().Run(plain)
	require.NoError(t, err)
	_, plainBody, err := grammarenv.Extract(gotPlain)
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t WHERE has(tags, {wanted: Array(String)})", plainBody)
}

func TestExprMacroExpanderErrors(t *testing.T) {
	inst := newTestExprMacros(t)
	pass := inst.Pass()

	t.Run("arity, fixed", func(t *testing.T) {
		_, err := pass.Run("SELECT MY_UDF_EXACT(a) FROM t")
		require.Error(t, err)
		f := ebtest.Fields(t, err)
		assert.Equal(t, "MY_UDF_EXACT", f["macro"])
		assert.Equal(t, "2", f["accepts"])
		assert.EqualValues(t, 1, f["given"])
	})
	t.Run("arity, range", func(t *testing.T) {
		_, err := pass.Run("SELECT MY_UDF_ARGS(a, b, c, d) FROM t")
		require.Error(t, err)
		f := ebtest.Fields(t, err)
		assert.Equal(t, "MY_UDF_ARGS", f["macro"])
		assert.Equal(t, "0 to 3", f["accepts"])
		assert.EqualValues(t, 4, f["given"])
	})
	t.Run("arity, variadic", func(t *testing.T) {
		_, err := pass.Run("SELECT MY_UDF_SUM() FROM t")
		require.Error(t, err)
		f := ebtest.Fields(t, err)
		assert.Equal(t, "MY_UDF_SUM", f["macro"])
		assert.Equal(t, "1 or more", f["accepts"])
		assert.EqualValues(t, 0, f["given"])
	})
	t.Run("arity, nested", func(t *testing.T) {
		_, err := pass.Run("SELECT MY_UDF_SUM(MY_UDF_NOT()) FROM t")
		require.Error(t, err)
		assert.Equal(t, "MY_UDF_NOT", ebtest.Fields(t, err)["macro"])
	})
	t.Run("macro function error is wrapped", func(t *testing.T) {
		_, err := pass.Run("SELECT my_udf_fail(a) FROM t")
		require.Error(t, err)
		require.ErrorIs(t, err, errExprMacroTest)
		assert.Equal(t, "MY_UDF_FAIL", ebtest.Fields(t, err)["macro"])
	})
	for name, sql := range map[string]string{
		"FILTER":     "SELECT MY_UDF_SUM(a) FILTER (WHERE b) FROM t",
		"OVER":       "SELECT MY_UDF_SUM(a) OVER (PARTITION BY b) FROM t",
		"OVER name":  "SELECT MY_UDF_SUM(a) OVER w FROM t WINDOW w AS (PARTITION BY b)",
		"DISTINCT":   "SELECT MY_UDF_SUM(DISTINCT a) FROM t",
		"parameters": "SELECT MY_UDF_SUM(1)(a) FROM t",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := pass.Run(sql)
			require.Error(t, err)
			assert.Equal(t, "MY_UDF_SUM", ebtest.Fields(t, err)["macro"])
		})
	}
	t.Run("expansion escapes its parentheses", func(t *testing.T) {
		_, err := pass.Run("SELECT MY_UDF_ESCAPE() FROM t")
		require.Error(t, err)
		assert.Equal(t, "MY_UDF_ESCAPE", ebtest.Fields(t, err)["macro"])
	})
	t.Run("expansion ends in a line comment", func(t *testing.T) {
		_, err := pass.Run("SELECT MY_UDF_TRAILING_COMMENT() FROM t")
		require.Error(t, err)
		assert.Equal(t, "MY_UDF_TRAILING_COMMENT", ebtest.Fields(t, err)["macro"])
	})
	t.Run("self-recursive macro does not converge", func(t *testing.T) {
		rec := passes.NewExprMacroExpander()
		require.NoError(t, rec.Register("MY_UDF_LOOP", 1, 1, func(args []string) (string, error) {
			return "MY_UDF_LOOP(" + args[0] + ")", nil
		}))
		// Each round nests one more pair of parentheses, so the input
		// guards stop it before the iteration cap does.
		_, err := rec.Pass().Run("SELECT MY_UDF_LOOP(a) FROM t")
		require.Error(t, err)
	})
	for _, sql := range []string{"", "   ", "SELECT", ";;;"} {
		t.Run("invalid input "+strings.TrimSpace(sql), func(t *testing.T) {
			_, err := pass.Run(sql)
			require.Error(t, err)
		})
	}
}

func TestExprMacroExpanderRegister(t *testing.T) {
	inst := passes.NewExprMacroExpander()
	fn := func(args []string) (string, error) { return "1", nil }
	for _, name := range []string{"", "a b", "a*/b", "*/", "1abc", "$abc", `"quoted"`, "a-b", "a.b", "naïve", "a/*"} {
		assert.Error(t, inst.Register(name, 0, 0, fn), "name %q", name)
	}
	assert.Error(t, inst.Register("ok", -1, 0, fn))
	assert.Error(t, inst.Register("ok", 2, 1, fn))
	assert.Error(t, inst.Register("ok", 0, 0, nil))
	for _, name := range []string{"_x", "abc", "MY_UDF_EXACT", "a1$b"} {
		assert.NoError(t, inst.Register(name, 0, -1, fn), "name %q", name)
	}
}

// TestExprMacroExpanderCorpusUnchanged checks that every corpus entry comes
// back byte for byte from a registry none of them calls.
func TestExprMacroExpanderCorpusUnchanged(t *testing.T) {
	inst := newTestExprMacros(t)
	pass := inst.Pass()
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	checked := 0
	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			_, body, extractErr := grammarenv.Extract(entry.SQL)
			require.NoError(t, extractErr)
			if _, parseErr := nanopass.Parse(body); parseErr != nil {
				t.Skipf("corpus entry does not parse: %v", parseErr)
			}
			got, applyErr := pass.Apply(grammarenv.NewEnvironment(), body)
			require.NoError(t, applyErr)
			assert.Equal(t, body, got)
			assert.False(t, inst.Mentions(entry.SQL))
			checked++
		})
	}
	require.NotZero(t, checked)
}

func TestExprMacroExpanderConcurrent(t *testing.T) {
	inst := newTestExprMacros(t)
	pass := inst.Pass()
	input := "SELECT MY_UDF_SUM(jsonField('k'), MY_UDF_TWICE(a)) FROM t WHERE MY_UDF_NOT(b OR c)"
	want, err := pass.Run(input)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 20 {
				got, runErr := pass.Run(input)
				assert.NoError(t, runErr)
				assert.Equal(t, want, got)
				assert.True(t, inst.Mentions(input))
				assert.False(t, inst.Mentions(got))
			}
		})
	}
	wg.Wait()
}

func TestExprMacroExpanderMentions(t *testing.T) {
	inst := newTestExprMacros(t)
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{name: "plain call", sql: "SELECT MY_UDF_EXACT(a, b) FROM t", want: true},
		{name: "other case", sql: "select my_udf_exact(a, b) from t", want: true},
		{name: "space before paren", sql: "SELECT jsonField ('k')", want: true},
		{name: "comment before paren", sql: "SELECT jsonField /* c */ ('k')", want: true},
		{name: "line comment before paren", sql: "SELECT jsonField -- c\n('k')", want: true},
		{name: "double-quoted", sql: `SELECT "jsonField"('k')`, want: true},
		{name: "backquoted", sql: "SELECT `JSONFIELD`('k')", want: true},
		{name: "after a SET prelude", sql: "SET param_x = 'jsonField(1)';\nSELECT MY_UDF_NOT(a)", want: true},
		{name: "glued to a number", sql: "SELECT 1jsonField('k')", want: true},
		{name: "after a dot", sql: "SELECT t.jsonField('k')", want: true},
		{name: "table-function position", sql: "SELECT a FROM jsonField('k')", want: true},

		{name: "no registered name", sql: "SELECT lower(a) FROM t", want: false},
		{name: "empty", sql: "", want: false},
		{name: "name without paren", sql: "SELECT jsonField FROM t", want: false},
		{name: "name as column then paren elsewhere", sql: "SELECT jsonField, (1) FROM t", want: false},
		{name: "in a string literal", sql: "SELECT 'jsonField(1)' FROM t", want: false},
		{name: "in a string literal with escapes", sql: `SELECT 'it\'s jsonField(1)', 'a''jsonField(2)' FROM t`, want: false},
		{name: "in a line comment", sql: "SELECT a -- jsonField('k')\nFROM t", want: false},
		{name: "in a slash line comment", sql: "SELECT a // jsonField('k')\nFROM t", want: false},
		{name: "in a block comment", sql: "SELECT a /* jsonField('k') */ FROM t", want: false},
		{name: "prefix of a longer identifier", sql: "SELECT MY_UDF_EXACTLY(a, b) FROM t", want: false},
		{name: "suffix of a longer identifier", sql: "SELECT xjsonField('k') FROM t", want: false},
		{name: "quoted longer identifier", sql: `SELECT "jsonField2"('k') FROM t`, want: false},
		{name: "name as a string followed by paren", sql: "SELECT 'jsonField' ('k')", want: false},
		{name: "slot name", sql: "SELECT {jsonField: String} FROM t", want: false},
		{name: "already expanded", sql: "SELECT " + marked("jsonField", "JSONExtractString(payload, 'k')") + " FROM t", want: false},
		{name: "unterminated block comment scanned as code", sql: "SELECT a /* jsonField('k')", want: true},
		{name: "unterminated quote scanned as code", sql: `SELECT "x, jsonField(1)`, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, inst.Mentions(tt.sql))
		})
	}
	assert.False(t, passes.NewExprMacroExpander().Mentions("SELECT jsonField('k')"), "empty registry")
}

// TestExprMacroExpanderMentionsNeverUnderReports checks, over the corpus with
// a call spliced in, that Mentions is true wherever the pass rewrites.
func TestExprMacroExpanderMentionsNeverUnderReports(t *testing.T) {
	inst := newTestExprMacros(t)
	pass := inst.Pass()
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	calls := []string{"jsonField('k')", "`jsonfield`('k')", "MY_UDF_NOT /* c */ (a)", "\"MY_UDF_SUM\"(1, 2)"}
	rewritten := 0
	for _, entry := range entries {
		for _, call := range calls {
			sql := strings.Replace(entry.SQL, "SELECT ", "SELECT "+call+" AS zz_probe, ", 1)
			got, runErr := pass.Run(sql)
			if runErr != nil || got == sql {
				continue
			}
			rewritten++
			assert.True(t, inst.Mentions(sql), "under-reported: %s", sql)
			assert.False(t, inst.Mentions(got), "expanded output still mentions a macro: %s", got)
		}
	}
	require.NotZero(t, rewritten)
}
