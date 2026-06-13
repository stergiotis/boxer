// Package markdownhighlight syntax-highlights markdown source by routing it
// through goldmark's AST and re-emitting a canonical form, recording the
// byte offset of every marker as it is written.
//
// The trick that makes this cheap: goldmark already knows exactly where
// every `#`, `**`, ` ``` `, `[`, `]`, `(`, `)` lives — it has to, in order
// to parse them — but its AST is built for *transforming* the source into
// HTML and so discards the marker bytes once it has folded them into
// structural nodes (Heading{Level:1}, Emphasis{Level:2}, ...). Recovering
// those offsets after the fact is the whole pain of writing a markdown
// highlighter.
//
// We sidestep recovery by walking the AST and *generating* the markers
// ourselves, deterministically. Every `emit` call appends bytes to the
// canonical buffer and records the byte range under a category — the
// offset is just "current buffer length" before/after the write. The
// canonical form is gofmt-style: idempotent on re-parse, opinionated
// where the input had freedom (`*` for unordered lists, `**` for strong,
// ` ``` ` for fenced code, sorted YAML keys in frontmatter).
//
// Round-trip is therefore NOT byte-exact with the input — that is the
// point. Line/column numbers refer to the canonical form (mirrors gofmt).
// Use [Highlight] for viewers, not for editor-source roundtripping.
//
// Consumed by the codeview widget's BuildMarkdown / PrepareMarkdown builders.
package markdownhighlight

import (
	"bytes"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/callout"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/comment"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/embed"
	highlightext "github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/highlight"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/wikilink"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// CategoryE classifies a span for highlighting.
type CategoryE int32

const (
	CategoryPlain          CategoryE = iota // unclassified prose / fallback
	CategoryWhitespace                      // gaps, newlines, list continuation
	CategoryHeadingMarker                   // leading `#`, `##`, ...
	CategoryHeadingText                     // heading body
	CategoryStrongDelim                     // `**`
	CategoryStrongText                      // text inside `**...**`
	CategoryEmphasisDelim                   // `*`
	CategoryEmphasisText                    // text inside `*...*`
	CategoryStrikeDelim                     // `~~`
	CategoryStrikeText                      // text inside `~~...~~`
	CategoryHighlightDelim                  // `==`
	CategoryHighlightText                   // text inside `==...==`
	CategoryInlineCodeDelim                 // single backtick
	CategoryInlineCodeText                  // text inside `...`
	CategoryFenceDelim                      // ` ``` ` or `~~~`
	CategoryFenceLang                       // info-string token after fence
	CategoryCodeBlockBody                   // body lines of a fenced/indented block
	CategoryBlockquoteMarker                // leading `>` on a line
	CategoryListMarker                      // `-`, `*`, `+`, `1.`, `2.`, ...
	CategoryLinkPunct                       // `[`, `]`, `(`, `)`, `<`, `>` around links
	CategoryLinkLabel                       // label text between `[` and `]`
	CategoryLinkUrl                         // URL inside `(...)` or `<...>`
	CategoryThematicBreak                   // standalone `---` / `***`
	CategoryFrontmatterDelim                // YAML frontmatter `---` fences + `:`
	CategoryFrontmatterKey                  // frontmatter key
	CategoryFrontmatterValue                // frontmatter value
	CategoryWikilinkPunct                   // `[[`, `]]`
	CategoryWikilinkTarget                  // target text inside `[[...]]`
	CategoryEmbedMarker                     // leading `!` of `![[...]]`
	CategoryCalloutMarker                   // `> [!...]` scaffolding
	CategoryCalloutType                     // callout type name (`note`, `warning`, ...)
	CategoryCommentDelim                    // `%%`
	CategoryCommentText                     // text inside `%%...%%`
	CategoryRawHtml                         // raw HTML / HTMLBlock passthrough
	CategoryTablePipe                       // `|` column separators
	CategoryTableAlign                      // `:---`, `---:`, `:---:`, `---` in the align row
	CategoryTableHeaderText                 // text inside header cells
	CategoryTableCellText                   // text inside body cells
	CategoryTaskMark                        // GFM `[ ]` / `[x]` task checkbox
	categoryMax
)

// CategoryCount is the number of CategoryE values — sized for palette
// arrays in consumers.
const CategoryCount = int(categoryMax)

// Span is one contiguous byte range of the canonical output, tagged with
// its category.
type Span struct {
	Start    int32
	Stop     int32
	Text     string
	Category CategoryE
}

// Highlight parses src as Obsidian-flavored markdown and re-emits a
// canonical form together with byte-offset spans suitable for a colored
// CodeView. Both returns reference the same canonical text — Span.Text
// holds the bytes that Span.Start..Span.Stop point at in canonical.
//
// The canonical form is intentionally lossy on input idiosyncrasy:
// emphasis always uses `*` / `**`, lists always use `-`, frontmatter
// keys are sorted alphabetically, code blocks always render fenced. The
// goldmark parser is what decides what counts as which node; we just
// rewrite the surface syntax.
func Highlight(src []byte) (canonical string, spans []Span) {
	gm := obsidian.New(obsidian.Options{
		Features: obsidian.FeatureFrontmatter |
			obsidian.FeatureGFM |
			obsidian.FeatureWikilink |
			obsidian.FeatureEmbed |
			obsidian.FeatureCallout |
			obsidian.FeatureHighlight |
			obsidian.FeatureComment,
	})
	pc := obsidian.NewParserContext()
	root := gm.Parser().Parse(text.NewReader(src), parser.WithContext(pc))

	r := renderer{src: src}
	if fm := obsidian.GetFrontmatter(pc); fm != nil {
		r.renderFrontmatter(fm)
	}
	first := true
	for child := root.FirstChild(); child != nil; child = child.NextSibling() {
		if !first {
			r.emit("\n", CategoryWhitespace)
		}
		r.renderBlock(child)
		first = false
	}

	canonical = r.buf.String()
	// Backfill Text now that the buffer is final — spans were emitted with
	// offsets only.
	for i := range r.spans {
		r.spans[i].Text = canonical[r.spans[i].Start:r.spans[i].Stop]
	}
	spans = r.spans
	return
}

// renderer accumulates the canonical text + spans during one walk of the
// AST. Each emit call appends to buf and records the just-written range.
type renderer struct {
	src   []byte
	buf   strings.Builder
	spans []Span
}

// emit appends s to the canonical buffer and records a span covering it.
// Empty strings are silently dropped (avoids zero-width spans).
func (inst *renderer) emit(s string, cat CategoryE) {
	if s == "" {
		return
	}
	start := int32(inst.buf.Len())
	inst.buf.WriteString(s)
	inst.spans = append(inst.spans, Span{
		Start:    start,
		Stop:     int32(inst.buf.Len()),
		Category: cat,
	})
}

func (inst *renderer) emitBytes(b []byte, cat CategoryE) {
	if len(b) == 0 {
		return
	}
	start := int32(inst.buf.Len())
	inst.buf.Write(b)
	inst.spans = append(inst.spans, Span{
		Start:    start,
		Stop:     int32(inst.buf.Len()),
		Category: cat,
	})
}

// renderBlock dispatches one block-level AST node. Unknown block kinds
// fall through silently — the prototype skips them rather than panic.
func (inst *renderer) renderBlock(n ast.Node) {
	switch v := n.(type) {
	case *ast.Heading:
		inst.renderHeading(v)
	case *ast.Paragraph:
		inst.renderInlines(v, CategoryPlain)
		inst.emit("\n", CategoryWhitespace)
	case *ast.TextBlock:
		// Tight-list item bodies arrive as TextBlock; same shape as
		// Paragraph for our purposes.
		inst.renderInlines(v, CategoryPlain)
		inst.emit("\n", CategoryWhitespace)
	case *ast.FencedCodeBlock:
		inst.renderFencedCodeBlock(v)
	case *ast.CodeBlock:
		inst.renderIndentedCodeBlock(v)
	case *ast.List:
		inst.renderList(v)
	case *ast.Blockquote:
		inst.renderBlockquote(v)
	case *ast.ThematicBreak:
		inst.emit("---", CategoryThematicBreak)
		inst.emit("\n", CategoryWhitespace)
	case *callout.Node:
		inst.renderCallout(v)
	case *ast.HTMLBlock:
		inst.renderHTMLBlock(v)
	case *east.Table:
		inst.renderTable(v)
	}
}

func (inst *renderer) renderHeading(h *ast.Heading) {
	inst.emit(strings.Repeat("#", h.Level), CategoryHeadingMarker)
	inst.emit(" ", CategoryWhitespace)
	for c := h.FirstChild(); c != nil; c = c.NextSibling() {
		inst.renderInline(c, CategoryHeadingText)
	}
	inst.emit("\n", CategoryWhitespace)
}

func (inst *renderer) renderInlines(parent ast.Node, plainCat CategoryE) {
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		inst.renderInline(c, plainCat)
	}
}

// renderInline emits one inline AST node. plainCat is the category to
// use for unstyled text — it propagates from the surrounding context
// (heading text inside a Heading, link label inside a Link, etc.) so
// that the same `*` emphasis run can paint its content differently
// depending on where it sits.
func (inst *renderer) renderInline(n ast.Node, plainCat CategoryE) {
	switch v := n.(type) {
	case *ast.Text:
		seg := v.Segment.Value(inst.src)
		inst.emitBytes(seg, plainCat)
		if v.HardLineBreak() {
			inst.emit("  \n", CategoryWhitespace)
		} else if v.SoftLineBreak() {
			inst.emit("\n", CategoryWhitespace)
		}
	case *ast.String:
		inst.emitBytes(v.Value, plainCat)
	case *ast.Emphasis:
		delim := "*"
		delimCat := CategoryEmphasisDelim
		textCat := CategoryEmphasisText
		if v.Level == 2 {
			delim = "**"
			delimCat = CategoryStrongDelim
			textCat = CategoryStrongText
		}
		inst.emit(delim, delimCat)
		for c := v.FirstChild(); c != nil; c = c.NextSibling() {
			inst.renderInline(c, textCat)
		}
		inst.emit(delim, delimCat)
	case *east.Strikethrough:
		inst.emit("~~", CategoryStrikeDelim)
		for c := v.FirstChild(); c != nil; c = c.NextSibling() {
			inst.renderInline(c, CategoryStrikeText)
		}
		inst.emit("~~", CategoryStrikeDelim)
	case *highlightext.Node:
		inst.emit("==", CategoryHighlightDelim)
		for c := v.FirstChild(); c != nil; c = c.NextSibling() {
			inst.renderInline(c, CategoryHighlightText)
		}
		inst.emit("==", CategoryHighlightDelim)
	case *ast.CodeSpan:
		inst.emit("`", CategoryInlineCodeDelim)
		for c := v.FirstChild(); c != nil; c = c.NextSibling() {
			switch tn := c.(type) {
			case *ast.Text:
				inst.emitBytes(tn.Segment.Value(inst.src), CategoryInlineCodeText)
			case *ast.String:
				inst.emitBytes(tn.Value, CategoryInlineCodeText)
			}
		}
		inst.emit("`", CategoryInlineCodeDelim)
	case *ast.Link:
		inst.emit("[", CategoryLinkPunct)
		for c := v.FirstChild(); c != nil; c = c.NextSibling() {
			inst.renderInline(c, CategoryLinkLabel)
		}
		inst.emit("](", CategoryLinkPunct)
		inst.emitBytes(v.Destination, CategoryLinkUrl)
		inst.emit(")", CategoryLinkPunct)
	case *ast.AutoLink:
		inst.emit("<", CategoryLinkPunct)
		url := v.URL(inst.src)
		if v.AutoLinkType == ast.AutoLinkEmail {
			// goldmark already strips "mailto:"; emit URL as-is.
			inst.emitBytes(url, CategoryLinkUrl)
		} else {
			inst.emitBytes(url, CategoryLinkUrl)
		}
		inst.emit(">", CategoryLinkPunct)
	case *wikilink.Node:
		inst.emit("[[", CategoryWikilinkPunct)
		target := string(v.Page)
		if h := string(v.Heading); h != "" {
			target += "#" + h
		}
		if a := string(v.Alias); a != "" {
			target += "|" + a
		}
		inst.emit(target, CategoryWikilinkTarget)
		inst.emit("]]", CategoryWikilinkPunct)
	case *embed.Node:
		inst.emit("!", CategoryEmbedMarker)
		inst.emit("[[", CategoryWikilinkPunct)
		target := string(v.Target)
		if h := string(v.Heading); h != "" {
			target += "#" + h
		}
		inst.emit(target, CategoryWikilinkTarget)
		inst.emit("]]", CategoryWikilinkPunct)
	case *comment.Node:
		// Obsidian's comment parser doesn't preserve the inner text —
		// only that a comment existed. Emit a placeholder so the marker
		// is at least visible in canonical output.
		inst.emit("%%", CategoryCommentDelim)
		inst.emit("…", CategoryCommentText)
		inst.emit("%%", CategoryCommentDelim)
	case *ast.RawHTML:
		for i := 0; i < v.Segments.Len(); i++ {
			seg := v.Segments.At(i)
			inst.emitBytes(seg.Value(inst.src), CategoryRawHtml)
		}
	case *east.TaskCheckBox:
		if v.IsChecked {
			inst.emit("[x]", CategoryTaskMark)
		} else {
			inst.emit("[ ]", CategoryTaskMark)
		}
		// goldmark keeps the source's space between `]` and the
		// following text inside the next Text node, so no trailing
		// space is emitted here — doubling would shift task-item text
		// one column right relative to plain bullets.
	}
	// Unknown inline kinds (math, footnote refs, ...) are silently
	// skipped — the prototype scope.
}

func (inst *renderer) renderFencedCodeBlock(c *ast.FencedCodeBlock) {
	inst.emit("```", CategoryFenceDelim)
	if lang := c.Language(inst.src); len(lang) > 0 {
		inst.emitBytes(lang, CategoryFenceLang)
	}
	inst.emit("\n", CategoryWhitespace)
	lines := c.BaseBlock.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		inst.emitBytes(line.Value(inst.src), CategoryCodeBlockBody)
	}
	inst.emit("```", CategoryFenceDelim)
	inst.emit("\n", CategoryWhitespace)
}

func (inst *renderer) renderIndentedCodeBlock(c *ast.CodeBlock) {
	lines := c.BaseBlock.Lines()
	for i := 0; i < lines.Len(); i++ {
		inst.emit("    ", CategoryWhitespace) // canonical 4-space marker
		line := lines.At(i)
		inst.emitBytes(line.Value(inst.src), CategoryCodeBlockBody)
	}
}

func (inst *renderer) renderList(l *ast.List) {
	idx := uint32(0)
	if l.IsOrdered() {
		idx = uint32(l.Start)
		if idx == 0 {
			idx = 1
		}
	}
	for item := l.FirstChild(); item != nil; item = item.NextSibling() {
		li, ok := item.(*ast.ListItem)
		if !ok {
			continue
		}
		if l.IsOrdered() {
			inst.emit(strconv.FormatUint(uint64(idx), 10)+". ", CategoryListMarker)
			idx++
		} else {
			inst.emit("- ", CategoryListMarker)
		}
		for inner := li.FirstChild(); inner != nil; inner = inner.NextSibling() {
			inst.renderBlock(inner)
		}
	}
}

// renderBlockquote prefixes each rendered line of the child blocks with
// "> ". The implementation re-runs each child through a nested renderer
// and rewraps its output line-by-line — cheap and correct without having
// to thread a "current line prefix" through every render path.
func (inst *renderer) renderBlockquote(b *ast.Blockquote) {
	for child := b.FirstChild(); child != nil; child = child.NextSibling() {
		inst.renderQuoted(child, "> ")
	}
}

// renderQuoted runs child through a fresh renderer and re-emits its
// output prefixed with `prefix` on every line. Spans from the inner pass
// are shifted into the outer offset space; the "> " bytes get a marker
// span synthesized here.
func (inst *renderer) renderQuoted(child ast.Node, prefix string) {
	inner := renderer{src: inst.src}
	inner.renderBlock(child)
	innerText := inner.buf.String()
	if innerText == "" {
		return
	}

	// Index inner lines so spans can be mapped per-line.
	lineStarts := []int32{0}
	for i := 0; i < len(innerText); i++ {
		if innerText[i] == '\n' && i+1 < len(innerText) {
			lineStarts = append(lineStarts, int32(i+1))
		}
	}

	// Walk inner spans line-by-line, re-emitting them shifted into the
	// outer buffer. Between lines we emit a fresh "> " marker.
	lineIdx := 0
	for spanIdx := 0; spanIdx < len(inner.spans); spanIdx++ {
		span := inner.spans[spanIdx]
		// Advance lineIdx to the line that contains this span's start.
		for lineIdx+1 < len(lineStarts) && span.Start >= lineStarts[lineIdx+1] {
			lineIdx++
		}
		if int32(inst.buf.Len()) == 0 || lastByte(&inst.buf) == '\n' {
			inst.emit(prefix, CategoryBlockquoteMarker)
		}
		// If span crosses a newline, split at the newline.
		text := innerText[span.Start:span.Stop]
		if nl := strings.IndexByte(text, '\n'); nl >= 0 && nl < len(text)-1 {
			inst.emit(text[:nl+1], span.Category)
			// Emit prefix for continuation; remaining text becomes a new
			// span emitted manually.
			inst.emit(prefix, CategoryBlockquoteMarker)
			inst.emit(text[nl+1:], span.Category)
		} else {
			inst.emit(text, span.Category)
		}
	}
}

func lastByte(b *strings.Builder) byte {
	s := b.String()
	if len(s) == 0 {
		return 0
	}
	return s[len(s)-1]
}

func (inst *renderer) renderCallout(c *callout.Node) {
	inst.emit("> [!", CategoryCalloutMarker)
	inst.emit(string(c.CalloutType), CategoryCalloutType)
	inst.emit("]", CategoryCalloutMarker)
	if c.Foldable {
		if c.DefaultOpen {
			inst.emit("+", CategoryCalloutMarker)
		} else {
			inst.emit("-", CategoryCalloutMarker)
		}
	}
	if len(c.Title) > 0 {
		inst.emit(" ", CategoryWhitespace)
		inst.emitBytes(c.Title, CategoryCalloutType)
	}
	inst.emit("\n", CategoryWhitespace)
	for child := c.FirstChild(); child != nil; child = child.NextSibling() {
		inst.renderQuoted(child, "> ")
	}
}

func (inst *renderer) renderHTMLBlock(h *ast.HTMLBlock) {
	lines := h.BaseBlock.Lines()
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		inst.emitBytes(line.Value(inst.src), CategoryRawHtml)
	}
}

// renderTable emits a GFM table as the canonical
// `| h1 | h2 |\n|:---|---:|\n| v1 | v2 |\n` form. The header row is
// always present (goldmark requires it); the align row is synthesized
// from t.Alignments.
func (inst *renderer) renderTable(t *east.Table) {
	var header *east.TableHeader
	rows := make([]*east.TableRow, 0, 4)
	for c := t.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *east.TableHeader:
			header = v
		case *east.TableRow:
			rows = append(rows, v)
		}
	}
	if header != nil {
		inst.renderTableRow(header, CategoryTableHeaderText)
	}
	inst.renderTableAlignRow(t.Alignments)
	for _, row := range rows {
		inst.renderTableRow(row, CategoryTableCellText)
	}
}

// renderTableRow emits one row's `| cell | cell |` line. The row's
// children are TableCell nodes; their inline children carry the cell
// content (with their own emphasis / link / code spans intact).
func (inst *renderer) renderTableRow(row ast.Node, cellCat CategoryE) {
	for c := row.FirstChild(); c != nil; c = c.NextSibling() {
		cell, ok := c.(*east.TableCell)
		if !ok {
			continue
		}
		inst.emit("| ", CategoryTablePipe)
		for ic := cell.FirstChild(); ic != nil; ic = ic.NextSibling() {
			inst.renderInline(ic, cellCat)
		}
		inst.emit(" ", CategoryWhitespace)
	}
	inst.emit("|", CategoryTablePipe)
	inst.emit("\n", CategoryWhitespace)
}

// renderTableAlignRow emits the `|:---|---:|` separator between header
// and body rows. `:---` / `---:` / `:---:` encode left / right / center;
// bare `---` is the default (AlignNone).
func (inst *renderer) renderTableAlignRow(aligns []east.Alignment) {
	for _, a := range aligns {
		inst.emit("|", CategoryTablePipe)
		marker := "---"
		switch a {
		case east.AlignLeft:
			marker = ":---"
		case east.AlignRight:
			marker = "---:"
		case east.AlignCenter:
			marker = ":---:"
		}
		inst.emit(marker, CategoryTableAlign)
	}
	inst.emit("|", CategoryTablePipe)
	inst.emit("\n", CategoryWhitespace)
}

// renderFrontmatter emits canonical YAML with sorted keys. The value
// formatter is best-effort %v — nested maps and arrays land as Go's
// default representation; that's a known prototype limitation.
func (inst *renderer) renderFrontmatter(fm map[string]any) {
	inst.emit("---", CategoryFrontmatterDelim)
	inst.emit("\n", CategoryWhitespace)
	keys := make([]string, 0, len(fm))
	for k := range fm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		inst.emit(k, CategoryFrontmatterKey)
		inst.emit(": ", CategoryFrontmatterDelim)
		inst.emit(formatFrontmatterValue(fm[k]), CategoryFrontmatterValue)
		inst.emit("\n", CategoryWhitespace)
	}
	inst.emit("---", CategoryFrontmatterDelim)
	inst.emit("\n", CategoryWhitespace)
}

func formatFrontmatterValue(v any) (out string) {
	switch t := v.(type) {
	case string:
		out = t
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				buf.WriteString(", ")
			}
			buf.WriteString(formatFrontmatterValue(item))
		}
		buf.WriteByte(']')
		out = buf.String()
	default:
		out = fmt.Sprintf("%v", t)
	}
	return
}
