package markdownhighlight

import (
	"strings"
	"testing"
)

// The canonical form is lossy on syntax, not on content: an explicit
// `{#anchor}` is parsed off the heading text, so renderHeading has to write
// it back or a document round-tripped through the highlighter would lose the
// link target of every anchored section.
func TestHighlight_HeadingAnchorSurvivesCanonicalisation(t *testing.T) {
	canonical, spans := Highlight([]byte("## Creating a table {#creating-a-table}\n"))
	if !strings.Contains(canonical, "## Creating a table {#creating-a-table}") {
		t.Fatalf("canonical form dropped the anchor: %q", canonical)
	}

	var anchorSpan *Span
	for i := range spans {
		if spans[i].Text == "creating-a-table" {
			anchorSpan = &spans[i]
			break
		}
	}
	if anchorSpan == nil {
		t.Fatal("no span covers the anchor — a span-less range renders uncoloured")
	}
	if anchorSpan.Category != CategoryLinkUrl {
		t.Errorf("anchor category: got %d want CategoryLinkUrl (%d)", anchorSpan.Category, CategoryLinkUrl)
	}
}

// A heading whose braces do not terminate the line keeps them as text, and
// the canonical form must not promote them to an anchor.
func TestHighlight_NonTerminatingAnchorStaysText(t *testing.T) {
	canonical, _ := Highlight([]byte("## Creating a table {#creating-a-table}.\n"))
	if !strings.Contains(canonical, "{#creating-a-table}.") {
		t.Errorf("canonical form altered literal braces: %q", canonical)
	}
}

// Spans must tile the canonical text with no gaps: CodeView drops the glyphs
// of any byte range no span covers, so an emit that forgets its span is a
// hole in the rendered document rather than an uncoloured stretch.
func TestHighlight_SpansCoverEveryByte(t *testing.T) {
	src := "# Title {#title}\n\nbody with *emphasis* and [a link](https://example.com)\n\n" +
		"- item\n\n```go\nprintln(\"hi\")\n```\n"
	canonical, spans := Highlight([]byte(src))
	next := int32(0)
	for i := range spans {
		if spans[i].Start != next {
			t.Fatalf("span[%d] starts at %d, leaving %q uncovered", i, spans[i].Start, canonical[next:spans[i].Start])
		}
		next = spans[i].Stop
	}
	if int(next) != len(canonical) {
		t.Errorf("spans stop at %d of %d bytes, leaving %q uncovered", next, len(canonical), canonical[next:])
	}
}
