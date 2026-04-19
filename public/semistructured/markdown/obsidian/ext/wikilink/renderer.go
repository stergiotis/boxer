//go:build llm_generated_opus46

package wikilink

import (
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type wikilinkRenderer struct {
	resolver resolver.ResolverI
}

func NewRenderer(r resolver.ResolverI) renderer.NodeRenderer {
	return &wikilinkRenderer{resolver: r}
}

func (inst *wikilinkRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(Kind, inst.renderWikilink)
}

func (inst *wikilinkRenderer) renderWikilink(w util.BufWriter, source []byte, n ast.Node, entering bool) (status ast.WalkStatus, err error) {
	status = ast.WalkSkipChildren
	if !entering {
		return
	}

	wl, ok := n.(*Node)
	if !ok {
		return
	}

	url, exists := inst.resolver.ResolveWikilink(string(wl.Page), string(wl.Heading))
	display := wl.DisplayText()

	_, _ = w.WriteString("<a href=\"")
	_, _ = w.Write(util.EscapeHTML([]byte(url)))
	_, _ = w.WriteString("\" class=\"wikilink")
	if !exists {
		_, _ = w.WriteString(" wikilink-broken")
	}
	_, _ = w.WriteString("\">")
	_, _ = w.Write(util.EscapeHTML([]byte(display)))
	_, _ = w.WriteString("</a>")
	return
}
