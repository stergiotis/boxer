package markdownhighlight

import (
	"fmt"
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
	{"tags", "#project and #a/b/c and #kebab-tag here\n"},
	{"tag-shaped prose", "C#sharp, foo#bar, open question #4, issue #1158\n"},
	{"half-typed tag", "a #\n"},
	{"tag at end of buffer", "trailing #tag"},
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

// TestHighlightLex_Tag covers the marker/body split and the nested form.
func TestHighlightLex_Tag(t *testing.T) {
	src := "See #project/frontend and #kebab-tag today\n"
	spans := HighlightLex([]byte(src))
	checkInvariants(t, src, spans)

	if s := spanOver(src, spans, "project/frontend"); s == nil || s.Category != CategoryTagText {
		t.Fatalf("nested tag body: %+v", s)
	}
	if s := spanOver(src, spans, "kebab-tag"); s == nil || s.Category != CategoryTagText {
		t.Fatalf("hyphenated tag body: %+v", s)
	}
	if cat, ok := catAt(spans, strings.Index(src, "#project")); !ok || cat != CategoryTagMarker {
		t.Fatalf("tag marker: %v %v", cat, ok)
	}
}

// TestHighlightLex_TagRulesMatchTheParser is the agreement this tier owes the
// preview. It is a second reading of the same syntax, so the place it may
// differ is in what it declines to recognise — never in what it claims. Both
// of these would otherwise be false tags, and this repo's prose is full of
// them: `#` glued to a word, and the English "number four".
func TestHighlightLex_TagRulesMatchTheParser(t *testing.T) {
	src := "C#sharp, foo#bar, open question #4, issue #1158, and #real\n"
	spans := HighlightLex([]byte(src))
	checkInvariants(t, src, spans)

	for _, notATag := range []string{"sharp", "bar", "4", "1158"} {
		at := strings.Index(src, "#"+notATag)
		if at < 0 {
			t.Fatalf("fixture drift: %q not in %q", "#"+notATag, src)
		}
		if cat, ok := catAt(spans, at); ok && (cat == CategoryTagMarker || cat == CategoryTagText) {
			t.Errorf("#%s was claimed as a tag", notATag)
		}
	}
	if s := spanOver(src, spans, "real"); s == nil || s.Category != CategoryTagText {
		t.Fatalf("a real tag on the same line must still be claimed: %+v", s)
	}
}

// ---------------------------------------------------------------------------
// Cross-tier parity
//
// The two tiers are independent readings of the same syntax — the cost
// ADR-0178 records — so a rule added to one and not the other is exactly how
// the source pane and the preview come to disagree about a document. The only
// agreement test used to be "#tag exists", a document-level boolean: the tiers
// could already disagree about WHERE a construct sits without failing
// anything (rendering review §3.3). This compares them per category, by byte
// range.
//
// Comparing byte ranges at all needs one thing to hold: [Highlight]'s spans
// index the CANONICAL form it re-emits, not the input, so a case is only
// comparable when that form is byte-identical to its source. Every case below
// is chosen for that property and the test asserts it rather than skipping —
// a case that stops round-tripping stops being a test, and should say so.
// ---------------------------------------------------------------------------

// mergeAdjacent joins neighbouring spans of the same category.
//
// The tiers legitimately differ in GRANULARITY: the canonical tier emits one
// span per goldmark inline node, so a run of prose arrives as several Plain
// spans where the lexer emits one. That is not a disagreement about what a
// byte IS, which is what parity is about, so it is normalised away before
// comparing.
func mergeAdjacent(spans []Span) (out []Span) {
	out = make([]Span, 0, len(spans))
	for _, s := range spans {
		if n := len(out); n > 0 && out[n-1].Category == s.Category && out[n-1].Stop == s.Start {
			out[n-1].Stop = s.Stop
			continue
		}
		out = append(out, Span{Start: s.Start, Stop: s.Stop, Category: s.Category})
	}
	return
}

// rangesByCategory keys the merged spans by category, each as a printable
// range list, so a mismatch reports what moved rather than that something did.
func rangesByCategory(spans []Span) (m map[CategoryE]string) {
	parts := map[CategoryE][]string{}
	for _, s := range mergeAdjacent(spans) {
		parts[s.Category] = append(parts[s.Category], fmt.Sprintf("%d..%d", s.Start, s.Stop))
	}
	m = make(map[CategoryE]string, len(parts))
	for cat, r := range parts {
		m[cat] = strings.Join(r, ",")
	}
	return
}

// parityCase is one input the tiers must agree on, plus the categories where
// they are known to differ and why. The allowlist is per case AND per
// category, never blanket: a divergence nobody wrote down is a bug.
type parityCase struct {
	name   string
	src    string
	differ []CategoryE
	why    string
}

var parityCorpus = []parityCase{
	{name: "plain prose", src: "just some words\nand a second line\n"},
	{name: "heading", src: "# Title\n\nbody\n"},
	{name: "tag not heading", src: "#tag is not a heading\n"},
	{name: "tags", src: "#project and #a/b/c and #kebab-tag here\n"},
	{name: "tag-shaped prose", src: "C#sharp, foo#bar, open question #4, issue #1158\n"},
	{name: "half-typed tag", src: "a #\n"},
	{name: "strike and highlight", src: "~~gone~~ and ==marked==\n"},
	{name: "code span", src: "a `code` span\n"},
	{name: "fenced block", src: "```go\nfunc main() {}\n```\n"},
	{name: "fence hides markup", src: "```\n# not a heading\n**not strong**\n```\n"},
	{name: "link", src: "a [label](https://example.com) link\n"},
	{name: "autolink", src: "see <https://example.com> now\n"},
	{name: "bare angle is not a link", src: "a < b and c > d\n"},
	{name: "wikilink", src: "see [[Some Note]] there\n"},
	{name: "embed", src: "![[picture.png]]\n"},
	{name: "pipe in prose", src: "the a | b case\n"},
	{name: "snake case is not emphasis", src: "call some_function_name(x) now\n"},
	// Half-typed input is the lex tier's reason to exist: goldmark parses it
	// into whatever structure it can, the lexer leaves it plain, and the two
	// still have to land on the same categories.
	{name: "half-typed strong", src: "some **bold and then nothing\n"},
	{name: "half-typed code", src: "a `code and no close\n"},
	{name: "half-typed link", src: "a [label](http\n"},
	{name: "half-typed wikilink", src: "see [[Some No\n"},

	{
		name:   "nested emphasis",
		src:    "some *em* and **strong** and ***both***\n",
		differ: []CategoryE{CategoryPlain, CategoryStrongDelim, CategoryStrongText, CategoryEmphasisDelim},
		why: "`***x***` is one `*` plus one `**` and the tiers split the three " +
			"bytes the other way round: goldmark nests emphasis outside strong, " +
			"the lexer claims the longest delimiter first. Same text, same two " +
			"styles, delimiter bytes attributed differently.",
	},
	{
		name:   "multibyte around emphasis",
		src:    "héllo wörld — em dash, ünicode ***bold***\n",
		differ: []CategoryE{CategoryPlain, CategoryStrongDelim, CategoryStrongText, CategoryEmphasisDelim},
		why:    "the `***` split above, at multibyte offsets — nothing new.",
	},
	{
		name:   "emoji around emphasis",
		src:    "🎉 party ***time*** 🎉\n",
		differ: []CategoryE{CategoryPlain, CategoryStrongDelim, CategoryStrongText, CategoryEmphasisDelim},
		why:    "the `***` split above, at 4-byte offsets — nothing new.",
	},
	{
		name:   "blockquote",
		src:    "> quoted\n> more\n",
		differ: []CategoryE{CategoryWhitespace, CategoryBlockquoteMarker},
		why: "the space after `>` is part of the marker for the canonical tier " +
			"and whitespace for the lexer. A boundary convention, not a reading.",
	},
	{
		name:   "callout",
		src:    "> [!note] Title\n> body\n",
		differ: []CategoryE{CategoryPlain, CategoryWhitespace, CategoryBlockquoteMarker, CategoryCalloutMarker, CategoryCalloutType},
		why: "two things: the `>` boundary above, and the callout TITLE — the " +
			"canonical tier tints it as CalloutType, the lexer leaves it prose. " +
			"The title is author text, so the lexer's reading is the one that " +
			"should win; changing it moves colours in a shipped pane.",
	},
	{
		name:   "foldable callout",
		src:    "> [!warning]- Folded\n> body\n",
		differ: []CategoryE{CategoryPlain, CategoryWhitespace, CategoryBlockquoteMarker, CategoryCalloutMarker, CategoryCalloutType},
		why:    "as `callout` above; the fold marker changes nothing here.",
	},
	{
		name:   "half-typed callout",
		src:    "> [!no\n",
		differ: []CategoryE{CategoryWhitespace, CategoryBlockquoteMarker},
		why:    "the `>` boundary convention again.",
	},
	{
		name:   "heading with anchor",
		src:    "## Creating a table {#creating-a-table}\n",
		differ: []CategoryE{CategoryWhitespace, CategoryHeadingText},
		why: "the space before `{#…}` goes to the heading text for the lexer " +
			"and to whitespace for the canonical tier, which strips the anchor " +
			"out of the heading and knows where it starts.",
	},
	{
		name:   "wikilink with alias",
		src:    "see [[Some Note|shown]] there\n",
		differ: []CategoryE{CategoryLinkLabel, CategoryWikilinkPunct, CategoryWikilinkTarget},
		why: "the canonical tier claims `Some Note|shown` as one target; the " +
			"lexer splits target / `|` / label. The lexer is finer here, and it " +
			"is the tier the editor uses — the coarse one is the gap.",
	},
	{
		name:   "html block",
		src:    "<div class=\"x\">\ntext\n</div>\n",
		differ: []CategoryE{CategoryPlain, CategoryWhitespace, CategoryRawHtml},
		why: "the canonical tier paints the whole block RawHtml, tags and prose " +
			"alike; the lexer claims only the tags and leaves the inner text " +
			"plain. Consistent with the renderer, which drops the block " +
			"entirely — see markdown.Doc.Dropped.",
	},
	{
		name:   "frontmatter value",
		src:    "---\nhome: https://example.com\n---\n",
		differ: []CategoryE{CategoryFrontmatterDelim, CategoryFrontmatterValue},
		why:    "the space after `:` goes to the delimiter for one tier, to the value for the other.",
	},
}

// TestTiersAgreePerCategory is the widened parity net.
func TestTiersAgreePerCategory(t *testing.T) {
	for _, tc := range parityCorpus {
		t.Run(tc.name, func(t *testing.T) {
			canon, canonSpans := Highlight([]byte(tc.src))
			if canon != tc.src {
				t.Fatalf("case is no longer comparable: the canonical form differs from the source\n"+
					" src   = %q\n canon = %q\nEither pick an input that round-trips or drop the case",
					tc.src, canon)
			}
			lexSpans := HighlightLex([]byte(tc.src))
			checkInvariants(t, tc.src, lexSpans)

			lex, canonical := rangesByCategory(lexSpans), rangesByCategory(canonSpans)
			allowed := map[CategoryE]bool{}
			for _, c := range tc.differ {
				allowed[c] = true
			}
			cats := map[CategoryE]bool{}
			for c := range lex {
				cats[c] = true
			}
			for c := range canonical {
				cats[c] = true
			}
			for c := range cats {
				same := lex[c] == canonical[c]
				if allowed[c] {
					// A stale allowlist entry is as bad as a missing one: it
					// silences a category that has since come into agreement,
					// and nobody would notice it drifting apart again.
					if same {
						t.Errorf("category %d now agrees (%s) — drop it from the allowlist", c, lex[c])
					}
					continue
				}
				if !same {
					t.Errorf("category %d disagrees\n  lex   = [%s]\n  canon = [%s]\n"+
						"Either fix the tier that is wrong, or add the category to this case's "+
						"`differ` list WITH a reason", c, lex[c], canonical[c])
				}
			}
		})
	}
}

// TestParityCorpusExercisesTheVocabulary keeps the corpus honest about its
// reach. The review's finding was that 23 of the categories had no
// category-level assertion at all; this names the ones still outside the net,
// so growing it is a matter of reading a list rather than guessing.
//
// The uncovered set is not empty and cannot be: every construct listed below
// is one whose canonical form is NOT byte-identical to its source (list
// markers are normalised to `-`, `~~~` fences to ` ``` `, table alignment
// rows are re-padded, comment bodies are elided to `…`, task marks lose their
// trailing space), so the two tiers index different byte strings and no range
// comparison exists to make. Those are pinned by the per-tier tests instead.
//
// Asserted as an exact set rather than a count: a category that leaves the
// list is progress worth recording, and one that joins it is a construct that
// silently stopped being compared.
func TestParityCorpusExercisesTheVocabulary(t *testing.T) {
	// The categories no parity case can reach, and why.
	wantUncovered := map[CategoryE]string{
		CategoryListMarker:      "list markers normalise to `-` / `N.`",
		CategoryThematicBreak:   "a leading `---` is read as frontmatter by the canonical tier",
		CategoryCommentDelim:    "comment bodies are elided to `…`",
		CategoryCommentText:     "comment bodies are elided to `…`",
		CategoryTablePipe:       "tables are re-padded to a canonical grid",
		CategoryTableAlign:      "alignment rows are re-padded (`:--:` becomes `:---:`)",
		CategoryTableHeaderText: "tables are re-padded to a canonical grid",
		CategoryTableCellText:   "tables are re-padded to a canonical grid",
		CategoryTaskMark:        "`- [x] done` loses the space after the mark",
	}

	seen := map[CategoryE]bool{}
	for _, tc := range parityCorpus {
		for _, s := range HighlightLex([]byte(tc.src)) {
			seen[s.Category] = true
		}
		_, canonSpans := Highlight([]byte(tc.src))
		for _, s := range canonSpans {
			seen[s.Category] = true
		}
	}
	for c := range CategoryCount {
		cat := CategoryE(c)
		why, expectedUncovered := wantUncovered[cat]
		switch {
		case seen[cat] && expectedUncovered:
			t.Errorf("category %d is now compared across tiers — drop it from "+
				"wantUncovered (it was listed as %q)", c, why)
		case !seen[cat] && !expectedUncovered:
			t.Errorf("category %d is claimed by neither tier over the parity corpus: "+
				"add a case that exercises it, or list it in wantUncovered with a reason", c)
		}
	}
}

// TestHighlightLex_DeclaredNonGoalsStayPlain pins what the lex tier says it
// does NOT do. Each of these is a construct it cannot recognise without block
// context or a second pass, and its documented answer is to leave the text
// plain rather than guess — a lex tier that guesses colours a buffer wrongly
// while someone is typing into it, which is worse than leaving it uncoloured.
//
// These inputs are not in the parity corpus because the canonical tier
// REWRITES them (setext becomes ATX, reference links are inlined), so their
// spans index a different byte string and no range comparison is possible.
// This is the honest form of the comparison for them: the lexer claims
// nothing structural.
func TestHighlightLex_DeclaredNonGoalsStayPlain(t *testing.T) {
	cases := []struct {
		name      string
		src       string
		mustNotBe []CategoryE
	}{
		{
			name:      "setext heading",
			src:       "Title\n=====\n\nbody\n",
			mustNotBe: []CategoryE{CategoryHeadingMarker, CategoryHeadingText},
		},
		{
			name:      "indented code block",
			src:       "prose\n\n    indented code\n\nmore\n",
			mustNotBe: []CategoryE{CategoryCodeBlockBody, CategoryFenceDelim},
		},
		{
			name:      "reference link and its definition",
			src:       "see [label][ref] here\n\n[ref]: https://example.com\n",
			mustNotBe: []CategoryE{CategoryLinkUrl},
		},
		{
			name:      "emphasis across a line break",
			src:       "start **bold\nstill bold** end\n",
			mustNotBe: []CategoryE{CategoryStrongDelim, CategoryStrongText},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spans := HighlightLex([]byte(tc.src))
			checkInvariants(t, tc.src, spans)
			banned := map[CategoryE]bool{}
			for _, c := range tc.mustNotBe {
				banned[c] = true
			}
			for _, s := range spans {
				if banned[s.Category] {
					t.Errorf("the lexer claimed %q as category %d; this construct is a "+
						"declared non-goal and must stay plain",
						tc.src[s.Start:s.Stop], s.Category)
				}
			}
		})
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
