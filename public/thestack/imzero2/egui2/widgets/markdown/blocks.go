package markdown

import (
	"bytes"

	"github.com/yuin/goldmark/ast"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The per-block surface (ADR-0334 §SD2, §SD3): a block editor over the
// markdown SOURCE needs to know, for each top-level block, which bytes it
// came from, and to render one block on its own under an id scope the editor
// chooses. Everything here is derived at parse time from what goldmark
// already reports — a node's position and its descendants' line segments —
// so no second parser is involved and the ranges describe the bytes the
// author wrote, not a canonical form.

// BlockKindE is the kind of a top-level block as the lowering classifies it.
type BlockKindE uint8

const (
	BlockParagraph BlockKindE = iota
	BlockHeading
	BlockCodeBlock
	BlockList
	BlockBlockquote
	BlockHorizontalRule
	BlockCallout
	BlockTable
	// BlockFootnotes is the definitions block goldmark appends at the end of
	// the document; it has no contiguous source range (BlockInfo.Synthetic).
	BlockFootnotes
)

// BlockInfo describes one top-level block of a parsed document.
type BlockInfo struct {
	Kind BlockKindE
	// Start and Stop are the byte range of the block's source, snapped to
	// line boundaries and excluding the trailing newline, so that a document
	// is its blocks joined by the whitespace between them: everything in
	// [Stop, next.Start) is blank lines and newlines. Meaningless when
	// Synthetic.
	Start, Stop int
	// Synthetic marks a block the lowering made up rather than read.
	Synthetic bool
	// HeadingLevel is the level of a BlockHeading, 0 otherwise.
	HeadingLevel uint8
	// CodeLang is the normalised fence language of a BlockCodeBlock.
	CodeLang string
}

// Blocks lists the document's top-level blocks in order, one per segment
// [Render] would draw, with the source range each was lowered from. The
// slice is built per call; the index of an entry is what [RenderBlock]
// takes.
func (inst *Doc) Blocks() (out []BlockInfo) {
	out = make([]BlockInfo, len(inst.segments))
	prevStop := 0
	for i := range inst.segments {
		seg := &inst.segments[i]
		b := BlockInfo{
			Kind:         blockKindOf(seg.kind),
			Start:        seg.srcStart,
			Stop:         seg.srcStop,
			Synthetic:    seg.srcStop < seg.srcStart,
			HeadingLevel: seg.headingLevel,
			CodeLang:     seg.codeLang,
		}
		// The footnote definitions goldmark gathers at the end carry the
		// positions of the lines they were read from, so a definitions
		// block that sits at the end of the source is a real block. One
		// whose definitions are scattered through the document would
		// overlap earlier blocks; it is reported synthetic rather than as
		// a range the editor could splice twice.
		if !b.Synthetic && b.Start < prevStop {
			b.Synthetic = true
		}
		if !b.Synthetic {
			prevStop = b.Stop
		}
		out[i] = b
	}
	return
}

// FrontmatterSpan is the byte range of the YAML frontmatter block, opening
// and closing delimiter lines included and the newline after the closing
// delimiter excluded, and whether the source had one. The block is not a
// segment: the parser consumes it, so [Blocks] never lists it.
func (inst *Doc) FrontmatterSpan() (start, stop int, ok bool) {
	return inst.fmStart, inst.fmStop, inst.fmOk
}

// RenderBlock emits one top-level block — the segment at index into
// [Blocks] — into the current Ui scope, under an IdScope keyed by
// Input.ScopeKey. The caller varies the key per block: a block's embedded
// widgets take ids in order within that scope, so two blocks under one key
// would share them. The block renders as if it opened the document (no gap
// above a heading); the host owns the spacing between blocks. Scroll-to-
// section counts headings up to the block, so a slug still resolves to the
// right occurrence. An index out of range draws nothing.
func RenderBlock(in Input, index int) (res Result) {
	doc := in.Doc
	if doc == nil || in.Ids == nil || index < 0 || index >= len(doc.segments) {
		return
	}
	scopeKey := in.ScopeKey
	if scopeKey == "" {
		scopeKey = "markdown"
	}
	for range c.IdScope(in.Ids.PrepareStr(scopeKey)) {
		rc := renderCtx{
			ids:            in.Ids,
			imageMaxW:      doc.imageMaxW,
			imageMaxH:      doc.imageMaxH,
			imageVersion:   doc.imageVersion,
			images:         doc.images,
			scrollToSlug:   in.ScrollToSection,
			headings:       doc.headings,
			actionsEnabled: len(in.ActionLabels) > 0,
			actionLabels:   in.ActionLabels,
			linkClaims:     in.LinkClaims,
			actionAccept:   in.CodeActionFilter,
			actionMask:     in.CodeActionMask,
		}
		for i := 0; i < index; i++ {
			if doc.segments[i].kind == segKindHeading {
				rc.headingIdx++
			}
			rc.codeBlockIdx += countCodeBlocks(&doc.segments[i])
		}
		doc.segments[index].render(&rc)
		res.Actions = rc.codeActions
		res.Links = rc.links
	}
	return
}

func blockKindOf(k segKindE) BlockKindE {
	switch k {
	case segKindHeading:
		return BlockHeading
	case segKindCodeBlock:
		return BlockCodeBlock
	case segKindList:
		return BlockList
	case segKindBlockquote:
		return BlockBlockquote
	case segKindHorizontalRule:
		return BlockHorizontalRule
	case segKindCallout:
		return BlockCallout
	case segKindTable:
		return BlockTable
	case segKindFootnotes:
		return BlockFootnotes
	default:
		return BlockParagraph
	}
}

// countCodeBlocks is how many code blocks a full [Render] would have
// counted by the time it finished seg: the segment itself and any nested
// in a list item or a quote, the order the action ordinals are issued in.
func countCodeBlocks(seg *segment) (n int) {
	if seg.kind == segKindCodeBlock {
		n++
	}
	for i := range seg.children {
		n += countCodeBlocks(&seg.children[i])
	}
	return
}

// blockRange is the source range of a top-level block node: from the start
// of the line its position (or first line) is on, to the end of the last
// line any block descendant covers, newline excluded. The same recipe
// mdspan uses (ADR-0282 §SD5). A node with no position and no lines — the
// synthetic footnote list — reports an inverted range.
func blockRange(n ast.Node, src []byte) (start, stop int) {
	off := n.Pos()
	if off < 0 {
		if ls := n.Lines(); ls != nil && ls.Len() > 0 {
			off = ls.At(0).Start
		}
	}
	last := -1
	_ = ast.Walk(n, func(d ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering || d.Type() != ast.TypeBlock {
			return ast.WalkContinue, nil
		}
		if ls := d.Lines(); ls != nil && ls.Len() > 0 {
			last = max(last, ls.At(ls.Len()-1).Start)
		}
		if p := d.Pos(); p > last {
			last = p
		}
		return ast.WalkContinue, nil
	})
	if off < 0 && last < 0 {
		return 0, -1
	}
	if off < 0 {
		off = last
	}
	if last < off {
		last = off
	}
	if off > len(src) {
		off = len(src)
	}
	if last > len(src) {
		last = len(src)
	}
	start = lineStartAt(src, off)
	stop = lineEndAt(src, last)
	switch n.(type) {
	case *ast.FencedCodeBlock:
		stop = closingFenceEnd(src, start, stop)
	case *ast.Heading:
		stop = setextUnderlineEnd(src, start, stop)
	}
	return
}

// setextUnderlineEnd extends a setext heading's range over its underline,
// which goldmark keeps out of the heading's Lines(): when the heading's
// first line is not an ATX one, the line after its last is the underline
// if it is nothing but `=` or `-` after up to three spaces.
func setextUnderlineEnd(src []byte, start, stop int) int {
	if isATXLine(src[start:lineEndAt(src, start)]) {
		return stop
	}
	next := stop + 1
	if next > len(src) {
		return stop
	}
	le := lineEndAt(src, next)
	line := bytes.TrimRight(src[next:le], " \t\r")
	line = bytes.TrimLeft(line, " ")
	if len(line) == 0 || (line[0] != '=' && line[0] != '-') || bytes.Count(line, line[:1]) != len(line) {
		return stop
	}
	return le
}

// isATXLine says whether a line opens an ATX heading: `#` after up to three
// spaces of indentation.
func isATXLine(line []byte) bool {
	return len(bytes.TrimLeft(line, " ")) > 0 && bytes.TrimLeft(line, " ")[0] == '#'
}

// closingFenceEnd extends a fenced block's range over its closing fence
// line, which goldmark keeps out of the block's Lines(): the line after the
// last body line is the closing fence when it is the opening fence's
// character, at least as long, with nothing but spaces around it. An
// unclosed fence runs to the end of its container and gets no extension.
func closingFenceEnd(src []byte, start, stop int) int {
	ch, n := fenceRun(src[start:lineEndAt(src, start)])
	if n == 0 {
		return stop
	}
	// With an empty body the "last line" found is the opening fence
	// itself, and the line after it is still the candidate.
	next := stop + 1
	if next > len(src) {
		return stop
	}
	le := lineEndAt(src, next)
	c2, n2 := fenceRun(src[next:le])
	if c2 == ch && n2 >= n && len(bytes.TrimSpace(src[next:le])) == n2 {
		return le
	}
	return stop
}

// fenceRun is a line's fence character and run length after up to three
// spaces of indentation and any blockquote markers; n is 0 for no fence.
func fenceRun(line []byte) (ch byte, n int) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '>' || line[i] == '\t') {
		i++
	}
	if i >= len(line) || (line[i] != '`' && line[i] != '~') {
		return 0, 0
	}
	ch = line[i]
	for i < len(line) && line[i] == ch {
		n++
		i++
	}
	if n < 3 {
		return 0, 0
	}
	return
}

// lineStartAt is the offset of the first byte of the line holding off.
func lineStartAt(src []byte, off int) int {
	if off <= 0 {
		return 0
	}
	if i := bytes.LastIndexByte(src[:off], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

// lineEndAt is the offset of the newline ending the line holding off, or
// len(src) on the last line.
func lineEndAt(src []byte, off int) int {
	if off >= len(src) {
		return len(src)
	}
	if i := bytes.IndexByte(src[off:], '\n'); i >= 0 {
		return off + i
	}
	return len(src)
}

// frontmatterSpan finds a leading `---` YAML block: the opening delimiter on
// the first line, the closing one on a later line of its own. The range
// covers both delimiter lines, newline after the closing one excluded.
func frontmatterSpan(src []byte) (start, stop int, ok bool) {
	if !bytes.HasPrefix(src, []byte("---")) {
		return 0, 0, false
	}
	first := lineEndAt(src, 0)
	if bytes.TrimRight(src[:first], " \t\r") == nil || string(bytes.TrimRight(src[:first], " \t\r")) != "---" {
		return 0, 0, false
	}
	pos := first
	for pos < len(src) {
		ls := pos + 1
		if ls > len(src) {
			break
		}
		le := lineEndAt(src, ls)
		if string(bytes.TrimRight(src[ls:le], " \t\r")) == "---" {
			return 0, le, true
		}
		pos = le
	}
	return 0, 0, false
}
