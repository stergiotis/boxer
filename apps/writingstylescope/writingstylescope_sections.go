package writingstylescope

import (
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// headingFeatures is the minimal obsidian feature set the splitter needs:
// frontmatter so a leading `---` block is recognised as metadata rather than
// prose, GFM because the corpus this app is aimed at is written in it, and
// heading anchors so a `{#anchor}` suffix does not end up in a section title.
// The renderer-facing features (wikilinks, embeds, callouts) are left off —
// they cost parse work and change no heading position.
const headingFeatures = obsidian.FeatureFrontmatter |
	obsidian.FeatureGFM |
	obsidian.FeatureHeadingAnchor

// Section is one heading's own prose: the span from that heading's line start
// to the next heading's line start, at whatever level the next heading sits
// (ADR-0175 §SD1). Sliced that way a section never contains a descendant
// section's text, so the same prose is never measured twice, and the sections
// that carry substantial text are the deep ones — which is the granularity the
// app compares at.
//
// The preamble (text before the first heading, after any frontmatter) is a
// Section with an empty Title and Level 0.
type Section struct {
	// Title is the heading's flattened plain text — inline styling dropped,
	// any `{#anchor}` suffix already stripped by the parser. Empty for the
	// preamble section.
	Title string
	// Level is the ATX heading depth, 1..6. Zero for the preamble.
	Level uint8
	// Start and End are byte offsets into the source the section was sliced
	// from; Text is that slice.
	Start int
	End   int
	Text  string
}

// Label names the section for an axis tick, a table cell, or a readout.
func (inst Section) Label() (label string) {
	if inst.Title == "" {
		return "(preamble)"
	}
	return inst.Title
}

// Bytes is the section's length in the source.
func (inst Section) Bytes() (n int) { return len(inst.Text) }

// splitSections slices src into one Section per heading, plus a leading
// preamble section when there is non-blank text before the first heading.
// Section boundaries land on the heading's line start, so a heading's `##`
// marker never trails into the previous section's body.
//
// The result is in document order and always covers a contiguous, non-
// overlapping run of src (from the end of the frontmatter to the end of the
// source), except that a blank preamble is omitted.
func splitSections(src string) (secs []Section) {
	heads := parseHeadings(src)
	docStart := frontmatterEnd(src)

	// Bounds are clamped monotonically: an offset that walked backwards
	// (which a well-formed parse never produces, but a slicer must not panic
	// on) folds into the previous bound rather than inverting a slice.
	bounds := make([]int, len(heads))
	prev := docStart
	for i, h := range heads {
		b := prev
		if h.offset >= 0 && h.offset <= len(src) {
			b = max(lineStart(src, h.offset), prev)
		}
		bounds[i] = b
		prev = b
	}

	firstBound := len(src)
	if len(bounds) > 0 {
		firstBound = bounds[0]
	}

	secs = make([]Section, 0, len(heads)+1)
	if firstBound > docStart && strings.TrimSpace(src[docStart:firstBound]) != "" {
		secs = append(secs, Section{
			Start: docStart,
			End:   firstBound,
			Text:  src[docStart:firstBound],
		})
	}
	for i, h := range heads {
		end := len(src)
		if i+1 < len(bounds) {
			end = bounds[i+1]
		}
		secs = append(secs, Section{
			Title: h.text,
			Level: h.level,
			Start: bounds[i],
			End:   end,
			Text:  src[bounds[i]:end],
		})
	}
	return
}

// keepAtLeast partitions secs into those whose text reaches minBytes and a
// count of those that did not. Below the floor a compression measurement is
// dominated by frame overhead rather than by content (ADR-0175 §SD1), and an
// empty nesting heading — a `## Part two` with nothing under it but its
// children's headings — is exactly the degenerate case this drops.
func keepAtLeast(secs []Section, minBytes int) (kept []Section, dropped int) {
	kept = make([]Section, 0, len(secs))
	for _, s := range secs {
		if len(s.Text) < minBytes {
			dropped++
			continue
		}
		kept = append(kept, s)
	}
	return
}

// headingRef is one heading as the parser reports it, before slicing.
type headingRef struct {
	text   string
	level  uint8
	offset int // byte offset of the heading text, -1 for a heading with none
}

// parseHeadings returns every top-level heading in document order. Only
// top-level AST children are considered, matching the markdown widget's own
// side table: a heading nested inside a blockquote or list item is not a
// document section.
func parseHeadings(src string) (heads []headingRef) {
	gm := obsidian.New(obsidian.Options{Features: headingFeatures})
	b := []byte(src)
	root := gm.Parser().Parse(text.NewReader(b), parser.WithContext(obsidian.NewParserContext()))
	for child := root.FirstChild(); child != nil; child = child.NextSibling() {
		h, ok := child.(*ast.Heading)
		if !ok {
			continue
		}
		offset := -1
		if h.Lines().Len() > 0 {
			offset = h.Lines().At(0).Start
		}
		heads = append(heads, headingRef{
			text:   headingPlainText(h, b),
			level:  uint8(h.Level),
			offset: offset,
		})
	}
	return
}

// headingPlainText flattens a heading's inline subtree to plain text, dropping
// bold/italic/code-span styling. Mirrors the markdown widget's helper of the
// same name so a section title reads the same here as in a rendered TOC.
func headingPlainText(h *ast.Heading, src []byte) (out string) {
	var sb strings.Builder
	_ = ast.Walk(h, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := n.(*ast.Text); ok {
			sb.Write(t.Segment.Value(src))
		}
		return ast.WalkContinue, nil
	})
	out = strings.TrimSpace(sb.String())
	return
}

// lineStart returns the offset of the first byte of the line containing
// offset. goldmark reports a heading's offset at its *text* (past the `## `
// marker); section regions want the whole heading line.
func lineStart(src string, offset int) (start int) {
	start = strings.LastIndexByte(src[:offset], '\n') + 1
	return
}

// frontmatterEnd returns the offset just past the closing `---` line of a
// leading YAML frontmatter block, or 0 when there is none. Frontmatter is
// document metadata, not authored prose: two repo documents sharing
// `type: adr` / `status: proposed` are not sharing writing.
func frontmatterEnd(src string) (end int) {
	rest, found := strings.CutPrefix(src, "---\n")
	if !found {
		if rest, found = strings.CutPrefix(src, "---\r\n"); !found {
			return
		}
	}
	for _, closer := range []string{"\n---\n", "\n---\r\n"} {
		if idx := strings.Index(rest, closer); idx >= 0 {
			candidate := len(src) - len(rest) + idx + len(closer)
			if end == 0 || candidate < end {
				end = candidate
			}
		}
	}
	return
}
