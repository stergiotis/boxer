//go:build llm_generated_opus47

package markdown

import (
	"bytes"
	"strings"

	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/callout"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/comment"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/embed"
	highlightext "github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/highlight"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/wikilink"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/codeview"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

// lowerCtx bundles the per-parse state threaded through every
// lowerXxx call: the source bytes (for ast.Text segments) and the
// resolver (for wikilinks and embeds). Allocated once at the top of
// parseAndLower and passed by pointer.
type lowerCtx struct {
	src      []byte
	resolver resolver.ResolverI
}

// parseAndLower runs goldmark with the configured Obsidian extensions,
// then lowers the AST top-level blocks into segment values that
// pre-build retained Atoms / CodeViewJob holders. Frontmatter is
// converted from goldmark-meta's map[string]interface{} into a
// key-sorted KV so callers iterate in stable order across frames.
// Headings are collected as a side table (plain text + slug + level)
// for help/TOC consumers; callers that don't need it ignore the
// third return value.
func parseAndLower(src []byte, cfg *config) (segments []segment, frontmatter *containers.BinarySearchGrowingKV[string, interface{}], headings []HeadingInfo) {
	gm := obsidian.New(obsidian.Options{
		Features: cfg.features,
		Resolver: cfg.resolver,
	})
	pc := obsidian.NewParserContext()
	root := gm.Parser().Parse(text.NewReader(src), parser.WithContext(pc))

	if cfg.features&obsidian.FeatureFrontmatter != 0 {
		frontmatter = containers.NewBinarySearchGrowingKVFromAnyMap(obsidian.GetFrontmatter(pc))
	}

	ctx := &lowerCtx{src: src, resolver: cfg.resolver}
	segments = make([]segment, 0, 16)
	for child := root.FirstChild(); child != nil; child = child.NextSibling() {
		if h, ok := child.(*ast.Heading); ok {
			text := headingPlainText(h, src)
			headings = append(headings, HeadingInfo{
				Text:  text,
				Slug:  SlugHeading(text),
				Level: uint8(h.Level),
			})
		}
		seg, ok := lowerBlock(ctx, child)
		if ok {
			segments = append(segments, seg)
		}
	}
	return
}

// headingPlainText flattens a heading's inline subtree into plain text
// for slug generation and TOC display. Inline styling (bold, italic,
// code spans, strikethrough) is dropped; the contained text segments
// are concatenated in document order. Wikilinks and embeds inside
// headings — uncommon but legal — contribute their visible label only.
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
	out = sb.String()
	return
}

// lowerBlock converts one block AST node into a segment. Unsupported
// block kinds (tables; phase-deferred constructs) are silently dropped.
func lowerBlock(ctx *lowerCtx, n ast.Node) (seg segment, ok bool) {
	switch v := n.(type) {
	case *ast.Heading:
		seg = lowerParagraphLike(ctx, v, uint8(v.Level))
		seg.kind = segKindHeading
		ok = true
	case *ast.Paragraph:
		seg = lowerParagraphLike(ctx, v, 0)
		seg.kind = segKindParagraph
		ok = true
	case *ast.TextBlock:
		// Tight list items (List.IsTight == true) wrap their content
		// in a TextBlock instead of a Paragraph. Same inline shape, no
		// extra spacing from goldmark — we render it identically.
		seg = lowerParagraphLike(ctx, v, 0)
		seg.kind = segKindParagraph
		ok = true
	case *ast.FencedCodeBlock:
		seg = lowerCodeBlock(v.BaseBlock.Lines(), ctx.src, string(v.Language(ctx.src)))
		ok = true
	case *ast.CodeBlock:
		seg = lowerCodeBlock(v.BaseBlock.Lines(), ctx.src, "")
		ok = true
	case *ast.List:
		seg = lowerList(ctx, v)
		ok = true
	case *ast.Blockquote:
		seg = lowerBlockquote(ctx, v)
		ok = true
	case *ast.ThematicBreak:
		seg = segment{kind: segKindHorizontalRule}
		ok = true
	case *callout.Node:
		seg = lowerCallout(ctx, v)
		ok = true
	}
	return
}

// lowerParagraphLike walks a paragraph or heading's inline children
// into a sequence of paragraphRun values. headingLevel is 0 for
// paragraphs, 1..6 for headings — heading levels apply font-size +
// Strong styling to every text fragment.
func lowerParagraphLike(ctx *lowerCtx, parent ast.Node, headingLevel uint8) (seg segment) {
	b := newInlineBuilder(headingLevel, ctx.resolver)
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		emitInline(ctx, child, &b, styleNone)
	}
	seg.runs = b.finish()
	return
}

// lowerCodeBlock packages a fenced or indented code block into a
// CodeViewJob retained holder. Fenced blocks tagged `go`/`golang`,
// `sql`, or `json` are syntax-highlighted via codeview's per-language
// builders; everything else (including indented blocks, which carry no
// fence language) falls back to plain [c.CodeViewJob]. The lang
// argument is the raw info-string token from the opening fence — it is
// trimmed and lower-cased here, so callers can pass it as-is.
func lowerCodeBlock(lines *text.Segments, src []byte, lang string) (seg segment) {
	var buf bytes.Buffer
	buf.Grow(64)
	for i := 0; i < lines.Len(); i++ {
		line := lines.At(i)
		buf.Write(line.Value(src))
	}
	seg.kind = segKindCodeBlock
	source := buf.String()
	// Retain the raw source alongside the highlighted job: the markdown
	// highlighter canonicalises its output (codeview.BuildMarkdown notes
	// the rendered text is not verbatim) and the job holder exposes no
	// way to read the text back. The copy-to-clipboard affordance
	// ([WithClipboard]) must copy what the author wrote, so we stash it.
	seg.codeText = source
	switch strings.ToLower(strings.TrimSpace(lang)) {
	case "go", "golang":
		seg.code = codeview.PrepareGo(source)
	case "sql":
		seg.code = codeview.PrepareSql(source)
	case "json":
		seg.code = codeview.PrepareJson(source)
	case "markdown", "md":
		seg.code = codeview.PrepareMarkdown(source)
	default:
		seg.code = c.CodeViewJob(source).Keep()
	}
	return
}

// lowerList walks an ast.List into a list segment with one
// segKindListItem child per item.
func lowerList(ctx *lowerCtx, list *ast.List) (seg segment) {
	seg.kind = segKindList
	seg.listOrdered = list.IsOrdered()
	if seg.listOrdered {
		seg.listStart = uint32(list.Start)
		if seg.listStart == 0 {
			seg.listStart = 1
		}
	}
	for item := list.FirstChild(); item != nil; item = item.NextSibling() {
		li, ok := item.(*ast.ListItem)
		if !ok {
			continue
		}
		seg.children = append(seg.children, lowerListItem(ctx, li))
	}
	return
}

func lowerListItem(ctx *lowerCtx, item *ast.ListItem) (seg segment) {
	seg.kind = segKindListItem
	for child := item.FirstChild(); child != nil; child = child.NextSibling() {
		inner, ok := lowerBlock(ctx, child)
		if ok {
			seg.children = append(seg.children, inner)
		}
	}
	return
}

func lowerBlockquote(ctx *lowerCtx, bq *ast.Blockquote) (seg segment) {
	seg.kind = segKindBlockquote
	for child := bq.FirstChild(); child != nil; child = child.NextSibling() {
		inner, ok := lowerBlock(ctx, child)
		if ok {
			seg.children = append(seg.children, inner)
		}
	}
	return
}

// lowerCallout maps an Obsidian callout (`> [!warning] Title`) onto a
// segKindCallout segment. The callout type drives the visual theme
// (border + fill + glyph) at render time; Foldable callouts render as
// a CollapsingHeader, non-foldable ones as a themed Frame with a
// strong title row above the body.
func lowerCallout(ctx *lowerCtx, n *callout.Node) (seg segment) {
	seg.kind = segKindCallout
	seg.calloutType = strings.ToLower(string(n.CalloutType))
	seg.calloutTitle = string(n.Title)
	seg.calloutFoldable = n.Foldable
	seg.calloutDefaultOpen = n.DefaultOpen
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		inner, ok := lowerBlock(ctx, child)
		if ok {
			seg.children = append(seg.children, inner)
		}
	}
	return
}

// styleE is a bitmask of inline style flags that propagate into nested
// inline nodes.
type styleE uint8

const (
	styleNone          styleE = 0
	styleStrong        styleE = 1 << 0
	styleEmphasis      styleE = 1 << 1
	styleCode          styleE = 1 << 2
	styleStrikethrough styleE = 1 << 3
	styleHighlight     styleE = 1 << 4
)

// emitInline walks one inline AST node and dispatches into the builder.
// parentStyle accumulates styles inherited from outer Strong / Emphasis
// / Strikethrough / Highlight wrappers.
func emitInline(ctx *lowerCtx, n ast.Node, b *inlineBuilder, parentStyle styleE) {
	switch v := n.(type) {
	case *ast.Text:
		segText := string(v.Segment.Value(ctx.src))
		// Markdown collapses soft line breaks into spaces; hard line
		// breaks become real newlines (egui's text wrap honours both).
		if v.HardLineBreak() {
			segText += "\n"
		} else if v.SoftLineBreak() {
			segText += " "
		}
		b.emitText(segText, parentStyle)
	case *ast.String:
		b.emitText(string(v.Value), parentStyle)
	case *ast.Emphasis:
		nested := parentStyle | styleEmphasis
		if v.Level == 2 {
			nested = parentStyle | styleStrong
		}
		walkInlineChildren(ctx, v, b, nested)
	case *east.Strikethrough:
		walkInlineChildren(ctx, v, b, parentStyle|styleStrikethrough)
	case *highlightext.Node:
		walkInlineChildren(ctx, v, b, parentStyle|styleHighlight)
	case *ast.CodeSpan:
		b.emitText(flattenInlineText(v, ctx.src), parentStyle|styleCode)
	case *ast.Link:
		label := flattenInlineText(v, ctx.src)
		url := string(v.Destination)
		b.emitLink(label, url)
	case *ast.AutoLink:
		url := string(v.URL(ctx.src))
		label := url
		if v.AutoLinkType == ast.AutoLinkEmail {
			label = url
			url = "mailto:" + url
		}
		b.emitLink(label, url)
	case *wikilink.Node:
		page := string(v.Page)
		heading := string(v.Heading)
		display := v.DisplayText()
		url, _ := b.resolver.ResolveWikilink(page, heading)
		b.emitLink(display, url)
	case *ast.Image:
		// CommonMark inline image: ![alt](url). The destination string
		// is the raw URL; the alt-text subtree is only needed for the
		// fallback hyperlink label, so its flattening is deferred to
		// the !ok branch.
		url := string(v.Destination)
		if !b.emitImage(url) {
			label := flattenInlineText(v, ctx.src)
			if label == "" {
				label = url
			}
			b.emitLink("🖼 "+label, url)
		}
	case *embed.Node:
		target := string(v.Target)
		heading := string(v.Heading)
		url, isImage, _ := b.resolver.ResolveEmbed(target, heading)
		// Obsidian image embeds (![[picture.png]]) get a real image
		// widget when the resolver can decode them. Non-image embeds
		// (note transclusions) and any image the loader doesn't
		// recognise fall back to the pre-image-widget glyph-hyperlink.
		ref := target
		if heading != "" {
			ref += "#" + heading
		}
		if isImage && b.emitImage(ref) {
			break
		}
		glyph := "📄 "
		if isImage {
			glyph = "🖼 "
		}
		label := glyph + target
		if heading != "" {
			label += " > " + heading
		}
		b.emitLink(label, url)
	case *comment.Node:
		// Obsidian %%comment%% — explicitly drop. Default branch would
		// also drop it, but enumerating it here documents intent.
	default:
		// RawHTML, RawInline, HTMLBlock, math (deferred), and unknown
		// extension nodes are silently skipped.
	}
}

// flattenInlineText collects the plain-text content of an inline subtree
// (used for code spans and link labels where embedded styling is not
// preserved).
func flattenInlineText(parent ast.Node, src []byte) (out string) {
	var buf bytes.Buffer
	ast.Walk(parent, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Text:
			buf.Write(v.Segment.Value(src))
		case *ast.String:
			buf.Write(v.Value)
		}
		return ast.WalkContinue, nil
	})
	out = buf.String()
	return
}

func walkInlineChildren(ctx *lowerCtx, parent ast.Node, b *inlineBuilder, parentStyle styleE) {
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		emitInline(ctx, child, b, parentStyle)
	}
}

// inlineBuilder accumulates a paragraph's runs while walking inline
// children. Each emitted text fragment carries a [styleE] mask;
// consecutive fragments with identical masks are coalesced into one
// Atoms text/styled-text segment so egui's text shaper can word-wrap
// across what was originally several soft-line-break-separated runs in
// the source.
//
// Without coalescing, every soft line break in a CommonMark paragraph
// produces a fresh ast.Text node and (previously) one Text() opcode,
// which egui::Atoms then lays out as a separate atom — wrapping each
// independently and breaking at atom boundaries. The result is the
// jittery ladder described in the markdown package's Trade-offs note.
//
// Coalescing keeps a single open atom per (style mask) until the style
// changes, a hyperlink interrupts the flow, or the paragraph ends.
type inlineBuilder struct {
	headingLevel uint8
	cur          c.AtomsFluid
	curOpen      bool
	runs         []paragraphRun
	resolver     resolver.ResolverI

	pendingText   strings.Builder
	pendingStyle  styleE
	pendingActive bool
}

func newInlineBuilder(headingLevel uint8, r resolver.ResolverI) (b inlineBuilder) {
	b.headingLevel = headingLevel
	b.cur = c.Atoms()
	b.resolver = r
	return
}

func (inst *inlineBuilder) emitText(s string, style styleE) {
	if s == "" {
		return
	}
	if inst.pendingActive && inst.pendingStyle == style {
		inst.pendingText.WriteString(s)
		return
	}
	inst.flushPending()
	inst.pendingStyle = style
	inst.pendingText.WriteString(s)
	inst.pendingActive = true
}

func (inst *inlineBuilder) emitLink(label, url string) {
	inst.flushAtoms()
	inst.runs = append(inst.runs, paragraphRun{
		kind:  runKindLink,
		label: label,
		url:   url,
	})
}

// imageMaxPixelCount is the upper bound the visitor accepts from
// [resolver.ResolverI.LoadImage]. A resolver returning a wildly
// oversized buffer (e.g. claiming 65536×65536 = 16 GiB RGBA) is
// rejected outright — defense in depth, even though the resolver
// owns the allocation. The chosen value (64 Mpx ≈ 256 MiB RGBA)
// comfortably covers 8K screenshots while staying inside what
// egui's texture upload can plausibly handle.
const imageMaxPixelCount uint64 = 64 * 1024 * 1024

// emitImage asks the configured resolver to decode ref into RGBA8
// pixels. On success it flushes the current atoms run and appends a
// runKindImage carrying the pixel buffer; on failure (loader returned
// ok=false, pixels/dims came back inconsistent, or the source exceeds
// [imageMaxPixelCount]) it returns false so the caller can render the
// pre-image-widget glyph-hyperlink fallback.
func (inst *inlineBuilder) emitImage(ref string) (ok bool) {
	pixels, w, h, loaded := inst.resolver.LoadImage(ref)
	if !loaded || w == 0 || h == 0 {
		return
	}
	pixelCount := uint64(w) * uint64(h)
	if pixelCount > imageMaxPixelCount || uint64(len(pixels)) != pixelCount {
		return
	}
	inst.flushAtoms()
	inst.runs = append(inst.runs, paragraphRun{
		kind:        runKindImage,
		imgPixels:   pixels,
		imgWidthPx:  w,
		imgHeightPx: h,
	})
	ok = true
	return
}

// flushPending materialises the coalesced text buffer as one
// Text / StyledText atom on the current Atoms builder.
func (inst *inlineBuilder) flushPending() {
	if !inst.pendingActive {
		return
	}
	s := inst.pendingText.String()
	inst.pendingText.Reset()
	inst.pendingActive = false
	inst.applyStyledText(s, inst.pendingStyle)
	inst.curOpen = true
}

func (inst *inlineBuilder) flushAtoms() {
	inst.flushPending()
	if !inst.curOpen {
		return
	}
	held := inst.cur.Keep()
	inst.runs = append(inst.runs, paragraphRun{
		kind:  runKindAtoms,
		atoms: held,
	})
	inst.cur = c.Atoms()
	inst.curOpen = false
}

func (inst *inlineBuilder) finish() (runs []paragraphRun) {
	inst.flushAtoms()
	runs = inst.runs
	return
}

// applyStyledText writes one styled fragment into the current Atoms
// builder. Heading levels imply font-size + Strong; inline style flags
// stack on top. The styleHighlight flag routes through StyledTextColored
// with the package-level highlight palette so the run renders against
// a yellow "highlighter pen" background.
func (inst *inlineBuilder) applyStyledText(s string, style styleE) {
	var fontSize float32
	var headingStrong bool
	if inst.headingLevel > 0 {
		fontSize = headingFontSize(inst.headingLevel)
		headingStrong = true
	}

	plain := style == styleNone && fontSize == 0 && !headingStrong
	if plain {
		inst.cur = inst.cur.Text(s)
		return
	}

	if style&styleHighlight != 0 {
		for rt := range inst.cur.StyledTextColored(highlightFg, highlightBg, s) {
			if fontSize > 0 {
				rt = rt.Size(fontSize)
			}
			if headingStrong || style&styleStrong != 0 {
				rt = rt.Strong()
			}
			if style&styleEmphasis != 0 {
				rt = rt.Italics()
			}
			if style&styleCode != 0 {
				rt = rt.Code()
			}
			if style&styleStrikethrough != 0 {
				rt = rt.Strikethrough()
			}
			_ = rt
		}
		return
	}

	for rt := range inst.cur.StyledText(s) {
		if fontSize > 0 {
			rt = rt.Size(fontSize)
		}
		if headingStrong || style&styleStrong != 0 {
			rt = rt.Strong()
		}
		if style&styleEmphasis != 0 {
			rt = rt.Italics()
		}
		if style&styleCode != 0 {
			rt = rt.Code()
		}
		if style&styleStrikethrough != 0 {
			rt = rt.Strikethrough()
		}
		_ = rt
	}
}

// headingFontSize returns the font size for an H1..H6. Sizes are tuned
// against egui's default 12pt body font; H1/H2 dominate, H6 stays close
// to body to mirror Obsidian's reading view.
func headingFontSize(level uint8) (sz float32) {
	switch level {
	case 1:
		sz = 26
	case 2:
		sz = 22
	case 3:
		sz = 18
	case 4:
		sz = 16
	case 5:
		sz = 14
	case 6:
		sz = 12.5
	default:
		sz = 14
	}
	return
}
