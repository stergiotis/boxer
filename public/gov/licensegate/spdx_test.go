package licensegate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// An identifier no policy map will ever carry.
const unknownID = "LicenseRef-not-in-the-map"

func TestEvaluateExpression(t *testing.T) {
	cases := []struct {
		expression string
		category   CategoryE
		elected    string
	}{
		{"MIT", CategoryNotice, "MIT"},
		{"GPL-3.0-only", CategoryRestricted, "GPL-3.0-only"},

		// OR elects; a tie keeps the author's order.
		{"MIT OR Apache-2.0", CategoryNotice, "MIT"},
		{"Apache-2.0 OR MIT", CategoryNotice, "Apache-2.0"},
		{"MIT OR Apache-2.0 OR LGPL-2.1-or-later", CategoryNotice, "MIT"},
		{"LGPL-2.1-or-later OR MIT", CategoryNotice, "MIT"},
		{"Unlicense OR MIT", CategoryUnencumbered, "Unlicense"},

		// AND binds every branch.
		{"Apache-2.0 AND ISC", CategoryNotice, "Apache-2.0 AND ISC"},
		{"MIT AND GPL-3.0-only", CategoryRestricted, "MIT AND GPL-3.0-only"},
		{"(MIT OR Apache-2.0) AND Unicode-3.0", CategoryNotice, "MIT AND Unicode-3.0"},
		{"(MIT OR Apache-2.0) AND OFL-1.1 AND Ubuntu-font-1.0", CategoryReciprocal, "MIT AND OFL-1.1 AND Ubuntu-font-1.0"},

		// Unknown never displaces a known election, and never masks a violation.
		{"MIT OR " + unknownID, CategoryNotice, "MIT"},
		{unknownID + " OR MIT", CategoryNotice, "MIT"},
		{"MIT AND " + unknownID, CategoryUnknown, "MIT AND " + unknownID},
		{unknownID + " AND GPL-3.0-only", CategoryRestricted, unknownID + " AND GPL-3.0-only"},
		{"GPL-3.0-only AND " + unknownID, CategoryRestricted, "GPL-3.0-only AND " + unknownID},
		{"GPL-3.0-only OR " + unknownID, CategoryRestricted, "GPL-3.0-only"},

		// Precedence: WITH, then AND, then OR.
		{"MIT OR GPL-3.0-only AND ISC", CategoryNotice, "MIT"},
		{"GPL-3.0-only AND ISC OR MIT", CategoryNotice, "MIT"},
		{"((MIT))", CategoryNotice, "MIT"},

		// WITH falls back to its base identifier.
		{"Apache-2.0 WITH LLVM-exception OR Apache-2.0 OR MIT", CategoryNotice, "Apache-2.0 WITH LLVM-exception"},
		{"GPL-2.0-or-later WITH Classpath-exception-2.0", CategoryRestricted, "GPL-2.0-or-later WITH Classpath-exception-2.0"},

		// Spellings older crates still carry.
		{"MIT/Apache-2.0", CategoryNotice, "MIT"},
		{"Apache-2.0 / MIT", CategoryNotice, "Apache-2.0"},
		{"MIT or Apache-2.0", CategoryNotice, "MIT"},
	}
	for _, tc := range cases {
		category, elected, err := EvaluateExpressionE(tc.expression)
		require.NoError(t, err, tc.expression)
		assert.Equal(t, tc.category, category, tc.expression)
		assert.Equal(t, tc.elected, elected, tc.expression)
	}
}

func TestEvaluateExpressionMalformed(t *testing.T) {
	for _, expression := range []string{
		"",
		"   ",
		"MIT OR",
		"OR MIT",
		"(MIT",
		"MIT)",
		"MIT AND",
		"MIT WITH",
		"MIT WITH OR Apache-2.0",
		"MIT Apache-2.0",
		"()",
	} {
		_, _, err := EvaluateExpressionE(expression)
		assert.Error(t, err, "%q", expression)
	}
}

// expressionLeaves are the identifiers the property draws from: violations,
// passing categories, and one the map does not know.
var expressionLeaves = []string{
	"AGPL-3.0-only", "GPL-3.0-only", "LGPL-2.1-or-later",
	"MPL-2.0", "MIT", "Apache-2.0", "ISC", "Unlicense",
	unknownID,
}

// drawExpression renders a random well-formed expression tree, parenthesizing
// every compound so the rendered text can be composed with other expressions
// without changing how it groups.
func drawExpression(t *rapid.T, depth int) (expression string) {
	if depth == 0 || rapid.Bool().Draw(t, "leaf") {
		expression = rapid.SampledFrom(expressionLeaves).Draw(t, "id")
		return
	}
	operator := rapid.SampledFrom([]string{" AND ", " OR "}).Draw(t, "operator")
	expression = "(" + drawExpression(t, depth-1) + operator + drawExpression(t, depth-1) + ")"
	return
}

func evaluate(t *rapid.T, expression string) (category CategoryE) {
	category, _, err := EvaluateExpressionE(expression)
	if err != nil {
		t.Fatalf("well-formed expression %q failed to parse: %v", expression, err)
	}
	return
}

// TestEvaluateExpressionProperties pins the two guarantees ADR-0246 SD3 rests
// on for arbitrary expressions, not just the table above: a violating conjunct
// always fails the conjunction, and a known-good alternative always rescues a
// disjunction. Both operators are also order-independent in category.
func TestEvaluateExpressionProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := drawExpression(t, 3)
		b := drawExpression(t, 3)

		if !evaluate(t, a+" AND GPL-3.0-only").IsViolation() {
			t.Fatalf("%q AND GPL-3.0-only passed the gate", a)
		}
		if evaluate(t, a+" OR MIT").IsViolation() {
			t.Fatalf("%q OR MIT failed the gate", a)
		}
		if ab, ba := evaluate(t, a+" AND "+b), evaluate(t, b+" AND "+a); ab != ba {
			t.Fatalf("AND is order-dependent: %q AND %q is %v, reversed %v", a, b, ab, ba)
		}
		if ab, ba := evaluate(t, a+" OR "+b), evaluate(t, b+" OR "+a); ab != ba {
			t.Fatalf("OR is order-dependent: %q OR %q is %v, reversed %v", a, b, ab, ba)
		}
	})
}
