// Package blockid parses Obsidian block identifiers (ADR-0347 §SD4): a
// trailing ` ^id` on the last line of a block names that block, so a link
// `[[note#^id]]` can reach it. The marker is stripped from the rendered
// text and recorded as the block's `id` attribute — the attribute
// goldmark's HTML renderer already emits on paragraphs and list items, and
// the one an explicit heading anchor lands in, so every consumer reads one
// key for "the fragment this block answers to".
//
// Two spellings, both Obsidian's:
//
//	A paragraph that ends with its id. ^para-1
//
//	| a table |
//	| ------- |
//
//	^table-1
//
// The first strips the marker off the paragraph's last text node. The id
// lands on the top-level block the paragraph closes when the paragraph is
// that block's last leaf (a list whose last item carries the marker, a
// blockquote's last line), otherwise on the paragraph itself — one node per
// id, so the HTML carries it once. The second is a paragraph holding nothing
// but a marker, blank lines on both sides so it is a paragraph of its own:
// it names the block above it and is removed from the tree. A `^id` line
// that continues the paragraph above it is that paragraph's text. A marker
// with nothing above it, one under a heading (a heading is named by its
// text or its `{#anchor}`), or one that does not end its line, is left as
// the text it is.
package blockid

import (
	"regexp"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Extender adds block ids to goldmark.
type Extender struct{}

func (inst *Extender) Extend(m goldmark.Markdown) {
	// Before the callout transformer (199): a marker inside a blockquote is
	// stripped while the paragraph is still where the parser put it, and
	// the callout then moves the stripped paragraph as any other.
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(NewTransformer(), 150),
	))
}

// NewTransformer is the AST pass; exported for composition in tests.
func NewTransformer() parser.ASTTransformer { return &transformer{} }

// attrId is the attribute the id is stored under — goldmark's own `id`, so
// the HTML renderer emits it and heading anchors share the key.
const attrId = "id"

// trailingMarker matches ` ^id` at the end of a line: whitespace, a caret,
// the id, optional trailing whitespace. The id alphabet is Obsidian's.
var trailingMarker = regexp.MustCompile(`[ \t]+\^([A-Za-z0-9-]+)[ \t]*$`)

// loneMarker matches a line that is nothing but an id.
var loneMarker = regexp.MustCompile(`^[ \t]*\^([A-Za-z0-9-]+)[ \t]*$`)

// Of returns the block id recorded on n, and whether there is one. A
// heading's explicit anchor lands in the same attribute; callers that tell
// the two apart do so by the node's kind.
func Of(n ast.Node) (id string, ok bool) {
	if n == nil {
		return
	}
	v, has := n.AttributeString(attrId)
	if !has {
		return
	}
	switch t := v.(type) {
	case []byte:
		id = string(t)
	case string:
		id = t
	default:
		return
	}
	ok = id != ""
	return
}

type transformer struct{}

func (inst *transformer) Transform(doc *ast.Document, reader text.Reader, pc parser.Context) {
	// Collect first, mutate after: a removal during the walk would skip the
	// sibling that follows it.
	var paras []ast.Node
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.(type) {
		case *ast.Paragraph, *ast.TextBlock:
			paras = append(paras, n)
		}
		return ast.WalkContinue, nil
	})
	src := reader.Source()
	for _, p := range paras {
		inst.apply(p, src)
	}
}

// apply reads one paragraph's last text node for a marker.
func (inst *transformer) apply(p ast.Node, src []byte) {
	last, ok := p.LastChild().(*ast.Text)
	if !ok {
		return
	}
	value := last.Segment.Value(src)
	// A paragraph that is only a marker names the block above it.
	if p.ChildCount() == 1 {
		if m := loneMarker.FindSubmatch(value); m != nil {
			prev := p.PreviousSibling()
			if prev == nil {
				return
			}
			if _, isHeading := prev.(*ast.Heading); isHeading {
				// A heading's `id` is its anchor; a marker under it would
				// rename the section and name no block.
				return
			}
			prev.SetAttributeString(attrId, m[1])
			if parent := p.Parent(); parent != nil {
				parent.RemoveChild(parent, p)
			}
			return
		}
	}
	loc := trailingMarker.FindSubmatchIndex(value)
	if loc == nil {
		return
	}
	id := value[loc[2]:loc[3]]
	// Cut the marker and the whitespace before it off the text node; the
	// line's bytes stay where they were, so a consumer mapping the block to
	// its source still sees the marker as part of the block.
	last.Segment = last.Segment.WithStop(last.Segment.Start + loc[0])
	if last.Segment.Len() == 0 {
		p.RemoveChild(p, last)
	}
	// The id names the top-level block the paragraph closes, when it does;
	// the paragraph itself otherwise.
	top, closes := p, true
	for top.Parent() != nil && top.Parent().Kind() != ast.KindDocument {
		if top.Parent().LastChild() != top {
			closes = false
		}
		top = top.Parent()
	}
	if closes {
		top.SetAttributeString(attrId, id)
	} else {
		p.SetAttributeString(attrId, id)
	}
}
