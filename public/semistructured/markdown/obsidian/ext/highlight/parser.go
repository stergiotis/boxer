package highlight

import (
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type highlightParser struct{}

func NewParser() parser.InlineParser {
	return &highlightParser{}
}

func (inst *highlightParser) Trigger() []byte {
	return []byte{'='}
}

func (inst *highlightParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) (node ast.Node) {
	line, segment := block.PeekLine()
	if len(line) < 2 || line[0] != '=' || line[1] != '=' {
		return
	}

	// Find closing ==
	i := 2
	for i < len(line)-1 {
		if line[i] == '=' && line[i+1] == '=' {
			// Found closing delimiter
			if i == 2 {
				// Empty highlight ==== is not valid
				return
			}
			n := &Node{}
			n.AppendChild(n, ast.NewTextSegment(text.NewSegment(segment.Start+2, segment.Start+i)))
			block.Advance(i + 2)
			return n
		}
		i++
	}
	return
}
