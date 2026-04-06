package wikilink

import (
	"bytes"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
)

type wikilinkParser struct{}

func NewParser() parser.InlineParser {
	return &wikilinkParser{}
}

func (inst *wikilinkParser) Trigger() []byte {
	return []byte{'['}
}

func (inst *wikilinkParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) (node ast.Node) {
	line, _ := block.PeekLine()
	if len(line) < 4 || line[0] != '[' || line[1] != '[' {
		return
	}

	// Scan for closing ]]
	i := 2
	for i < len(line)-1 {
		if line[i] == ']' && line[i+1] == ']' {
			inner := line[2:i]
			if len(inner) == 0 {
				return
			}
			n := parseWikilinkInner(inner)
			block.Advance(i + 2)
			return n
		}
		// Wikilinks cannot contain newlines
		if line[i] == '\n' {
			return
		}
		i++
	}
	return
}

func parseWikilinkInner(inner []byte) (node *Node) {
	n := &Node{}

	// Split on | for alias: [[page|alias]]
	if idx := bytes.IndexByte(inner, '|'); idx >= 0 {
		n.Alias = bytes.TrimSpace(inner[idx+1:])
		inner = inner[:idx]
	}

	// Split on # for heading: [[page#heading]]
	if idx := bytes.IndexByte(inner, '#'); idx >= 0 {
		n.Heading = bytes.TrimSpace(inner[idx+1:])
		inner = inner[:idx]
	}

	n.Page = bytes.TrimSpace(inner)
	node = n
	return
}
