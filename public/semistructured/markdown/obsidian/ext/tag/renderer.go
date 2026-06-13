package tag

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// RenderModeE controls how tags are rendered.
type RenderModeE uint8

const (
	RenderModeSpan RenderModeE = 0
	RenderModeLink RenderModeE = 1
)

type tagRenderer struct {
	mode RenderModeE
}

func NewRenderer(mode RenderModeE) renderer.NodeRenderer {
	return &tagRenderer{mode: mode}
}

func (inst *tagRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(Kind, inst.renderTag)
}

func (inst *tagRenderer) renderTag(w util.BufWriter, source []byte, n ast.Node, entering bool) (status ast.WalkStatus, err error) {
	status = ast.WalkSkipChildren
	if !entering {
		return
	}

	t, ok := n.(*Node)
	if !ok {
		return
	}

	escaped := util.EscapeHTML(t.Tag)

	switch inst.mode {
	case RenderModeLink:
		_, _ = w.WriteString("<a href=\"#")
		_, _ = w.Write(escaped)
		_, _ = w.WriteString("\" class=\"tag\">#")
		_, _ = w.Write(escaped)
		_, _ = w.WriteString("</a>")
	default:
		_, _ = w.WriteString("<span class=\"tag\">#")
		_, _ = w.Write(escaped)
		_, _ = w.WriteString("</span>")
	}
	return
}
