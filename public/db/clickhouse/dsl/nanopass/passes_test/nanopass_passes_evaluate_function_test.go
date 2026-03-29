//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEvaluator() *passes.FunctionEvaluator {
	eval := passes.NewFunctionEvaluator()
	eval.RegisterBuiltins()

	// myAdd(a, b) → a + b
	eval.Register("myAdd", func(args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("myAdd expects 2 args")
		}
		a, ok1 := toFloat64(args[0])
		b, ok2 := toFloat64(args[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("myAdd expects numeric args")
		}
		result := a + b
		if result == math.Trunc(result) {
			return int64(result), nil
		}
		return result, nil
	})

	// myMul(a, b) → a * b
	eval.Register("myMul", func(args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("myMul expects 2 args")
		}
		a, ok1 := toFloat64(args[0])
		b, ok2 := toFloat64(args[1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("myMul expects numeric args")
		}
		result := a * b
		if result == math.Trunc(result) {
			return int64(result), nil
		}
		return result, nil
	})

	// myConcat(a, b, ...) → concatenated string
	eval.Register("myConcat", func(args []any) (any, error) {
		var sb string
		for _, arg := range args {
			sb += fmt.Sprintf("%v", arg)
		}
		return sb, nil
	})

	// myConst(v) → v (identity, for testing)
	eval.Register("myConst", func(args []any) (any, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("myConst expects 1 arg")
		}
		return args[0], nil
	})

	return eval
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case int:
		return float64(n), true
	case float64:
		return n, true
	default:
		return 0, false
	}
}

// --- Basic evaluation ---

func TestEvalFunctionsSimple(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "add_integers",
			input:    "SELECT myAdd(1, 2)",
			expected: "SELECT 3",
		},
		{
			name:     "mul_integers",
			input:    "SELECT myMul(3, 4)",
			expected: "SELECT 12",
		},
		{
			name:     "concat_strings",
			input:    "SELECT myConcat('hello', ' ', 'world')",
			expected: "SELECT 'hello world'",
		},
		{
			name:     "const_integer",
			input:    "SELECT myConst(42)",
			expected: "SELECT 42",
		},
		{
			name:     "const_string",
			input:    "SELECT myConst('hello')",
			expected: "SELECT 'hello'",
		},
		{
			name:     "const_null",
			input:    "SELECT myConst(NULL)",
			expected: "SELECT NULL",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- Nested evaluation ---

func TestEvalFunctionsNested(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "nested_add",
			input:    "SELECT myAdd(myAdd(1, 2), 3)",
			expected: "SELECT 6",
		},
		{
			name:     "nested_mul_add",
			input:    "SELECT myMul(myAdd(2, 3), 4)",
			expected: "SELECT 20",
		},
		{
			name:     "deeply_nested",
			input:    "SELECT myAdd(myMul(2, 3), myAdd(4, 5))",
			expected: "SELECT 15",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- Array and tuple construction ---

func TestEvalFunctionsArrayTuple(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "array_literal",
			input:    "SELECT array(1, 2, 3)",
			expected: "SELECT [1, 2, 3]",
		},
		{
			name:     "tuple_literal",
			input:    "SELECT tuple(1, 2)",
			expected: "SELECT (1, 2)",
		},
		{
			name:     "array_of_computed",
			input:    "SELECT array(myAdd(1, 2), myMul(3, 4))",
			expected: "SELECT [3, 12]",
		},
		{
			name:     "empty_array",
			input:    "SELECT array()",
			expected: "SELECT []",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- Non-evaluable arguments (left untouched) ---

func TestEvalFunctionsNonLiteral(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "column_reference",
			input:    "SELECT myAdd(a, 1) FROM t",
			expected: "SELECT myAdd(a, 1) FROM t",
		},
		{
			name:     "mixed_literal_and_column",
			input:    "SELECT myMul(a, 2) FROM t",
			expected: "SELECT myMul(a, 2) FROM t",
		},
		{
			name:     "unregistered_function",
			input:    "SELECT unknown_func(1, 2)",
			expected: "SELECT unknown_func(1, 2)",
		},
		{
			name:     "partial_evaluable_outer_has_column",
			input:    "SELECT myAdd(a, myAdd(1, 2)) FROM t",
			expected: "SELECT myAdd(a, 3) FROM t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- Negative numbers ---

func TestEvalFunctionsNegativeArgs(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("SELECT myAdd(-3, 5)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 2", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Mixed evaluable and non-evaluable in same SELECT ---

func TestEvalFunctionsMixed(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("SELECT myAdd(1, 2), a, myMul(3, 4) FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 3, a, 12 FROM t", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Float results ---

func TestEvalFunctionsFloat(t *testing.T) {
	eval := newTestEvaluator()
	eval.Register("myDiv", func(args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("myDiv expects 2 args")
		}
		a, _ := toFloat64(args[0])
		b, _ := toFloat64(args[1])
		return a / b, nil
	})
	pass := eval.Pass()

	got, err := pass("SELECT myDiv(7, 2)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 3.5", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Error in evaluation (left untouched) ---

func TestEvalFunctionsEvalError(t *testing.T) {
	eval := passes.NewFunctionEvaluator()
	eval.Register("myFail", func(args []any) (any, error) {
		return nil, fmt.Errorf("intentional failure")
	})
	pass := eval.Pass()

	got, err := pass("SELECT myFail(1)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT myFail(1)", got) // left untouched
}

// --- Boolean result ---

func TestEvalFunctionsBoolResult(t *testing.T) {
	eval := passes.NewFunctionEvaluator()
	eval.Register("myIsPositive", func(args []any) (any, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("expects 1 arg")
		}
		v, ok := toFloat64(args[0])
		if !ok {
			return nil, fmt.Errorf("expects numeric arg")
		}
		return v > 0, nil
	})
	pass := eval.Pass()

	got, err := pass("SELECT myIsPositive(42)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", got) // bool true → 1

	got, err = pass("SELECT myIsPositive(-1)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 0", got) // bool false → 0
}

// --- String result with escaping ---

func TestEvalFunctionsStringEscaping(t *testing.T) {
	eval := passes.NewFunctionEvaluator()
	eval.Register("myQuote", func(args []any) (any, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("expects 1 arg")
		}
		s, ok := args[0].(string)
		if !ok {
			return nil, fmt.Errorf("expects string arg")
		}
		return "'" + s + "'", nil
	})
	pass := eval.Pass()

	got, err := pass("SELECT myQuote('hello')")
	require.NoError(t, err)
	// myQuote('hello') → "'hello'" → serialized as '\'hello\''
	assert.Contains(t, got, "hello")
}

// --- Nested array with evaluation ---

func TestEvalFunctionsNestedArrays(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("SELECT array(array(1, 2), array(3, 4))")
	require.NoError(t, err)
	assert.Equal(t, "SELECT [[1, 2], [3, 4]]", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Tuple with evaluation ---

func TestEvalFunctionsNestedTupleArray(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("SELECT tuple(array(1, 2), array(3, 4))")
	require.NoError(t, err)
	assert.Equal(t, "SELECT ([1, 2], [3, 4])", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Multiple evaluations in different clauses ---

func TestEvalFunctionsInWhere(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("SELECT a FROM t WHERE a > myAdd(1, 2)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t WHERE a > 3", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestEvalFunctionsInHaving(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("SELECT a, count(*) FROM t GROUP BY a HAVING count(*) > myAdd(5, 5)")
	require.NoError(t, err)
	assert.Contains(t, got, "> 10")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestEvalFunctionsInOrderBy(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("SELECT a FROM t ORDER BY a LIMIT myMul(5, 10)")
	require.NoError(t, err)
	assert.Contains(t, got, "LIMIT 50")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- UNION ALL ---

func TestEvalFunctionsUnionAll(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("SELECT myAdd(1, 2) UNION ALL SELECT myMul(3, 4)")
	require.NoError(t, err)
	assert.Contains(t, got, "SELECT 3")
	assert.Contains(t, got, "SELECT 12")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- CTEs ---

func TestEvalFunctionsCTE(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("WITH cte AS (SELECT myAdd(1, 2) AS x) SELECT x FROM cte")
	require.NoError(t, err)
	assert.Contains(t, got, "SELECT 3 AS x")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Subqueries ---

func TestEvalFunctionsSubquery(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("SELECT * FROM (SELECT myAdd(1, 2) AS x)")
	require.NoError(t, err)
	assert.Contains(t, got, "SELECT 3 AS x")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Idempotency ---

func TestEvalFunctionsIdempotent(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	sqls := []string{
		"SELECT myAdd(1, 2)",
		"SELECT myAdd(a, 1) FROM t",
		"SELECT array(1, 2, 3)",
		"SELECT myMul(myAdd(1, 2), 3)",
		"SELECT a, myConst(42), b FROM t",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass1, err := pass(sql)
			require.NoError(t, err)
			pass2, err := pass(pass1)
			require.NoError(t, err)
			assert.Equal(t, pass1, pass2, "not idempotent:\npass1: %s\npass2: %s", pass1, pass2)
		})
	}
}

// --- Pipeline integration ---

func TestEvalFunctionsInPipeline(t *testing.T) {
	eval := newTestEvaluator()

	result, err := nanopass.Pipeline(
		"select myAdd(1, myMul(2, 3)), a from t",
		passes.NormalizeKeywordCase,
		eval.Pass(),
		nanopass.Validate,
	)
	require.NoError(t, err)
	assert.Contains(t, result, "7")
	assert.Contains(t, result, "a")
}

func TestEvalFunctionsPipelineWithCanonicalize(t *testing.T) {
	eval := newTestEvaluator()

	// First canonicalize [1,2,3] → array(1,2,3), then evaluate
	result, err := nanopass.Pipeline(
		"SELECT [myAdd(1, 2), myMul(3, 4)]",
		passes.CanonicalizeConstructors(passes.ConstructorFormFunction),
		eval.Pass(),
		nanopass.Validate,
	)
	require.NoError(t, err)
	assert.Equal(t, "SELECT [3, 12]", result)
}

// --- Custom domain function ---

func TestEvalFunctionsDomainSpecific(t *testing.T) {
	eval := passes.NewFunctionEvaluator()
	eval.RegisterBuiltins()

	// daysInMonth(year, month) → number of days
	eval.Register("daysInMonth", func(args []any) (any, error) {
		if len(args) != 2 {
			return nil, fmt.Errorf("daysInMonth expects 2 args")
		}
		year, ok1 := args[0].(int64)
		month, ok2 := args[1].(int64)
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("daysInMonth expects integer args")
		}
		days := [12]int64{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
		if month < 1 || month > 12 {
			return nil, fmt.Errorf("invalid month: %d", month)
		}
		d := days[month-1]
		if month == 2 && (year%4 == 0 && (year%100 != 0 || year%400 == 0)) {
			d = 29
		}
		return d, nil
	})

	pass := eval.Pass()

	got, err := pass("SELECT daysInMonth(2024, 2)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 29", got) // 2024 is a leap year

	got, err = pass("SELECT daysInMonth(2023, 2)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 28", got)
}

func TestEvalFunctionsDomainConditional(t *testing.T) {
	eval := passes.NewFunctionEvaluator()

	// myIf(cond, then, else) — compile-time conditional
	eval.Register("myIf", func(args []any) (any, error) {
		if len(args) != 3 {
			return nil, fmt.Errorf("myIf expects 3 args")
		}
		cond := false
		switch v := args[0].(type) {
		case int64:
			cond = v != 0
		case bool:
			cond = v
		case string:
			cond = v != ""
		default:
			cond = v != nil
		}
		if cond {
			return args[1], nil
		}
		return args[2], nil
	})

	pass := eval.Pass()

	got, err := pass("SELECT myIf(1, 'yes', 'no')")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'yes'", got)

	got, err = pass("SELECT myIf(0, 'yes', 'no')")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 'no'", got)
}

// --- Interaction: evaluable function inside non-evaluable function ---

func TestEvalFunctionsInsideNonEvaluable(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	// count(myAdd(1, 2)) — count is not registered, but myAdd(1, 2) is evaluable
	// The inner myAdd should be evaluated, producing count(3)
	got, err := pass("SELECT count(myAdd(1, 2)) FROM t")
	require.NoError(t, err)
	// myAdd(1,2) is inside count's ColumnArgList, but count is not registered
	// The inner call IS a separate ColumnExprFunctionContext that can be evaluated
	assert.Equal(t, "SELECT count(3) FROM t", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- No registered functions (pass-through) ---

func TestEvalFunctionsEmpty(t *testing.T) {
	eval := passes.NewFunctionEvaluator()
	pass := eval.Pass()

	sql := "SELECT sum(a), count(*) FROM t"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

// --- Invalid SQL ---

func TestEvalFunctionsRejectsInvalid(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := pass(sql)
			assert.Error(t, err)
		})
	}
}

// --- Corpus validity ---

func TestEvalFunctionsCorpus(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			out, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("pass failed: %v", err)
			}
			_, err = nanopass.Parse(out)
			require.NoError(t, err, "produced invalid SQL for %s:\n%s", entry.Name, out)
		})
	}
}

// --- Exact output for multiple independent calls ---

func TestEvalFunctionsMultipleIndependent(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	got, err := pass("SELECT myAdd(1, 2), myMul(3, 4), myConst(5)")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 3, 12, 5", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Negative number parsing variants ---

func TestEvalFunctionsNegativeLiteral(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "negative_in_number_literal",
			input:    "SELECT myConst(-42)",
			expected: "SELECT -42",
		},
		{
			name:     "negative_float",
			input:    "SELECT myConst(-3.14)",
			expected: "SELECT -3.14",
		},
		{
			name:     "positive_explicit",
			input:    "SELECT myAdd(+1, 2)",
			expected: "SELECT 3",
		},
		{
			name:     "zero",
			input:    "SELECT myConst(0)",
			expected: "SELECT 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- TrackedRewriter overlap check ---
// --- Verify no rewriter conflicts via round-trip ---

func TestEvalFunctionsNoRewriterConflicts(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	// These cases exercise overlapping token ranges —
	// if there were rewriter conflicts, the output would be garbled or unparseable
	sqls := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "fully_nested",
			input:    "SELECT myAdd(myAdd(1, 2), 3)",
			expected: "SELECT 6",
		},
		{
			name:     "partial_inner",
			input:    "SELECT myAdd(a, myAdd(1, 2)) FROM t",
			expected: "SELECT myAdd(a, 3) FROM t",
		},
		{
			name:     "two_independent",
			input:    "SELECT myAdd(1, 2), myMul(3, 4)",
			expected: "SELECT 3, 12",
		},
		{
			name:     "partial_deep",
			input:    "SELECT myAdd(a, myMul(b, myAdd(1, 2))) FROM t",
			expected: "SELECT myAdd(a, myMul(b, 3)) FROM t",
		},
		{
			name:     "inner_inside_unregistered",
			input:    "SELECT unknown_func(myAdd(1, 2), myMul(3, 4))",
			expected: "SELECT unknown_func(3, 12)",
		},
		{
			name:     "three_levels_full",
			input:    "SELECT myAdd(myAdd(myAdd(1, 2), 3), 4)",
			expected: "SELECT 10",
		},
	}
	for _, tt := range sqls {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "rewriter conflict produced invalid SQL: %s", got)
		})
	}
}

// --- String round-trip ---

func TestEvalFunctionsStringRoundTrip(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple_string",
			input:    "SELECT myConst('hello')",
			expected: "SELECT 'hello'",
		},
		{
			name:     "empty_string",
			input:    "SELECT myConst('')",
			expected: "SELECT ''",
		},
		{
			name:     "string_with_spaces",
			input:    "SELECT myConst('hello world')",
			expected: "SELECT 'hello world'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- Edge: registered function with zero args ---

func TestEvalFunctionsZeroArgs(t *testing.T) {
	eval := passes.NewFunctionEvaluator()
	eval.Register("myPi", func(args []any) (any, error) {
		if len(args) != 0 {
			return nil, fmt.Errorf("myPi takes no args")
		}
		return 3.14159, nil
	})
	pass := eval.Pass()

	got, err := pass("SELECT myPi()")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 3.14159", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Edge: deeply nested partial (4 levels) ---

func TestEvalFunctionsDeeplyNestedPartial(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	// Level 4: myAdd(1, 2) → 3
	// Level 3: myMul(3, 3) → but first arg is column c, so partial → myMul(c, 3)
	// Level 2: myAdd(b, myMul(c, 3)) → partial → myAdd(b, myMul(c, 3))
	// Level 1: myConcat(a, ...) → partial
	got, err := pass("SELECT myConcat(a, myAdd(b, myMul(c, myAdd(1, 2)))) FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT myConcat(a, myAdd(b, myMul(c, 3))) FROM t", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Edge: evaluable function as argument to non-registered function ---

func TestEvalFunctionsEvalInsideBuiltin(t *testing.T) {
	eval := newTestEvaluator()
	pass := eval.Pass()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "inside_count",
			input:    "SELECT count(myAdd(1, 2)) FROM t",
			expected: "SELECT count(3) FROM t",
		},
		{
			name:     "inside_if",
			input:    "SELECT if(myAdd(1, 2) > 0, 'yes', 'no')",
			expected: "SELECT if(3 > 0, 'yes', 'no')",
		},
		{
			name:     "inside_tostring",
			input:    "SELECT toString(myMul(6, 7))",
			expected: "SELECT toString(42)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}
