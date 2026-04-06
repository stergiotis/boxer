package obsidian

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"

	"github.com/stergiotis/boxer/public/markdown/obsidian/ext/callout"
	"github.com/stergiotis/boxer/public/markdown/obsidian/ext/comment"
	"github.com/stergiotis/boxer/public/markdown/obsidian/ext/embed"
	"github.com/stergiotis/boxer/public/markdown/obsidian/ext/highlight"
	"github.com/stergiotis/boxer/public/markdown/obsidian/ext/tag"
	"github.com/stergiotis/boxer/public/markdown/obsidian/ext/wikilink"
	"github.com/stergiotis/boxer/public/markdown/obsidian/resolver"
)

// New creates a goldmark.Markdown instance configured with the requested
// Obsidian-flavored extensions.
func New(opts Options) (md goldmark.Markdown) {
	exts := collectExtensions(opts)
	md = goldmark.New(goldmark.WithExtensions(exts...))
	return
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
}

func collectExtensions(opts Options) (exts []goldmark.Extender) {
	r := opts.Resolver
	if r == nil {
		r = resolver.NoopResolver{}
	}

	exts = make([]goldmark.Extender, 0, 8)

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
		renderMode := tag.RenderModeSpan
		if opts.TagRender == TagRenderLink {
			renderMode = tag.RenderModeLink
		}
		exts = append(exts, &tag.Extender{RenderMode: renderMode})
	}

	return
}
