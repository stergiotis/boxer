package comment

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type commentParser struct{}

func NewParser() parser.InlineParser {
	return &commentParser{}
}

func (inst *commentParser) Trigger() []byte {
	return []byte{'%'}
}

func (inst *commentParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) (node ast.Node) {
	line, _ := block.PeekLine()
	if len(line) < 2 || line[0] != '%' || line[1] != '%' {
		return
	}

	// Find closing %%
	i := 2
	for i < len(line)-1 {
		if line[i] == '%' && line[i+1] == '%' {
			block.Advance(i + 2)
			return &Node{}
		}
		i++
	}
	return
}
