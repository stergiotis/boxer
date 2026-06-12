//go:build llm_generated_opus47

package play

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

func TestExtractCallArgsAllLiteral(t *testing.T) {
	sql := "SELECT multiMatchIndexAny('hay', 'foo.*', 'bar.*')"
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
	sql := "SELECT multiMatchIndexAny(text, 'foo.*', 'bar.*')"
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
	sql := "SELECT multiMatchIndexAny(text, concat('a', 'b'), 'c.*')"
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
	sql := "SELECT multiMatchIndexAny('hay', 'a,b', 'c')"
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
	sql := `SELECT multiMatchIndexAny('hay', 'it\'s', 'x')`
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
