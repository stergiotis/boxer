package markdownhighlight

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The invariants every caller depends on
// ---------------------------------------------------------------------------

// checkInvariants asserts what [HighlightLex] promises: ascending,
// non-overlapping spans covering every byte exactly once, all in range.
//
// Total coverage is the one that bites silently — a CodeViewJob does not
// gap-fill, and egui drops the glyphs of bytes no section claims, so a hole
// here is invisible text rather than uncoloured text.
func checkInvariants(t *testing.T, src string, spans []Span) {
	t.Helper()
	want := 0
	for i, s := range spans {
		if s.Start < 0 || s.Stop > int32(len(src)) {
			t.Fatalf("span %d out of range: %d..%d, len(src)=%d", i, s.Start, s.Stop, len(src))
		}
		if s.Stop <= s.Start {
			t.Fatalf("span %d is empty or inverted: %d..%d", i, s.Start, s.Stop)
		}
		if int(s.Start) != want {
			t.Fatalf("span %d starts at %d, want %d — %s", i, s.Start, want,
				map[bool]string{true: "gap (glyphs would be dropped)", false: "overlap"}[int(s.Start) > want])
		}
		if s.Category >= categoryMax {
			t.Fatalf("span %d has out-of-vocabulary category %d", i, s.Category)
		}
		want = int(s.Stop)
	}
	if want != len(src) {
		t.Fatalf("spans cover %d of %d bytes — the tail would render as dropped glyphs", want, len(src))
	}
}

// corpus is the set every invariant is checked against: ordinary documents,
// degenerate ones, and half-typed ones. The last group is the point of a lex
// tier — it is what a buffer holds between keystrokes.
var corpus = []struct {
	name string
	src  string
}{
	{"empty", ""},
	{"single newline", "\n"},
	{"blank lines", "\n\n\n"},
	{"plain prose", "just some words\nand a second line\n"},
	{"no trailing newline", "no newline at the end"},
	{"heading", "# Title\n\nbody\n"},
	{"all heading levels", "# a\n## b\n### c\n#### d\n##### e\n###### f\n"},
	{"heading with anchor", "## Creating a table {#creating-a-table}\n"},
	{"tag not heading", "#tag is not a heading\n"},
	{"setext-ish", "Title\n=====\n"},
	{"emphasis", "some *em* and **strong** and ***both***\n"},
	{"underscore emphasis", "_em_ and __strong__\n"},
	{"snake case is not emphasis", "call some_function_name(x) now\n"},
	{"strike and highlight", "~~gone~~ and ==marked==\n"},
	{"comment", "text %%hidden%% more\n"},
	{"code span", "a `code` span\n"},
	{"doubled code span", "a ``code with ` inside`` span\n"},
	{"fenced block", "```go\nfunc main() {}\n```\n"},
	{"fenced tilde", "~~~\nraw\n~~~\n"},
	{"unterminated fence", "```go\nfunc main() {}\n"},
	{"fence with markup inside", "```\n# not a heading\n**not strong**\n```\n"},
	{"list", "- one\n- two\n* three\n+ four\n"},
	{"ordered list", "1. one\n2. two\n10) ten\n"},
	{"task list", "- [ ] todo\n- [x] done\n- [X] also done\n"},
	{"blockquote", "> quoted\n> more\n"},
	{"nested blockquote", ">> deep\n"},
	{"callout", "> [!note] Title\n> body\n"},
	{"foldable callout", "> [!warning]- Folded\n> body\n"},
	{"thematic break", "---\n***\n___\n"},
	{"link", "a [label](https://example.com) link\n"},
	{"image", "an ![alt](pic.png) image\n"},
	{"autolink", "see <https://example.com> now\n"},
	{"bare angle is not a link", "a < b and c > d\n"},
	{"wikilink", "see [[Some Note]] there\n"},
	{"wikilink with alias", "see [[Some Note|shown]] there\n"},
	{"embed", "![[picture.png]]\n"},
	{"html", "<div class=\"x\">\ntext\n</div>\n"},
	{"table", "| a | b |\n|---|:--:|\n| 1 | 2 |\n"},
	{"table without leading pipe", "a | b\n--- | ---\n1 | 2\n"},
	{"pipe in prose", "the a | b case\n"},
	{"frontmatter", "---\ntitle: Test\ntags: [a, b]\n---\n\nbody\n"},
	{"frontmatter with list", "---\ntags:\n  - one\n  - two\n---\n"},
	{"frontmatter with url value", "---\nhome: https://example.com\n---\n"},
	{"unterminated frontmatter", "---\ntitle: Test\n"},
	{"dashes not frontmatter", "text\n---\nmore\n"},
	// Half-typed input — no parser accepts these, and the tier must not care.
	{"half-typed strong", "some **bold and then nothing\n"},
	{"half-typed code", "a `code and no close\n"},
	{"half-typed link", "a [label](http\n"},
	{"half-typed wikilink", "see [[Some No\n"},
	{"half-typed callout", "> [!no\n"},
	{"lone brackets", "[ ] ( ) [[ ]] ** __ ~~ == %%\n"},
	{"multibyte", "héllo wörld — em dash, ünicode ***bold***\n"},
	{"emoji", "🎉 party ***time*** 🎉\n"},
	{"tabs", "\t- indented with a tab\n"},
	{"windows newlines", "line one\r\nline two\r\n"},
}

func TestHighlightLex_Invariants(t *testing.T) {
	for _, tc := range corpus {
		t.Run(tc.name, func(t *testing.T) {
			checkInvariants(t, tc.src, HighlightLex([]byte(tc.src)))
		})
	}
}

// TestHighlightLex_InvariantsOnTruncations is the real stress on the tier: for
// one representative document, every prefix of it is a state the buffer passes
// through while being typed, and every one of them must still cover its bytes.
func TestHighlightLex_InvariantsOnTruncations(t *testing.T) {
	full := "---\ntitle: Doc\n---\n\n# Head\n\nSome **bold** and `code` and a [link](url).\n\n" +
		"- [ ] item with ==mark==\n\n> [!note] Callout\n> body\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n" +
		"```go\nfunc main() {}\n```\n"
	for n := 0; n <= len(full); n++ {
		src := full[:n]
		spans := HighlightLex([]byte(src))
		checkInvariants(t, src, spans)
	}
}

// ---------------------------------------------------------------------------
// Offsets index the SOURCE — the whole reason this tier exists
// ---------------------------------------------------------------------------

// spanOver returns the first span whose range is exactly the given substring
// occurrence, or nil.
func spanOver(src string, spans []Span, sub string) (s *Span) {
	at := strings.Index(src, sub)
	if at < 0 {
		return nil
	}
	for i := range spans {
		if int(spans[i].Start) == at && int(spans[i].Stop) == at+len(sub) {
			return &spans[i]
		}
	}
	return nil
}

// catAt returns the category covering byte off.
func catAt(spans []Span, off int) (cat CategoryE, ok bool) {
	for _, s := range spans {
		if off >= int(s.Start) && off < int(s.Stop) {
			return s.Category, true
		}
	}
	return 0, false
}

// TestHighlightLex_OffsetsAreSourceOffsets is the difference from [Highlight]
// stated as a test: the input is deliberately NON-canonical (a `+` bullet,
// underscore emphasis, an over-long fence), all of which Highlight would
// rewrite — moving every offset after them. Here the offsets must land on the
// author's own bytes.
func TestHighlightLex_OffsetsAreSourceOffsets(t *testing.T) {
	// The emphasised word is deliberately one that occurs nowhere else on the
	// line, so spanOver cannot match a substring of the surrounding prose.
	src := "+ bullet holds _slanted_ prose\n"
	spans := HighlightLex([]byte(src))
	checkInvariants(t, src, spans)

	if s := spanOver(src, spans, "_"); s == nil || s.Category != CategoryEmphasisDelim {
		t.Fatalf("the opening `_` is not marked as an emphasis delimiter: %+v", s)
	}
	if s := spanOver(src, spans, "slanted"); s == nil || s.Category != CategoryEmphasisText {
		t.Fatalf("emphasis text not found at its source offset: %+v", s)
	}
	// And the canonical tier disagrees, which is exactly why both exist.
	canonical, _ := Highlight([]byte(src))
	if strings.Contains(canonical, "+ bullet") {
		t.Fatal("fixture drift: Highlight was expected to rewrite the `+` bullet")
	}
}

func TestHighlightLex_HeadingMarkerAndText(t *testing.T) {
	src := "## Some Title\n"
	spans := HighlightLex([]byte(src))
	checkInvariants(t, src, spans)

	if s := spanOver(src, spans, "##"); s == nil || s.Category != CategoryHeadingMarker {
		t.Fatalf("heading marker: %+v", s)
	}
	if s := spanOver(src, spans, "Some Title"); s == nil || s.Category != CategoryHeadingText {
		t.Fatalf("heading text: %+v", s)
	}
}

// TestHighlightLex_FenceBodyIsNotMarkdown pins the property that makes a code
// block readable: markup inside it is body, not markup.
func TestHighlightLex_FenceBodyIsNotMarkdown(t *testing.T) {
	src := "```\n# not a heading\n```\n"
	spans := HighlightLex([]byte(src))
	checkInvariants(t, src, spans)

	at := strings.Index(src, "# not a heading")
	cat, ok := catAt(spans, at)
	if !ok || cat != CategoryCodeBlockBody {
		t.Fatalf("hash inside a fence: got category %d ok=%v, want CategoryCodeBlockBody (%d)",
			cat, ok, CategoryCodeBlockBody)
	}
}

// TestHighlightLex_UnterminatedMarkersStayPlain is the behaviour that makes
// the tier usable while typing: an opener with no closer must not colour the
// rest of the line, because for a few keystrokes that is every opener.
func TestHighlightLex_UnterminatedMarkersStayPlain(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"strong", "text **and more words here\n"},
		{"code", "text `and more words here\n"},
		{"highlight", "text ==and more words here\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.src
			spans := HighlightLex([]byte(src))
			checkInvariants(t, src, spans)
			at := strings.Index(src, "more words")
			cat, ok := catAt(spans, at)
			if !ok || cat != CategoryPlain {
				t.Fatalf("text after an unterminated opener: got category %d, want CategoryPlain (%d)",
					cat, CategoryPlain)
			}
		})
	}
}

// TestHighlightLex_SnakeCaseIsNotEmphasis guards the underscore word-boundary
// rule. Identifiers are ordinary content in the prose this runs on, and
// italicising half a line on an underscore is worse than not colouring it.
func TestHighlightLex_SnakeCaseIsNotEmphasis(t *testing.T) {
	src := "call some_function_name(x) now\n"
	spans := HighlightLex([]byte(src))
	checkInvariants(t, src, spans)
	for _, s := range spans {
		if s.Category == CategoryEmphasisDelim || s.Category == CategoryEmphasisText {
			t.Fatalf("snake_case produced emphasis at %d..%d", s.Start, s.Stop)
		}
	}
}

func TestHighlightLex_TableHeaderNeedsTheDelimiterRow(t *testing.T) {
	src := "| a | b |\n|---|---|\n| 1 | 2 |\n"
	spans := HighlightLex([]byte(src))
	checkInvariants(t, src, spans)

	header, _ := catAt(spans, strings.Index(src, " a "))
	if header != CategoryTableHeaderText {
		t.Errorf("first row: got category %d, want CategoryTableHeaderText (%d)", header, CategoryTableHeaderText)
	}
	body, _ := catAt(spans, strings.Index(src, " 1 "))
	if body != CategoryTableCellText {
		t.Errorf("body row: got category %d, want CategoryTableCellText (%d)", body, CategoryTableCellText)
	}
	align, _ := catAt(spans, strings.Index(src, "---"))
	if align != CategoryTableAlign {
		t.Errorf("delimiter row: got category %d, want CategoryTableAlign (%d)", align, CategoryTableAlign)
	}
}

func TestHighlightLex_FrontmatterOnlyAtTheTop(t *testing.T) {
	top := "---\ntitle: Doc\n---\nbody\n"
	spans := HighlightLex([]byte(top))
	checkInvariants(t, top, spans)
	if s := spanOver(top, spans, "title"); s == nil || s.Category != CategoryFrontmatterKey {
		t.Fatalf("frontmatter key: %+v", s)
	}

	// The same three dashes further down are a thematic break, not metadata.
	mid := "body\n---\ntitle: Doc\n"
	spans = HighlightLex([]byte(mid))
	checkInvariants(t, mid, spans)
	at := strings.Index(mid, "---")
	cat, _ := catAt(spans, at)
	if cat != CategoryThematicBreak {
		t.Errorf("mid-document dashes: got category %d, want CategoryThematicBreak (%d)", cat, CategoryThematicBreak)
	}
}

func TestHighlightLex_CalloutHeader(t *testing.T) {
	src := "> [!note] Title\n> body\n"
	spans := HighlightLex([]byte(src))
	checkInvariants(t, src, spans)

	if s := spanOver(src, spans, "note"); s == nil || s.Category != CategoryCalloutType {
		t.Fatalf("callout type: %+v", s)
	}
	if cat, _ := catAt(spans, 0); cat != CategoryBlockquoteMarker {
		t.Errorf("leading `>`: got category %d, want CategoryBlockquoteMarker (%d)", cat, CategoryBlockquoteMarker)
	}
}

func TestHighlightLex_WikilinkAlias(t *testing.T) {
	src := "see [[Target|shown]] here\n"
	spans := HighlightLex([]byte(src))
	checkInvariants(t, src, spans)

	if s := spanOver(src, spans, "Target"); s == nil || s.Category != CategoryWikilinkTarget {
		t.Fatalf("wikilink target: %+v", s)
	}
	if s := spanOver(src, spans, "shown"); s == nil || s.Category != CategoryLinkLabel {
		t.Fatalf("wikilink alias: %+v", s)
	}
}

func TestHighlightLex_LinkParts(t *testing.T) {
	src := "a [label](https://example.com) link\n"
	spans := HighlightLex([]byte(src))
	checkInvariants(t, src, spans)

	if s := spanOver(src, spans, "label"); s == nil || s.Category != CategoryLinkLabel {
		t.Fatalf("link label: %+v", s)
	}
	if s := spanOver(src, spans, "https://example.com"); s == nil || s.Category != CategoryLinkUrl {
		t.Fatalf("link url: %+v", s)
	}
}

// TestHighlightLex_TextIsEmpty documents the deliberate difference from
// [Highlight]: the per-keystroke tier does not allocate a string per span.
func TestHighlightLex_TextIsEmpty(t *testing.T) {
	spans := HighlightLex([]byte("# Head\n\n**bold**\n"))
	for _, s := range spans {
		if s.Text != "" {
			t.Fatalf("span %d..%d carries Text %q; this tier leaves it empty", s.Start, s.Stop, s.Text)
		}
	}
}

// ---------------------------------------------------------------------------
// Cost
// ---------------------------------------------------------------------------

func benchDoc(sections int) []byte {
	var b strings.Builder
	b.WriteString("---\ntitle: Bench\ntags: [a, b]\n---\n\n")
	for range sections {
		b.WriteString("## Heading level two\n\n")
		b.WriteString("Some **bold** and *italic* prose with `inline code`, a [link](https://example.com) and a [[wikilink]].\n\n")
		b.WriteString("- first item\n- second item\n- [ ] third item\n\n")
		b.WriteString("```go\nfunc main() {\n\tprintln(\"hello\")\n}\n```\n\n")
		b.WriteString("> [!note] A callout\n> with a body line\n\n")
	}
	return []byte(b.String())
}

func BenchmarkHighlightLex(b *testing.B) {
	for _, n := range []int{1, 10, 50} {
		src := benchDoc(n)
		b.Run(sizeLabel(len(src)), func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for range b.N {
				_ = HighlightLex(src)
			}
		})
	}
}

// BenchmarkHighlightCanonical is the comparison that matters: the canonical
// tier is the one an editor cannot afford per keystroke.
func BenchmarkHighlightCanonical(b *testing.B) {
	for _, n := range []int{1, 10, 50} {
		src := benchDoc(n)
		b.Run(sizeLabel(len(src)), func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for range b.N {
				_, _ = Highlight(src)
			}
		})
	}
}

func sizeLabel(n int) (s string) {
	switch {
	case n < 1024:
		return "under1KB"
	case n < 8*1024:
		return "few-KB"
	default:
		return "tens-KB"
	}
}
