package mdextract

import (
	"net/url"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"

	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/embed"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/highlight"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/tag"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/wikilink"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
)

// features is the parse configuration: every wired Obsidian feature. Comments
// are stripped by the parser, so nothing inside `%% … %%` is extracted — the
// same reading Obsidian gives them.
const features = obsidian.FeatureAll

// Extract reads src. It never fails: a frontmatter block that does not parse
// is reported on [Frontmatter.Err] and the body is extracted regardless.
func Extract(src []byte) (doc *Document) {
	gm := obsidian.New(obsidian.Options{Features: features, Resolver: resolver.NoopResolver{}})
	pc := obsidian.NewParserContext()
	root := gm.Parser().Parse(text.NewReader(src), parser.WithContext(pc))

	doc = &Document{}
	fm, raw := extractFrontmatter(pc)
	doc.Frontmatter = fm

	w := &walker{src: src, doc: doc, lines: lineStarts(src), section: -1}
	_ = ast.Walk(root, w.visit)

	doc.Title = firstHeadingText(doc.Headings)
	doc.Tags = append(doc.Tags, frontmatterTags(raw, uint64(len(doc.Tags)))...)
	return
}

// walker carries the state of one document walk.
type walker struct {
	src   []byte
	doc   *Document
	lines []int

	// section is the index of the heading most recently entered; items that
	// follow it in document order sit under it.
	section int
	// blockStart is the source offset of the nearest enclosing leaf block, the
	// fallback position for inline nodes that carry no text segment of their
	// own (a wikilink, a tag).
	blockStart int
	// stack tracks open headings for parent resolution: indices into
	// doc.Headings, strictly increasing in level.
	stack []int
}

func (inst *walker) visit(n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	if n.Type() == ast.TypeBlock {
		if ls := n.Lines(); ls != nil && ls.Len() > 0 {
			inst.blockStart = ls.At(0).Start
		}
	}
	switch t := n.(type) {
	case *ast.Text:
		inst.doc.Words += uint64(len(strings.Fields(string(t.Segment.Value(inst.src)))))
	case *ast.Heading:
		inst.heading(t)
	case *ast.FencedCodeBlock:
		inst.codeBlock(t)
	case *wikilink.Node:
		inst.link(Link{
			Kind:     LinkKindWikilink,
			Target:   string(t.Page),
			Fragment: string(t.Heading),
			Text:     string(t.Alias),
		}, n)
	case *embed.Node:
		inst.link(Link{
			Kind:     LinkKindEmbed,
			Target:   string(t.Target),
			Fragment: string(t.Heading),
		}, n)
	case *ast.Link:
		l := splitDestination(string(t.Destination))
		l.Kind = LinkKindInline
		l.Text = inst.plainText(n)
		inst.link(l, n)
	case *ast.Image:
		l := splitDestination(string(t.Destination))
		l.Kind = LinkKindImage
		l.Text = inst.plainText(n)
		inst.link(l, n)
	case *ast.AutoLink:
		l := splitDestination(string(t.URL(inst.src)))
		l.Kind = LinkKindAutolink
		inst.link(l, n)
	case *ast.Emphasis:
		style := EmphasisStyleItalic
		if t.Level >= 2 {
			style = EmphasisStyleBold
		}
		inst.emphasis(style, n)
	case *east.Strikethrough:
		inst.emphasis(EmphasisStyleStrikethrough, n)
	case *highlight.Node:
		inst.emphasis(EmphasisStyleHighlight, n)
	case *tag.Node:
		inst.doc.Tags = append(inst.doc.Tags, Tag{
			Ordinal: uint64(len(inst.doc.Tags)),
			Line:    inst.lineOf(inst.blockStart),
			Section: inst.section,
			Source:  TagSourceBody,
			Tag:     string(t.Tag),
		})
	}
	return ast.WalkContinue, nil
}

func (inst *walker) heading(h *ast.Heading) {
	idx := len(inst.doc.Headings)
	level := uint8(h.Level)
	for len(inst.stack) > 0 && inst.doc.Headings[inst.stack[len(inst.stack)-1]].Level >= level {
		inst.stack = inst.stack[:len(inst.stack)-1]
	}
	parent := -1
	var path []string
	if len(inst.stack) > 0 {
		parent = inst.stack[len(inst.stack)-1]
		p := inst.doc.Headings[parent]
		path = make([]string, 0, len(p.Path)+1)
		path = append(path, p.Path...)
		path = append(path, p.Text)
	}
	txt := inst.plainText(h)
	hd := Heading{
		Ordinal: uint64(idx),
		Line:    inst.lineOf(inst.blockStart),
		Level:   level,
		Text:    txt,
		Slug:    Slug(txt),
		Parent:  parent,
		Path:    path,
	}
	if anchor, ok := obsidian.HeadingAnchor(h); ok {
		hd.Anchor = anchor
		hd.Slug = Slug(anchor)
	}
	inst.doc.Headings = append(inst.doc.Headings, hd)
	inst.stack = append(inst.stack, idx)
	inst.section = idx
}

func (inst *walker) codeBlock(cb *ast.FencedCodeBlock) {
	var sb strings.Builder
	ls := cb.Lines()
	for i := 0; i < ls.Len(); i++ {
		seg := ls.At(i)
		sb.Write(seg.Value(inst.src))
	}
	// The fence line itself carries no segment. The info string sits on it;
	// failing that, the first content line is the one after it.
	var line uint64
	switch {
	case cb.Info != nil:
		line = inst.lineOf(cb.Info.Segment.Start)
	case ls.Len() > 0:
		line = inst.lineOf(ls.At(0).Start) - 1
	}
	info := ""
	if cb.Info != nil {
		info = string(cb.Info.Segment.Value(inst.src))
	}
	inst.doc.CodeBlocks = append(inst.doc.CodeBlocks, CodeBlock{
		Ordinal:  uint64(len(inst.doc.CodeBlocks)),
		Line:     line,
		Section:  inst.section,
		Language: string(cb.Language(inst.src)),
		Info:     info,
		Content:  sb.String(),
		Lines:    uint64(ls.Len()),
	})
}

func (inst *walker) link(l Link, n ast.Node) {
	l.Ordinal = uint64(len(inst.doc.Links))
	l.Line = inst.lineOf(inst.inlineStart(n))
	l.Section = inst.section
	inst.doc.Links = append(inst.doc.Links, l)
}

func (inst *walker) emphasis(style EmphasisStyleE, n ast.Node) {
	inst.doc.Emphases = append(inst.doc.Emphases, Emphasis{
		Ordinal: uint64(len(inst.doc.Emphases)),
		Line:    inst.lineOf(inst.inlineStart(n)),
		Section: inst.section,
		Style:   style,
		Text:    inst.plainText(n),
	})
}

// inlineStart is the offset of the first text segment under n, or the
// enclosing block's start when n carries none.
func (inst *walker) inlineStart(n ast.Node) (off int) {
	off = inst.blockStart
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := c.(*ast.Text); ok {
			off = t.Segment.Start
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	return
}

// plainText flattens n's inline subtree: text segments in order, and the
// visible label of the Obsidian inline nodes that carry their text in a field
// rather than in children.
func (inst *walker) plainText(n ast.Node) string {
	var sb strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			sb.Write(t.Segment.Value(inst.src))
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
	return sb.String()
}

// lineOf maps a source offset to its 1-based line.
func (inst *walker) lineOf(off int) uint64 {
	if off < 0 {
		return 0
	}
	return uint64(sort.SearchInts(inst.lines, off+1))
}

// lineStarts lists the offset every line begins at.
func lineStarts(src []byte) (starts []int) {
	starts = append(starts, 0)
	for i, b := range src {
		if b == '\n' && i+1 < len(src) {
			starts = append(starts, i+1)
		}
	}
	return
}

// Slug is the fragment key for a heading text: lower-cased, spaces to
// hyphens. It matches the imzero2 markdown widget's SlugHeading and the
// resolver.NoopResolver sanitiser, so a `[[page#Heading Text]]` written in one
// document meets the heading row written from another.
func Slug(text string) string {
	return strings.ReplaceAll(strings.ToLower(text), " ", "-")
}

// splitDestination separates an inline destination into target, fragment and
// the external flag. A non-URL target is percent-decoded, since markdown
// links to files with spaces are commonly written encoded.
func splitDestination(dest string) (l Link) {
	target, fragment, _ := strings.Cut(dest, "#")
	if u, err := url.Parse(dest); err == nil && u.Scheme != "" {
		l.External = true
	} else if decoded, derr := url.PathUnescape(target); derr == nil {
		target = decoded
	}
	l.Target = target
	l.Fragment = fragment
	return
}

func firstHeadingText(hs []Heading) string {
	for _, h := range hs {
		if h.Text != "" {
			return h.Text
		}
	}
	return ""
}
