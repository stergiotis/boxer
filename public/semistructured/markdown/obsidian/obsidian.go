package obsidian

import (
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/callout"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/comment"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/embed"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/highlight"
	tag2 "github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/tag"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/ext/wikilink"
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	"github.com/yuin/goldmark"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// New creates a goldmark.Markdown instance configured with the requested
// Obsidian-flavored extensions.
//
// When FeatureFrontmatter is enabled, use NewParserContext() to create a
// parser.Context, pass it to Convert via parser.WithContext, then retrieve
// metadata with GetFrontmatter.
func New(opts Options) (md goldmark.Markdown) {
	exts := collectExtensions(opts)
	gmOpts := make([]goldmark.Option, 0, 2)
	gmOpts = append(gmOpts, goldmark.WithExtensions(exts...))
	if parserOpts := collectParserOptions(opts); len(parserOpts) > 0 {
		gmOpts = append(gmOpts, goldmark.WithParserOptions(parserOpts...))
	}
	md = goldmark.New(gmOpts...)
	return
}

// NewParserContext creates a parser.Context for use with Convert.
// This is required to retrieve frontmatter metadata after rendering.
func NewParserContext() parser.Context {
	return parser.NewContext()
}

// Extension returns a composite goldmark.Extender that adds all enabled
// Obsidian features. Use this to compose with other goldmark extensions.
func Extension(opts Options) goldmark.Extender {
	return &compositeExtender{opts: opts}
}

type compositeExtender struct {
	opts Options
}

func (inst *compositeExtender) Extend(m goldmark.Markdown) {
	for _, ext := range collectExtensions(inst.opts) {
		ext.Extend(m)
	}
	// Heading anchors are a parser option rather than an extension, so the
	// composite extender has to reach for the parser itself — goldmark
	// applies options added here on the next Parse, which is why this is
	// equivalent to the WithParserOptions path [New] takes.
	if parserOpts := collectParserOptions(inst.opts); len(parserOpts) > 0 {
		m.Parser().AddOptions(parserOpts...)
	}
}

// collectParserOptions returns the goldmark parser options implied by the
// enabled features. Unlike the extension list this is usually empty — only
// features goldmark implements natively (heading attributes) land here.
func collectParserOptions(opts Options) (parserOpts []parser.Option) {
	if opts.Features&FeatureHeadingAnchor != 0 {
		parserOpts = append(parserOpts, parser.WithHeadingAttribute())
	}
	return
}

// HeadingAnchor returns the explicit `{#slug}` anchor of a heading parsed
// with [FeatureHeadingAnchor], and whether the heading carried one. It is
// the accessor for what that feature parses: goldmark stores the anchor as
// the node's `id` attribute, holding raw bytes rather than a string.
//
// The anchor is returned as authored — callers that use it as a lookup key
// alongside slugs derived from heading text are the ones that must
// normalise (lower-case, spaces to dashes), because that normalisation is
// the consumer's fragment convention, not the parser's.
func HeadingAnchor(h *ast.Heading) (anchor string, ok bool) {
	if h == nil {
		return
	}
	v, has := h.AttributeString("id")
	if !has {
		return
	}
	switch t := v.(type) {
	case []byte:
		anchor = string(t)
	case string:
		anchor = t
	default:
		return
	}
	ok = anchor != ""
	return
}

func collectExtensions(opts Options) (exts []goldmark.Extender) {
	r := opts.Resolver
	if r == nil {
		r = resolver.NoopResolver{}
	}

	exts = make([]goldmark.Extender, 0, 8)

	if opts.Features&FeatureFrontmatter != 0 {
		exts = append(exts, meta.Meta)
	}
	if opts.Features&FeatureGFM != 0 {
		exts = append(exts, extension.GFM)
	}
	if opts.Features&FeatureWikilink != 0 {
		exts = append(exts, &wikilink.Extender{Resolver: r})
	}
	if opts.Features&FeatureEmbed != 0 {
		exts = append(exts, &embed.Extender{Resolver: r})
	}
	if opts.Features&FeatureCallout != 0 {
		exts = append(exts, &callout.Extender{})
	}
	if opts.Features&FeatureHighlight != 0 {
		exts = append(exts, &highlight.Extender{})
	}
	if opts.Features&FeatureComment != 0 {
		exts = append(exts, &comment.Extender{})
	}
	if opts.Features&FeatureTag != 0 {
		renderMode := tag2.RenderModeSpan
		if opts.TagRender == TagRenderLink {
			renderMode = tag2.RenderModeLink
		}
		exts = append(exts, &tag2.Extender{RenderMode: renderMode})
	}

	return
}
