package mdspan

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

const sample = `---
title: x
tags: [a]
---
# Top

intro

## Design

text

` + "```sh\n# not a heading\n```" + `

### Goals

g

## Plan
p

Sub
---
s
`

func sectionText(d *Doc, i int) string {
	s := d.Headings[i].Section
	return string(d.Src[s.Start:s.End])
}

func TestParseHeadingsAndSections(t *testing.T) {
	d := Parse([]byte(sample))
	require.True(t, d.HasFrontmatter())
	assert.Equal(t, "---\ntitle: x\ntags: [a]\n---\n", string(d.Src[d.Frontmatter.Start:d.Frontmatter.End]))
	assert.Equal(t, "title: x\ntags: [a]\n", string(d.Src[d.FrontmatterBody.Start:d.FrontmatterBody.End]))

	texts := make([]string, 0, len(d.Headings))
	for _, h := range d.Headings {
		texts = append(texts, h.Text)
	}
	// The `#` line in the fence is no heading; the setext Sub is one.
	assert.Equal(t, []string{"Top", "Design", "Goals", "Plan", "Sub"}, texts)
	assert.Equal(t, []string{"Top", "Design", "Goals"}, d.HeadingPath(2))
	assert.Equal(t, 5, d.Headings[0].Line)

	assert.Equal(t, "### Goals\n\ng\n\n", sectionText(d, 2))
	design := sectionText(d, 1)
	assert.Contains(t, design, "# not a heading")
	assert.Contains(t, design, "### Goals")
	assert.NotContains(t, design, "## Plan")

	sub := d.Headings[4]
	assert.Equal(t, uint8(2), sub.Level)
	assert.Equal(t, "Sub\n---\n", string(d.Src[sub.Lines.Start:sub.Lines.End]))
	assert.Equal(t, "s\n", string(d.Src[sub.Body.Start:sub.Body.End]))
	// Top's section runs to the end.
	assert.Equal(t, len(d.Src), d.Headings[0].Section.End)
}

func TestCodeBlocksAndLiteral(t *testing.T) {
	src := "a `x` b\n\n```go\nfmt\n```\n\n~~~\nopen\n"
	d := Parse([]byte(src))
	require.Len(t, d.CodeBlocks, 2)
	assert.True(t, d.CodeBlocks[0].IsClosed)
	assert.Equal(t, "go", d.CodeBlocks[0].Info)
	assert.Equal(t, "```go\nfmt\n```\n", string(d.Src[d.CodeBlocks[0].Span.Start:d.CodeBlocks[0].Span.End]))
	assert.False(t, d.CodeBlocks[1].IsClosed)
	assert.True(t, d.IsLiteral(3))
	assert.False(t, d.IsLiteral(0))
	assert.True(t, d.IsLiteral(len("a `x` b\n\n``")))
}

func TestCallout(t *testing.T) {
	src := "p\n\n> [!warning]- Careful\n> body\n> more\n\nafter\n"
	d := Parse([]byte(src))
	require.Len(t, d.Callouts, 1)
	c := d.Callouts[0]
	assert.Equal(t, "warning", c.Type)
	assert.Equal(t, "Careful", c.Title)
	assert.True(t, c.Foldable)
	assert.Equal(t, 3, c.Line)
	assert.Equal(t, "> [!warning]- Careful\n> body\n> more\n", string(d.Src[c.Span.Start:c.Span.End]))
}

func TestNoHeadingsNoFrontmatter(t *testing.T) {
	d := Parse([]byte("just text\n"))
	assert.Empty(t, d.Headings)
	assert.False(t, d.HasFrontmatter())
	d = Parse(nil)
	assert.Equal(t, 0, d.Index.Count())
	// An unterminated frontmatter is none.
	d = Parse([]byte("---\na: b\n"))
	assert.False(t, d.HasFrontmatter())
}

func TestFindHeading(t *testing.T) {
	src := "# A\n## Goals\n# B\n## goals\n### X\n"
	d := Parse([]byte(src))
	_, ok, cands := d.FindHeading([]string{"Goals"})
	assert.False(t, ok)
	assert.Len(t, cands, 2)
	i, ok, _ := d.FindHeading([]string{"b", "GOALS"})
	require.True(t, ok)
	assert.Equal(t, 3, i)
	i, err := d.ResolveHeading([]string{"## X"})
	require.NoError(t, err)
	assert.Equal(t, 4, i)
	_, err = d.ResolveHeading([]string{"A", "X"})
	assert.Error(t, err)
}

func TestLineIndex(t *testing.T) {
	src := []byte("ab\nçd\n\nx")
	idx := NewLineIndex(src)
	assert.Equal(t, 4, idx.Count())
	l, c := idx.Position(5) // after "ç"
	assert.Equal(t, 2, l)
	assert.Equal(t, 2, c)
	assert.Equal(t, 5, idx.Offset(2, 2))
	assert.Equal(t, Span{Start: 3, End: 7}, idx.Lines(LineSpan{First: 2, Last: 2}))
	assert.Equal(t, len(src), idx.LineEnd(4))
	assert.Equal(t, LineSpan{First: 2, Last: 3}, idx.LineSpanOf(Span{Start: 3, End: 8}))
}

func TestSplice(t *testing.T) {
	out, ch, err := Splice([]byte("a\nb\nc\n"), Span{Start: 2, End: 4}, []byte("x\ny\n"))
	require.NoError(t, err)
	assert.Equal(t, "a\nx\ny\nc\n", string(out))
	assert.Equal(t, LineSpan{First: 2, Last: 3}, ch)
	_, _, err = Splice([]byte("a"), Span{Start: 0, End: 2}, nil)
	assert.Error(t, err)
}

// Sections nest: every heading's section lies inside its parent's, and two
// sibling sections never overlap.
func TestSectionsNestProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(0, 12).Draw(t, "n")
		var src []byte
		for i := 0; i < n; i++ {
			lvl := rapid.IntRange(1, 4).Draw(t, "lvl")
			for j := 0; j < lvl; j++ {
				src = append(src, '#')
			}
			src = append(src, " h\n"...)
			if rapid.Bool().Draw(t, "body") {
				src = append(src, "text\n\n"...)
			}
		}
		d := Parse(src)
		require.Len(t, d.Headings, n)
		for i, h := range d.Headings {
			assert.LessOrEqual(t, h.Section.Start, h.Body.Start)
			assert.LessOrEqual(t, h.Body.Start, h.Section.End)
			if h.Parent >= 0 {
				p := d.Headings[h.Parent]
				assert.LessOrEqual(t, p.Section.Start, h.Section.Start)
				assert.LessOrEqual(t, h.Section.End, p.Section.End)
			}
			if i+1 < len(d.Headings) && d.Headings[i+1].Level <= h.Level {
				assert.Equal(t, d.Headings[i+1].Section.Start, h.Section.End)
			}
		}
	})
}

func TestSetFrontmatter(t *testing.T) {
	src := "---\n# kept comment\ntitle: Old # inline\ntags: [a]\ndraft: true\n---\nbody\n"
	out, ch, err := SetFrontmatter([]byte(src), map[string]any{"title": "New", "status": "open"}, []string{"draft"})
	require.NoError(t, err)
	s := string(out)
	assert.Contains(t, s, "# kept comment")
	assert.Contains(t, s, "title: New # inline")
	assert.Contains(t, s, "status: open")
	assert.NotContains(t, s, "draft")
	assert.Regexp(t, `(?s)^---\n.*title.*tags.*status.*\n---\nbody\n$`, s)
	assert.Equal(t, 1, ch.First)

	out, _, err = SetFrontmatter([]byte("body\n"), map[string]any{"tags": []any{"x", "z"}}, nil)
	require.NoError(t, err)
	assert.Equal(t, "---\ntags:\n  - x\n  - z\n---\nbody\n", string(out))

	out, _, err = SetFrontmatter([]byte("---\na: 1\n---\nbody\n"), nil, []string{"a"})
	require.NoError(t, err)
	assert.Equal(t, "body\n", string(out))

	_, _, err = SetFrontmatter([]byte("---\na: [\n---\n"), map[string]any{"b": 1}, nil)
	assert.Error(t, err)
}
