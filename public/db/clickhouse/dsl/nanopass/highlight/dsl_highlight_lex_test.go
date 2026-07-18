package highlight

import (
	"testing"
)

// findSpan returns the first span whose text equals t, or -1.
func findSpan(spans []Span, t string) int {
	for i, s := range spans {
		if s.Text == t {
			return i
		}
	}
	return -1
}

// TestHighlightLexCoverage: spans must cover the input contiguously in
// source order — the editor-path contract (ADR-0130): the Rust side
// gap-fills defensively, but the Go side is expected to emit full coverage.
func TestHighlightLexCoverage(t *testing.T) {
	sql := "SELECT count(x) AS n, 'lit' -- c\nFROM t WHERE a >= 42"
	spans := HighlightLex(sql)
	if len(spans) == 0 {
		t.Fatal("no spans")
	}
	off := 0
	for i, s := range spans {
		if s.Start != off {
			t.Fatalf("span %d: start %d, want %d (gap or overlap)", i, s.Start, off)
		}
		if s.Stop < s.Start {
			t.Fatalf("span %d: inverted range %d..%d", i, s.Start, s.Stop)
		}
		if sql[s.Start:s.Stop] != s.Text {
			t.Fatalf("span %d: text %q does not match source slice %q", i, s.Text, sql[s.Start:s.Stop])
		}
		off = s.Stop
	}
	if off != len(sql) {
		t.Fatalf("coverage ends at %d, want %d", off, len(sql))
	}
}

func TestHighlightLexFunctionNames(t *testing.T) {
	sql := "SELECT count( x ), foo, bar (y) FROM t"
	spans := HighlightLex(sql)

	if i := findSpan(spans, "count"); i < 0 || spans[i].Category != CatFunctionName {
		t.Fatalf("count: want CatFunctionName, got %v", spans[max(i, 0)].Category)
	}
	// whitespace between identifier and paren still promotes
	if i := findSpan(spans, "bar"); i < 0 || spans[i].Category != CatFunctionName {
		t.Fatalf("bar: want CatFunctionName (paren after whitespace)")
	}
	if i := findSpan(spans, "foo"); i < 0 || spans[i].Category != CatIdentifier {
		t.Fatalf("foo: want CatIdentifier")
	}
	// keywords are never promoted
	if i := findSpan(spans, "SELECT"); i < 0 || spans[i].Category != CatKeyword {
		t.Fatalf("SELECT: want CatKeyword")
	}
}

// TestHighlightLexUnparseable: mid-edit buffers rarely parse; the lex tier
// must still classify what it can.
func TestHighlightLexUnparseable(t *testing.T) {
	sql := "SELECT sum( FROM WHERE (("
	spans := HighlightLex(sql)
	if len(spans) == 0 {
		t.Fatal("no spans on unparseable input")
	}
	if i := findSpan(spans, "sum"); i < 0 || spans[i].Category != CatFunctionName {
		t.Fatalf("sum: want CatFunctionName on unparseable input")
	}
}

// TestHighlightFallbackMatchesLexTier: Highlight's parse-failure fallback
// applies the same function-name lookahead as HighlightLex.
func TestHighlightFallbackMatchesLexTier(t *testing.T) {
	sql := "SELECT sum( FROM WHERE (("
	spans := Highlight(sql)
	if i := findSpan(spans, "sum"); i < 0 || spans[i].Category != CatFunctionName {
		t.Fatalf("sum: want CatFunctionName in Highlight's lexical fallback")
	}
}

func TestHighlightLexEmpty(t *testing.T) {
	if spans := HighlightLex(""); len(spans) != 0 {
		t.Fatalf("empty input: want no spans, got %d", len(spans))
	}
}
