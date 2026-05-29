//go:build llm_generated_opus47

package gohighlight

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertHasCat asserts that some span with the given text has the given
// category. Useful when the same text appears multiple times in the source
// with the same expected classification (e.g. a type referenced repeatedly).
func assertHasCat(t *testing.T, spans []Span, text string, cat CategoryE) {
	t.Helper()
	for _, s := range spans {
		if s.Text == text && s.Category == cat {
			return
		}
	}
	t.Errorf("no span with text %q and category %v; got: %s", text, cat, dumpSpans(spans, text))
}

// assertNoCatExcept asserts that every span whose Text equals text has the
// given category — used to catch wrong refinements (e.g. a type-name that
// got mis-classified as identifier on one occurrence).
func assertAllCat(t *testing.T, spans []Span, text string, cat CategoryE) {
	t.Helper()
	count := 0
	for _, s := range spans {
		if s.Text != text {
			continue
		}
		count++
		assert.Equal(t, cat, s.Category, "span %q at %d expected %v, got %v", text, s.Start, cat, s.Category)
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
	case CategoryKeyword:
		s = "Keyword"
	case CategoryOperator:
		s = "Operator"
	case CategoryPunctuation:
		s = "Punctuation"
	case CategoryIdentifier:
		s = "Identifier"
	case CategoryPackageName:
		s = "PackageName"
	case CategoryTypeName:
		s = "TypeName"
	case CategoryFuncDecl:
		s = "FuncDecl"
	case CategoryFuncCall:
		s = "FuncCall"
	case CategoryFieldName:
		s = "FieldName"
	case CategoryBuiltin:
		s = "Builtin"
	case CategoryConstName:
		s = "ConstName"
	case CategoryLabel:
		s = "Label"
	case CategoryStringLit:
		s = "StringLit"
	case CategoryNumberLit:
		s = "NumberLit"
	case CategoryRuneLit:
		s = "RuneLit"
	case CategoryBoolLit:
		s = "BoolLit"
	case CategoryNilLit:
		s = "NilLit"
	case CategoryComment:
		s = "Comment"
	case CategoryDocComment:
		s = "DocComment"
	case CategoryImportPath:
		s = "ImportPath"
	case CategoryBuildTag:
		s = "BuildTag"
	default:
		s = "?"
	}
	return
}

// TestOffsetInvariant ensures every span's [Start,Stop) slice equals its Text.
// This is the load-bearing invariant for the CodeViewJob.Section consumer.
func TestOffsetInvariant(t *testing.T) {
	srcs := []string{
		`package main

import "fmt"

func main() {
	fmt.Println("hello", 42, 3.14)
}
`,
		`xx := 42 + "y"`, // parse-error case
		`package p

type T struct {
	X int
	Y string
}
`,
	}
	for _, src := range srcs {
		spans := Highlight(src)
		for _, s := range spans {
			require.Equalf(t, int32(len(s.Text)), s.Stop-s.Start,
				"span %q has Stop-Start=%d want %d", s.Text, s.Stop-s.Start, len(s.Text))
			require.Equalf(t, s.Text, src[s.Start:s.Stop],
				"span %q at [%d,%d) doesn't match source slice %q",
				s.Text, s.Start, s.Stop, src[s.Start:s.Stop])
		}
	}
}

func TestLexicalBaseline(t *testing.T) {
	// Test the lex pass directly so AST refinement (which can salvage
	// partial parses under parser.AllErrors) doesn't mask classification.
	spans := lexHighlight(`xx := 42 + "y" / 'q'`)

	assertHasCat(t, spans, "xx", CategoryIdentifier)
	assertHasCat(t, spans, ":=", CategoryOperator)
	assertHasCat(t, spans, "42", CategoryNumberLit)
	assertHasCat(t, spans, "+", CategoryOperator)
	assertHasCat(t, spans, `"y"`, CategoryStringLit)
	assertHasCat(t, spans, "/", CategoryOperator)
	assertHasCat(t, spans, "'q'", CategoryRuneLit)
}

// TestParseErrorFallback verifies that completely unparseable input still
// yields lex-classified spans (graceful degradation).
func TestParseErrorFallback(t *testing.T) {
	// Leading punctuation defeats the parser's package-decl recovery heuristic.
	src := `}{ 42 "x"`
	spans := Highlight(src)
	assertHasCat(t, spans, "42", CategoryNumberLit)
	assertHasCat(t, spans, `"x"`, CategoryStringLit)
}

func TestPackageImportFunc(t *testing.T) {
	src := `package mypkg

import "fmt"

func main() {
	fmt.Println("hi")
}
`
	spans := Highlight(src)

	assertHasCat(t, spans, "package", CategoryKeyword)
	assertHasCat(t, spans, "mypkg", CategoryPackageName)
	assertHasCat(t, spans, "import", CategoryKeyword)
	assertHasCat(t, spans, `"fmt"`, CategoryImportPath)
	assertHasCat(t, spans, "func", CategoryKeyword)
	assertHasCat(t, spans, "main", CategoryFuncDecl)
	assertHasCat(t, spans, "Println", CategoryFuncCall)
	assertHasCat(t, spans, `"hi"`, CategoryStringLit)
	assertHasCat(t, spans, "(", CategoryPunctuation)
	assertHasCat(t, spans, "{", CategoryPunctuation)
}

func TestStructDeclAndFields(t *testing.T) {
	src := `package p

import "io"

// Reader doc.
type Reader struct {
	src io.Reader
	buf []byte
}

const MaxSize = 1024

// Read doc.
func (inst *Reader) Read(p []byte) (n int, err error) {
	n = len(p)
	return
}

func New() *Reader {
	return &Reader{}
}
`
	spans := Highlight(src)

	// declarations
	assertAllCat(t, spans, "MaxSize", CategoryConstName)
	assertHasCat(t, spans, "1024", CategoryNumberLit)
	assertAllCat(t, spans, "Reader", CategoryTypeName)
	assertAllCat(t, spans, "Read", CategoryFuncDecl)
	assertAllCat(t, spans, "New", CategoryFuncDecl)

	// doc comments
	assertHasCat(t, spans, "// Reader doc.", CategoryDocComment)
	assertHasCat(t, spans, "// Read doc.", CategoryDocComment)

	// struct fields
	assertHasCat(t, spans, "src", CategoryFieldName)
	assertHasCat(t, spans, "buf", CategoryFieldName)

	// predeclared types
	assertAllCat(t, spans, "byte", CategoryTypeName)
	assertAllCat(t, spans, "int", CategoryTypeName)
	assertAllCat(t, spans, "error", CategoryTypeName)

	// builtin
	assertHasCat(t, spans, "len", CategoryBuiltin)

	// package qualifier inside a type expression
	assertHasCat(t, spans, "io", CategoryPackageName)
}

func TestCallVsFieldAccess(t *testing.T) {
	src := `package p

func f(x interface{ Method() int }) int {
	_ = struct{ Field int }{Field: 1}.Field
	return x.Method() + 1
}
`
	spans := Highlight(src)

	// "Method" appears twice: as an interface-method declaration (FuncDecl)
	// and as a call site (FuncCall). Both must show up.
	assertHasCat(t, spans, "Method", CategoryFuncDecl)
	assertHasCat(t, spans, "Method", CategoryFuncCall)
	// "Field" appears as struct field decl, composite-literal key, and
	// selector access — every occurrence is a field reference.
	assertAllCat(t, spans, "Field", CategoryFieldName)
}

func TestPredeclaredConstantsAndZero(t *testing.T) {
	src := `package p

const (
	A = iota
	B
	C
)

func f() {
	var ok = true
	var bad = false
	var p any = nil
	_, _, _ = ok, bad, p
}
`
	spans := Highlight(src)

	// const block names
	assertAllCat(t, spans, "A", CategoryConstName)
	assertAllCat(t, spans, "B", CategoryConstName)
	assertAllCat(t, spans, "C", CategoryConstName)
	// iota recognised even though it is not a literal token
	assertAllCat(t, spans, "iota", CategoryConstName)

	assertHasCat(t, spans, "true", CategoryBoolLit)
	assertHasCat(t, spans, "false", CategoryBoolLit)
	assertHasCat(t, spans, "nil", CategoryNilLit)
	assertAllCat(t, spans, "any", CategoryTypeName)
}

func TestGenerics(t *testing.T) {
	src := `package p

type Pair[K comparable, V any] struct {
	Key K
	Val V
}

func MakePair[K comparable, V any](k K, v V) Pair[K, V] {
	return Pair[K, V]{Key: k, Val: v}
}
`
	spans := Highlight(src)

	assertAllCat(t, spans, "Pair", CategoryTypeName)
	assertAllCat(t, spans, "K", CategoryTypeName)
	assertAllCat(t, spans, "V", CategoryTypeName)
	assertAllCat(t, spans, "comparable", CategoryTypeName)
	assertAllCat(t, spans, "any", CategoryTypeName)
	assertHasCat(t, spans, "Key", CategoryFieldName)
	assertHasCat(t, spans, "Val", CategoryFieldName)
	assertHasCat(t, spans, "MakePair", CategoryFuncDecl)
}

func TestBuildTagBeatsDocComment(t *testing.T) {
	src := `//go:build linux

// Package p does p.
package p
`
	spans := Highlight(src)

	// Build tag must remain BuildTag even if it sits in the package's doc group.
	assertHasCat(t, spans, "//go:build linux", CategoryBuildTag)
	assertHasCat(t, spans, "// Package p does p.", CategoryDocComment)
}

func TestPredeclaredConversionInCallPosition(t *testing.T) {
	// `int(x)` is a type conversion — `int` should be TypeName, not FuncCall
	// or Builtin. This exercises the predeclared-type branch of markCallFun.
	src := `package p

func f(x float64) int {
	return int(x)
}
`
	spans := Highlight(src)
	assertAllCat(t, spans, "int", CategoryTypeName)
}

func TestMethodChain(t *testing.T) {
	// obj.field.method() — `field` is FieldName, `method` is FuncCall.
	src := `package p

type S struct{ inner T }
type T struct{}

func (T) Run() {}

func f(s S) {
	s.inner.Run()
}
`
	spans := Highlight(src)
	assertHasCat(t, spans, "inner", CategoryFieldName)
	assertHasCat(t, spans, "Run", CategoryFuncCall)
}

func TestLabelStmt(t *testing.T) {
	src := `package p

func f() {
loop:
	for {
		break loop
	}
}
`
	spans := Highlight(src)
	assertAllCat(t, spans, "loop", CategoryLabel)
}

// --- HighlightLines coverage ---

func TestLineCount(t *testing.T) {
	cases := []struct {
		in   string
		want int32
	}{
		{"", 0},
		{"abc", 1},
		{"abc\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
		{"a\n\nc", 3},
	}
	for _, tc := range cases {
		assert.Equalf(t, tc.want, LineCount(tc.in), "LineCount(%q)", tc.in)
	}
}

func TestHighlightLinesBasic(t *testing.T) {
	src := `package p

func main() {
	println("hi")
}
`
	// 5 lines: 1 "package p", 2 blank, 3 "func main() {", 4 body, 5 "}"
	slice, spans := HighlightLines(src, 3, 4)

	require.Equal(t, "func main() {\n\tprintln(\"hi\")\n", slice)

	// Offset invariant: spans index into the returned slice.
	for _, s := range spans {
		require.Equalf(t, s.Text, slice[s.Start:s.Stop],
			"span %q at [%d,%d)", s.Text, s.Start, s.Stop)
	}

	// AST refinement survived clipping.
	assertHasCat(t, spans, "func", CategoryKeyword)
	assertHasCat(t, spans, "main", CategoryFuncDecl)
	assertHasCat(t, spans, "println", CategoryBuiltin)
	assertHasCat(t, spans, `"hi"`, CategoryStringLit)
}

func TestHighlightLinesClipsMultilineSpan(t *testing.T) {
	// Raw-string literal spans lines 3 and 4. Requesting only line 4
	// must produce a clipped string-lit span — not drop it entirely and
	// not pull in any of line 3.
	src := "package p\n\nvar x = `multi\nline`\n"
	//                        ^line 3       ^line 4

	slice, spans := HighlightLines(src, 4, 4)
	require.Equal(t, "line`\n", slice)

	// The clipped string-lit span should cover the leading "line`" of the slice.
	var found bool
	for _, s := range spans {
		if s.Category == CategoryStringLit {
			found = true
			require.Equal(t, int32(0), s.Start, "clipped lit should start at slice offset 0")
			require.Equal(t, "line`", s.Text)
			require.Equal(t, s.Text, slice[s.Start:s.Stop])
		}
	}
	require.True(t, found, "expected a clipped string-lit span on line 4")
}

func TestHighlightLinesClampsOutOfRange(t *testing.T) {
	src := "package p\n\nfunc f() {}\n"
	// totalLines = 3

	// firstLine 0 → clamped to 1; lastLine huge → clamped to 3.
	slice, _ := HighlightLines(src, 0, 999)
	require.Equal(t, src, slice)

	// firstLine past EOF → empty result.
	slice, spans := HighlightLines(src, 100, 200)
	require.Empty(t, slice)
	require.Empty(t, spans)

	// lastLine < firstLine → empty.
	slice, spans = HighlightLines(src, 3, 1)
	require.Empty(t, slice)
	require.Empty(t, spans)
}

func TestHighlightLinesPreservesWhitespaceCoverage(t *testing.T) {
	// Every byte of the returned slice must be covered by exactly one span,
	// otherwise the egui LayoutJob will drop bytes (the same bug that
	// motivated CategoryWhitespace in the first place).
	src := `package p

func f() {
	x := 1 + 2
	_ = x
}
`
	slice, spans := HighlightLines(src, 3, 5)

	covered := make([]bool, len(slice))
	for _, s := range spans {
		for i := s.Start; i < s.Stop; i++ {
			require.Falsef(t, covered[i],
				"byte %d covered twice by span %q (cat %v)", i, s.Text, s.Category)
			covered[i] = true
		}
	}
	for i, c := range covered {
		require.Truef(t, c, "byte %d (%q) of slice not covered by any span", i, string(slice[i]))
	}
}
