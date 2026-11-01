package obsidian

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// A trailing marker names its paragraph: the id lands in the `id` attribute
// the HTML renderer emits, and the marker leaves the text.
func TestBlockId_TrailingMarkerNamesTheParagraph(t *testing.T) {
	out := render(t, allFeatures(), "A paragraph that ends with its id. ^para-1\n")
	require.Contains(t, out, `<p id="para-1">A paragraph that ends with its id.</p>`)
}

// A line holding only a marker names the block above it and is dropped —
// the spelling for tables, code blocks and lists, whose last line cannot
// carry one.
func TestBlockId_LoneMarkerNamesTheBlockAbove(t *testing.T) {
	out := render(t, allFeatures(), "- one\n- two\n\n^list-1\n")
	require.Contains(t, out, `<ul id="list-1">`)
	require.NotContains(t, out, "^list-1")
}

// A marker on a list's last item names the list it closes; one on an
// earlier item names that item's text block alone. One node per id.
func TestBlockId_ListItemMarkers(t *testing.T) {
	src := []byte("- one ^first\n- two ^second\n")
	md := New(allFeatures())
	root := md.Parser().Parse(text.NewReader(src), parser.WithContext(parser.NewContext()))
	list := root.FirstChild()
	id, ok := BlockId(list)
	require.True(t, ok)
	require.Equal(t, "second", id)
	items := []string{}
	for li := list.FirstChild(); li != nil; li = li.NextSibling() {
		v, _ := BlockId(li.FirstChild())
		items = append(items, v)
	}
	require.Equal(t, []string{"first", ""}, items)
	// A quoted line's id lands on the quote, once.
	out := render(t, allFeatures(), "> quoted ^q\n")
	require.Contains(t, out, `<blockquote id="q">`)
	require.Equal(t, 1, strings.Count(out, `id="q"`))
}

// A marker under a heading, or one continuing the paragraph above it
// without a blank line, is text.
func TestBlockId_HeadingsAndContinuationsAreLeftAlone(t *testing.T) {
	out := render(t, allFeatures(), "# Title\n\n^h1\n\npara\n")
	require.Contains(t, out, `<h1>Title</h1>`)
	require.Contains(t, out, "^h1")
	out = render(t, allFeatures(), "line one\n^id\n")
	require.Contains(t, out, "^id")
	require.NotContains(t, out, `id="id"`)
}

// Not a marker: a caret word that does not end the line, a marker with no
// whitespace before it, a lone marker with nothing above it, and any of
// them with the feature off.
func TestBlockId_WhatIsLeftAlone(t *testing.T) {
	require.Contains(t, render(t, allFeatures(), "see ^ref below\n"), "see ^ref below")
	require.Contains(t, render(t, allFeatures(), "glued^ref\n"), "glued^ref")
	require.Contains(t, render(t, allFeatures(), "^orphan\n"), "^orphan")
	opts := allFeatures()
	opts.Features &^= FeatureBlockId
	require.Contains(t, render(t, opts, "kept ^id\n"), "kept ^id")
}

// The marker is stripped from the node, not from the source: the paragraph's
// lines still cover the marker, so a consumer mapping the block to its bytes
// keeps the id inside the block's range.
func TestBlockId_SourceRangeKeepsTheMarker(t *testing.T) {
	src := []byte("text ^abc\n")
	md := New(allFeatures())
	root := md.Parser().Parse(text.NewReader(src), parser.WithContext(parser.NewContext()))
	p, ok := root.FirstChild().(*ast.Paragraph)
	require.True(t, ok)
	lines := p.Lines()
	require.Equal(t, 1, lines.Len())
	seg := lines.At(0)
	require.Equal(t, "text ^abc", string(seg.Value(src)))
	last, ok := p.LastChild().(*ast.Text)
	require.True(t, ok)
	require.Equal(t, "text", string(last.Segment.Value(src)))
}
