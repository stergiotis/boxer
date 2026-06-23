package play

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

func TestExtractCallArgsAllLiteral(t *testing.T) {
	sql := "SELECT multiMatchAnyIndex('hay', 'foo.*', 'bar.*')"
	src := nanopass.SourceRange{Start: 7, End: len(sql)}
	args := extractCallArgs(sql, src)

	if got := len(args); got != 3 {
		t.Fatalf("got %d args, want 3", got)
	}
	for i, want := range []string{"hay", "foo.*", "bar.*"} {
		if !args[i].Literal {
			t.Errorf("args[%d].Literal=false, want true", i)
		}
		if args[i].Text != want {
			t.Errorf("args[%d].Text=%q, want %q", i, args[i].Text, want)
		}
	}
}

func TestExtractCallArgsMixedLiteralAndColumnRef(t *testing.T) {
	sql := "SELECT multiMatchAnyIndex(text, 'foo.*', 'bar.*')"
	src := nanopass.SourceRange{Start: 7, End: len(sql)}
	args := extractCallArgs(sql, src)

	if got := len(args); got != 3 {
		t.Fatalf("got %d args, want 3", got)
	}
	if args[0].Literal {
		t.Errorf("haystack column ref should not be Literal")
	}
	if args[0].Text != "text" {
		t.Errorf("haystack arg text=%q, want %q", args[0].Text, "text")
	}
	for i, want := range []string{"foo.*", "bar.*"} {
		if !args[i+1].Literal {
			t.Errorf("args[%d].Literal=false, want true", i+1)
		}
		if args[i+1].Text != want {
			t.Errorf("args[%d].Text=%q, want %q", i+1, args[i+1].Text, want)
		}
	}
}

// Nested function calls in an arg position must not confuse the
// top-level comma scanner.
func TestExtractCallArgsNestedCall(t *testing.T) {
	sql := "SELECT multiMatchAnyIndex(text, concat('a', 'b'), 'c.*')"
	src := nanopass.SourceRange{Start: 7, End: len(sql)}
	args := extractCallArgs(sql, src)

	if got := len(args); got != 3 {
		t.Fatalf("got %d args, want 3", got)
	}
	if args[1].Literal {
		t.Errorf("nested concat() should not be Literal")
	}
	if args[1].Text != "concat('a', 'b')" {
		t.Errorf("nested arg text=%q, want %q",
			args[1].Text, "concat('a', 'b')")
	}
	if !args[2].Literal || args[2].Text != "c.*" {
		t.Errorf("args[2]=%+v, want literal 'c.*'", args[2])
	}
}

// Commas inside string literals are not argument separators.
func TestExtractCallArgsCommaInsideLiteral(t *testing.T) {
	sql := "SELECT multiMatchAnyIndex('hay', 'a,b', 'c')"
	src := nanopass.SourceRange{Start: 7, End: len(sql)}
	args := extractCallArgs(sql, src)

	if got := len(args); got != 3 {
		t.Fatalf("got %d args, want 3 — comma inside literal must not split", got)
	}
	if args[1].Text != "a,b" {
		t.Errorf("args[1].Text=%q, want %q", args[1].Text, "a,b")
	}
}

// Backslash-escaped quote inside a literal must not terminate the literal.
func TestExtractCallArgsEscapedQuote(t *testing.T) {
	sql := `SELECT multiMatchAnyIndex('hay', 'it\'s', 'x')`
	src := nanopass.SourceRange{Start: 7, End: len(sql)}
	args := extractCallArgs(sql, src)

	if got := len(args); got != 3 {
		t.Fatalf("got %d args, want 3", got)
	}
	if !args[1].Literal {
		t.Errorf("args[1] with escaped quote should still be Literal")
	}
	if args[1].Text != "it's" {
		t.Errorf("args[1].Text=%q, want %q (after unescape)", args[1].Text, "it's")
	}
}

func TestExtractCallArgsEmptySrc(t *testing.T) {
	if got := extractCallArgs("", nanopass.SourceRange{}); got != nil {
		t.Errorf("empty src should return nil; got %+v", got)
	}
}

// The real ClickHouse signature passes patterns as a single Array(String);
// the comma between elements must not split the argument list, so the call
// has exactly two args: the haystack and the array literal.
func TestExtractCallArgsArrayArgNotSplit(t *testing.T) {
	sql := "SELECT multiMatchAnyIndex(col, ['a.*', 'b.*'])"
	src := nanopass.SourceRange{Start: 7, End: len(sql)}
	args := extractCallArgs(sql, src)

	if got := len(args); got != 2 {
		t.Fatalf("got %d args, want 2 (haystack + array literal); array comma must not split", got)
	}
	if args[0].Literal || args[0].Text != "col" {
		t.Errorf("args[0]=%+v, want non-literal col", args[0])
	}
	if args[1].Text != "['a.*', 'b.*']" {
		t.Errorf("args[1].Text=%q, want the whole array literal", args[1].Text)
	}
}

func TestArrayStringLiterals(t *testing.T) {
	elems := arrayStringLiterals("['a.*', 'b.*', 'c']")
	if got := len(elems); got != 3 {
		t.Fatalf("got %d elements, want 3", got)
	}
	for i, want := range []string{"a.*", "b.*", "c"} {
		if !elems[i].Literal || elems[i].Text != want {
			t.Errorf("elems[%d]=%+v, want literal %q", i, elems[i], want)
		}
	}
	if arrayStringLiterals("col") != nil {
		t.Errorf("non-array text should return nil")
	}
}

func TestSplitMultiMatchArgsArray(t *testing.T) {
	sql := "SELECT multiMatchAnyIndex(col, ['a.*', 'b.*'])"
	args := extractCallArgs(sql, nanopass.SourceRange{Start: 7, End: len(sql)})
	haystack, distance, patterns := splitMultiMatchArgs(args, false)
	if haystack != "col" {
		t.Errorf("haystack=%q, want col", haystack)
	}
	if distance != "" {
		t.Errorf("distance=%q, want empty (non-fuzzy)", distance)
	}
	if got := len(patterns); got != 2 {
		t.Fatalf("got %d patterns, want 2", got)
	}
	for i, want := range []string{"a.*", "b.*"} {
		if !patterns[i].Literal || patterns[i].Text != want {
			t.Errorf("patterns[%d]=%+v, want literal %q", i, patterns[i], want)
		}
	}
}

// Fuzzy matchers carry a UInt `distance` between the haystack and the array;
// it must be peeled off rather than treated as a pattern.
func TestSplitMultiMatchArgsFuzzy(t *testing.T) {
	sql := "SELECT multiFuzzyMatchAny(col, 2, ['a.*'])"
	args := extractCallArgs(sql, nanopass.SourceRange{Start: 7, End: len(sql)})
	haystack, distance, patterns := splitMultiMatchArgs(args, true)
	if haystack != "col" || distance != "2" {
		t.Errorf("haystack=%q distance=%q, want col / 2", haystack, distance)
	}
	if len(patterns) != 1 || patterns[0].Text != "a.*" {
		t.Errorf("patterns=%+v, want [a.*]", patterns)
	}
}

func TestMultiMatchAffordanceMatchesFamily(t *testing.T) {
	a := &multiMatchAffordance{}
	for _, name := range []string{
		"multimatchany", "multimatchanyindex", "multimatchallindices",
		"multifuzzymatchany", "multifuzzymatchanyindex", "multifuzzymatchallindices",
	} {
		if !a.Matches(nanopass.Observation{Name: name}) {
			t.Errorf("Matches(%q)=false, want true", name)
		}
	}
	for _, name := range []string{"multimatchindexany", "multisearchany", "has", "concat"} {
		if a.Matches(nanopass.Observation{Name: name}) {
			t.Errorf("Matches(%q)=true, want false", name)
		}
	}
}
