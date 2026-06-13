package jsonhighlight

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertHasCat asserts that some span with the given text has the given
// category — symmetric with the gohighlight assertion helpers so the
// test invariants line up across the two highlighters.
func assertHasCat(t *testing.T, spans []Span, text string, cat CategoryE) {
	t.Helper()
	for _, s := range spans {
		if s.Text == text && s.Category == cat {
			return
		}
	}
	t.Errorf("no span with text %q and category %v; got: %s", text, cat, dumpSpans(spans, text))
}

// assertAllCat asserts that every span whose Text equals text has the given
// category — used to catch wrong refinements (e.g. a key string that got
// classified as a value string on one occurrence).
func assertAllCat(t *testing.T, spans []Span, text string, cat CategoryE) {
	t.Helper()
	count := 0
	for _, s := range spans {
		if s.Text != text {
			continue
		}
		count++
		assert.Equalf(t, cat, s.Category, "span %q at %d expected %v, got %v", text, s.Start, cat, s.Category)
	}
	require.Greaterf(t, count, 0, "no span with text %q", text)
}

func dumpSpans(spans []Span, filterText string) (out string) {
	for _, s := range spans {
		if filterText != "" && s.Text != filterText {
			continue
		}
		out += " {"
		out += s.Text
		out += "→"
		out += catName(s.Category)
		out += "}"
	}
	if out == "" {
		out = "(no spans matched)"
	}
	return
}

func catName(c CategoryE) (s string) {
	switch c {
	case CategoryPlain:
		s = "Plain"
	case CategoryPunctuation:
		s = "Punctuation"
	case CategoryKey:
		s = "Key"
	case CategoryStringLit:
		s = "StringLit"
	case CategoryNumberLit:
		s = "NumberLit"
	case CategoryBoolLit:
		s = "BoolLit"
	case CategoryNullLit:
		s = "NullLit"
	case CategoryWhitespace:
		s = "Whitespace"
	default:
		s = "?"
	}
	return
}

// TestOffsetInvariant ensures every span's [Start,Stop) slice equals its Text.
// This is the load-bearing invariant for the CodeViewJob.Section consumer.
func TestOffsetInvariant(t *testing.T) {
	srcs := []string{
		`{"name":"alice","age":30}`,
		`[1, 2, 3, true, null, "x"]`,
		`{"a":{"b":[1,2]}}`,
		"{\n  \"k\": \"v\"\n}\n",
		`{"truncated":`, // parse-error case
		``,              // empty
		"   \n\t",       // whitespace-only
	}
	for _, src := range srcs {
		spans := Highlight(src)
		for _, s := range spans {
			require.Equalf(t, int32(len(s.Text)), s.Stop-s.Start,
				"src %q: span %q has Stop-Start=%d want %d", src, s.Text, s.Stop-s.Start, len(s.Text))
			require.Equalf(t, s.Text, src[s.Start:s.Stop],
				"src %q: span %q at [%d,%d) doesn't match source slice %q",
				src, s.Text, s.Start, s.Stop, src[s.Start:s.Stop])
		}
	}
}

// TestEveryByteCoveredOnce — every byte of src is covered by exactly one
// span. egui's LayoutJob drops bytes outside any LayoutSection, which is
// the same bug that motivated CategoryWhitespace in gohighlight.
func TestEveryByteCoveredOnce(t *testing.T) {
	srcs := []string{
		`{"name":"alice","age":30,"active":true,"score":null}`,
		"{\n  \"a\": [1, 2.5, -3e10],\n  \"b\": {\"nested\": false}\n}\n",
		`[]`,
		`{}`,
		`null`,
		`42`,
		`"hello"`,
		"\n\n  {  \"k\"  :  \"v\"  }  \n",
	}
	for _, src := range srcs {
		spans := Highlight(src)
		covered := make([]bool, len(src))
		for _, s := range spans {
			for i := s.Start; i < s.Stop; i++ {
				require.Falsef(t, covered[i],
					"src %q: byte %d covered twice by span %q (cat %v)", src, i, s.Text, s.Category)
				covered[i] = true
			}
		}
		for i, c := range covered {
			require.Truef(t, c, "src %q: byte %d (%q) not covered by any span", src, i, string(src[i]))
		}
	}
}

func TestKeyVsValueString(t *testing.T) {
	src := `{"name":"alice","city":"berlin"}`
	spans := Highlight(src)

	// Keys must be CategoryKey, never CategoryStringLit, even though both
	// are syntactically `"…"`.
	assertAllCat(t, spans, `"name"`, CategoryKey)
	assertAllCat(t, spans, `"city"`, CategoryKey)
	assertAllCat(t, spans, `"alice"`, CategoryStringLit)
	assertAllCat(t, spans, `"berlin"`, CategoryStringLit)
}

func TestStringInArrayIsValue(t *testing.T) {
	// Arrays don't have keys — every string in an array is a value, even
	// though a naive even/odd-position check would mis-flag the first one.
	src := `["a","b","c"]`
	spans := Highlight(src)

	assertAllCat(t, spans, `"a"`, CategoryStringLit)
	assertAllCat(t, spans, `"b"`, CategoryStringLit)
	assertAllCat(t, spans, `"c"`, CategoryStringLit)
}

func TestPrimitives(t *testing.T) {
	src := `{"i":42,"f":3.14,"e":1.5e-10,"neg":-7,"t":true,"f2":false,"n":null}`
	spans := Highlight(src)

	assertHasCat(t, spans, "42", CategoryNumberLit)
	assertHasCat(t, spans, "3.14", CategoryNumberLit)
	assertHasCat(t, spans, "1.5e-10", CategoryNumberLit)
	assertHasCat(t, spans, "-7", CategoryNumberLit)
	assertHasCat(t, spans, "true", CategoryBoolLit)
	assertHasCat(t, spans, "false", CategoryBoolLit)
	assertHasCat(t, spans, "null", CategoryNullLit)

	// Punctuation: braces and brackets always Punctuation.
	assertAllCat(t, spans, "{", CategoryPunctuation)
	assertAllCat(t, spans, "}", CategoryPunctuation)
}

func TestNestedObjectInArray(t *testing.T) {
	src := `[{"k":1},{"k":2}]`
	spans := Highlight(src)

	// Both inner "k" occurrences are object keys.
	assertAllCat(t, spans, `"k"`, CategoryKey)
	assertHasCat(t, spans, "1", CategoryNumberLit)
	assertHasCat(t, spans, "2", CategoryNumberLit)
}

func TestNestedArrayInObject(t *testing.T) {
	src := `{"items":[1,2,3]}`
	spans := Highlight(src)

	assertAllCat(t, spans, `"items"`, CategoryKey)
	// "1" / "2" / "3" inside the array are array values — no key/value
	// alternation applies because the parent is `[`, not `{`.
	assertHasCat(t, spans, "1", CategoryNumberLit)
	assertHasCat(t, spans, "2", CategoryNumberLit)
	assertHasCat(t, spans, "3", CategoryNumberLit)
}

func TestParseErrorFallback(t *testing.T) {
	// Truncated JSON: the prefix `{"k":"v",` decodes cleanly; the trailing
	// `xx?` is unparseable and should land in a single CategoryPlain span
	// (graceful degradation — we don't go blank).
	src := `{"k":"v",xx?`
	spans := Highlight(src)

	// Prefix is correctly classified.
	assertHasCat(t, spans, `"k"`, CategoryKey)
	assertHasCat(t, spans, `"v"`, CategoryStringLit)

	// Tail surfaces as Plain.
	var plainTails []string
	for _, s := range spans {
		if s.Category == CategoryPlain {
			plainTails = append(plainTails, s.Text)
		}
	}
	require.NotEmptyf(t, plainTails, "expected a CategoryPlain tail span; got: %s", dumpSpans(spans, ""))
	require.Truef(t, strings.Contains(plainTails[0], "xx?"),
		"expected the Plain tail to contain the unparseable suffix; got %q", plainTails[0])
}

func TestEmptyInput(t *testing.T) {
	require.Empty(t, Highlight(""))
}

func TestWhitespaceOnly(t *testing.T) {
	// PeekKind on whitespace-only input returns KindInvalid (no token);
	// the trailing-filler branch must still cover the whole slice.
	src := "  \n\t"
	spans := Highlight(src)
	require.Len(t, spans, 1)
	require.Equal(t, CategoryWhitespace, spans[0].Category)
	require.Equal(t, src, spans[0].Text)
}

func TestPrettyPrintedCoverage(t *testing.T) {
	// A pretty-printed document with mixed indentation. The classified
	// tokens must cover the structural bytes (`{` `}` `[` `]`) and the gap
	// fillers must absorb every comma, colon, space, tab and newline.
	src := `{
  "users": [
    {"id": 1, "name": "alice"},
    {"id": 2, "name": "bob"}
  ],
  "active": true
}`
	spans := Highlight(src)

	assertAllCat(t, spans, `"users"`, CategoryKey)
	assertAllCat(t, spans, `"id"`, CategoryKey)
	assertAllCat(t, spans, `"name"`, CategoryKey)
	assertAllCat(t, spans, `"active"`, CategoryKey)
	assertHasCat(t, spans, `"alice"`, CategoryStringLit)
	assertHasCat(t, spans, `"bob"`, CategoryStringLit)
	assertHasCat(t, spans, "true", CategoryBoolLit)

	// Coverage invariant — re-asserted here against this specific shape so
	// any regression in the gap-handling path lights this test up first.
	covered := make([]bool, len(src))
	for _, s := range spans {
		for i := s.Start; i < s.Stop; i++ {
			require.Falsef(t, covered[i], "byte %d double-covered by %q", i, s.Text)
			covered[i] = true
		}
	}
	for i, c := range covered {
		require.Truef(t, c, "byte %d (%q) uncovered", i, string(src[i]))
	}
}

func TestRootScalar(t *testing.T) {
	// JSON allows a bare scalar as a complete document.
	cases := []struct {
		src string
		cat CategoryE
	}{
		{`42`, CategoryNumberLit},
		{`"hi"`, CategoryStringLit},
		{`true`, CategoryBoolLit},
		{`false`, CategoryBoolLit},
		{`null`, CategoryNullLit},
	}
	for _, tc := range cases {
		spans := Highlight(tc.src)
		require.Lenf(t, spans, 1, "src %q produced %d spans, want 1", tc.src, len(spans))
		require.Equalf(t, tc.cat, spans[0].Category, "src %q: cat", tc.src)
		require.Equal(t, tc.src, spans[0].Text)
	}
}
