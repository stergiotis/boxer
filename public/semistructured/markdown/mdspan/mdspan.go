// Package mdspan locates the parts of an Obsidian-flavoured markdown document
// as byte ranges, so a caller can read or replace one part without touching
// the rest: a section by its heading path, the frontmatter block, a callout,
// a code block (ADR-0282 §SD5).
//
// Ranges come from the parse, not from scanning for `#`: a `#` line inside a
// fenced block is not a heading, and a setext heading is one. The parse is
// the obsidian package's with every wired feature.
package mdspan

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"

	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/callout"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/embed"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/tag"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/wikilink"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
)

// Features is the parse configuration: every wired Obsidian feature.
const Features = obsidian.FeatureAll

// Heading is one heading and the ranges it governs.
type Heading struct {
	Level uint8
	// Text is the heading's visible text; Anchor its explicit `{#anchor}`,
	// if any. Slug is the fragment key: the anchor's when there is one.
	Text   string
	Anchor string
	Slug   string
	// Parent indexes the enclosing heading in Doc.Headings, -1 for none.
	Parent int
	// Line is the heading's first line.
	Line int
	// Lines is the heading itself: one line for ATX, the text and the
	// underline for setext.
	Lines Span
	// Section runs from the heading to the next heading of the same or a
	// higher level, or the end of the document.
	Section Span
	// Body is Section after the heading's own lines.
	Body Span
}

// Callout is an Obsidian callout block (`> [!type] title`).
type Callout struct {
	Type     string
	Title    string
	Foldable bool
	Line     int
	Span     Span
}

// CodeBlock is a fenced or indented code block.
type CodeBlock struct {
	Fenced bool
	Info   string
	Line   int
	Span   Span
	// IsClosed is false for a fence that runs to the end of its container
	// without a closing fence. Indented blocks are always closed.
	IsClosed bool
}

// Doc is a parsed document.
type Doc struct {
	Src   []byte
	Index *LineIndex
	// Root is the goldmark tree, for walks this package does not do.
	Root ast.Node
	// Frontmatter is the `---` block at the very top, delimiters included;
	// FrontmatterBody is the YAML between them. Both are empty without one.
	Frontmatter     Span
	FrontmatterBody Span
	Headings        []Heading
	Callouts        []Callout
	CodeBlocks      []CodeBlock
	// Literal holds the ranges where markdown syntax is not interpreted:
	// code blocks and code spans, in source order.
	Literal []Span
}

// Parse parses src. It never fails; malformed constructs parse as text the
// way goldmark reads them.
func Parse(src []byte) (doc *Doc) {
	gm := obsidian.New(obsidian.Options{Features: Features, Resolver: resolver.NoopResolver{}})
	pc := obsidian.NewParserContext()
	root := gm.Parser().Parse(text.NewReader(src), parser.WithContext(pc))
	doc = &Doc{Src: src, Index: NewLineIndex(src), Root: root}
	doc.Frontmatter, doc.FrontmatterBody = frontmatterSpans(src)
	w := walker{doc: doc}
	_ = ast.Walk(root, w.visit)
	doc.closeSections()
	return
}

// HasFrontmatter says whether the document opens with a frontmatter block.
func (inst *Doc) HasFrontmatter() bool { return !inst.Frontmatter.IsEmpty() }

// IsLiteral says whether off falls inside a code block or code span.
func (inst *Doc) IsLiteral(off int) bool {
	for _, s := range inst.Literal {
		if off < s.Start {
			return false
		}
		if off < s.End {
			return true
		}
	}
	return false
}

// HeadingPath is the texts of a heading's ancestors and itself, outermost
// first.
func (inst *Doc) HeadingPath(i int) (path []string) {
	for j := i; j >= 0; j = inst.Headings[j].Parent {
		path = append(path, inst.Headings[j].Text)
	}
	for l, r := 0, len(path)-1; l < r; l, r = l+1, r-1 {
		path[l], path[r] = path[r], path[l]
	}
	return
}

type walker struct {
	doc   *Doc
	stack []int
}

func (inst *walker) visit(n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	switch t := n.(type) {
	case *ast.Heading:
		inst.heading(t)
	case *callout.Node:
		inst.callout(t)
	case *ast.FencedCodeBlock:
		inst.fenced(t)
		return ast.WalkSkipChildren, nil
	case *ast.CodeBlock:
		inst.indented(t)
		return ast.WalkSkipChildren, nil
	case *ast.CodeSpan:
		inst.codeSpan(t)
		return ast.WalkSkipChildren, nil
	}
	return ast.WalkContinue, nil
}

func (inst *walker) heading(h *ast.Heading) {
	d := inst.doc
	start := blockStart(h, d.Index)
	first := d.Index.LineOf(start)
	last := first
	if ls := h.Lines(); ls != nil && ls.Len() > 0 {
		last = d.Index.LineOf(ls.At(ls.Len() - 1).Start)
	}
	if !isAtxLine(d.Src[d.Index.LineStart(first):]) {
		// Setext: the underline follows the text.
		last++
	}
	level := uint8(h.Level)
	for len(inst.stack) > 0 && d.Headings[inst.stack[len(inst.stack)-1]].Level >= level {
		inst.stack = inst.stack[:len(inst.stack)-1]
	}
	parent := -1
	if len(inst.stack) > 0 {
		parent = inst.stack[len(inst.stack)-1]
	}
	txt := PlainText(h, d.Src)
	hd := Heading{
		Level:  level,
		Text:   txt,
		Slug:   Slug(txt),
		Parent: parent,
		Line:   first,
		Lines:  d.Index.Lines(LineSpan{First: first, Last: last}),
	}
	if a, ok := obsidian.HeadingAnchor(h); ok {
		hd.Anchor = a
		hd.Slug = Slug(a)
	}
	inst.stack = append(inst.stack, len(d.Headings))
	d.Headings = append(d.Headings, hd)
}

// closeSections sets each heading's section end: the start of the next
// heading at the same or a higher level.
func (inst *Doc) closeSections() {
	for i := range inst.Headings {
		h := &inst.Headings[i]
		end := len(inst.Src)
		for j := i + 1; j < len(inst.Headings); j++ {
			if inst.Headings[j].Level <= h.Level {
				end = inst.Headings[j].Lines.Start
				break
			}
		}
		h.Section = Span{Start: h.Lines.Start, End: end}
		h.Body = Span{Start: min(h.Lines.End, end), End: end}
	}
}

func (inst *walker) callout(c *callout.Node) {
	d := inst.doc
	start := blockStart(c, d.Index)
	end := lastLineEnd(c, d.Index, start)
	d.Callouts = append(d.Callouts, Callout{
		Type:     string(c.CalloutType),
		Title:    string(c.Title),
		Foldable: c.Foldable,
		Line:     d.Index.LineOf(start),
		Span:     Span{Start: start, End: end},
	})
}

func (inst *walker) fenced(cb *ast.FencedCodeBlock) {
	d := inst.doc
	start := blockStart(cb, d.Index)
	open := d.Index.LineOf(start)
	last := open
	if ls := cb.Lines(); ls.Len() > 0 {
		last = d.Index.LineOf(ls.At(ls.Len() - 1).Start)
	}
	fenceLine := d.Src[d.Index.LineStart(open):d.Index.LineEnd(open)]
	ch, n := fenceOf(fenceLine)
	closed := false
	if last < d.Index.Count() {
		next := d.Src[d.Index.LineStart(last+1):d.Index.LineEnd(last+1)]
		if c2, n2 := fenceOf(next); c2 == ch && n2 >= n && isBlankAfterFence(next, ch) {
			closed = true
			last++
		}
	}
	info := ""
	if cb.Info != nil {
		info = string(cb.Info.Segment.Value(d.Src))
	}
	s := d.Index.Lines(LineSpan{First: open, Last: last})
	d.CodeBlocks = append(d.CodeBlocks, CodeBlock{Fenced: true, Info: info, Line: open, Span: s, IsClosed: closed})
	d.Literal = append(d.Literal, s)
}

func (inst *walker) indented(cb *ast.CodeBlock) {
	d := inst.doc
	ls := cb.Lines()
	if ls.Len() == 0 {
		return
	}
	s := Span{Start: d.Index.LineStart(d.Index.LineOf(ls.At(0).Start)), End: ls.At(ls.Len() - 1).Stop}
	d.CodeBlocks = append(d.CodeBlocks, CodeBlock{Line: d.Index.LineOf(s.Start), Span: s, IsClosed: true})
	d.Literal = append(d.Literal, s)
}

func (inst *walker) codeSpan(cs *ast.CodeSpan) {
	d := inst.doc
	start := cs.Pos()
	end := -1
	for c := cs.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			if start < 0 {
				start = t.Segment.Start
			}
			end = t.Segment.Stop
		}
	}
	if start < 0 || end < 0 {
		return
	}
	for end < len(d.Src) && d.Src[end] == '`' {
		end++
	}
	d.Literal = append(d.Literal, Span{Start: start, End: end})
}

// blockStart is the start of the line a block opens on.
func blockStart(n ast.Node, idx *LineIndex) int {
	off := n.Pos()
	if off < 0 {
		if ls := n.Lines(); ls != nil && ls.Len() > 0 {
			off = ls.At(0).Start
		} else {
			off = 0
		}
	}
	return idx.LineStart(idx.LineOf(off))
}

// lastLineEnd is the end of the last source line any descendant of n covers.
func lastLineEnd(n ast.Node, idx *LineIndex, floor int) int {
	last := floor
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if c.Type() == ast.TypeBlock {
			if ls := c.Lines(); ls != nil && ls.Len() > 0 {
				last = max(last, ls.At(ls.Len()-1).Start)
			}
			if p := c.Pos(); p > last {
				last = p
			}
		}
		return ast.WalkContinue, nil
	})
	return idx.LineEnd(idx.LineOf(last))
}

// isAtxLine says whether a line opens with up to three spaces and a `#`.
func isAtxLine(line []byte) bool {
	i := 0
	for i < 3 && i < len(line) && line[i] == ' ' {
		i++
	}
	return i < len(line) && line[i] == '#'
}

// fenceOf is a line's fence character and run length after the container
// prefixes (blockquote markers, indentation) a fence can sit behind; n is 0
// when the line is no fence.
func fenceOf(line []byte) (ch byte, n int) {
	line = stripContainerPrefix(line)
	if len(line) == 0 || (line[0] != '`' && line[0] != '~') {
		return
	}
	ch = line[0]
	for n < len(line) && line[n] == ch {
		n++
	}
	if n < 3 {
		return 0, 0
	}
	return
}

func isBlankAfterFence(line []byte, ch byte) bool {
	line = bytes.TrimLeft(stripContainerPrefix(line), string(ch))
	return len(bytes.TrimSpace(line)) == 0
}

func stripContainerPrefix(line []byte) []byte {
	for {
		t := bytes.TrimLeft(line, " \t")
		if len(t) > 0 && t[0] == '>' {
			line = t[1:]
			continue
		}
		return t
	}
}

// frontmatterSpans finds a `---` block that opens the source and closes on
// `---` or `...`.
func frontmatterSpans(src []byte) (all Span, body Span) {
	first, rest, ok := bytes.Cut(src, []byte("\n"))
	if !ok || string(bytes.TrimRight(first, " \t\r")) != "---" {
		return
	}
	off := len(first) + 1
	bodyStart := off
	for len(rest) > 0 {
		line, tail, _ := bytes.Cut(rest, []byte("\n"))
		lineEnd := off + len(line)
		if lineEnd < len(src) {
			lineEnd++
		}
		t := string(bytes.TrimRight(line, " \t\r"))
		if t == "---" || t == "..." {
			return Span{Start: 0, End: lineEnd}, Span{Start: bodyStart, End: off}
		}
		off = lineEnd
		rest = tail
	}
	return
}

// PlainText flattens an inline subtree: text in order, and the visible label
// of the Obsidian inline nodes that keep their text in a field.
func PlainText(n ast.Node, src []byte) string {
	var sb strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			sb.Write(t.Segment.Value(src))
			if t.SoftLineBreak() {
				sb.WriteByte(' ')
			}
		case *ast.String:
			sb.Write(t.Value)
		case *wikilink.Node:
			sb.WriteString(t.DisplayText())
		case *embed.Node:
			sb.Write(t.Target)
		case *tag.Node:
			sb.WriteByte('#')
			sb.Write(t.Tag)
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(sb.String())
}

// Slug is the fragment key for a heading text: lower-cased, spaces to
// hyphens — the convention of mdextract.Slug and the markdown widget.
func Slug(s string) string {
	return strings.ReplaceAll(strings.ToLower(s), " ", "-")
}
