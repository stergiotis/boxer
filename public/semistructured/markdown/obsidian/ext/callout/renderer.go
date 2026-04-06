package callout

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

type calloutRenderer struct{}

func NewRenderer() renderer.NodeRenderer {
	return &calloutRenderer{}
}

func (inst *calloutRenderer) RegisterFuncs(registerer renderer.NodeRendererFuncRegisterer) {
	registerer.Register(Kind, inst.renderCallout)
}

func (inst *calloutRenderer) renderCallout(w util.BufWriter, source []byte, n ast.Node, entering bool) (status ast.WalkStatus, err error) {
	status = ast.WalkContinue
	c, ok := n.(*Node)
	if !ok {
		return
	}

	calloutType := bytes.ToLower(c.CalloutType)
	title := c.Title
	if len(title) == 0 {
		// Default title is the callout type, capitalized
		title = make([]byte, len(calloutType))
		copy(title, calloutType)
		if len(title) > 0 && title[0] >= 'a' && title[0] <= 'z' {
			title[0] -= 'a' - 'A'
		}
	}

	if entering {
		_, _ = w.WriteString("<div class=\"callout callout-")
		_, _ = w.Write(util.EscapeHTML(calloutType))
		_, _ = w.WriteString("\">\n")

		if c.Foldable {
			_, _ = w.WriteString("<details")
			if c.DefaultOpen {
				_, _ = w.WriteString(" open")
			}
			_, _ = w.WriteString(">\n<summary class=\"callout-title\">")
			_, _ = w.Write(util.EscapeHTML(title))
			_, _ = w.WriteString("</summary>\n")
		} else {
			_, _ = w.WriteString("<div class=\"callout-title\">")
			_, _ = w.Write(util.EscapeHTML(title))
			_, _ = w.WriteString("</div>\n")
		}
		_, _ = w.WriteString("<div class=\"callout-content\">\n")
	} else {
		_, _ = w.WriteString("</div>\n")
		if c.Foldable {
			_, _ = w.WriteString("</details>\n")
		}
		_, _ = w.WriteString("</div>\n")
	}
	return
}
