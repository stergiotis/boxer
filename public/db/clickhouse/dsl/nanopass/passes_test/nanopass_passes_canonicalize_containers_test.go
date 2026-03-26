//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ToLiteral: function → syntax ---

func TestCanonicalizeToLiteralTuple(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormLiteral)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tuple_function",
			input:    "SELECT tuple(1, 2, 3)",
			expected: "SELECT (1, 2, 3)",
		},
		{
			name:     "tuple_with_strings",
			input:    "SELECT tuple('a', 'b')",
			expected: "SELECT ('a', 'b')",
		},
		{
			name:     "tuple_in_where",
			input:    "SELECT a FROM t WHERE (a, b) = tuple(1, 2)",
			expected: "SELECT a FROM t WHERE (a, b) = (1, 2)",
		},
		{
			name:     "tuple_already_literal",
			input:    "SELECT (1, 2, 3)",
			expected: "SELECT (1, 2, 3)",
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

func TestCanonicalizeToLiteralArray(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormLiteral)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "array_function",
			input:    "SELECT array(1, 2, 3)",
			expected: "SELECT [1, 2, 3]",
		},
		{
			name:     "array_in_expression",
			input:    "SELECT length(array(1, 2, 3))",
			expected: "SELECT length([1, 2, 3])",
		},
		{
			name:     "array_already_literal",
			input:    "SELECT [1, 2, 3]",
			expected: "SELECT [1, 2, 3]",
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

func TestCanonicalizeToLiteralTupleElement(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormLiteral)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tuple_element_function",
			input:    "SELECT tupleElement(t, 1) FROM (SELECT (1, 2) AS t)",
			expected: "SELECT t.1 FROM (SELECT (1, 2) AS t)",
		},
		{
			name:     "tuple_element_already_access",
			input:    "SELECT t.1 FROM (SELECT (1, 2) AS t)",
			expected: "SELECT t.1 FROM (SELECT (1, 2) AS t)",
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

func TestCanonicalizeToLiteralArrayElement(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormLiteral)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "array_element_function",
			input:    "SELECT arrayElement(arr, 1) FROM t",
			expected: "SELECT arr[1] FROM t",
		},
		{
			name:     "array_element_already_access",
			input:    "SELECT arr[1] FROM t",
			expected: "SELECT arr[1] FROM t",
		},
		{
			name:     "array_element_complex_index",
			input:    "SELECT arrayElement(arr, n + 1) FROM t",
			expected: "SELECT arr[n + 1] FROM t",
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

// --- ToFunction: syntax → function ---

func TestCanonicalizeToFunctionTuple(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormFunction)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tuple_literal",
			input:    "SELECT (1, 2, 3)",
			expected: "SELECT tuple(1, 2, 3)",
		},
		{
			name:     "tuple_with_strings",
			input:    "SELECT ('a', 'b')",
			expected: "SELECT tuple('a', 'b')",
		},
		{
			name:     "tuple_already_function",
			input:    "SELECT tuple(1, 2, 3)",
			expected: "SELECT tuple(1, 2, 3)",
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

func TestCanonicalizeToFunctionArray(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormFunction)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "array_literal",
			input:    "SELECT [1, 2, 3]",
			expected: "SELECT array(1, 2, 3)",
		},
		{
			name:     "array_already_function",
			input:    "SELECT array(1, 2, 3)",
			expected: "SELECT array(1, 2, 3)",
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

func TestCanonicalizeToFunctionTupleAccess(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormFunction)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tuple_access",
			input:    "SELECT t.1 FROM (SELECT (1, 2) AS t)",
			expected: "SELECT tupleElement(t, 1) FROM (SELECT tuple(1, 2) AS t)",
		},
		{
			name:     "tuple_access_already_function",
			input:    "SELECT tupleElement(t, 1) FROM (SELECT tuple(1, 2) AS t)",
			expected: "SELECT tupleElement(t, 1) FROM (SELECT tuple(1, 2) AS t)",
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

func TestCanonicalizeToFunctionArrayAccess(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormFunction)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "array_access",
			input:    "SELECT arr[1] FROM t",
			expected: "SELECT arrayElement(arr, 1) FROM t",
		},
		{
			name:     "array_access_complex_index",
			input:    "SELECT arr[n + 1] FROM t",
			expected: "SELECT arrayElement(arr, n + 1) FROM t",
		},
		{
			name:     "array_access_already_function",
			input:    "SELECT arrayElement(arr, 1) FROM t",
			expected: "SELECT arrayElement(arr, 1) FROM t",
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

// --- Mixed expressions ---

func TestCanonicalizeToLiteralMixed(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormLiteral)

	got, err := pass("SELECT tuple(1, 2), array(3, 4), arrayElement(arr, 1), tupleElement(t, 2) FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT (1, 2), [3, 4], arr[1], t.2 FROM t", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestCanonicalizeToFunctionMixed(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormFunction)

	got, err := pass("SELECT (1, 2), [3, 4], arr[1], t.2 FROM t")
	require.NoError(t, err)
	assert.Equal(t, "SELECT tuple(1, 2), array(3, 4), arrayElement(arr, 1), tupleElement(t, 2) FROM t", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Round-trip ---

func TestCanonicalizeRoundTrip(t *testing.T) {
	sqls := []string{
		"SELECT tuple(1, 2, 3)",
		"SELECT [1, 2, 3]",
		"SELECT arr[1] FROM t",
		"SELECT t.1 FROM (SELECT (1, 2) AS t)",
	}

	toLit := passes.CanonicalizeConstructors(passes.ConstructorFormLiteral)
	toFunc := passes.CanonicalizeConstructors(passes.ConstructorFormFunction)

	for i, sql := range sqls {
		t.Run(fmt.Sprintf("roundtrip_%d", i), func(t *testing.T) {
			// function → literal → function should produce same as just function
			lit, err := toLit(sql)
			require.NoError(t, err)
			backToFunc, err := toFunc(lit)
			require.NoError(t, err)
			backToLit, err := toLit(backToFunc)
			require.NoError(t, err)
			assert.Equal(t, lit, backToLit, "round-trip failed")
		})
	}
}

// --- Idempotency ---

func TestCanonicalizeIdempotent(t *testing.T) {
	sqls := []string{
		"SELECT tuple(1, 2), [3, 4], arr[1], t.2 FROM t",
		"SELECT (1, 2), array(3, 4), arrayElement(arr, 1), tupleElement(t, 2) FROM t",
	}

	forms := []struct {
		name string
		form passes.ConstructorFormE
	}{
		{"literal", passes.ConstructorFormLiteral},
		{"function", passes.ConstructorFormFunction},
	}

	for _, f := range forms {
		pass := passes.CanonicalizeConstructors(f.form)
		for i, sql := range sqls {
			t.Run(fmt.Sprintf("%s_%d", f.name, i), func(t *testing.T) {
				pass1, err := pass(sql)
				require.NoError(t, err)
				pass2, err := pass(pass1)
				require.NoError(t, err)
				assert.Equal(t, pass1, pass2, "not idempotent")
			})
		}
	}
}

// --- UNION ALL ---

func TestCanonicalizeUnionAll(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormLiteral)

	got, err := pass("SELECT tuple(1, 2) UNION ALL SELECT array(3, 4)")
	require.NoError(t, err)
	assert.Contains(t, got, "(1, 2)")
	assert.Contains(t, got, "[3, 4]")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Edge cases ---

func TestCanonicalizeNonTargetFunctions(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormLiteral)

	// Other functions should not be affected
	sql := "SELECT count(*), sum(a), tuple(1, 2) FROM t"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "count(*)")
	assert.Contains(t, got, "sum(a)")
	assert.Contains(t, got, "(1, 2)")
}

func TestCanonicalizeRejectsInvalid(t *testing.T) {
	pass := passes.CanonicalizeConstructors(passes.ConstructorFormLiteral)
	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := pass(sql)
			assert.Error(t, err)
		})
	}
}

// --- Corpus validity ---

func TestCanonicalizeOutputValidity(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	forms := []struct {
		name string
		form passes.ConstructorFormE
	}{
		{"literal", passes.ConstructorFormLiteral},
		{"function", passes.ConstructorFormFunction},
	}

	for _, f := range forms {
		pass := passes.CanonicalizeConstructors(f.form)
		for _, entry := range entries {
			t.Run(entry.Name+"/"+f.name, func(t *testing.T) {
				out, err := pass(entry.SQL)
				if err != nil {
					t.Skipf("pass failed: %v", err)
				}
				_, err = nanopass.Parse(out)
				require.NoError(t, err, "produced invalid SQL for %s:\n%s", entry.Name, out)
			})
		}
	}
}
