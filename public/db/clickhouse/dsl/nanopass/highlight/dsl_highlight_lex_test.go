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

// assertContiguousCoverage checks the invariant HighlightLex documents:
// spans start at 0, abut, carry their own source slice, and end at len(sql).
func assertContiguousCoverage(t *testing.T, sql string, spans []Span) {
	t.Helper()
	off := 0
	for i, s := range spans {
		if s.Start != off {
			t.Fatalf("span %d: start %d, want %d (gap or overlap); dropped %q",
				i, s.Start, off, sql[min(off, s.Start):max(off, s.Start)])
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
		t.Fatalf("coverage ends at %d, want %d (dropped tail %q)", off, len(sql), sql[off:])
	}
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
	assertContiguousCoverage(t, sql, spans)
}

// TestHighlightLexCoverageUnrecognised pins coverage for input the lexer
// cannot tokenise. ANTLR reports a recognition error and skips the offending
// characters, so before the fix they were absent from the span list and — on
// the CodeView path, which does not gap-fill — absent from the render too.
// `|>` is ClickHouse 26.8's pipe operator; the rest are ordinary typos.
func TestHighlightLexCoverageUnrecognised(t *testing.T) {
	for _, sql := range []string{
		"FROM orders |> WHERE amount >= 250 |> ORDER BY amount",
		"SELECT a # comment",
		"SELECT a @ b FROM t",
		"SELECT a ~ b FROM t",
		"SELECT a ^ b FROM t",
		"SELECT a & b FROM t",
		"@ SELECT 1",               // leading
		"SELECT 1 @",               // trailing, no following token
		"@",                        // nothing but garbage
		"@@@",                      // one run, not three spans
		"SELECT '@ inside' FROM t", // inside a literal: lexes, must not split
		"SELECT ⍵ FROM t",          // multi-byte rune the lexer rejects
	} {
		t.Run(sql, func(t *testing.T) {
			assertContiguousCoverage(t, sql, HighlightLex(sql))
		})
	}
}

// TestHighlightLexUnrecognisedIsOneSpanPerRun: a run of unrecognised bytes is
// claimed once, not once per byte, so span counts stay proportional to tokens.
func TestHighlightLexUnrecognisedIsOneSpanPerRun(t *testing.T) {
	spans := HighlightLex("SELECT @@@ 1")
	n := 0
	for _, s := range spans {
		if s.Text == "@@@" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("got %d spans covering the @@@ run, want 1: %+v", n, spans)
	}
}

// TestHighlightSemanticSurvivesUnrecognised: the semantic tier pairs tokens
// with spans positionally, so an extra unpaired span must not shift the
// pairing. Unrecognised input fails the parse and takes the lexical fallback,
// which is exactly why this has to be asserted rather than assumed.
func TestHighlightSemanticSurvivesUnrecognised(t *testing.T) {
	clean := "SELECT count(x) FROM orders"
	spans := Highlight(clean)
	i := findSpan(spans, "orders")
	if i < 0 {
		t.Fatal("no span for the table name")
	}
	if spans[i].Category != CatTableName {
		t.Fatalf("orders: category %v, want CatTableName", spans[i].Category)
	}

	// The same query with one stray byte no longer parses, so the semantic
	// tier cannot run at all. What matters is that it degrades to the lex
	// tier — full coverage, no semantic categories — rather than pairing
	// tokens with spans that have shifted.
	dirty := clean + " @"
	spans = Highlight(dirty)
	assertContiguousCoverage(t, dirty, spans)
	i = findSpan(spans, "orders")
	if i < 0 {
		t.Fatal("no span for the table name")
	}
	if spans[i].Category == CatTableName {
		t.Fatal("unparseable input reached the semantic tier")
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
