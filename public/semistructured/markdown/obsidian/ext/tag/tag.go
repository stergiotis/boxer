//go:build llm_generated_opus46

package tag

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// Extender adds #tag support to goldmark.
type Extender struct {
	RenderMode RenderModeE
}

func (inst *Extender) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(NewParser(), 200),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(NewRenderer(inst.RenderMode), 200),
	))
}
