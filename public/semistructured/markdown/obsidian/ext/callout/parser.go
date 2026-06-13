package callout

import (
	"bytes"
	"math"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// transformer converts blockquote nodes whose first line matches [!type]
// into CalloutNode AST nodes. This runs after the standard blockquote parser.
type transformer struct{}

func NewTransformer() parser.ASTTransformer {
	return &transformer{}
}

func (inst *transformer) Transform(node *ast.Document, reader text.Reader, pc parser.Context) {
	var blockquotes []*ast.Blockquote
	ast.Walk(node, func(n ast.Node, entering bool) (status ast.WalkStatus, err error) {
		if !entering {
			status = ast.WalkContinue
			return
		}
		bq, ok := n.(*ast.Blockquote)
		if ok {
			blockquotes = append(blockquotes, bq)
			status = ast.WalkSkipChildren
			return
		}
		status = ast.WalkContinue
		return
	})

	source := reader.Source()
	for _, bq := range blockquotes {
		inst.tryConvert(bq, source)
	}
}

func (inst *transformer) tryConvert(bq *ast.Blockquote, source []byte) {
	// The first child of a blockquote should be a paragraph.
	firstChild := bq.FirstChild()
	if firstChild == nil {
		return
	}
	para, ok := firstChild.(*ast.Paragraph)
	if !ok {
		return
	}

	// Extract the raw text of the first line of the paragraph.
	firstLine := extractFirstLine(para, source)
	if firstLine == nil {
		return
	}

	// Check for [!type] pattern
	calloutType, title, foldable, defaultOpen, matched := parseCalloutHeader(firstLine)
	if !matched {
		return
	}

	// Build the callout node
	callout := &Node{
		CalloutType: calloutType,
		Title:       title,
		Foldable:    foldable,
		DefaultOpen: defaultOpen,
	}

	// Move all children from the blockquote to the callout.
	// Skip the first text segment of the first paragraph (the [!type] line).
	removeFirstLine(para, source)
	if para.ChildCount() == 0 && para.Lines().Len() == 0 {
		// The paragraph only contained the callout header — remove it
		bq.RemoveChild(bq, para)
	}

	for c := bq.FirstChild(); c != nil; {
		next := c.NextSibling()
		bq.RemoveChild(bq, c)
		callout.AppendChild(callout, c)
		c = next
	}

	// Replace the blockquote with the callout node in the tree.
	parent := bq.Parent()
	parent.ReplaceChild(parent, bq, callout)
}

func extractFirstLine(para *ast.Paragraph, source []byte) []byte {
	lines := para.Lines()
	if lines.Len() == 0 {
		return nil
	}
	seg := lines.At(0)
	return seg.Value(source)
}

func removeFirstLine(para *ast.Paragraph, source []byte) {
	lines := para.Lines()
	if lines.Len() <= 1 {
		// Remove all lines — paragraph becomes empty. We also need to
		// drop inline children: goldmark's parser ran inline parsing
		// *before* this ASTTransformer fires (parser.go:906), so the
		// paragraph's ast.Text / ast.Emphasis / ... children still
		// cover the original byte ranges. Without this strip, the
		// title text leaks into the callout body via stale children.
		para.SetLines(text.NewSegments())
		stripChildrenBefore(para, math.MaxInt)
		return
	}
	newLines := text.NewSegments()
	for i := 1; i < lines.Len(); i++ {
		newLines.Append(lines.At(i))
	}
	para.SetLines(newLines)

	// Same retroactive cleanup as the all-lines path above, scoped to
	// the bytes that were just stripped.
	bodyStart := newLines.At(0).Start
	stripChildrenBefore(para, bodyStart)
}

// stripChildrenBefore walks para's inline children and removes those
// whose byte coverage is fully at or before cutoff. ast.Text nodes are
// checked via Segment; container inlines (Emphasis, Link, CodeSpan,
// AutoLink, RawHTML, ...) are checked by recursing into their children
// and dropping the container if it no longer covers any post-cutoff
// content. Text nodes that straddle cutoff have their Segment.Start
// clipped forward.
func stripChildrenBefore(para *ast.Paragraph, cutoff int) {
	for c := para.FirstChild(); c != nil; {
		next := c.NextSibling()
		if nodeFullyBefore(c, cutoff) {
			para.RemoveChild(para, c)
		} else {
			clipNodeStart(c, cutoff)
		}
		c = next
	}
}

func nodeFullyBefore(n ast.Node, cutoff int) bool {
	switch v := n.(type) {
	case *ast.Text:
		return v.Segment.Stop <= cutoff
	case *ast.String:
		// Inline-injected raw bytes — no Segment to check. Conservative:
		// keep them.
		return false
	case *ast.RawHTML:
		segs := v.Segments
		if segs.Len() == 0 {
			return true
		}
		last := segs.At(segs.Len() - 1)
		return last.Stop <= cutoff
	default:
		// Container inline (Emphasis, Link, CodeSpan, AutoLink, ...) —
		// fully before cutoff iff every child is.
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			if !nodeFullyBefore(c, cutoff) {
				return false
			}
		}
		// All children before cutoff (or no children) → container can go.
		return n.FirstChild() != nil
	}
}

func clipNodeStart(n ast.Node, cutoff int) {
	switch v := n.(type) {
	case *ast.Text:
		if v.Segment.Start < cutoff && v.Segment.Stop > cutoff {
			v.Segment.Start = cutoff
		}
	default:
		// Recurse into container children — pre-cutoff sub-trees get
		// removed in place.
		for c := n.FirstChild(); c != nil; {
			next := c.NextSibling()
			if nodeFullyBefore(c, cutoff) {
				n.RemoveChild(n, c)
			} else {
				clipNodeStart(c, cutoff)
			}
			c = next
		}
	}
}

func parseCalloutHeader(line []byte) (calloutType []byte, title []byte, foldable bool, defaultOpen bool, matched bool) {
	line = bytes.TrimSpace(line)
	if len(line) < 4 || line[0] != '[' || line[1] != '!' {
		return
	}

	// Find closing ]
	closeBracket := bytes.IndexByte(line[2:], ']')
	if closeBracket < 0 {
		return
	}
	closeBracket += 2

	calloutType = bytes.ToLower(bytes.TrimSpace(line[2:closeBracket]))
	if len(calloutType) == 0 {
		return
	}

	rest := line[closeBracket+1:]

	// Check for foldable marker (- or +) immediately after ]
	if len(rest) > 0 {
		if rest[0] == '-' {
			foldable = true
			rest = rest[1:]
		} else if rest[0] == '+' {
			foldable = true
			defaultOpen = true
			rest = rest[1:]
		}
	}

	title = bytes.TrimSpace(rest)
	matched = true
	return
}
