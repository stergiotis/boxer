package wikilink

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"

	"github.com/stergiotis/boxer/public/markdown/obsidian/resolver"
)

// Extender adds [[wikilink]] support to goldmark.
type Extender struct {
	Resolver resolver.ResolverI
}

func (inst *Extender) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(NewParser(), 101),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(NewRenderer(inst.Resolver), 101),
	))
}
