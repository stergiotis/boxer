package callout

import (
	"bytes"

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
		// Remove all lines — paragraph becomes empty
		para.SetLines(text.NewSegments())
		return
	}
	newLines := text.NewSegments()
	for i := 1; i < lines.Len(); i++ {
		newLines.Append(lines.At(i))
	}
	para.SetLines(newLines)
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
