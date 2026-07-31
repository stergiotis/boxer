package highlight

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// at resolves the caret marked by `|` in the input, which keeps the offsets in
// the test data legible rather than hand-counted.
func at(t *testing.T, marked string) (e CaretEntity, ok bool) {
	t.Helper()
	off := strings.IndexByte(marked, '|')
	require.GreaterOrEqual(t, off, 0, "test input must mark the caret with |")
	sql := marked[:off] + marked[off+1:]
	return EntityAtIn(sql, off)
}

func TestEntityAtNameUnderCaret(t *testing.T) {
	cases := []struct {
		name   string
		marked string
		want   string
		call   bool
	}{
		{"mid-name", "SELECT toH|our(x)", "toHour", true},
		{"start of name", "SELECT |toHour(x)", "toHour", true},
		// The caret after a name belongs to that name — where it sits the
		// moment you finish typing one.
		{"end of name", "SELECT toHour|(x)", "toHour", true},
		{"bare identifier", "SELECT * FROM merge|tree_table", "mergetree_table", false},
		// A data type is a bare identifier to the lexer; whether it is
		// documented is the consumer's lookup, not this walk's guess.
		{"type name", "SELECT CAST(x AS UInt|8)", "UInt8", false},
		{"table engine", "CREATE TABLE t (a UInt8) ENGINE = Merge|Tree", "MergeTree", false},
		{"keyword", "SEL|ECT 1", "SELECT", false},
		{"quoted identifier", "SELECT `my col|umn` FROM t", "my column", false},
		{"at buffer end", "SELECT toHour|", "toHour", false},
		{"aggregate call", "SELECT cou|nt(*) FROM t", "count", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, ok := at(t, tc.marked)
			require.True(t, ok)
			require.Equal(t, tc.want, e.Name)
			require.Equal(t, tc.call, e.Call)
		})
	}
}

// The reported range must slice the name back out of the buffer, so a consumer
// can highlight or replace it.
func TestEntityAtRangeSlicesTheName(t *testing.T) {
	const sql = "SELECT toHour(now())"
	e, ok := EntityAtIn(sql, 9)
	require.True(t, ok)
	require.Equal(t, "toHour", sql[e.Start:e.Stop])
}

func TestEntityAtNothingNameable(t *testing.T) {
	for _, marked := range []string{
		"SELECT 1|23",        // a number literal
		"SELECT 'te|xt'",     // a string literal
		"SELECT 1 +| 2",      // an operator
		"-- just a co|mment", // a comment
		"   |   ",            // whitespace only
	} {
		t.Run(marked, func(t *testing.T) {
			e, ok := at(t, marked)
			require.Empty(t, e.Name, "nothing nameable is under the caret")
			require.False(t, ok, "and nothing encloses it")
		})
	}
}

func TestEntityAtEmptyBuffer(t *testing.T) {
	e, ok := EntityAtIn("", 0)
	require.False(t, ok)
	require.Empty(t, e.Name)
	// A caret past the end of a buffer clamps rather than failing.
	_, ok = EntityAtIn("SELECT 1", 999)
	require.False(t, ok, "the last token is a literal")
}

func TestEntityAtEnclosingCalls(t *testing.T) {
	cases := []struct {
		name   string
		marked string
		want   []string
	}{
		{"inside one call", "SELECT toHour(|x)", []string{"toHour"}},
		{"on the argument", "SELECT toHour(x|)", []string{"toHour"}},
		{"nested, innermost first", "SELECT concat(toString(|x), 'a')", []string{"toString", "concat"}},
		// A call that opened and closed before the caret must not be reported:
		// its paren is balanced on the way out.
		{"after a closed sibling", "SELECT concat(toString(x), |'a')", []string{"concat"}},
		// Grouping parens and subqueries have no callee, but the walk still
		// passes through them to whatever encloses those.
		{"through a grouping paren", "SELECT f(a + (b|))", []string{"f"}},
		{"through a subquery", "SELECT f((SELECT |1))", []string{"f"}},
		{"statement level", "SELECT a, |b FROM t", nil},
		{"subquery, no call", "SELECT * FROM (SELECT |1)", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e, _ := at(t, tc.marked)
			require.Equal(t, tc.want, e.Enclosing)
		})
	}
}

// Name and Enclosing are independent: a caret can carry both, either, or
// neither, and a consumer choosing between them needs each answered on its own
// terms.
func TestEntityAtNameAndEnclosingAreIndependent(t *testing.T) {
	// Both: a nested callee, inside the outer call's argument list.
	e, ok := at(t, "SELECT concat(toS|tring(x))")
	require.True(t, ok)
	require.Equal(t, "toString", e.Name)
	require.True(t, e.Call)
	require.Equal(t, []string{"concat"}, e.Enclosing)

	// Enclosing only: the caret is on a comma inside the argument list.
	e, ok = at(t, "SELECT concat(a,| b)")
	require.True(t, ok)
	require.Empty(t, e.Name)
	require.Equal(t, []string{"concat"}, e.Enclosing)

	// Name only: a callee at statement level.
	e, ok = at(t, "SELECT toH|our(x) FROM t")
	require.True(t, ok)
	require.Equal(t, "toHour", e.Name)
	require.Empty(t, e.Enclosing)
}

// The walk survives what the parser does not — which is the whole reason it is
// at the lex tier.
func TestEntityAtOnAnUnparseableBuffer(t *testing.T) {
	e, ok := at(t, "SELCT toHo|ur(")
	require.True(t, ok)
	require.Equal(t, "toHour", e.Name)

	e, ok = at(t, "SELECT toHour(|")
	require.True(t, ok, "an unclosed call still encloses the caret")
	require.Equal(t, []string{"toHour"}, e.Enclosing)
}

// A `;` inside a string literal cannot fool the walk, because the lexer owns
// the tokenisation — the same property the statement split rests on.
func TestEntityAtIgnoresLiteralContents(t *testing.T) {
	e, ok := at(t, "SELECT f('a)b', |x)")
	require.True(t, ok)
	require.Equal(t, []string{"f"}, e.Enclosing,
		"the ) inside the literal must not balance the call's own paren")
}
