//go:build llm_generated_opus46

package comment

import (
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// Extender adds %%comment%% support to goldmark (strips comments from output).
type Extender struct{}

func (inst *Extender) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(NewParser(), 198),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(NewRenderer(), 198),
	))
}
