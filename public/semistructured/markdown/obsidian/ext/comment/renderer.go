package comment

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type commentRenderer struct{}

func NewRenderer() renderer.NodeRenderer {
	return &commentRenderer{}
}

func (inst *commentRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(Kind, inst.renderComment)
}

func (inst *commentRenderer) renderComment(w util.BufWriter, source []byte, node ast.Node, entering bool) (status ast.WalkStatus, err error) {
	// Comments are stripped from output — emit nothing.
	status = ast.WalkSkipChildren
	return
}
