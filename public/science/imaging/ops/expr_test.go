package ops

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExpressionGrammarAccepts(t *testing.T) {
	for src, canonical := range map[string]string{
		"a":                            "a",
		"  a+b ":                       "a + b",
		"(a-b)/(a+b)":                  "(a - b) / (a + b)",
		"a - (b - c)":                  "a - (b - c)",
		"(a - b) - c":                  "a - b - c",
		"a / (b * c)":                  "a / (b * c)",
		"-a*b":                         "-a * b",
		"-(a*b)":                       "-(a * b)",
		"a - -b":                       "a - -b",
		"--a":                          "--a",
		"!(a == b)":                    "!(a == b)",
		"!a == b":                      "!a == b",
		"a < b && b <= c || !d":        "a < b && b <= c || !d",
		"a || (b && c)":                "a || b && c",
		"(a || b) && c":                "(a || b) && c",
		"(a < b) == (c < d)":           "(a < b) == (c < d)",
		"1.5e3 + .5 + 2. + 1E-2":       "1500 + 0.5 + 2 + 0.01",
		"pow(a,2)+sqrt(abs(b))":        "pow(a, 2) + sqrt(abs(b))",
		"clamp(a, 0, 1)":               "clamp(a, 0, 1)",
		"where(isvalid(a), a, 0)":      "where(isvalid(a), a, 0)",
		"min(a, max(b, c))":            "min(a, max(b, c))",
		"log(a)\t+ log10(b)\n- exp(c)": "log(a) + log10(b) - exp(c)",
		"_x1 * B8":                     "_x1 * B8",
		"(((a)))":                      "a",
		"a != b":                       "a != b",
		"a >= 1e308":                   "a >= 1e+308",
	} {
		e, err := ParseExpression(src)
		require.NoError(t, err, src)
		require.Equal(t, canonical, e.String(), src)
		again, err := ParseExpression(e.String())
		require.NoError(t, err, src)
		require.Equal(t, canonical, again.String(), "the canonical spelling of %q reads back to itself", src)
	}
}

func TestExpressionGrammarRefuses(t *testing.T) {
	for _, c := range []struct {
		src      string
		column   int
		expected string
	}{
		{"", 1, "an operand"},
		{"a +", 4, "an operand"},
		{"a b", 3, "an operator or the end"},
		{"(a", 3, "')'"},
		{"a)", 2, "an operator or the end"},
		{"a < b < c", 7, "comparisons do not chain"},
		{"foo(a)", 1, "one of the functions"},
		{"abs", 4, "'(' and the arguments of abs"},
		{"abs(a, b)", 6, "abs takes 1 argument"},
		{"pow(a)", 6, "pow takes 2 arguments"},
		{"clamp(a, b)", 11, "clamp takes 3 arguments"},
		{"a = b", 3, "a number, a name"},
		{"a; b", 2, "a number, a name"},
		{"a ** b", 4, "an operand"},
		{"1e", 3, "the digits of an exponent"},
		{"1.2.3", 4, "an operator after a number"},
		{"2a", 2, "an operator after a number"},
		{".", 1, "a digit"},
		{"1e999", 1, "a number a float64 holds"},
		{"a & b", 3, "a number, a name"},
		{"a + é", 5, "a number, a name"},
		{"min(a,)", 7, "an operand"},
		{"where(a b)", 9, "',' or ')'"},
	} {
		_, err := ParseExpression(c.src)
		var pe *ParseError
		require.True(t, errors.As(err, &pe), "%q: %v", c.src, err)
		require.Equal(t, c.column, pe.Column, "%q: %v", c.src, err)
		require.Contains(t, pe.Expected, c.expected, c.src)
	}
}

func TestExpressionBounds(t *testing.T) {
	deep := strings.Repeat("(", maxExprDepth+2) + "a" + strings.Repeat(")", maxExprDepth+2)
	_, err := ParseExpression(deep)
	require.ErrorContains(t, err, "levels of nesting")
	_, err = ParseExpression(strings.Repeat("-", maxExprDepth+2) + "a")
	require.ErrorContains(t, err, "levels of nesting")
	_, err = ParseExpression(strings.Repeat("a+", maxExprNodes) + "a")
	require.ErrorContains(t, err, "terms")
	_, err = ParseExpression(strings.Repeat(" ", maxTextBytes) + "a")
	require.ErrorContains(t, err, "bytes")
	// Within the limit as written, past it once the spaces are written.
	tight := strings.Repeat("a", maxTextBytes-3) + "+a"
	require.Less(t, len(tight), maxTextBytes)
	_, err = ParseExpression(tight)
	require.ErrorContains(t, err, "canonical spelling")
}

// evalOne evaluates an expression over one pixel of each named input.
func evalOne(t *testing.T, src string, names []string, values ...float64) float64 {
	t.Helper()
	e, err := ParseExpression(src)
	require.NoError(t, err, src)
	prog, err := compileExpression(e, names)
	require.NoError(t, err, src)
	m := newExprMachine(&prog, 1)
	return m.run(func(i int) []float64 { return []float64{values[i]} })[0]
}

func TestExpressionSemantics(t *testing.T) {
	nan, inf := math.NaN(), math.Inf(1)
	names := []string{"a", "b"}
	for _, c := range []struct {
		src  string
		a, b float64
		want float64
	}{
		{"a + b", 1, 2, 3},
		{"a / b", 1, 0, nan},
		{"a / b", 0, 0, nan},
		{"isvalid(a / b)", 1, 0, 0},
		{"isvalid(a)", nan, 0, 0},
		{"isvalid(a)", inf, 0, 1},
		{"a + 0", inf, 0, nan},
		{"-a", inf, 0, nan},
		{"a * 1e300 * 1e300", 1, 0, nan},
		{"sqrt(a)", -1, 0, nan},
		{"log(a)", 0, 0, nan},
		{"log10(a)", 1000, 0, 3},
		{"exp(a)", 1000, 0, nan},
		{"pow(a, b)", nan, 0, nan},
		{"pow(a, b)", 1, nan, nan},
		{"pow(a, b)", 2, 10, 1024},
		{"pow(a, b)", 0, -1, nan},
		{"min(a, b)", nan, 1, nan},
		{"max(a, b)", 3, 1, 3},
		{"clamp(a, 0, 1)", 3, 0, 1},
		{"clamp(a, 0, 1)", -3, 0, 0},
		{"clamp(a, 0, b)", 0.5, nan, nan},
		{"a < b", 1, 2, 1},
		{"a >= b", 1, 2, 0},
		{"a == b", 2, 2, 1},
		{"a != b", 2, 2, 0},
		{"a < b", nan, 2, nan},
		{"a && b", 2, 0, 0},
		{"a || b", 0, 0.1, 1},
		{"a || b", 1, nan, nan},
		{"!a", 0, 0, 1},
		{"!a", 5, 0, 0},
		{"!a", nan, 0, nan},
		{"where(a, b, 7)", 1, 4, 4},
		{"where(a, b, 7)", 0, 4, 7},
		{"where(a, b, 7)", nan, 4, nan},
		{"where(a, b, 7)", 0, nan, 7},
		{"where(a, b, 7)", 1, nan, nan},
		{"where(isvalid(b), b, -1)", 0, nan, -1},
		{"abs(a)", -2, 0, 2},
		{"a - b * 2", 10, 3, 4},
		{"(a - b) * 2", 10, 3, 14},
	} {
		got := evalOne(t, c.src, names, c.a, c.b)
		if math.IsNaN(c.want) {
			require.True(t, math.IsNaN(got), "%s at a=%v b=%v: %v, want invalid", c.src, c.a, c.b, got)
			continue
		}
		require.Equal(t, c.want, got, "%s at a=%v b=%v", c.src, c.a, c.b)
	}
}

func TestExpressionNamesAndCompile(t *testing.T) {
	e, err := ParseExpression("b + a * b + abs(c)")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "c"}, e.Names())
	_, err = compileExpression(e, []string{"a", "b"})
	require.ErrorIs(t, err, ErrRefused)
}

// FuzzExpression holds the parser and the compiler to what they promise on
// any text: no panic; a text that parses has a canonical spelling that
// parses to itself; a parsed expression compiles over its own names and
// evaluates over a row of awkward values without panicking, every isvalid
// and comparison giving 0, 1 or invalid.
func FuzzExpression(f *testing.F) {
	for _, s := range []string{
		"(a - b) / (a + b)", "where(isvalid(a), a, 0)", "clamp(a, 0, 1) >= .5 && !b", "pow(a, -b) + log10(abs(a))",
		"--a", "1e308 * 10", "min(max(a, b), 1.5e-3)", "a < b < c", "foo(a)", "((a)", "", ".", "1e", "a || b && c != d",
	} {
		f.Add(s)
	}
	row := []float64{0, math.Copysign(0, -1), 1, -1, 2.5, math.NaN(), math.Inf(1), math.Inf(-1), 1e308, -1e-308, math.MaxFloat64}
	f.Fuzz(func(t *testing.T, src string) {
		e, err := ParseExpression(src)
		if err != nil {
			var pe *ParseError
			if !errors.As(err, &pe) || pe.Column < 1 || pe.Expected == "" {
				t.Fatalf("a refusal that is no parse error with a column: %v", err)
			}
			return
		}
		canonical := e.String()
		again, err := ParseExpression(canonical)
		if err != nil {
			t.Fatalf("the canonical spelling %q of %q does not parse: %v", canonical, src, err)
		}
		if again.String() != canonical {
			t.Fatalf("the canonical spelling of %q is %q, and of that %q", src, canonical, again.String())
		}
		names := e.Names()
		prog, err := compileExpression(e, names)
		if err != nil {
			t.Fatalf("%q does not compile over its own names: %v", src, err)
		}
		m := newExprMachine(&prog, len(row))
		out := m.run(func(i int) []float64 { return row })
		if len(out) != len(row) {
			t.Fatalf("a row of %d values from a row of %d", len(out), len(row))
		}
		if e.root.op == exprOpIsValid || exprBinary[e.root.op].prec == precCmp || exprBinary[e.root.op].prec == precAnd ||
			exprBinary[e.root.op].prec == precOr || e.root.op == exprOpNot {
			for _, v := range out {
				if v == v && v != 0 && v != 1 {
					t.Fatalf("%q gives %v, which is no truth value", src, v)
				}
			}
		}
	})
}
