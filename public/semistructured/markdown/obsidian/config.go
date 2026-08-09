package obsidian

import (
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
)

// FeatureE controls which Obsidian-flavored extensions are enabled.
type FeatureE uint16

const (
	FeatureWikilink  FeatureE = 1 << 0
	FeatureEmbed     FeatureE = 1 << 1
	FeatureCallout   FeatureE = 1 << 2
	FeatureHighlight FeatureE = 1 << 3
	FeatureComment   FeatureE = 1 << 4
	FeatureTag       FeatureE = 1 << 5
	// FeatureMath is RESERVED and wired to nothing: no extender consults
	// it, so setting it changes neither the parse nor the render. The bit
	// is kept declared so the flag space stays stable if math ever lands,
	// but it is deliberately excluded from [FeatureAll] — a flag that is
	// part of "all" and does nothing reads as a capability the stack has,
	// and the next consumer writes `$x$` expecting it to render.
	FeatureMath        FeatureE = 1 << 6
	FeatureGFM         FeatureE = 1 << 7
	FeatureFrontmatter FeatureE = 1 << 8
	// FeatureHeadingAnchor enables explicit heading anchors: a trailing
	// `{#slug}` on an ATX or setext heading, as in
	//
	//	## Creating a table {#creating-a-table}
	//
	// The `{#slug}` is stripped from the heading text and becomes the
	// heading's `id`, so a heading can be retitled without invalidating the
	// fragments that link to it. Retrieve it from a goldmark ast.Heading
	// with [HeadingAnchor].
	//
	// The anchor must terminate the line — the pandoc / kramdown /
	// Docusaurus convention goldmark implements. A heading ending in
	// `{#slug}.` keeps the whole thing as literal text, which is also what
	// leaves prose that happens to contain braces alone.
	FeatureHeadingAnchor FeatureE = 1 << 9

	// FeatureAll is every WIRED feature — the whole declared space MINUS
	// the reserved-and-unwired bits. Only [FeatureMath] is subtracted
	// today. Written as the full mask minus the exclusions rather than as
	// an OR of the wired flags so a newly declared bit is opted IN by
	// default and has to be excluded on purpose, which is the direction
	// that fails loudly.
	FeatureAll FeatureE = ((1 << 10) - 1) &^ FeatureMath
)

// TagRenderE controls how tags are rendered in HTML.
type TagRenderE uint8

const (
	TagRenderSpan TagRenderE = 0
	TagRenderLink TagRenderE = 1
)

// Options configures the Obsidian markdown renderer.
type Options struct {
	Features  FeatureE
	Resolver  resolver.ResolverI
	TagRender TagRenderE
}
