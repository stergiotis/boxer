//go:build llm_generated_opus46

package embed

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type embedParser struct{}

func NewParser() parser.InlineParser {
	return &embedParser{}
}

func (inst *embedParser) Trigger() []byte {
	return []byte{'!'}
}

func (inst *embedParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) (node ast.Node) {
	line, _ := block.PeekLine()
	if len(line) < 5 || line[0] != '!' || line[1] != '[' || line[2] != '[' {
		return
	}

	// Scan for closing ]]
	i := 3
	for i < len(line)-1 {
		if line[i] == ']' && line[i+1] == ']' {
			inner := line[3:i]
			if len(inner) == 0 {
				return
			}
			n := parseEmbedInner(inner)
			block.Advance(i + 2)
			return n
		}
		if line[i] == '\n' {
			return
		}
		i++
	}
	return
}

func parseEmbedInner(inner []byte) (node *Node) {
	n := &Node{}

	// Split on # for heading: ![[note#heading]]
	if idx := bytes.IndexByte(inner, '#'); idx >= 0 {
		n.Heading = bytes.TrimSpace(inner[idx+1:])
		inner = inner[:idx]
	}

	n.Target = bytes.TrimSpace(inner)
	node = n
	return
}
