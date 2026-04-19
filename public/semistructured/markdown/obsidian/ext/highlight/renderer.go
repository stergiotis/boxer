//go:build llm_generated_opus46

package highlight

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
)

type highlightRenderer struct {
	html.Config
}

func NewRenderer() renderer.NodeRenderer {
	return &highlightRenderer{
		Config: html.NewConfig(),
	}
}

func (inst *highlightRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(Kind, inst.renderHighlight)
}

func (inst *highlightRenderer) renderHighlight(w util.BufWriter, source []byte, node ast.Node, entering bool) (status ast.WalkStatus, err error) {
	if entering {
		_, _ = w.WriteString("<mark>")
	} else {
		_, _ = w.WriteString("</mark>")
	}
	status = ast.WalkContinue
	return
}
