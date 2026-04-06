package embed

import (
	"github.com/stergiotis/boxer/public/semistructured/markdown/obsidian/resolver"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type embedRenderer struct {
	resolver resolver.ResolverI
}

func NewRenderer(r resolver.ResolverI) renderer.NodeRenderer {
	return &embedRenderer{resolver: r}
}

func (inst *embedRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(Kind, inst.renderEmbed)
}

func (inst *embedRenderer) renderEmbed(w util.BufWriter, source []byte, n ast.Node, entering bool) (status ast.WalkStatus, err error) {
	status = ast.WalkSkipChildren
	if !entering {
		return
	}

	emb, ok := n.(*Node)
	if !ok {
		return
	}

	url, isImage, exists := inst.resolver.ResolveEmbed(string(emb.Target), string(emb.Heading))

	if isImage {
		_, _ = w.WriteString("<img src=\"")
		_, _ = w.Write(util.EscapeHTML([]byte(url)))
		_, _ = w.WriteString("\" alt=\"")
		_, _ = w.Write(util.EscapeHTML(emb.Target))
		_, _ = w.WriteString("\" class=\"embed-image")
		if !exists {
			_, _ = w.WriteString(" embed-broken")
		}
		_, _ = w.WriteString("\" />")
	} else {
		_, _ = w.WriteString("<div class=\"embed-note")
		if !exists {
			_, _ = w.WriteString(" embed-broken")
		}
		_, _ = w.WriteString("\" data-src=\"")
		_, _ = w.Write(util.EscapeHTML([]byte(url)))
		_, _ = w.WriteString("\">")
		_, _ = w.Write(util.EscapeHTML(emb.Target))
		if len(emb.Heading) > 0 {
			_, _ = w.WriteString(" &gt; ")
			_, _ = w.Write(util.EscapeHTML(emb.Heading))
		}
		_, _ = w.WriteString("</div>")
	}
	return
}
