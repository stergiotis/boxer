package embed

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	"github.com/stergiotis/boxer/public/markdown/obsidian/resolver"
)

// Extender adds ![[embed]] support to goldmark.
type Extender struct {
	Resolver resolver.ResolverI
}

func (inst *Extender) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		// Higher priority than wikilink so ![[...]] is matched before [[...]]
		util.Prioritized(NewParser(), 100),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(NewRenderer(inst.Resolver), 100),
	))
}
