//go:build llm_generated_opus46

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
	FeatureMath        FeatureE = 1 << 6
	FeatureGFM         FeatureE = 1 << 7
	FeatureFrontmatter FeatureE = 1 << 8

	FeatureAll FeatureE = (1 << 9) - 1
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
