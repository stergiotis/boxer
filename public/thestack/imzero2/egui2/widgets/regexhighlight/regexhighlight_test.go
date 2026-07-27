package regexhighlight_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/regexhighlight"
	"pgregory.net/rapid"
)

// assertCoverage checks the output guarantee the CodeViewJob contract
// depends on: spans cover every byte of src exactly once, in ascending
// order, with src[Start:Stop] == Text and no span splitting a rune
// (ADR-0015 §SD5). A LayoutJob that does not cover every byte drops
// glyphs (ADR-0130 §3), so this is the invariant, not a nicety.
func assertCoverage(t *testing.T, src string, spans []regexhighlight.Span) {
	t.Helper()
	cursor := int32(0)
	for i, s := range spans {
		if s.Start != cursor {
			t.Fatalf("span %d: Start=%d, want %d (gap or overlap in %q)", i, s.Start, cursor, src)
		}
		if s.Stop <= s.Start {
			t.Fatalf("span %d: empty or inverted range [%d,%d) in %q", i, s.Start, s.Stop, src)
		}
		if int(s.Stop) > len(src) {
			t.Fatalf("span %d: Stop=%d past end %d of %q", i, s.Stop, len(src), src)
		}
		if got := src[s.Start:s.Stop]; got != s.Text {
			t.Fatalf("span %d: Text=%q, want %q", i, s.Text, got)
		}
		// A pattern with invalid UTF-8 still has to be covered; the
		// rune-boundary claim only applies to valid input.
		if utf8.ValidString(src) && !utf8.ValidString(s.Text) {
			t.Fatalf("span %d: %q splits a UTF-8 sequence in %q", i, s.Text, src)
		}
		if s.Depth < 0 {
			t.Fatalf("span %d: negative depth %d in %q", i, s.Depth, src)
		}
		cursor = s.Stop
	}
	if cursor != int32(len(src)) {
		t.Fatalf("coverage stops at %d of %d bytes in %q", cursor, len(src), src)
	}
}

// catOf returns the category of the span covering byte `at`.
func catOf(t *testing.T, spans []regexhighlight.Span, at int32) regexhighlight.CategoryE {
	t.Helper()
	for _, s := range spans {
		if at >= s.Start && at < s.Stop {
			return s.Category
		}
	}
	t.Fatalf("no span covers byte %d", at)
	return 0
}

// render renders spans as "text:category" pairs, the compact form the
// table tests compare against.
func render(spans []regexhighlight.Span) string {
	parts := make([]string, 0, len(spans))
	for _, s := range spans {
		parts = append(parts, fmt.Sprintf("%s:%s", s.Text, catName(s.Category)))
	}
	return strings.Join(parts, " ")
}

func catName(c regexhighlight.CategoryE) string {
	switch c {
	case regexhighlight.CategoryLiteral:
		return "lit"
	case regexhighlight.CategoryMeta:
		return "meta"
	case regexhighlight.CategoryQuantifier:
		return "quant"
	case regexhighlight.CategoryAnchor:
		return "anchor"
	case regexhighlight.CategoryEscape:
		return "esc"
	case regexhighlight.CategoryClassName:
		return "clsname"
	case regexhighlight.CategoryClassDelim:
		return "clsdelim"
	case regexhighlight.CategoryClassLiteral:
		return "clslit"
	case regexhighlight.CategoryGroup:
		return "group"
	case regexhighlight.CategoryGroupName:
		return "gname"
	case regexhighlight.CategoryFlags:
		return "flags"
	case regexhighlight.CategoryError:
		return "err"
	}
	return "?"
}

func TestHighlightClassifiesEachCategory(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{"plain literal run", "abc", "abc:lit"},
		{"meta", "a.b|c", "a:lit .:meta b:lit |:meta c:lit"},
		{"anchors", "^a$", "^:anchor a:lit $:anchor"},
		{"escape anchors", `\ba\z`, `\b:anchor a:lit \z:anchor`},
		{"star plus question", "a*b+c?", "a:lit *:quant b:lit +:quant c:lit ?:quant"},
		{"lazy quantifier", "a*?", "a:lit *?:quant"},
		{"repeat", "a{2,3}", "a:lit {2,3}:quant"},
		{"lazy repeat", "a{2,}?", "a:lit {2,}?:quant"},
		{"repeat without lower bound is literal", "a{,3}", "a{,3}:lit"},
		{"unterminated repeat is literal", "a{2", "a{2:lit"},
		{"escapes", `\.\n`, `\.:esc \n:esc`},
		{"hex escapes", `\x7f\x{1F600}`, `\x7f:esc \x{1F600}:esc`},
		{"octal escape", `\012`, `\012:esc`},
		{"perl classes", `\d\W`, `\d:clsname \W:clsname`},
		{"unicode classes", `\pL\p{Greek}`, `\pL:clsname \p{Greek}:clsname`},
		{"quoted run", `\Qa.b\Ec`, `\Q:esc a.b:lit \E:esc c:lit`},
		{"unterminated quoted run", `\Qa.b`, `\Q:esc a.b:lit`},
		{"character class", "[a-z]", "[:clsdelim a:clslit -:clsdelim z:clslit ]:clsdelim"},
		{"negated class", "[^ab]", "[^:clsdelim ab:clslit ]:clsdelim"},
		{"posix class", "[[:alpha:]]", "[:clsdelim [:alpha:]:clsname ]:clsdelim"},
		{"class with perl class", `[\d.]`, `[:clsdelim \d:clsname .:clslit ]:clsdelim`},
		{"leading bracket in class is a literal", "[]]", "[:clsdelim ]:clslit ]:clsdelim"},
		{"leading bracket in negated class", "[^]]", "[^:clsdelim ]:clslit ]:clsdelim"},
		// `a` and `-` coalesce: adjacent bytes of the same category are
		// one span, so the run reads `a-` rather than two spans.
		{"trailing dash in class is a literal", "[a-]", "[:clsdelim a-:clslit ]:clsdelim"},
		{"leading dash in class is a literal", "[-a]", "[:clsdelim -a:clslit ]:clsdelim"},
		{"escaped bracket in class", `[\]]`, `[:clsdelim \]:esc ]:clsdelim`},
		{"backslash-b in class is not an anchor", `[\b]`, `[:clsdelim \b:esc ]:clsdelim`},
		{"capturing group", "(a)", "(:group a:lit ):group"},
		{"non-capturing group", "(?:a)", "(?:group ::group a:lit ):group"},
		{"named group", "(?P<x>a)", "(?P<:group x:gname >:group a:lit ):group"},
		{"go-style named group", "(?<x>a)", "(?<:group x:gname >:group a:lit ):group"},
		{"flag setting", "(?i)a", "(?:group i:flags ):group a:lit"},
		{"flag group", "(?i:a)", "(?:group i:flags ::group a:lit ):group"},
		{"negated flags", "(?i-s:a)", "(?:group i-s:flags ::group a:lit ):group"},
		{"bare brace is a literal", "a}b", "a}b:lit"},
		{"lone trailing backslash", `a\`, `a:lit \:err`},
		{"unbalanced close paren", "a)b", "a:lit ):err b:lit"},
		{"unterminated group keeps its content", "(ab", "(:group ab:lit"},
		{"unterminated class keeps its content", "[ab", "[:clsdelim ab:clslit"},
		{"unterminated group name", "(?P<ab", "(?P<:group ab:gname"},
		{"half-typed flag group", "(?i", "(?:group i:flags"},
		{"escaped non-ascii rune", `\é`, `\é:esc`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spans := regexhighlight.Highlight(tc.src)
			assertCoverage(t, tc.src, spans)
			if got := render(spans); got != tc.want {
				t.Errorf("Highlight(%q)\n got: %s\nwant: %s", tc.src, got, tc.want)
			}
		})
	}
}

// TestFlagSettingVersusFlagGroupDepth pins the trap ADR-0015 §SD2 names:
// `(?i)` and `(?i:` share a two-character prefix and have opposite
// structural effects.
func TestFlagSettingVersusFlagGroupDepth(t *testing.T) {
	// A flag setting is not a group: what follows it stays at depth 0,
	// and the `)` after it is its own, not an unbalanced close.
	spans := regexhighlight.Highlight("(?i)ab")
	assertCoverage(t, "(?i)ab", spans)
	for _, s := range spans {
		if s.Depth != 0 {
			t.Fatalf("(?i)ab: span %q at depth %d, want 0", s.Text, s.Depth)
		}
	}
	if got := catOf(t, spans, 3); got != regexhighlight.CategoryGroup {
		t.Fatalf("(?i)ab: the `)` classified as %s, want group — a flag setting eats its own paren", catName(got))
	}

	// A flag group is a group: its body sits one level deeper.
	spans = regexhighlight.Highlight("(?i:ab)")
	assertCoverage(t, "(?i:ab)", spans)
	if got := depthOfText(spans, "ab"); got != 1 {
		t.Fatalf("(?i:ab): body at depth %d, want 1", got)
	}
	if got := depthOfText(spans, ")"); got != 0 {
		t.Fatalf("(?i:ab): closing paren at depth %d, want 0 (it pairs with its opener)", got)
	}
}

func TestNestedGroupDepth(t *testing.T) {
	const src = "((a)(?:b))"
	spans := regexhighlight.Highlight(src)
	assertCoverage(t, src, spans)

	// Byte-indexed expectations: (  (  a  )  (  ?  :  b  )  )
	want := []int32{0, 1, 2, 1, 1, 1, 1, 2, 1, 0}
	for i, w := range want {
		var got int32 = -1
		for _, s := range spans {
			if int32(i) >= s.Start && int32(i) < s.Stop {
				got = s.Depth
				break
			}
		}
		if got != w {
			t.Errorf("byte %d (%q): depth %d, want %d", i, src[i:i+1], got, w)
		}
	}
}

// TestUnbalancedCloseIsAnErrorOnlyAtDepthZero — a `)` that closes
// something is a group delimiter; one that closes nothing is one of the
// two byte-level certainties CategoryError is reserved for.
func TestUnbalancedCloseIsAnErrorOnlyAtDepthZero(t *testing.T) {
	const src = "(a))"
	spans := regexhighlight.Highlight(src)
	assertCoverage(t, src, spans)
	if got := catOf(t, spans, 2); got != regexhighlight.CategoryGroup {
		t.Errorf("first `)` classified as %s, want group", catName(got))
	}
	if got := catOf(t, spans, 3); got != regexhighlight.CategoryError {
		t.Errorf("second `)` classified as %s, want err", catName(got))
	}
}

func depthOfText(spans []regexhighlight.Span, text string) int32 {
	for _, s := range spans {
		if s.Text == text {
			return s.Depth
		}
	}
	return -1
}

func TestHighlightLinesAreIndependent(t *testing.T) {
	const src = "(a\nb)\n[c"
	spans := regexhighlight.HighlightLines(src)
	assertCoverage(t, src, spans)

	// Line 1 leaves a group open; line 2 must not inherit it, so its `)`
	// is an unbalanced close rather than a delimiter — and `b` sits at
	// depth 0, not depth 1.
	if got := depthOfText(spans, "b"); got != 0 {
		t.Errorf("line 2 body at depth %d, want 0 — depth must reset per line", got)
	}
	if got := catOf(t, spans, 4); got != regexhighlight.CategoryError {
		t.Errorf("line 2 `)` classified as %s, want err — line 1's `(` must not carry over", catName(got))
	}
	// Newlines are covered, as plain literal bytes.
	if got := catOf(t, spans, 2); got != regexhighlight.CategoryLiteral {
		t.Errorf("newline classified as %s, want lit", catName(got))
	}
}

func TestHighlightLinesCoversTrailingNewlineAndEmptyLines(t *testing.T) {
	for _, src := range []string{"", "\n", "a\n", "\na", "a\n\nb", "\n\n"} {
		spans := regexhighlight.HighlightLines(src)
		assertCoverage(t, src, spans)
	}
}

func TestHighlightEmptyInput(t *testing.T) {
	if spans := regexhighlight.Highlight(""); len(spans) != 0 {
		t.Fatalf("Highlight(\"\") returned %d span(s), want none", len(spans))
	}
}

// TestCoverageInvariantProperty is the SD5 property test: whatever the
// bytes, the coverage guarantee holds. Invalid patterns are the normal
// interactive state, so the generator deliberately draws from a
// metacharacter-heavy alphabet rather than from well-formed patterns.
func TestCoverageInvariantProperty(t *testing.T) {
	alphabet := []string{
		"a", "Z", "0", "9", " ", "é", "ÿ", "\n",
		`\`, `\d`, `\p{Greek}`, `\x{1F600}`, `\Q`, `\E`, `\b`,
		"(", ")", "(?", "(?:", "(?i)", "(?i:", "(?P<", ">", ":",
		"[", "[^", "]", "-", "^", "$", ".", "|",
		"*", "+", "?", "{", "}", "{2,3}", ",",
	}
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(0, 24).Draw(rt, "n")
		var b strings.Builder
		for range n {
			b.WriteString(rapid.SampledFrom(alphabet).Draw(rt, "tok"))
		}
		src := b.String()
		assertCoverage(t, src, regexhighlight.Highlight(src))
		assertCoverage(t, src, regexhighlight.HighlightLines(src))
	})
}

// TestCoverageInvariantOverArbitraryBytes covers the case the alphabet
// generator cannot reach: invalid UTF-8. Coverage must still be total —
// the layouter is fed bytes, not runes.
func TestCoverageInvariantOverArbitraryBytes(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		src := string(rapid.SliceOfN(rapid.Byte(), 0, 32).Draw(rt, "bytes"))
		assertCoverage(t, src, regexhighlight.Highlight(src))
		assertCoverage(t, src, regexhighlight.HighlightLines(src))
	})
}

// ---------------------------------------------------------------------------
// Oracles — the lexer's structural claims, checked against Go's regexp
// rather than against itself.
// ---------------------------------------------------------------------------

// oracleAtoms builds random patterns out of whole constructs, so a
// meaningful share of the draws actually compile and the oracle has
// something to say about them.
var oracleAtoms = []string{
	"a", "é", ".", "|", "x",
	"(", ")", "(?:", "(?i)", "(?i:", "(?s:", "(?P<n>", "(?<m>",
	"[a-z]", "[^a]", "[]]", `\d`, `\w`, `\p{Greek}`, `\x{41}`, `\.`, `\Q`, `\E`,
	"*", "+", "?", "{2}", "{2,3}", "{,3}", "^", "$", `\b`, "}", "]",
}

func drawPattern(rt *rapid.T, maxAtoms int) (src string) {
	n := rapid.IntRange(0, maxAtoms).Draw(rt, "n")
	var b strings.Builder
	for range n {
		b.WriteString(rapid.SampledFrom(oracleAtoms).Draw(rt, "atom"))
	}
	src = b.String()
	return
}

// countCapturingOpeners counts the group openers the lexer classified as
// *capturing*: a bare `(`, `(?P<`, or `(?<`. A non-capturing `(?:` or a
// flag group `(?i:` opens a group but captures nothing, and a flag
// setting `(?i)` opens nothing at all.
func countCapturingOpeners(spans []regexhighlight.Span) (n int) {
	for _, s := range spans {
		if s.Category != regexhighlight.CategoryGroup {
			continue
		}
		switch s.Text {
		case "(", "(?P<", "(?<":
			n++
		}
	}
	return
}

// TestCaptureCountMatchesRegexp is the strongest independent check on the
// group classification: for any pattern that compiles, the number of
// capturing openers the lexer saw must equal re.NumSubexp(). Nothing in
// the lexer computes that number, so this cannot pass by construction —
// it catches `(?i)` counted as a group, `(?:` counted as a capture, and
// a named group missed.
func TestCaptureCountMatchesRegexp(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		src := drawPattern(rt, 14)
		re, err := regexp.Compile(src)
		if err != nil {
			return // only compiling patterns have a defined answer
		}
		if got, want := countCapturingOpeners(regexhighlight.Highlight(src)), re.NumSubexp(); got != want {
			rt.Fatalf("pattern %q: lexer saw %d capturing group(s), regexp says %d", src, got, want)
		}
	})
}

// TestDepthIsBalancedForCompilingPatterns — a pattern that compiles is
// balanced, so depth never goes negative, never steps by more than one
// between adjacent spans, and returns to 0 at the end.
func TestDepthIsBalancedForCompilingPatterns(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		src := drawPattern(rt, 14)
		if _, err := regexp.Compile(src); err != nil {
			return
		}
		spans := regexhighlight.Highlight(src)
		if len(spans) == 0 {
			return
		}
		prev := int32(0)
		for i, s := range spans {
			if s.Depth < 0 {
				rt.Fatalf("pattern %q span %d (%q): negative depth %d", src, i, s.Text, s.Depth)
			}
			if d := s.Depth - prev; d > 1 || d < -1 {
				rt.Fatalf("pattern %q span %d (%q): depth stepped %d -> %d", src, i, s.Text, prev, s.Depth)
			}
			prev = s.Depth
		}
		if last := spans[len(spans)-1]; last.Depth != 0 {
			rt.Fatalf("pattern %q compiles but ends at depth %d", src, last.Depth)
		}
	})
}

// TestCompilingPatternsNeverPaintAnErrorProperty is the property-test
// counterpart of the fixed corpus below: CategoryError claims only two
// byte-level certainties, and neither can occur in a pattern Go accepts.
func TestCompilingPatternsNeverPaintAnErrorProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		src := drawPattern(rt, 12)
		if _, err := regexp.Compile(src); err != nil {
			return
		}
		for _, s := range regexhighlight.Highlight(src) {
			if s.Category == regexhighlight.CategoryError {
				rt.Fatalf("pattern %q compiles but span %q was painted as an error", src, s.Text)
			}
		}
	})
}

// TestValidPatternsNeverPaintAnError pins the authority split: Go's
// regexp decides validity (ADR-0054), and CategoryError claims only two
// byte-level certainties. Neither can occur in a pattern that compiles,
// so a compiling pattern that the lexer paints red is a lexer bug.
func TestValidPatternsNeverPaintAnError(t *testing.T) {
	patterns := []string{
		`^\d{3}-\d{4}$`,
		`(?i)hello|(?:wor)ld`,
		`(?P<year>\d{4})-(?P<month>\d{2})`,
		`[a-zA-Z_][a-zA-Z0-9_]*`,
		`a(b(c(d)e)f)g`,
		`\Qliteral.dots\E`,
		`[[:alpha:][:digit:]]+`,
		`(?s:.*?)\z`,
		`\p{Greek}+\x{1F600}?`,
		`[]]|[^]]`,
		`a{,3}`,
		`(?i-s:x)`,
	}
	for _, p := range patterns {
		if _, err := regexp.Compile(p); err != nil {
			t.Fatalf("test corpus is wrong: %q does not compile: %v", p, err)
		}
		spans := regexhighlight.Highlight(p)
		assertCoverage(t, p, spans)
		for _, s := range spans {
			if s.Category == regexhighlight.CategoryError {
				t.Errorf("%q: span %q painted err, but the pattern compiles", p, s.Text)
			}
		}
		// A pattern that compiles is balanced, so depth must return to 0.
		if len(spans) > 0 {
			last := spans[len(spans)-1]
			if last.Depth != 0 {
				t.Errorf("%q: ends at depth %d, want 0", p, last.Depth)
			}
		}
	}
}
